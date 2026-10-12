package requirement

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// Topics this package emits, which M2 and M3 build on.
const (
	TopicCreated  = "requirement.created"
	TopicRevised  = "requirement.revised"
	TopicChanged  = "requirement.changed"
	TopicTrashed  = "requirement.trashed"
	TopicRestored = "requirement.restored"
)

var (
	ErrNotFound = errors.New("requirement not found")
	// ErrForbidden is somebody who may view a project changing what only its editors change.
	ErrForbidden = errors.New("only the project's editors change its requirements")
	// ErrNotAdministrator is somebody who does not administer the project purging its trash.
	ErrNotAdministrator = errors.New("only the project's administrators purge its trash")
	// ErrTrashed is a change to a requirement in the trash, which waits for its restore.
	ErrTrashed = errors.New("the requirement is in the trash")
)

// FieldError is input refused on one field, with the sentence to show beside it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// ConflictError is a save over a revision that is no longer the latest.
type ConflictError struct {
	Key      string
	Revision int
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("somebody saved revision %d of %s meanwhile", e.Revision, e.Key)
}

// Service keeps requirements, their revisions and their trash.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// loaded is a requirement with what deciding a change needs besides.
type loaded struct {
	Requirement
	projectID uuid.UUID
	archived  bool
	// editor says the reader edits in the project, which a trashed requirement's Can leaves out.
	editor bool
	hash   string
	// requireComment is the project's ask for a comment on every new revision.
	requireComment bool
}

// selectRequirement reads requirements with what $1 may do to each; the type, status, labels, place and copy
// come with #14, #17, #19 and #15.
const selectRequirement = `
SELECT r.id, p.key, r.number, r.kind, r.title, r.statement, r.rationale, r.attributes, r.revision,
       r.owner_id, COALESCE(o.name, ''), r.priority, r.created_at, COALESCE(cu.name, ''), r.updated_at, COALESCE(uu.name, ''),
       r.trashed_at, r.project_id, p.archived_at IS NOT NULL, r.normative_hash,
       COALESCE((p.settings ->> 'requireRevisionComment')::boolean, false),
       perm_project_holds(r.project_id, $1, 'edit'), perm_project_holds(r.project_id, $1, 'approve'),
       perm_project_holds(r.project_id, $1, 'comment'),
       COALESCE((SELECT array_agg(k.key ORDER BY k.created_at DESC) FROM requirement_old_key k
                 WHERE k.org_id = r.org_id AND k.requirement_id = r.id), '{}')
       || COALESCE((SELECT array_agg(a.key || '-R' || r.number ORDER BY a.created_at DESC, a.key) FROM project_key_alias a
                 WHERE a.org_id = r.org_id AND a.project_id = r.project_id), '{}')
FROM requirement r
JOIN project p ON p.org_id = r.org_id AND p.id = r.project_id
LEFT JOIN app_user o ON o.id = r.owner_id
LEFT JOIN app_user cu ON cu.id = r.created_by
LEFT JOIN app_user uu ON uu.id = r.updated_by`

func scan(row pgx.Row) (*loaded, error) {
	var (
		l                    loaded
		ownerID              *uuid.UUID
		ownerName            string
		priority             *string
		attrs                []byte
		rationale            []byte
		edit, approve, alter bool
	)
	r := &l.Requirement
	err := row.Scan(&r.ID, &r.ProjectKey, &r.Number, &r.Kind, &r.Title, &r.Statement, &rationale, &attrs, &r.Revision,
		&ownerID, &ownerName, &priority, &r.CreatedAt, &r.CreatedByName, &r.UpdatedAt, &r.UpdatedByName,
		&r.TrashedAt, &l.projectID, &l.archived, &l.hash, &l.requireComment, &edit, &approve, &alter, &r.FormerKeys)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.Key = Key(r.ProjectKey, r.Number)
	if len(rationale) > 0 {
		r.Rationale = rationale
	}
	if err := json.Unmarshal(attrs, &r.Attributes); err != nil {
		return nil, fmt.Errorf("read the attributes of %s: %w", r.Key, err)
	}
	if ownerID != nil {
		r.Owner = &PersonRef{ID: *ownerID, Name: ownerName}
	}
	if priority != nil {
		p := Priority(*priority)
		r.Priority = &p
	}
	r.Labels = []string{}
	open := r.TrashedAt == nil
	l.editor = edit
	r.Can = Can{Edit: edit && open, Transition: (edit || approve) && open, Delete: edit && open, Link: edit && open, Comment: alter && open}
	return &l, nil
}

// find reads the requirement a key names: a key it left on a move first, then its project's key or one its
// project had, with its number.
func find(ctx context.Context, tx db.DBTX, reader uuid.UUID, key string) (*loaded, error) {
	projectKey, number, ok := ParseKey(key)
	if !ok {
		return nil, ErrNotFound
	}
	l, err := scan(tx.QueryRow(ctx, selectRequirement+`
		WHERE r.org_id = current_org_id() AND r.id = (SELECT k.requirement_id FROM requirement_old_key k
		      WHERE k.org_id = current_org_id() AND k.key = $2)`, reader, Key(projectKey, number)))
	if !errors.Is(err, ErrNotFound) {
		return l, err
	}
	return scan(tx.QueryRow(ctx, selectRequirement+`
		WHERE r.org_id = current_org_id() AND r.number = $3 AND r.project_id = (
		      SELECT x.id FROM project x WHERE x.org_id = current_org_id() AND x.key = $2
		      UNION ALL SELECT a.project_id FROM project_key_alias a WHERE a.org_id = current_org_id() AND a.key = $2
		      LIMIT 1)`, reader, projectKey, number))
}

func findByID(ctx context.Context, tx db.DBTX, reader, id uuid.UUID) (*loaded, error) {
	return scan(tx.QueryRow(ctx, selectRequirement+` WHERE r.org_id = current_org_id() AND r.id = $2`, reader, id))
}

// Get answers a requirement by its key or a key it had; the answer carries its current key.
func (s *Service) Get(ctx context.Context, reader uuid.UUID, key string) (*Requirement, error) {
	var out *Requirement
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		l, err := find(ctx, tx, reader, key)
		if err == nil {
			out = &l.Requirement
		}
		return err
	})
	return out, err
}

// GetByID answers a requirement by its id, which never changes.
func (s *Service) GetByID(ctx context.Context, reader, id uuid.UUID) (*Requirement, error) {
	var out *Requirement
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		l, err := findByID(ctx, tx, reader, id)
		if err == nil {
			out = &l.Requirement
		}
		return err
	})
	return out, err
}

func checkTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", &FieldError{"title", "Give the requirement a title."}
	}
	if utf8.RuneCountInString(title) > MaxTitleLength {
		return "", &FieldError{"title", fmt.Sprintf("Keep the title to %d characters; put the rest in the statement.", MaxTitleLength)}
	}
	return title, nil
}

func checkComment(comment string) (string, error) {
	comment = strings.TrimSpace(comment)
	if utf8.RuneCountInString(comment) > MaxCommentLength {
		return "", &FieldError{"comment", fmt.Sprintf("Keep the comment to %d characters.", MaxCommentLength)}
	}
	return comment, nil
}

// isNull says a field was sent as JSON null, which clears it.
func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

// checkDocument holds a statement or a rationale to the editor's allowlist.
func checkDocument(field string, raw json.RawMessage) (json.RawMessage, error) {
	if _, err := document.Parse(raw); err != nil {
		var invalid *document.InvalidError
		if errors.As(err, &invalid) {
			return nil, &FieldError{field, invalid.Message}
		}
		return nil, err
	}
	return raw, nil
}

// untilLater refuses what the requirements core does not have yet: types and attributes (#14), labels (#19)
// and a place in the tree (#15).
func untilLater(typeID *uuid.UUID, attributes map[string]json.RawMessage, labels []string, place *Place) error {
	switch {
	case typeID != nil:
		return &FieldError{"typeId", "This project has no requirement types yet. Leave the type out."}
	case len(attributes) > 0:
		return &FieldError{"attributes", "This project has no attributes yet. Leave them out."}
	case len(labels) > 0:
		return &FieldError{"labels", "Requirements take no labels yet. Leave them out."}
	case place != nil:
		return &FieldError{"place", "This project has no tree yet. Leave the place out, and the requirement goes last."}
	}
	return nil
}

func checkPriority(p Priority) error {
	if !slices.Contains(Priorities, p) {
		return &FieldError{"priority", "Choose must, should, could or wont, or leave the priority out."}
	}
	return nil
}

// checkOwner holds an owner to a member who may view the project.
func checkOwner(ctx context.Context, tx db.DBTX, projectID, owner uuid.UUID) error {
	var views bool
	if err := tx.QueryRow(ctx, `SELECT perm_project_holds($1, $2, 'view')`, projectID, owner).Scan(&views); err != nil {
		return err
	}
	if !views {
		return &FieldError{"ownerId", "Pick an owner who may view this project."}
	}
	return nil
}

// Create writes a requirement or a section as revision 1.
func (s *Service) Create(ctx context.Context, actor uuid.UUID, projectKey string, in CreateInput) (*Requirement, db.LSN, error) {
	kind := in.Kind
	if kind == "" {
		kind = KindRequirement
	}
	if !slices.Contains(Kinds, kind) {
		return nil, 0, &FieldError{"kind", "Make a requirement or a section."}
	}
	title, err := checkTitle(in.Title)
	if err != nil {
		return nil, 0, err
	}
	statement := document.Empty
	if len(in.Statement) > 0 && !isNull(in.Statement) {
		if statement, err = checkDocument("statement", in.Statement); err != nil {
			return nil, 0, err
		}
	}
	var rationale json.RawMessage
	if len(in.Rationale) > 0 && !isNull(in.Rationale) {
		if kind == KindSection {
			return nil, 0, &FieldError{"rationale", "A section has no rationale. Put its words in the statement."}
		}
		if rationale, err = checkDocument("rationale", in.Rationale); err != nil {
			return nil, 0, err
		}
	}
	if err := untilLater(in.TypeID, in.Attributes, in.Labels, in.Place); err != nil {
		return nil, 0, err
	}
	if kind == KindSection && (in.OwnerID != nil || in.Priority != nil) {
		return nil, 0, &FieldError{"ownerId", "A section has no owner or priority. Give them to the requirements in it."}
	}
	if in.Priority != nil {
		if err := checkPriority(*in.Priority); err != nil {
			return nil, 0, err
		}
	}
	comment, err := checkComment(in.Comment)
	if err != nil {
		return nil, 0, err
	}
	hash := Normative{Title: title, Statement: statement, Rationale: rationale}.Hash()

	var out *Requirement
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if p.ArchivedAt != nil {
			return project.ErrArchived
		}
		if !p.Can.Edit {
			return ErrForbidden
		}
		if in.OwnerID != nil {
			if err := checkOwner(ctx, tx, p.ID, *in.OwnerID); err != nil {
				return err
			}
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO requirement (org_id, project_id, kind, title, statement, rationale, normative_hash, owner_id, priority, created_by, updated_by)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $9) RETURNING id`,
			p.ID, kind, title, statement, rationale, hash, in.OwnerID, in.Priority, actor).Scan(&id); err != nil {
			return fmt.Errorf("write the requirement: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO requirement_revision (org_id, requirement_id, number, title, statement, rationale, hash, comment, created_by)
			VALUES (current_org_id(), $1, 1, $2, $3, $4, $5, $6, $7)`, id, title, statement, rationale, hash, comment, actor); err != nil {
			return fmt.Errorf("write revision 1: %w", err)
		}
		if err := events.Emit(ctx, tx, TopicCreated, map[string]any{"actorId": actor, "projectId": p.ID, "requirementId": id, "revision": 1}); err != nil {
			return err
		}
		l, err := findByID(ctx, tx, actor, id)
		if err == nil {
			out = &l.Requirement
		}
		return err
	})
	return out, lsn, err
}

// changing reads a requirement for a change its project's editors make, and holds its row. The lock is taken
// once the reader may change it, since a row they may not change is one the lock cannot see.
func changing(ctx context.Context, tx db.DBTX, actor uuid.UUID, key string) (*loaded, error) {
	l, err := find(ctx, tx, actor, key)
	if err != nil {
		return nil, err
	}
	switch {
	case l.archived:
		return nil, project.ErrArchived
	case !l.editor:
		return nil, ErrForbidden
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM requirement WHERE id = $1 FOR UPDATE`, l.ID); err != nil {
		return nil, fmt.Errorf("hold %s: %w", l.Key, err)
	}
	// Read again under the lock, so a save that landed meanwhile is the one this change is decided on.
	return findByID(ctx, tx, actor, l.ID)
}

// logChange notes a change that writes no revision.
func logChange(ctx context.Context, tx db.DBTX, actor, id uuid.UUID, field ChangeField, from, to string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO requirement_change (org_id, requirement_id, field, from_value, to_value, actor_id)
		VALUES (current_org_id(), $1, $2, $3, $4, $5)`, id, field, from, to, actor); err != nil {
		return fmt.Errorf("log the %s change: %w", field, err)
	}
	return events.Emit(ctx, tx, TopicChanged, map[string]any{"actorId": actor, "requirementId": id, "field": field})
}

func personName(ctx context.Context, tx db.DBTX, id *uuid.UUID) (string, error) {
	if id == nil {
		return "", nil
	}
	var name string
	err := tx.QueryRow(ctx, `SELECT name FROM app_user WHERE id = $1`, *id).Scan(&name)
	return name, err
}

func priorityText(p *Priority) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

// Update changes what in names. A change to the normative content writes the next revision over baseRevision;
// the owner and the priority are logged, and the last save of them wins.
func (s *Service) Update(ctx context.Context, actor uuid.UUID, key string, in UpdateInput) (*Requirement, db.LSN, error) {
	var out *Requirement
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		cur, err := changing(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		if cur.TrashedAt != nil {
			return ErrTrashed
		}
		next := Normative{Title: cur.Title, Statement: cur.Statement, Rationale: cur.Rationale}
		if in.Title != nil {
			if next.Title, err = checkTitle(*in.Title); err != nil {
				return err
			}
		}
		if len(in.Statement) > 0 {
			if isNull(in.Statement) {
				return &FieldError{"statement", "A requirement always has a statement. Send an empty document to clear it."}
			}
			if next.Statement, err = checkDocument("statement", in.Statement); err != nil {
				return err
			}
		}
		if len(in.Rationale) > 0 {
			switch {
			case isNull(in.Rationale):
				next.Rationale = nil
			case cur.Kind == KindSection:
				return &FieldError{"rationale", "A section has no rationale. Put its words in the statement."}
			default:
				if next.Rationale, err = checkDocument("rationale", in.Rationale); err != nil {
					return err
				}
			}
		}
		if err := untilLater(in.TypeID, in.Attributes, nil, nil); err != nil {
			return err
		}
		if hash := next.Hash(); hash != cur.hash {
			if err := s.revise(ctx, tx, actor, cur, next, hash, in); err != nil {
				return err
			}
		}
		if err := s.changeOwner(ctx, tx, actor, cur, in.OwnerID); err != nil {
			return err
		}
		if err := s.changePriority(ctx, tx, actor, cur, in.Priority); err != nil {
			return err
		}
		l, err := findByID(ctx, tx, actor, cur.ID)
		if err == nil {
			out = &l.Requirement
		}
		return err
	})
	return out, lsn, err
}

// revise writes the next revision of cur with the content next, which differs from it.
func (s *Service) revise(ctx context.Context, tx db.DBTX, actor uuid.UUID, cur *loaded, next Normative, hash string, in UpdateInput) error {
	if in.BaseRevision == nil {
		return &FieldError{"baseRevision", "Say which revision you changed, as baseRevision, so a newer one is not overwritten."}
	}
	if *in.BaseRevision != cur.Revision {
		return &ConflictError{Key: cur.Key, Revision: cur.Revision}
	}
	comment, err := checkComment(in.Comment)
	if err != nil {
		return err
	}
	if comment == "" && cur.requireComment {
		return &FieldError{"comment", "This project asks for a comment on every new revision. Say what changed and why."}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE requirement SET title = $2, statement = $3, rationale = $4, normative_hash = $5, updated_by = $6
		WHERE id = $1`, cur.ID, next.Title, next.Statement, next.Rationale, hash, actor); err != nil {
		return fmt.Errorf("change %s: %w", cur.Key, err)
	}
	number := cur.Revision + 1
	if _, err := tx.Exec(ctx, `
		INSERT INTO requirement_revision (org_id, requirement_id, number, title, statement, rationale, hash, comment, created_by)
		VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8)`,
		cur.ID, number, next.Title, next.Statement, next.Rationale, hash, comment, actor); err != nil {
		return fmt.Errorf("write revision %d of %s: %w", number, cur.Key, err)
	}
	return events.Emit(ctx, tx, TopicRevised, map[string]any{"actorId": actor, "requirementId": cur.ID, "revision": number})
}

func (s *Service) changeOwner(ctx context.Context, tx db.DBTX, actor uuid.UUID, cur *loaded, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var owner *uuid.UUID
	if !isNull(raw) {
		if err := json.Unmarshal(raw, &owner); err != nil {
			return &FieldError{"ownerId", "Pick the owner from the list of people."}
		}
	}
	was := (*uuid.UUID)(nil)
	if cur.Owner != nil {
		was = &cur.Owner.ID
	}
	if (owner == nil) == (was == nil) && (owner == nil || *owner == *was) {
		return nil
	}
	if owner != nil {
		if cur.Kind == KindSection {
			return &FieldError{"ownerId", "A section has no owner. Give one to the requirements in it."}
		}
		if err := checkOwner(ctx, tx, cur.projectID, *owner); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE requirement SET owner_id = $2, updated_by = $3 WHERE id = $1`, cur.ID, owner, actor); err != nil {
		return fmt.Errorf("change the owner of %s: %w", cur.Key, err)
	}
	from, err := personName(ctx, tx, was)
	if err != nil {
		return err
	}
	to, err := personName(ctx, tx, owner)
	if err != nil {
		return err
	}
	return logChange(ctx, tx, actor, cur.ID, ChangeOwner, from, to)
}

func (s *Service) changePriority(ctx context.Context, tx db.DBTX, actor uuid.UUID, cur *loaded, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var priority *Priority
	if !isNull(raw) {
		if err := json.Unmarshal(raw, &priority); err != nil {
			return checkPriority("")
		}
		if err := checkPriority(*priority); err != nil {
			return err
		}
		if cur.Kind == KindSection {
			return &FieldError{"priority", "A section has no priority. Give one to the requirements in it."}
		}
	}
	if priorityText(priority) == priorityText(cur.Priority) {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE requirement SET priority = $2, updated_by = $3 WHERE id = $1`, cur.ID, priority, actor); err != nil {
		return fmt.Errorf("change the priority of %s: %w", cur.Key, err)
	}
	return logChange(ctx, tx, actor, cur.ID, ChangePriority, priorityText(cur.Priority), priorityText(priority))
}

// trashedNote is how the activity log words a requirement going to the trash and coming back.
const (
	trashedNote  = "trashed"
	restoredNote = "restored"
)

// Trash moves a requirement to its project's trash; one already there stays as it is.
func (s *Service) Trash(ctx context.Context, actor uuid.UUID, key string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		cur, err := changing(ctx, tx, actor, key)
		if err != nil || cur.TrashedAt != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE requirement SET trashed_at = now(), trashed_by = $2, trash_id = id, updated_by = $2 WHERE id = $1`, cur.ID, actor); err != nil {
			return fmt.Errorf("trash %s: %w", cur.Key, err)
		}
		if err := logChange(ctx, tx, actor, cur.ID, ChangeTrash, "", trashedNote); err != nil {
			return err
		}
		return events.Emit(ctx, tx, TopicTrashed, map[string]any{"actorId": actor, "requirementId": cur.ID})
	})
}

// trashOf reads a project for its trash, which its editors open and its administrators empty.
func trashOf(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectKey string, purge bool) (*project.Project, error) {
	p, err := project.Find(ctx, tx, actor, projectKey)
	switch {
	case err != nil:
		return nil, err
	case p.ArchivedAt != nil:
		return nil, project.ErrArchived
	case purge && !p.Can.Administer:
		return nil, ErrNotAdministrator
	case !p.Can.Edit:
		return nil, ErrForbidden
	}
	return p, nil
}

// TrashItems lists what went to a project's trash, the latest first, each with what went with it.
func (s *Service) TrashItems(ctx context.Context, actor uuid.UUID, projectKey string) ([]TrashItem, error) {
	out := []TrashItem{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := trashOf(ctx, tx, actor, projectKey, false)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT r.id, r.number, r.kind, r.title, r.trashed_at, COALESCE(u.name, ''),
			       (SELECT count(*)::int FROM requirement x WHERE x.org_id = r.org_id AND x.trash_id = r.id)
			FROM requirement r LEFT JOIN app_user u ON u.id = r.trashed_by
			WHERE r.org_id = current_org_id() AND r.project_id = $1 AND r.trash_id = r.id
			ORDER BY r.trashed_at DESC, r.number DESC`, p.ID)
		if err != nil {
			return err
		}
		items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (TrashItem, error) {
			var (
				item   TrashItem
				number int
			)
			err := row.Scan(&item.ID, &number, &item.Kind, &item.Title, &item.TrashedAt, &item.TrashedByName, &item.Requirements)
			item.Key = Key(p.Key, number)
			// Without a tree (#15) every requirement goes back to the top, where it was.
			item.ParentInTree = true
			return item, err
		})
		if items != nil {
			out = items
		}
		return err
	})
	return out, err
}

// Restore puts a trashed requirement, and what went with it, back.
func (s *Service) Restore(ctx context.Context, actor uuid.UUID, projectKey string, id uuid.UUID) (*Requirement, db.LSN, error) {
	var out *Requirement
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := trashOf(ctx, tx, actor, projectKey, false)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			UPDATE requirement SET trashed_at = NULL, trashed_by = NULL, trash_id = NULL, updated_by = $3
			WHERE org_id = current_org_id() AND project_id = $1 AND trash_id = $2 RETURNING id`, p.ID, id, actor)
		if err != nil {
			return err
		}
		restored, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		if len(restored) == 0 {
			return ErrNotFound
		}
		for _, each := range restored {
			if err := logChange(ctx, tx, actor, each, ChangeTrash, trashedNote, restoredNote); err != nil {
				return err
			}
			if err := events.Emit(ctx, tx, TopicRestored, map[string]any{"actorId": actor, "requirementId": each}); err != nil {
				return err
			}
		}
		l, err := findByID(ctx, tx, actor, id)
		if err == nil {
			out = &l.Requirement
		}
		return err
	})
	return out, lsn, err
}

// Purge deletes a trashed requirement, what went with it and their history for good.
func (s *Service) Purge(ctx context.Context, actor uuid.UUID, projectKey string, id uuid.UUID) (db.LSN, error) {
	return s.purge(ctx, actor, projectKey, &id)
}

// EmptyTrash deletes everything in a project's trash for good.
func (s *Service) EmptyTrash(ctx context.Context, actor uuid.UUID, projectKey string) (db.LSN, error) {
	return s.purge(ctx, actor, projectKey, nil)
}

// purge deletes one item of the trash, or all of it when item is nil, and keeps in the audit log which keys
// went, since their history goes with them.
func (s *Service) purge(ctx context.Context, actor uuid.UUID, projectKey string, item *uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := trashOf(ctx, tx, actor, projectKey, true)
		if err != nil {
			return err
		}
		// What went with a root goes by the cascade on trash_id; the count is taken first, to say it.
		var gone int
		if err := tx.QueryRow(ctx, `
			SELECT count(*)::int FROM requirement x JOIN requirement r ON r.org_id = x.org_id AND r.id = x.trash_id
			WHERE r.org_id = current_org_id() AND r.project_id = $1 AND r.trash_id = r.id AND ($2::uuid IS NULL OR r.id = $2)`,
			p.ID, item).Scan(&gone); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			DELETE FROM requirement WHERE org_id = current_org_id() AND project_id = $1 AND trash_id = id AND ($2::uuid IS NULL OR id = $2)
			RETURNING number`, p.ID, item)
		if err != nil {
			return err
		}
		numbers, err := pgx.CollectRows(rows, pgx.RowTo[int])
		if err != nil {
			return err
		}
		if item != nil && len(numbers) == 0 {
			return ErrNotFound
		}
		slices.Sort(numbers)
		keys := make([]string, 0, len(numbers))
		for _, n := range numbers {
			keys = append(keys, Key(p.Key, n))
		}
		org, err := tenant.MustFromContext(ctx)
		if err != nil {
			return err
		}
		action := audit.ActionTrashEmptied
		target := p.ID
		targetType := "project"
		if item != nil {
			action, target, targetType = audit.ActionRequirementPurged, *item, "requirement"
		}
		return audit.Write(ctx, tx, org.ID, audit.Entry{Action: action, TargetType: targetType, TargetID: &target, Actor: actor,
			Data: map[string]any{"project": p.Key, "keys": keys, "requirements": gone}})
	})
}
