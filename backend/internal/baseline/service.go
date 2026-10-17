package baseline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/signing"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// TopicTaken is a baseline taken of a project.
const TopicTaken = "baseline.taken"

var (
	// ErrNotFound is a baseline that is gone, or one of a project the reader does not view.
	ErrNotFound = errors.New("baseline not found")
	// ErrForbidden is somebody but the project's administrators taking, changing or deleting a baseline.
	ErrForbidden = errors.New("only a project's administrators keep its baselines")
	// ErrLocked is a change to a locked baseline, which keeps its name and description and is never deleted.
	ErrLocked = errors.New("the baseline is locked")
	// ErrNameTaken is a name another baseline of the project has.
	ErrNameTaken = errors.New("another baseline of the project has that name")
)

// FieldError is a request refused on one field, with the sentence to show beside it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Service takes, keeps and compares baselines.
type Service struct {
	db        *db.Cluster
	passwords signing.Passwords
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxNameLength {
		return "", &FieldError{"name", fmt.Sprintf("Name the baseline in at most %d characters, such as Release 1.0.", MaxNameLength)}
	}
	return name, nil
}

func checkDescription(description string) (string, error) {
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(description) > MaxDescriptionLength {
		return "", &FieldError{"description", fmt.Sprintf("Keep the description to %d characters.", MaxDescriptionLength)}
	}
	return description, nil
}

// selectBaseline reads baselines with their project, their root as it is now and how many items they hold.
const selectBaseline = `
SELECT b.id, p.key, b.name, b.description, b.root_id, b.root_number, COALESCE(r.kind, 'section'), COALESCE(r.title, ''),
       COALESCE(r.revision, 0), r.trashed_at IS NOT NULL, (SELECT count(*)::int FROM baseline_item i WHERE i.baseline_id = b.id),
       b.locked_at, COALESCE(u.name, ''), b.created_at, b.detail, b.project_id,
       (SELECT count(*)::int FROM baseline_signature s WHERE s.baseline_id = b.id), bs.required, rr.required,
       p.archived_at IS NULL AND NOT EXISTS (SELECT 1 FROM baseline_signature s WHERE s.baseline_id = b.id AND s.signer_id = current_actor_id())
       AND CASE WHEN bs.baseline_id IS NOT NULL THEN baseline_signer_named(b.id, current_actor_id())
                ELSE rr.project_id IS NOT NULL AND baseline_signing_rule_names(b.project_id, current_actor_id()) END
FROM baseline b
JOIN project p ON p.id = b.project_id
LEFT JOIN requirement r ON r.id = b.root_id
LEFT JOIN app_user u ON u.id = b.created_by
LEFT JOIN baseline_signing bs ON bs.baseline_id = b.id
LEFT JOIN baseline_signing_rule rr ON rr.project_id = b.project_id`

// found is a baseline with the project it is of.
type found struct {
	Baseline
	projectID uuid.UUID
	// signatures, frozen and rule are how many signed, the number its first signature froze, and the project's now.
	signatures   int
	frozen, rule *int
	maySign      bool
}

func scanBaseline(row pgx.CollectableRow) (found, error) {
	var (
		f          found
		rootID     *uuid.UUID
		rootNumber *int
		root       requirement.Ref
	)
	err := row.Scan(&f.ID, &f.ProjectKey, &f.Name, &f.Description, &rootID, &rootNumber, &root.Kind, &root.Title, &root.Revision,
		&root.Trashed, &f.Items, &f.LockedAt, &f.CreatedByName, &f.CreatedAt, &f.Detail, &f.projectID,
		&f.signatures, &f.frozen, &f.rule, &f.maySign)
	if rootID != nil && rootNumber != nil {
		root.ID, root.ProjectKey, root.Key = *rootID, f.ProjectKey, requirement.Key(f.ProjectKey, *rootNumber)
		f.Root = &root
	}
	f.Locked = f.LockedAt != nil
	return f, err
}

func baselines(ctx context.Context, tx db.DBTX, rest string, args ...any) ([]found, error) {
	rows, err := tx.Query(ctx, selectBaseline+rest, args...)
	if err != nil {
		return nil, fmt.Errorf("read the baselines: %w", err)
	}
	all, err := pgx.CollectRows(rows, scanBaseline)
	if err != nil {
		return nil, err
	}
	return all, fillSigning(ctx, tx, all)
}

func get(ctx context.Context, tx db.DBTX, id uuid.UUID) (*found, error) {
	all, err := baselines(ctx, tx, ` WHERE b.id = $1`, id)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, ErrNotFound
	}
	return &all[0], nil
}

// List answers a project's baselines, newest first.
func (s *Service) List(ctx context.Context, reader uuid.UUID, projectKey string) ([]Baseline, error) {
	out := []Baseline{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		all, err := baselines(ctx, tx, ` WHERE b.project_id = $1 ORDER BY b.created_at DESC, b.id DESC`, p.ID)
		for _, f := range all {
			out = append(out, f.Baseline)
		}
		return err
	})
	return out, err
}

// Get answers one baseline.
func (s *Service) Get(ctx context.Context, reader, id uuid.UUID) (*Baseline, error) {
	var out *Baseline
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := get(ctx, tx, id)
		if f != nil {
			out = &f.Baseline
		}
		return err
	})
	return out, err
}

// keeping reads a project for a change to its baselines, which only its administrators make while it is open.
func keeping(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectKey string) (*project.Project, error) {
	p, err := project.Find(ctx, tx, actor, projectKey)
	switch {
	case err != nil:
		return nil, err
	case p.ArchivedAt != nil:
		return nil, project.ErrArchived
	case !p.Can.Administer:
		return nil, ErrForbidden
	}
	return p, nil
}

// rootOf is the section a baseline is taken of, which must be one of the project's out of the trash.
func rootOf(ctx context.Context, tx db.DBTX, actor uuid.UUID, p *project.Project, key string) (*uuid.UUID, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}
	refused := &FieldError{"rootKey", fmt.Sprintf("Pick a section of %s out of the trash, or none for the whole project.", p.Key)}
	r, err := requirement.FindEndpoint(ctx, tx, actor, key)
	switch {
	case errors.Is(err, requirement.ErrNotFound):
		return nil, refused
	case err != nil:
		return nil, err
	case r.ProjectID != p.ID || r.Kind != requirement.KindSection || r.Trashed:
		return nil, refused
	}
	return &r.ID, nil
}

// takeItems records every requirement and section of the project out of the trash, or of the subtree of $3, with
// its revision, place and status. $1 is the project, $2 the baseline.
const takeItems = requirement.WalkTree + `, scope AS (
	SELECT id FROM requirement WHERE id = $3::uuid
	UNION ALL
	SELECT c.id FROM requirement c JOIN scope s ON c.parent_id = s.id WHERE c.trashed_at IS NULL
)
INSERT INTO baseline_item (baseline_id, requirement_id, number, kind, revision, parent_id, rank, path, status_id, status_name,
                           status_category, type_id)
SELECT $2, r.id, r.number, r.kind, r.revision, r.parent_id, r.rank, w.path, r.status_id, st.name, st.category, r.type_id
FROM walk w JOIN requirement r ON r.id = w.id LEFT JOIN req_status st ON st.id = r.status_id
WHERE $3::uuid IS NULL OR w.id IN (SELECT id FROM scope)`

// takeLinks records the trace links between the baseline's items, takeExternal and takePages their Armature and
// Stator links neither broken nor detached, each with its suspect state and, since #153, its status and type's key.
const (
	takeLinks = `
INSERT INTO baseline_link (baseline_id, link_id, type_id, type_name, source_id, target_id, type_key, suspect)
SELECT $1, l.id, l.type_id, lt.name, l.source_id, l.target_id, lt.key, l.suspect
FROM trace_link_state l JOIN link_type lt ON lt.id = l.type_id
WHERE l.source_id IN (SELECT requirement_id FROM baseline_item WHERE baseline_id = $1)
  AND l.target_id IN (SELECT requirement_id FROM baseline_item WHERE baseline_id = $1)`
	takeExternal = `
INSERT INTO baseline_external_link (baseline_id, link_id, requirement_id, issue_key, relation, status_category, suspect)
SELECT $1, a.id, a.requirement_id, a.issue_key, a.relation, a.status_category, r.revision > a.reviewed_rev
FROM armature_issue_link a JOIN requirement r ON r.id = a.requirement_id
WHERE a.requirement_id IN (SELECT requirement_id FROM baseline_item WHERE baseline_id = $1)
  AND a.broken_at IS NULL AND a.detached_at IS NULL`
	takePages = `
INSERT INTO baseline_page_link (baseline_id, link_id, requirement_id, relation, page_id, space_key, url, suspect)
SELECT $1, l.id, l.requirement_id, l.relation, l.page_id, l.space_key, l.url, ` + trace.StatorSuspectSQL + `
FROM stator_page_link l JOIN requirement r ON r.id = l.requirement_id
WHERE l.requirement_id IN (SELECT requirement_id FROM baseline_item WHERE baseline_id = $1)
  AND l.broken_at IS NULL AND l.detached_at IS NULL`
	// takeEvidence records each item's latest test results on the counting branch, copied whole (#200).
	takeEvidence = `
INSERT INTO baseline_evidence (baseline_id, requirement_id, kind, repository, branch, job, suite, name, outcome, revision, commit_sha, url)
SELECT $1, tr.requirement_id, 'test', l.repository, l.branch, l.job, tr.suite, tr.name, tr.outcome, tr.revision, t.commit_sha, t.run_url
FROM baseline_item i
JOIN requirement r ON r.id = i.requirement_id
JOIN test_run_latest l ON l.project_id = r.project_id AND l.branch = evidence_counting_branch(r.project_id)
JOIN test_result tr ON tr.run_id = l.run_id AND tr.requirement_id = i.requirement_id
JOIN test_run t ON t.id = tr.run_id
WHERE i.baseline_id = $1`
	// takeCode records each item's code tags on the counting branch, copied whole (#201).
	takeCode = `
INSERT INTO baseline_evidence (baseline_id, requirement_id, kind, repository, branch, name, outcome, revision, commit_sha, url, path, line, word)
SELECT $1, t.requirement_id, 'code', s.repository, s.branch, t.path, code_tag_relation(s.project_id, t.word), t.revision, s.commit_sha,
       s.file_url, t.path, t.line, t.word
FROM baseline_item i
JOIN requirement r ON r.id = i.requirement_id
JOIN code_scan s ON s.project_id = r.project_id AND s.branch = evidence_counting_branch(r.project_id)
JOIN code_tag t ON t.scan_id = s.id AND t.requirement_id = i.requirement_id
WHERE i.baseline_id = $1 AND code_tag_relation(s.project_id, t.word) IS NOT NULL`
)

// Take records a baseline of the project, or of one section's subtree, in one transaction that reads the database as
// it stood when it began, so a change committed meanwhile is wholly in or wholly out.
func (s *Service) Take(ctx context.Context, actor uuid.UUID, projectKey string, in Input) (*Baseline, db.LSN, error) {
	name, err := checkName(in.Name)
	if err != nil {
		return nil, 0, err
	}
	description, err := checkDescription(in.Description)
	if err != nil {
		return nil, 0, err
	}
	var out *Baseline
	lsn, err := s.db.WriteSnapshot(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := keeping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		root, err := rootOf(ctx, tx, actor, p, in.RootKey)
		if err != nil {
			return err
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO baseline (project_id, name, description, root_id, root_number, created_by, detail)
			VALUES ($1, $2, $3, $4, (SELECT number FROM requirement WHERE id = $4), $5, $6) RETURNING id`,
			p.ID, name, description, root, actor, DetailFull).Scan(&id)
		if err != nil {
			return nameRefusal(err)
		}
		for _, step := range []struct {
			sql  string
			args []any
		}{{takeItems, []any{p.ID, id, root}}, {takeLinks, []any{id}}, {takeExternal, []any{id}}, {takePages, []any{id}},
			{takeEvidence, []any{id}}, {takeCode, []any{id}}} {
			if _, err := tx.Exec(ctx, step.sql, step.args...); err != nil {
				return fmt.Errorf("take the baseline: %w", err)
			}
		}
		f, err := get(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionBaselineTaken, TargetType: "baseline", TargetID: &id, Actor: actor,
			Data: map[string]any{"project": p.Key, "name": name, "items": f.Items}}); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicTaken, map[string]any{"actorId": actor, "baselineId": id, "projectId": p.ID}); err != nil {
			return err
		}
		out = &f.Baseline
		return nil
	})
	return out, lsn, err
}

func nameRefusal(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "baseline_name_taken" {
		return ErrNameTaken
	}
	return err
}

// managed reads a baseline for a change, refusing whoever is not one of its project's administrators.
func managed(ctx context.Context, tx db.DBTX, actor, id uuid.UUID) (*found, error) {
	f, err := get(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if _, err := keeping(ctx, tx, actor, f.ProjectKey); err != nil {
		return nil, err
	}
	return f, nil
}

// Update renames a baseline or locks it for good; a locked baseline refuses every change but locking it again.
func (s *Service) Update(ctx context.Context, actor, id uuid.UUID, in UpdateInput) (*Baseline, db.LSN, error) {
	var out *Baseline
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := managed(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		renames := in.Name != nil || in.Description != nil
		if f.signatures > 0 && (renames || (in.Locked != nil && !*in.Locked)) {
			return ErrSigned
		}
		if f.Locked {
			if renames || (in.Locked != nil && !*in.Locked) {
				return ErrLocked
			}
			out = &f.Baseline
			return nil
		}
		name, description := f.Name, f.Description
		if in.Name != nil {
			if name, err = checkName(*in.Name); err != nil {
				return err
			}
		}
		if in.Description != nil {
			if description, err = checkDescription(*in.Description); err != nil {
				return err
			}
		}
		lock := in.Locked != nil && *in.Locked
		_, err = tx.Exec(ctx, `
			UPDATE baseline SET name = $2, description = $3,
			       locked_at = CASE WHEN $4 THEN now() END, locked_by = CASE WHEN $4 THEN $5::uuid END
			WHERE id = $1`, id, name, description, lock, actor)
		if err != nil {
			return nameRefusal(err)
		}
		if lock {
			if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionBaselineLocked, TargetType: "baseline", TargetID: &id, Actor: actor,
				Data: map[string]any{"project": f.ProjectKey, "name": name}}); err != nil {
				return err
			}
		}
		got, err := get(ctx, tx, id)
		if got != nil {
			out = &got.Baseline
		}
		return err
	})
	return out, lsn, err
}

// Delete removes a baseline that is not locked, with what it holds.
func (s *Service) Delete(ctx context.Context, actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := managed(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if f.signatures > 0 {
			return ErrSigned
		}
		if f.Locked {
			return ErrLocked
		}
		if _, err := tx.Exec(ctx, `DELETE FROM baseline WHERE id = $1`, id); err != nil {
			return fmt.Errorf("delete the baseline: %w", err)
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionBaselineDeleted, TargetType: "baseline", TargetID: &id, Actor: actor,
			Data: map[string]any{"project": f.ProjectKey, "name": f.Name}})
	})
}

// Spec answers a window of a baseline read as a specification: each requirement at the revision it holds, in the
// outline it had. A requirement purged since, or in the trash where the reader may not look, comes without content.
func (s *Service) Spec(ctx context.Context, reader, id uuid.UUID, limit, offset int) ([]Entry, int, error) {
	out := []Entry{}
	total := 0
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := get(ctx, tx, id)
		if err != nil {
			return err
		}
		total = f.Items
		rows, err := tx.Query(ctx, `
			SELECT i.requirement_id, i.number, i.kind, COALESCE(v.title, ''), i.revision, i.path,
			       cardinality(i.path) - (SELECT min(cardinality(m.path)) FROM baseline_item m WHERE m.baseline_id = $1),
			       v.statement, v.rationale, i.status_id, COALESCE(i.status_name, ''), COALESCE(i.status_category, '')
			FROM baseline_item i
			LEFT JOIN requirement_revision v ON v.requirement_id = i.requirement_id AND v.number = i.revision
			WHERE i.baseline_id = $1 ORDER BY i.path LIMIT $2 OFFSET $3`, id, limit, offset)
		if err != nil {
			return fmt.Errorf("read the baseline: %w", err)
		}
		entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Entry, error) {
			var (
				e                    Entry
				number               int
				path                 []int32
				statement, rationale []byte
				statusID             *uuid.UUID
				status               requirement.StatusRef
			)
			r := &e.Requirement
			err := row.Scan(&r.ID, &number, &r.Kind, &r.Title, &r.Revision, &path, &e.Depth, &statement, &rationale,
				&statusID, &status.Name, &status.Category)
			r.ProjectKey, r.Key, e.Outline = f.ProjectKey, requirement.Key(f.ProjectKey, number), requirement.OutlineOf(path)
			if statusID != nil {
				status.ID = *statusID
				r.Status, e.Status = &status, &status
			}
			e.Statement, e.Rationale = raw(statement), raw(rationale)
			return e, err
		})
		if entries != nil {
			out = entries
		}
		return err
	})
	return out, total, err
}

// raw passes stored JSON through as it is, null when there is none.
func raw(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}

// Compare answers what differs from a baseline to another of its project, or to now when with is empty or now.
func (s *Service) Compare(ctx context.Context, reader, id uuid.UUID, with string) (*Comparison, error) {
	var out *Comparison
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		from, err := get(ctx, tx, id)
		if err != nil {
			return err
		}
		c := &Comparison{From: Side{Baseline: &from.Baseline, At: from.CreatedAt}}
		before, err := baselineSide(ctx, tx, from.ID)
		if err != nil {
			return err
		}
		var after *side
		switch with = strings.TrimSpace(with); with {
		case "", "now":
			var at time.Time
			if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&at); err != nil {
				return err
			}
			c.To = Side{At: at}
			if after, err = nowSide(ctx, tx, from); err != nil {
				return err
			}
		default:
			other, err := otherBaseline(ctx, tx, from, with)
			if err != nil {
				return err
			}
			c.To = Side{Baseline: &other.Baseline, At: other.CreatedAt}
			if after, err = baselineSide(ctx, tx, other.ID); err != nil {
				return err
			}
		}
		numbers := map[uuid.UUID]int{}
		for _, h := range append(append([]held{}, before.items...), after.items...) {
			numbers[h.ID] = h.Number
		}
		keyOf := func(id uuid.UUID) string { return requirement.Key(from.ProjectKey, numbers[id]) }
		c.Items = Classify(from.ProjectKey, before.items, after.items)
		c.Links = DiffLinks(before.links, after.links, keyOf)
		c.ExternalLinks = DiffExternal(before.external, after.external, keyOf)
		if c.Items == nil {
			c.Items = []ItemChange{}
		}
		out = c
		return nil
	})
	return out, err
}

func otherBaseline(ctx context.Context, tx db.DBTX, from *found, with string) (*found, error) {
	refused := &FieldError{"with", "Compare with another baseline of this project, or with now."}
	id, err := uuid.Parse(with)
	if err != nil {
		return nil, refused
	}
	other, err := get(ctx, tx, id)
	if errors.Is(err, ErrNotFound) || (err == nil && other.projectID != from.projectID) {
		return nil, refused
	}
	return other, err
}
