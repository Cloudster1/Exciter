package glossary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// TopicRevised is a term changed or made; its owners hear of a changed definition.
const TopicRevised = "term.revised"

var (
	// ErrNotFound is also the answer for a term in a project the caller may not read.
	ErrNotFound = errors.New("term not found")
	// ErrForbidden is a change by somebody who may not make it.
	ErrForbidden = errors.New("not allowed to change this glossary")
	// ErrStale is a change or confirmation against a revision of a term that is no longer current.
	ErrStale = errors.New("the term changed since it was read")
	// ErrNotUsed is a confirmation of a term the requirement does not use.
	ErrNotUsed = errors.New("the requirement does not use that term")
)

// FieldError is a field that does not hold, with the sentence that says how to fix it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// ExistsError is a name or synonym another term of the glossary has.
type ExistsError struct {
	Word string
}

func (e *ExistsError) Error() string { return e.Word + " already names a term" }

// Service keeps glossaries, their terms and the requirements that use them.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

const selectTerm = `
SELECT t.id, p.key, t.name, t.status, t.definition, t.synonyms, t.avoid, t.revision, t.definition_revision,
       (SELECT count(*) FROM term_use u WHERE u.term_id = t.id)::int,
       COALESCE(cu.id, '00000000-0000-0000-0000-000000000000'), COALESCE(cu.name, ''),
       COALESCE(uu.id, '00000000-0000-0000-0000-000000000000'), COALESCE(uu.name, ''), t.created_at, t.updated_at
FROM glossary_term t
JOIN project p ON p.id = t.project_id
LEFT JOIN app_user cu ON cu.id = t.created_by
LEFT JOIN app_user uu ON uu.id = t.updated_by`

func scanTerm(row pgx.Row) (Term, error) {
	var t Term
	var status string
	err := row.Scan(&t.ID, &t.ProjectKey, &t.Name, &status, &t.Definition, &t.Synonyms, &t.Avoid, &t.Revision, &t.DefinitionRevision,
		&t.Uses, &t.CreatedBy.ID, &t.CreatedBy.Name, &t.UpdatedBy.ID, &t.UpdatedBy.Name, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	t.Status = Status(status)
	return t, err
}

func termByID(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Term, error) {
	t, err := scanTerm(tx.QueryRow(ctx, selectTerm+` WHERE t.id = $1`, id))
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// List is the terms that mark a project's requirements, as the reader may read them, by name: the project's own and
// its libraries', or its own alone.
func (s *Service) List(ctx context.Context, reader uuid.UUID, projectKey, q string, statuses []Status, own bool, limit, offset int) ([]Term, int, error) {
	out, total := []Term{}, 0
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		for _, st := range statuses {
			if !slices.Contains(Statuses, st) {
				return &FieldError{"status", "Choose proposed, approved or deprecated."}
			}
		}
		where := ` WHERE t.id IN (SELECT term_id FROM glossary_terms_of($1)) AND ($2 = false OR t.project_id = $1)
			AND (cardinality($3::text[]) = 0 OR t.status = ANY ($3))
			AND ($4 = '' OR t.search_vector @@ plainto_tsquery('exciter_search', $4) OR t.name ILIKE $4 || '%')`
		args := []any{p.ID, own, statusStrings(statuses), strings.TrimSpace(q)}
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM glossary_term t`+where, args...).Scan(&total); err != nil {
			return fmt.Errorf("count the terms: %w", err)
		}
		rows, err := tx.Query(ctx, selectTerm+where+` ORDER BY lower(t.name), t.id LIMIT $5 OFFSET $6`, append(args, limit, offset)...)
		if err != nil {
			return fmt.Errorf("list the terms: %w", err)
		}
		found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Term, error) { return scanTerm(row) })
		if err != nil {
			return err
		}
		out = append(out, found...)
		return nil
	})
	return out, total, err
}

func statusStrings(in []Status) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = string(s)
	}
	return out
}

// Get answers one term.
func (s *Service) Get(ctx context.Context, reader uuid.UUID, id uuid.UUID) (*Term, error) {
	var out *Term
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = termByID(ctx, tx, id)
		return err
	})
	return out, err
}

// checked is a term's fields as they hold.
type checked struct {
	name            string
	definition      json.RawMessage
	synonyms, avoid []string
	status          Status
	comment         string
}

func checkName(raw string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	if name == "" || utf8.RuneCountInString(name) > MaxNameLength {
		return "", &FieldError{"name", fmt.Sprintf("Name the term in at most %d characters.", MaxNameLength)}
	}
	return name, nil
}

func checkWords(field string, in []string, most int) ([]string, error) {
	out, seen := []string{}, map[string]bool{}
	for _, w := range in {
		w = strings.Join(strings.Fields(w), " ")
		if w == "" || seen[strings.ToLower(w)] {
			continue
		}
		if utf8.RuneCountInString(w) > MaxNameLength {
			return nil, &FieldError{field, fmt.Sprintf("Keep each word to at most %d characters.", MaxNameLength)}
		}
		seen[strings.ToLower(w)] = true
		out = append(out, w)
	}
	if len(out) > most {
		return nil, &FieldError{field, fmt.Sprintf("A term holds at most %d of these.", most)}
	}
	return out, nil
}

func checkDefinition(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return document.Empty, nil
	}
	if _, err := document.Parse(raw); err != nil {
		var invalid *document.InvalidError
		if errors.As(err, &invalid) {
			return nil, &FieldError{"definition", invalid.Message}
		}
		return nil, err
	}
	return raw, nil
}

func checkStatus(s Status) error {
	if !slices.Contains(Statuses, s) {
		return &FieldError{"status", "Choose proposed, approved or deprecated."}
	}
	return nil
}

func checkComment(raw string) (string, error) {
	c := strings.TrimSpace(raw)
	if utf8.RuneCountInString(c) > MaxCommentChars {
		return "", &FieldError{"comment", fmt.Sprintf("Keep the comment to at most %d characters.", MaxCommentChars)}
	}
	return c, nil
}

// refusal reads what the database said of a term's words.
func refusal(err error, name string, synonyms []string) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.ConstraintName {
	case "glossary_term_name_once":
		return &ExistsError{Word: name}
	case "glossary_term_words":
		word := name
		if len(synonyms) > 0 {
			word = name + " or one of its synonyms"
		}
		return &ExistsError{Word: word}
	case "glossary_term_limit":
		return &FieldError{"name", fmt.Sprintf("This glossary holds %d terms already. Remove one before you add another.", MaxTerms)}
	}
	return err
}

// editable is the project of a term the actor may change: open, and theirs to edit or, for delete, to administer.
func editable(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectID uuid.UUID, right string) error {
	var holds, open bool
	if err := tx.QueryRow(ctx, `SELECT perm_project_holds($1, $2, $3), project_open($1)`, projectID, actor, right).Scan(&holds, &open); err != nil {
		return err
	}
	switch {
	case !open:
		return project.ErrArchived
	case !holds:
		return ErrForbidden
	}
	return nil
}

// Create defines a term in a project's glossary.
func (s *Service) Create(ctx context.Context, actor uuid.UUID, projectKey string, in TermInput) (*Term, db.LSN, error) {
	name, err := checkName(in.Name)
	if err != nil {
		return nil, 0, err
	}
	synonyms, err := checkWords("synonyms", in.Synonyms, MaxSynonyms)
	if err != nil {
		return nil, 0, err
	}
	avoid, err := checkWords("avoid", in.Avoid, MaxAvoid)
	if err != nil {
		return nil, 0, err
	}
	definition, err := checkDefinition(in.Definition)
	if err != nil {
		return nil, 0, err
	}
	status := in.Status
	if status == "" {
		status = StatusProposed
	}
	if err := checkStatus(status); err != nil {
		return nil, 0, err
	}
	var out *Term
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if err := editable(ctx, tx, actor, p.ID, "edit"); err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO glossary_term (project_id, name, definition, synonyms, avoid, status, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, p.ID, name, definition, synonyms, avoid, string(status), actor).Scan(&id); err != nil {
			return refusal(err, name, synonyms)
		}
		if err := revised(ctx, tx, actor, id, p.ID, 1, false, true); err != nil {
			return err
		}
		out, err = termByID(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// revised tells what follows terms that one changed: its owners when its meaning did, and the writing rules of every
// project it marks when its words did.
func revised(ctx context.Context, tx db.DBTX, actor, id, projectID uuid.UUID, revision int, meaning, words bool) error {
	if err := events.Emit(ctx, tx, TopicRevised, map[string]any{"actorId": actor, "termId": id, "projectId": projectID,
		"revision": revision, "definitionChanged": meaning}); err != nil {
		return err
	}
	if !words {
		return nil
	}
	return recheck(ctx, tx, actor, projectID)
}

// recheck has the writing rules check again every project a glossary marks: its own, and those that use it.
func recheck(ctx context.Context, tx db.DBTX, actor, projectID uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT project FROM glossary_users($1) project`, projectID)
	if err != nil {
		return fmt.Errorf("find the projects the glossary marks: %w", err)
	}
	projects, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, p := range append([]uuid.UUID{projectID}, projects...) {
		if err := events.Emit(ctx, tx, project.TopicQualityRecheck, map[string]any{"actorId": actor, "projectId": p}); err != nil {
			return err
		}
	}
	return nil
}

// Update changes a term as a new revision, against the revision it was read at; nothing to change is no revision.
func (s *Service) Update(ctx context.Context, actor uuid.UUID, id uuid.UUID, in TermPatch) (*Term, db.LSN, error) {
	var out *Term
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var projectID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT project_id FROM glossary_term WHERE id = $1 FOR UPDATE`, id).Scan(&projectID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if err := editable(ctx, tx, actor, projectID, "edit"); err != nil {
			return err
		}
		cur, err := termByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if in.BaseRevision != cur.Revision {
			return ErrStale
		}
		next, err := patched(cur, in)
		if err != nil {
			return err
		}
		meaning := next.name != cur.Name || !sameDocument(next.definition, cur.Definition)
		words := meaning || !slices.Equal(next.synonyms, cur.Synonyms) || !slices.Equal(next.avoid, cur.Avoid)
		if !words && next.status == cur.Status {
			out = cur
			return nil
		}
		if _, err := tx.Exec(ctx, `
			UPDATE glossary_term SET name = $2, definition = $3, synonyms = $4, avoid = $5, status = $6, comment = $7, updated_by = $8
			WHERE id = $1`, id, next.name, next.definition, next.synonyms, next.avoid, string(next.status), next.comment, actor); err != nil {
			return refusal(err, next.name, next.synonyms)
		}
		if err := revised(ctx, tx, actor, id, projectID, cur.Revision+1, meaning, words); err != nil {
			return err
		}
		out, err = termByID(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

func patched(cur *Term, in TermPatch) (*checked, error) {
	next := &checked{name: cur.Name, definition: cur.Definition, synonyms: cur.Synonyms, avoid: cur.Avoid, status: cur.Status}
	var err error
	if in.Name != nil {
		if next.name, err = checkName(*in.Name); err != nil {
			return nil, err
		}
	}
	if in.Definition != nil {
		if next.definition, err = checkDefinition(*in.Definition); err != nil {
			return nil, err
		}
	}
	if in.Synonyms != nil {
		if next.synonyms, err = checkWords("synonyms", *in.Synonyms, MaxSynonyms); err != nil {
			return nil, err
		}
	}
	if in.Avoid != nil {
		if next.avoid, err = checkWords("avoid", *in.Avoid, MaxAvoid); err != nil {
			return nil, err
		}
	}
	if in.Status != nil {
		if err := checkStatus(*in.Status); err != nil {
			return nil, err
		}
		next.status = *in.Status
	}
	if next.comment, err = checkComment(in.Comment); err != nil {
		return nil, err
	}
	return next, nil
}

// sameDocument compares two documents by what they say, not how their JSON is spelled.
func sameDocument(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return string(a) == string(b)
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return string(ja) == string(jb)
}

// Delete removes a term and its revisions; the project's administrators'.
func (s *Service) Delete(ctx context.Context, actor uuid.UUID, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		cur, err := termByID(ctx, tx, id)
		if err != nil {
			return err
		}
		var projectID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT project_id FROM glossary_term WHERE id = $1`, id).Scan(&projectID); err != nil {
			return err
		}
		if err := editable(ctx, tx, actor, projectID, "administer"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM glossary_term WHERE id = $1`, id); err != nil {
			return fmt.Errorf("delete the term: %w", err)
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTermDeleted, TargetType: "project", TargetID: &projectID, Actor: actor,
			Data: map[string]any{"term": cur.Name, "termId": id}}); err != nil {
			return err
		}
		return recheck(ctx, tx, actor, projectID)
	})
}

// Revisions are every revision of a term, newest first.
func (s *Service) Revisions(ctx context.Context, reader uuid.UUID, id uuid.UUID) ([]TermRevision, error) {
	out := []TermRevision{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := termByID(ctx, tx, id); err != nil {
			return err
		}
		found, err := revisionsOf(ctx, tx, id, 0)
		out = append(out, found...)
		return err
	})
	return out, err
}

// Revision is one revision of a term.
func (s *Service) Revision(ctx context.Context, reader uuid.UUID, id uuid.UUID, number int) (*TermRevision, error) {
	var out *TermRevision
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := termByID(ctx, tx, id); err != nil {
			return err
		}
		found, err := revisionsOf(ctx, tx, id, number)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			return ErrNotFound
		}
		out = &found[0]
		return nil
	})
	return out, err
}

func revisionsOf(ctx context.Context, tx db.DBTX, id uuid.UUID, number int) ([]TermRevision, error) {
	rows, err := tx.Query(ctx, `
		SELECT v.number, v.name, v.definition, v.synonyms, v.avoid, v.status, v.comment,
		       COALESCE(u.id, '00000000-0000-0000-0000-000000000000'), COALESCE(u.name, ''), v.created_at
		FROM glossary_term_revision v LEFT JOIN app_user u ON u.id = v.created_by
		WHERE v.term_id = $1 AND ($2 = 0 OR v.number = $2) ORDER BY v.number DESC`, id, number)
	if err != nil {
		return nil, fmt.Errorf("read the revisions: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (TermRevision, error) {
		var r TermRevision
		var status string
		err := row.Scan(&r.Number, &r.Name, &r.Definition, &r.Synonyms, &r.Avoid, &status, &r.Comment, &r.CreatedBy.ID, &r.CreatedBy.Name, &r.CreatedAt)
		r.Status = Status(status)
		return r, err
	})
}

// Uses are the requirements the reader may read that use a term, by key.
func (s *Service) Uses(ctx context.Context, reader uuid.UUID, id uuid.UUID, limit, offset int) ([]Use, int, error) {
	out, total := []Use{}, 0
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := termByID(ctx, tx, id); err != nil {
			return err
		}
		const from = `
			FROM term_use u JOIN glossary_term t ON t.id = u.term_id
			JOIN requirement r ON r.id = u.requirement_id JOIN project p ON p.id = r.project_id
			LEFT JOIN req_status st ON st.id = r.status_id
			WHERE u.term_id = $1 AND r.trashed_at IS NULL`
		if err := tx.QueryRow(ctx, `SELECT count(*)::int`+from, id).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT u.explicit, u.seen_revision, t.definition_revision, r.id, p.key, r.number, r.kind, r.title, r.revision,
			       r.status_id, COALESCE(st.name, ''), COALESCE(st.category, '')`+from+`
			ORDER BY p.key, r.number LIMIT $2 OFFSET $3`, id, limit, offset)
		if err != nil {
			return fmt.Errorf("read the uses: %w", err)
		}
		found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Use, error) {
			var (
				u        Use
				current  int
				number   int
				statusID *uuid.UUID
				status   requirement.StatusRef
			)
			ref := &u.Requirement
			if err := row.Scan(&u.Explicit, &u.SeenRevision, &current, &ref.ID, &ref.ProjectKey, &number, &ref.Kind, &ref.Title, &ref.Revision,
				&statusID, &status.Name, &status.Category); err != nil {
				return u, err
			}
			ref.Key = requirement.Key(ref.ProjectKey, number)
			if statusID != nil {
				status.ID = *statusID
				ref.Status = &status
			}
			u.Suspect = current > u.SeenRevision
			return u, nil
		})
		out = append(out, found...)
		return err
	})
	return out, total, err
}

// RequirementTerms are the terms a requirement uses, as the reader may read them.
func (s *Service) RequirementTerms(ctx context.Context, reader uuid.UUID, key string) ([]RequirementTerm, error) {
	out := []RequirementTerm{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := requirement.FindEndpoint(ctx, tx, reader, key)
		if err != nil {
			return err
		}
		found, err := termsOf(ctx, tx, r.ID, uuid.Nil)
		out = append(out, found...)
		return err
	})
	return out, err
}

func termsOf(ctx context.Context, tx db.DBTX, requirementID, termID uuid.UUID) ([]RequirementTerm, error) {
	rows, err := tx.Query(ctx, `
		SELECT t.id, p.key, t.name, t.status, u.explicit, u.seen_revision, t.definition_revision
		FROM term_use u JOIN glossary_term t ON t.id = u.term_id JOIN project p ON p.id = t.project_id
		WHERE u.requirement_id = $1 AND ($2 = '00000000-0000-0000-0000-000000000000'::uuid OR u.term_id = $2)
		ORDER BY lower(t.name)`, requirementID, termID)
	if err != nil {
		return nil, fmt.Errorf("read the terms: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (RequirementTerm, error) {
		var r RequirementTerm
		var status string
		err := row.Scan(&r.Term.ID, &r.Term.ProjectKey, &r.Term.Name, &status, &r.Explicit, &r.SeenRevision, &r.DefinitionRevision)
		r.Term.Status = Status(status)
		r.Suspect = r.DefinitionRevision > r.SeenRevision
		return r, err
	})
}

// Clear confirms that a requirement still holds under a term's current definition.
func (s *Service) Clear(ctx context.Context, actor uuid.UUID, key string, termID uuid.UUID, in ClearInput) (*RequirementTerm, db.LSN, error) {
	comment := strings.TrimSpace(in.Comment)
	if comment == "" || utf8.RuneCountInString(comment) > MaxCommentChars {
		return nil, 0, &FieldError{"comment", fmt.Sprintf("Say in at most %d characters why the requirement still holds.", MaxCommentChars)}
	}
	var out *RequirementTerm
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := requirement.FindEndpoint(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		if r.Archived {
			return project.ErrArchived
		}
		if _, err := tx.Exec(ctx, `SELECT term_use_clear($1, $2, $3, $4)`, r.ID, termID, in.TermRevision, comment); err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) {
				switch {
				case pg.Message == "term_use_stale":
					return ErrStale
				case pg.Message == "term_use_missing":
					return ErrNotUsed
				case pg.Message == "term_use_clear_forbidden":
					return ErrForbidden
				}
			}
			return fmt.Errorf("confirm the term: %w", err)
		}
		found, err := termsOf(ctx, tx, r.ID, termID)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			return ErrNotUsed
		}
		out = &found[0]
		return nil
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// Glossaries are the library glossaries a project uses, in order.
func (s *Service) Glossaries(ctx context.Context, reader uuid.UUID, projectKey string) (*Glossaries, error) {
	var out *Glossaries
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		out, err = glossariesOf(ctx, tx, p.ID)
		return err
	})
	return out, err
}

func glossariesOf(ctx context.Context, tx db.DBTX, projectID uuid.UUID) (*Glossaries, error) {
	rows, err := tx.Query(ctx, `
		SELECT p.key, p.name, g.position, (SELECT count(*) FROM glossary_term t WHERE t.project_id = g.library_id)::int, g.added_at
		FROM project_glossary g JOIN project p ON p.id = g.library_id
		WHERE g.project_id = $1 ORDER BY g.position`, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the glossaries: %w", err)
	}
	out := &Glossaries{Libraries: []Library{}}
	for rows.Next() {
		var l Library
		var at time.Time
		if err := rows.Scan(&l.ProjectKey, &l.Name, &l.Position, &l.Terms, &at); err != nil {
			rows.Close()
			return nil, err
		}
		out.Libraries = append(out.Libraries, l)
		if out.UpdatedAt == nil || at.After(*out.UpdatedAt) {
			out.UpdatedAt = &at
		}
	}
	rows.Close()
	return out, rows.Err()
}

// SaveGlossaries names the library projects whose glossaries a project uses, in order; its administrators'.
func (s *Service) SaveGlossaries(ctx context.Context, actor uuid.UUID, projectKey string, in GlossariesInput) (*Glossaries, db.LSN, error) {
	if len(in.Libraries) > MaxLibraries {
		return nil, 0, &FieldError{"libraries", fmt.Sprintf("A project uses at most %d library glossaries.", MaxLibraries)}
	}
	var out *Glossaries
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		switch {
		case p.ArchivedAt != nil:
			return project.ErrArchived
		case !p.Can.Administer:
			return ErrForbidden
		}
		var libraries []uuid.UUID
		for i, key := range in.Libraries {
			lib, err := project.Find(ctx, tx, actor, key)
			if errors.Is(err, project.ErrNotFound) || (err == nil && (!lib.Settings.Library || lib.ID == p.ID || slices.Contains(libraries, lib.ID))) {
				return &FieldError{fmt.Sprintf("libraries[%d]", i), "Choose library projects you may read, each once, other than this one."}
			}
			if err != nil {
				return err
			}
			libraries = append(libraries, lib.ID)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM project_glossary WHERE project_id = $1`, p.ID); err != nil {
			return err
		}
		for i, lib := range libraries {
			if _, err := tx.Exec(ctx, `INSERT INTO project_glossary (project_id, library_id, position, added_by) VALUES ($1, $2, $3, $4)`,
				p.ID, lib, i, actor); err != nil {
				return fmt.Errorf("use the glossary: %w", err)
			}
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionGlossariesChosen, TargetType: "project", TargetID: &p.ID, Actor: actor,
			Data: map[string]any{"libraries": in.Libraries}}); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, project.TopicQualityRecheck, map[string]any{"actorId": actor, "projectId": p.ID}); err != nil {
			return err
		}
		out, err = glossariesOf(ctx, tx, p.ID)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// Hits are the terms a search finds, of every glossary the reader may read, the best first.
func (s *Service) Hits(ctx context.Context, reader uuid.UUID, q string, limit int) ([]Hit, error) {
	out := []Hit{}
	q = strings.TrimSpace(q)
	if q == "" {
		return out, nil
	}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT t.id, p.key, t.name, t.status,
			       ts_headline('exciter_search', left(requirement_plain_text(t.definition), 2000), plainto_tsquery('exciter_search', $1),
			                   'MaxWords=20, MinWords=5, StartSel="", StopSel=""')
			FROM glossary_term t JOIN project p ON p.id = t.project_id
			WHERE t.search_vector @@ plainto_tsquery('exciter_search', $1) OR t.name ILIKE $1 || '%'
			ORDER BY t.name ILIKE $1 || '%' DESC, ts_rank(t.search_vector, plainto_tsquery('exciter_search', $1)) DESC, lower(t.name)
			LIMIT $2`, q, limit)
		if err != nil {
			return fmt.Errorf("find the terms: %w", err)
		}
		found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Hit, error) {
			var h Hit
			var status string
			err := row.Scan(&h.ID, &h.ProjectKey, &h.Name, &status, &h.Snippet)
			h.Status = Status(status)
			return h, err
		})
		out = append(out, found...)
		return err
	})
	return out, err
}
