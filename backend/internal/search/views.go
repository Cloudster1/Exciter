package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
)

var (
	// ErrViewNotFound is a view that is not there, or not the reader's to see.
	ErrViewNotFound = errors.New("view not found")
	// ErrViewForbidden is a change to a view that is somebody else's, or shared and not the reader's to manage.
	ErrViewForbidden = errors.New("only a view's owner changes it, and a project administrator once it is shared")
	// ErrShareForbidden is sharing a view in a project the reader does not edit.
	ErrShareForbidden = errors.New("only the project's editors share views")
	// ErrUnshareForbidden is somebody but its owner taking a view back, which would leave it to nobody they can see.
	ErrUnshareForbidden = errors.New("only a view's owner stops sharing it")
)

// selectViews reads views with whether $1 may change each.
const selectViews = `
SELECT v.id, p.key, v.name, v.owner_id, COALESCE(u.name, ''), v.shared, v.filter, v.columns, v.sort, v.created_at, v.updated_at,
       v.owner_id = $1 OR (v.shared AND perm_project_holds(v.project_id, $1, 'administer'))
FROM saved_view v
JOIN project p ON p.id = v.project_id
LEFT JOIN app_user u ON u.id = v.owner_id`

func scanView(row pgx.Row) (*View, error) {
	var (
		v            View
		filter, sort []byte
	)
	err := row.Scan(&v.ID, &v.ProjectKey, &v.Name, &v.OwnerID, &v.OwnerName, &v.Shared, &filter, &v.Columns, &sort, &v.CreatedAt, &v.UpdatedAt, &v.CanEdit)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrViewNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(filter, &v.Filter); err != nil {
		return nil, fmt.Errorf("read view %s: %w", v.ID, err)
	}
	if err := json.Unmarshal(sort, &v.Sort); err != nil {
		return nil, fmt.Errorf("read view %s: %w", v.ID, err)
	}
	if v.Columns == nil {
		v.Columns = []string{}
	}
	v.Filter = v.Filter.filled()
	return &v, nil
}

// Views answers the reader's own views of a project's grid, then those shared with them, each by name.
func (s *Service) Views(ctx context.Context, reader uuid.UUID, projectKey string) ([]View, error) {
	out := []View{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectViews+` WHERE v.project_id = $2
			ORDER BY v.owner_id <> $1, lower(v.name), v.id`, reader, p.ID)
		if err != nil {
			return fmt.Errorf("list the views: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanView(rows)
			if err != nil {
				return err
			}
			out = append(out, *v)
		}
		return rows.Err()
	})
	return out, err
}

// checkView holds what a view says to the grid's rules, with the project's attributes.
func checkView(name string, f Filter, columns []string, sort Sort, defs []attribute.Def) (string, Filter, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", f, &FieldError{"name", "Give the view a name."}
	}
	if utf8.RuneCountInString(name) > MaxViewNameLength {
		return "", f, &FieldError{"name", fmt.Sprintf("Keep the name to %d characters.", MaxViewNameLength)}
	}
	checked, err := check(f, sort, defs)
	if err != nil {
		var field *FieldError
		if errors.As(err, &field) && field.Field != "sort" {
			return "", f, &FieldError{"filter", field.Message}
		}
		return "", f, err
	}
	if len(columns) > MaxColumns {
		return "", f, &FieldError{"columns", fmt.Sprintf("A view shows at most %d columns.", MaxColumns)}
	}
	for _, c := range columns {
		if !slices.Contains(SortColumns, c) && !slices.Contains(ShowColumns, c) && defOf(c, defs) == nil {
			return "", f, &FieldError{"columns", "Show the key, title, type, status, owner, priority, updated, labels, outline or the project's attributes."}
		}
	}
	return name, checked, nil
}

// nameTaken says a view already has that name for its owner in the project.
func nameTaken(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return &FieldError{"name", "You already have a view with that name here. Choose another name."}
	}
	return err
}

// CreateView saves a view of a project's grid as the reader's; sharing it is for the project's editors.
func (s *Service) CreateView(ctx context.Context, actor uuid.UUID, projectKey string, in ViewInput) (*View, db.LSN, error) {
	var out *View
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		v := View{Columns: []string{}}
		apply(&v, in)
		if v.Shared && !p.Can.Edit {
			return ErrShareForbidden
		}
		defs, err := attribute.Defs(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if v.Name, v.Filter, err = checkView(v.Name, v.Filter, v.Columns, v.Sort, defs); err != nil {
			return err
		}
		filter, sort, err := encodeView(v)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO saved_view (project_id, owner_id, name, shared, filter, columns, sort)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
			p.ID, actor, v.Name, v.Shared, filter, v.Columns, sort).Scan(&id); err != nil {
			return fmt.Errorf("save the view: %w", nameTaken(err))
		}
		out, err = scanView(tx.QueryRow(ctx, selectViews+` WHERE v.id = $2`, actor, id))
		return err
	})
	return out, lsn, err
}

// apply sets on v what in names.
func apply(v *View, in ViewInput) {
	if in.Name != nil {
		v.Name = *in.Name
	}
	if in.Shared != nil {
		v.Shared = *in.Shared
	}
	if in.Filter != nil {
		v.Filter = *in.Filter
	}
	if in.Columns != nil {
		v.Columns = *in.Columns
	}
	if in.Sort != nil {
		v.Sort = *in.Sort
	}
}

func encodeView(v View) ([]byte, []byte, error) {
	filter, err := json.Marshal(v.Filter)
	if err != nil {
		return nil, nil, err
	}
	sort, err := json.Marshal(v.Sort)
	return filter, sort, err
}

// UpdateView changes what in names of a view: its owner's to do, or a project administrator's once shared.
func (s *Service) UpdateView(ctx context.Context, actor, id uuid.UUID, in ViewInput) (*View, db.LSN, error) {
	var out *View
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		v, p, err := managed(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		wasShared := v.Shared
		apply(v, in)
		if v.Shared && !wasShared && !p.Can.Edit {
			return ErrShareForbidden
		}
		if wasShared && !v.Shared && v.OwnerID != actor {
			return ErrUnshareForbidden
		}
		defs, err := attribute.Defs(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if v.Name, v.Filter, err = checkView(v.Name, v.Filter, v.Columns, v.Sort, defs); err != nil {
			return err
		}
		filter, sort, err := encodeView(*v)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE saved_view SET name = $2, shared = $3, filter = $4, columns = $5, sort = $6 WHERE id = $1`,
			id, v.Name, v.Shared, filter, v.Columns, sort); err != nil {
			return fmt.Errorf("change the view: %w", nameTaken(err))
		}
		out, err = scanView(tx.QueryRow(ctx, selectViews+` WHERE v.id = $2`, actor, id))
		return err
	})
	return out, lsn, err
}

// DeleteView deletes a view: its owner's to do, or a project administrator's once shared.
func (s *Service) DeleteView(ctx context.Context, actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, _, err := managed(ctx, tx, actor, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM saved_view WHERE id = $1`, id)
		return err
	})
}

// managed reads a view the actor may change, with its project.
func managed(ctx context.Context, tx db.DBTX, actor, id uuid.UUID) (*View, *project.Project, error) {
	v, err := scanView(tx.QueryRow(ctx, selectViews+` WHERE v.id = $2`, actor, id))
	if err != nil {
		return nil, nil, err
	}
	if !v.CanEdit {
		return nil, nil, ErrViewForbidden
	}
	p, err := project.Find(ctx, tx, actor, v.ProjectKey)
	if err != nil {
		return nil, nil, err
	}
	return v, p, nil
}
