package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// Topics this package emits, which M2 and M3 build on.
const (
	TopicKeyChanged = "project.key_changed"
	TopicDeleted    = "project.deleted"
)

// maxKeySuggestions bounds the numbered tries at a free key for a name.
const maxKeySuggestions = 99

var (
	ErrNotFound = errors.New("project not found")
	// ErrNotAdmin is somebody who does not administer the organization making a project.
	ErrNotAdmin = errors.New("only an owner or an administrator of the organization makes projects")
	// ErrForbidden is somebody who may view a project changing what only its administrators change.
	ErrForbidden = errors.New("only the project's administrators change it")
	ErrArchived  = errors.New("the project is archived")
	// ErrNotArchived is a delete of a project that was not archived first.
	ErrNotArchived = errors.New("the project is not archived")
)

// FieldError is input refused on one field, with the sentence to show beside it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Service keeps projects and the keys they had.
type Service struct {
	db *db.Cluster
	// seed makes what a template holds; nil makes blank projects alone.
	seed Seeder
}

func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster}
}

// selectProjects reads projects with what $1 may do in each; requirements count from #13 on.
const selectProjects = `
SELECT p.id, p.key, p.name, p.description, p.settings, p.template, p.created_at, p.updated_at, p.archived_at,
       COALESCE(a.name, ''),
       COALESCE((SELECT array_agg(k.key ORDER BY k.created_at DESC, k.key) FROM project_key_alias k
                 WHERE k.org_id = p.org_id AND k.project_id = p.id), '{}'),
       perm_project_holds(p.id, $1, 'view'), perm_project_holds(p.id, $1, 'comment'),
       perm_project_holds(p.id, $1, 'edit'), perm_project_holds(p.id, $1, 'approve'),
       perm_project_holds(p.id, $1, 'administer')
FROM project p
LEFT JOIN app_user a ON a.id = p.archived_by`

// byKey finds a project by its key or a key it had; $2 is the key, normalized.
const byKey = ` WHERE p.org_id = current_org_id() AND (p.key = $2 OR p.id = (
	SELECT k.project_id FROM project_key_alias k WHERE k.org_id = current_org_id() AND k.key = $2))`

func scan(row pgx.Row) (*Project, error) {
	var (
		p        Project
		settings []byte
	)
	err := row.Scan(&p.ID, &p.Key, &p.Name, &p.Description, &settings, &p.Template, &p.CreatedAt, &p.UpdatedAt,
		&p.ArchivedAt, &p.ArchivedByName, &p.FormerKeys,
		&p.Can.View, &p.Can.Comment, &p.Can.Edit, &p.Can.Approve, &p.Can.Administer)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(settings, &p.Settings); err != nil {
		return nil, fmt.Errorf("read the settings of project %s: %w", p.Key, err)
	}
	p.Can.Purge = p.Can.Administer && p.ArchivedAt == nil
	return &p, nil
}

// List answers the projects reader may view, by name; archived ones only when asked for.
func (s *Service) List(ctx context.Context, reader uuid.UUID, archived bool) ([]Project, error) {
	out := []Project{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectProjects+`
			WHERE p.org_id = current_org_id() AND ($2 OR p.archived_at IS NULL)
			ORDER BY lower(p.name), p.key`, reader, archived)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, *p)
		}
		return rows.Err()
	})
	return out, err
}

// Find reads, in the caller's transaction, the project reader may view by its key or a key it had; ErrNotFound
// when there is none they may view.
func Find(ctx context.Context, tx db.DBTX, reader uuid.UUID, key string) (*Project, error) {
	p, err := scan(tx.QueryRow(ctx, selectProjects+byKey, reader, NormalizeKey(key)))
	if err != nil {
		return nil, err
	}
	if !p.Can.View {
		return nil, ErrNotFound
	}
	return p, nil
}

// Get answers one project by its key or a key it had.
func (s *Service) Get(ctx context.Context, reader uuid.UUID, key string) (*Project, error) {
	var out *Project
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := Find(ctx, tx, reader, key)
		out = p
		return err
	})
	return out, err
}

// CheckKey says whether a key may be used, or suggests a free one for a name.
func (s *Service) CheckKey(ctx context.Context, key, name string) (*KeyCheck, error) {
	out := &KeyCheck{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if key = NormalizeKey(key); key != "" {
			out.Key = key
			out.Valid = ValidKey(key)
			if !out.Valid {
				return nil
			}
			taken, err := keyTaken(ctx, tx, key)
			out.Available = !taken
			return err
		}
		base := SuggestKey(name)
		if base == "" {
			return nil
		}
		for n := 1; n <= maxKeySuggestions; n++ {
			candidate := numbered(base, n)
			taken, err := keyTaken(ctx, tx, candidate)
			if err != nil {
				return err
			}
			if !taken {
				out.Suggestion = candidate
				return nil
			}
		}
		return nil
	})
	return out, err
}

func keyTaken(ctx context.Context, tx db.DBTX, key string) (bool, error) {
	var taken bool
	err := tx.QueryRow(ctx, `SELECT project_key_taken($1)`, key).Scan(&taken)
	return taken, err
}

// checkKey refuses a key that cannot be one, with the sentence the form shows.
func checkKey(key string) error {
	switch {
	case key == "":
		return &FieldError{"key", "A project needs a key: two to ten letters or digits, such as CHK."}
	case !ValidKey(key):
		return &FieldError{"key", "A key is two to ten capital letters or digits and starts with a letter, such as CHK or SYS2."}
	}
	return nil
}

func keyTakenError(key string) error {
	return &FieldError{"key", fmt.Sprintf("The key %s is taken by another project. Choose another.", key)}
}

// isKeyTaken recognizes the database refusing a key another project has or had.
func isKeyTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		(pgErr.ConstraintName == "project_key_taken" || pgErr.ConstraintName == "project_org_key_idx")
}

func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", &FieldError{"name", "A project needs a name."}
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", &FieldError{"name", fmt.Sprintf("Keep the name to %d characters.", MaxNameLength)}
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

// Create makes a project; the organization's owners and administrators may.
func (s *Service) Create(ctx context.Context, reader uuid.UUID, in CreateInput) (*Project, db.LSN, error) {
	key := NormalizeKey(in.Key)
	if err := checkKey(key); err != nil {
		return nil, 0, err
	}
	name, err := checkName(in.Name)
	if err != nil {
		return nil, 0, err
	}
	description, err := checkDescription(in.Description)
	if err != nil {
		return nil, 0, err
	}
	blueprint, ok := BlueprintOf(in.Template)
	if in.Template != "" && (!ok || s.seed == nil) {
		return nil, 0, &FieldError{"template", fmt.Sprintf(
			"There is no project template called %s. Pick one from the list, or leave it out for a blank project.", in.Template)}
	}
	settings, err := json.Marshal(Settings{})
	if err != nil {
		return nil, 0, err
	}
	var out *Project
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var admin bool
		if err := tx.QueryRow(ctx, `SELECT current_actor_administers()`).Scan(&admin); err != nil {
			return err
		}
		if !admin {
			return ErrNotAdmin
		}
		// Without RETURNING: the policy that would let it be read back cannot see the row its own statement adds.
		_, err := tx.Exec(ctx, `
			INSERT INTO project (org_id, key, name, description, settings, template, created_by)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6)`, key, name, description, settings, in.Template, reader)
		if isKeyTaken(err) {
			return keyTakenError(key)
		}
		if err != nil {
			return err
		}
		if out, err = Find(ctx, tx, reader, key); err != nil {
			return err
		}
		if in.Template != "" {
			if err := s.seed(ctx, tx, reader, out.ID, blueprint); err != nil {
				return fmt.Errorf("start the project from %s: %w", in.Template, err)
			}
			if out, err = Find(ctx, tx, reader, key); err != nil {
				return err
			}
		}
		return s.note(ctx, tx, reader, audit.ActionProjectCreated, out, map[string]any{"key": key, "name": name, "template": in.Template})
	})
	return out, lsn, err
}

// note records an act on a project in the audit log.
func (s *Service) note(ctx context.Context, tx db.DBTX, actor uuid.UUID, action string, p *Project, data map[string]any) error {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return err
	}
	return audit.Write(ctx, tx, org.ID, audit.Entry{Action: action, TargetType: "project", TargetID: &p.ID, Actor: actor, Data: data})
}

// administered reads a project for a change only its administrators make; an archived one refuses all but
// unarchiving and deleting, which say so with keepArchived.
func administered(ctx context.Context, tx db.DBTX, reader uuid.UUID, key string, keepArchived bool) (*Project, error) {
	p, err := Find(ctx, tx, reader, key)
	if err != nil {
		return nil, err
	}
	if !p.Can.Administer {
		return nil, ErrForbidden
	}
	if p.ArchivedAt != nil && !keepArchived {
		return nil, ErrArchived
	}
	return p, nil
}

// Update renames or describes a project, or changes its settings.
func (s *Service) Update(ctx context.Context, reader uuid.UUID, key string, in UpdateInput) (*Project, db.LSN, error) {
	var out *Project
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := administered(ctx, tx, reader, key, false)
		if err != nil {
			return err
		}
		name, description, settings := p.Name, p.Description, p.Settings
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
		if in.Settings != nil {
			if in.Settings.DefaultTypeID != nil {
				var usable bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM requirement_type WHERE project_id = $1 AND id = $2 AND NOT retired)`,
					p.ID, *in.Settings.DefaultTypeID).Scan(&usable); err != nil {
					return err
				}
				if !usable {
					return &FieldError{"settings.defaultTypeId", "That type is not one of this project's, or it is retired. Pick one from its types."}
				}
				settings.DefaultTypeID = in.Settings.DefaultTypeID
			}
			if in.Settings.RequireRevisionComment != nil {
				settings.RequireRevisionComment = *in.Settings.RequireRevisionComment
			}
		}
		raw, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE project SET name = $2, description = $3, settings = $4 WHERE id = $1`,
			p.ID, name, description, raw); err != nil {
			return err
		}
		out, err = Find(ctx, tx, reader, p.Key)
		return err
	})
	return out, lsn, err
}

// ChangeKey gives a project a new key; the database keeps the old one as an alias.
func (s *Service) ChangeKey(ctx context.Context, reader uuid.UUID, key, newKey string) (*Project, db.LSN, error) {
	next := NormalizeKey(newKey)
	if err := checkKey(next); err != nil {
		return nil, 0, err
	}
	var out *Project
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := administered(ctx, tx, reader, key, false)
		if err != nil {
			return err
		}
		if p.Key == next {
			out = p
			return nil
		}
		_, err = tx.Exec(ctx, `UPDATE project SET key = $2 WHERE id = $1`, p.ID, next)
		if isKeyTaken(err) {
			return keyTakenError(next)
		}
		if err != nil {
			return err
		}
		if out, err = Find(ctx, tx, reader, next); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicKeyChanged, map[string]any{
			"actorId": reader, "projectId": p.ID, "fromKey": p.Key, "toKey": next,
		}); err != nil {
			return err
		}
		return s.note(ctx, tx, reader, audit.ActionProjectKeyChanged, out, map[string]any{"from": p.Key, "to": next})
	})
	return out, lsn, err
}

// SetArchived archives or unarchives a project; asking for what it already is changes nothing.
func (s *Service) SetArchived(ctx context.Context, reader uuid.UUID, key string, archived bool) (*Project, db.LSN, error) {
	var out *Project
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := administered(ctx, tx, reader, key, true)
		if err != nil {
			return err
		}
		if (p.ArchivedAt != nil) == archived {
			out = p
			return nil
		}
		action := audit.ActionProjectUnarchived
		if archived {
			action = audit.ActionProjectArchived
			_, err = tx.Exec(ctx, `UPDATE project SET archived_at = now(), archived_by = $2 WHERE id = $1`, p.ID, reader)
		} else {
			_, err = tx.Exec(ctx, `UPDATE project SET archived_at = NULL, archived_by = NULL WHERE id = $1`, p.ID)
		}
		if err != nil {
			return err
		}
		if out, err = Find(ctx, tx, reader, p.Key); err != nil {
			return err
		}
		return s.note(ctx, tx, reader, action, out, map[string]any{"key": p.Key})
	})
	return out, lsn, err
}

// Delete removes an archived project and everything in it for good.
func (s *Service) Delete(ctx context.Context, reader uuid.UUID, key string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := administered(ctx, tx, reader, key, true)
		if err != nil {
			return err
		}
		if p.ArchivedAt == nil {
			return ErrNotArchived
		}
		if _, err := tx.Exec(ctx, `DELETE FROM project WHERE id = $1`, p.ID); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicDeleted, map[string]any{"actorId": reader, "projectId": p.ID}); err != nil {
			return err
		}
		return s.note(ctx, tx, reader, audit.ActionProjectDeleted, p, map[string]any{"key": p.Key, "name": p.Name})
	})
}
