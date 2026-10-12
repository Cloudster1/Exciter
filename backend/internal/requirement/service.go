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

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
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

// selectRequirement reads requirements with what $1 may do to each; the status, labels, place and copy come
// with #17, #19 and #15.
const selectRequirement = `
SELECT r.id, p.key, r.number, r.kind, r.title, r.statement, r.rationale, r.attributes, r.revision,
       r.owner_id, COALESCE(o.name, ''), r.priority, r.created_at, COALESCE(cu.name, ''), r.updated_at, COALESCE(uu.name, ''),
       r.trashed_at, r.project_id, p.archived_at IS NOT NULL, r.normative_hash,
       r.type_id, COALESCE(t.name, ''), COALESCE(t.icon, ''), COALESCE(t.colour, ''),
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
LEFT JOIN app_user uu ON uu.id = r.updated_by
LEFT JOIN requirement_type t ON t.id = r.type_id`

// typeID is the requirement's type, nil for a section or one written before types.
func (l *loaded) typeID() *uuid.UUID {
	if l.Type == nil {
		return nil
	}
	id := l.Type.ID
	return &id
}

func scan(row pgx.Row) (*loaded, error) {
	var (
		l                    loaded
		ownerID              *uuid.UUID
		ownerName            string
		priority             *string
		attrs                []byte
		rationale            []byte
		edit, approve, alter bool
		typeID               *uuid.UUID
		typ                  TypeRef
	)
	r := &l.Requirement
	err := row.Scan(&r.ID, &r.ProjectKey, &r.Number, &r.Kind, &r.Title, &r.Statement, &rationale, &attrs, &r.Revision,
		&ownerID, &ownerName, &priority, &r.CreatedAt, &r.CreatedByName, &r.UpdatedAt, &r.UpdatedByName,
		&r.TrashedAt, &l.projectID, &l.archived, &l.hash, &typeID, &typ.Name, &typ.Icon, &typ.Colour,
		&l.requireComment, &edit, &approve, &alter, &r.FormerKeys)
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
	if typeID != nil {
		typ.ID = *typeID
		r.Type = &typ
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

// untilLater refuses what the requirements core does not have yet: labels (#19) and a place in the tree (#15).
func untilLater(labels []string, place *Place) error {
	switch {
	case len(labels) > 0:
		return &FieldError{"labels", "Requirements take no labels yet. Leave them out."}
	case place != nil:
		return &FieldError{"place", "This project has no tree yet. Leave the place out, and the requirement goes last."}
	}
	return nil
}

// checkType holds a requirement's type to one of its project's that new requirements may take; the type it
// already has may stay, retired or not.
func checkType(ctx context.Context, tx db.DBTX, projectID uuid.UUID, typeID, had *uuid.UUID) error {
	if typeID == nil {
		return &FieldError{"typeId", "Pick the requirement's type."}
	}
	var retired *bool
	err := tx.QueryRow(ctx, `SELECT retired FROM requirement_type WHERE project_id = $1 AND id = $2`, projectID, *typeID).Scan(&retired)
	if errors.Is(err, pgx.ErrNoRows) {
		return &FieldError{"typeId", "That type is not one of this project's. Pick one from its types."}
	}
	if err != nil {
		return err
	}
	if *retired && (had == nil || *had != *typeID) {
		return &FieldError{"typeId", "That type is retired, so no requirement newly takes it. Pick another."}
	}
	return nil
}

// viewerIn answers whether a person may view a project, which an owner and a user value must.
func viewerIn(ctx context.Context, tx db.DBTX, projectID uuid.UUID) attribute.Viewer {
	return func(person uuid.UUID) (bool, error) {
		var views bool
		err := tx.QueryRow(ctx, `SELECT perm_project_holds($1, $2, 'view')`, projectID, person).Scan(&views)
		return views, err
	}
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
