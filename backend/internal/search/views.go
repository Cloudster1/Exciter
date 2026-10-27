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
	// ErrSharedChangeForbidden is a change to a shared view by somebody who no longer edits its project, which the
	// database refuses too: a shared view is everybody's, kept so only by who edits there.
	ErrSharedChangeForbidden = errors.New("only the project's editors change a shared view")
	// ErrUnshareForbidden is somebody but its owner taking a view back, which would leave it to nobody they can see.
	ErrUnshareForbidden = errors.New("only a view's owner stops sharing it")
)

// selectViews reads views with whether $1 may change each.
const selectViews = `
SELECT v.id, p.key, v.name, v.owner_id, COALESCE(u.name, ''), v.shared, v.kind, v.filter, v.columns, v.settings, v.sort, v.created_at, v.updated_at,
       v.owner_id = $1 OR (v.shared AND perm_project_holds(v.project_id, $1, 'administer'))
FROM saved_view v
JOIN project p ON p.id = v.project_id
LEFT JOIN app_user u ON u.id = v.owner_id`

func scanView(row pgx.Row) (*View, error) {
	var (
		v                      View
		filter, settings, sort []byte
	)
	err := row.Scan(&v.ID, &v.ProjectKey, &v.Name, &v.OwnerID, &v.OwnerName, &v.Shared, &v.Kind, &filter, &v.Columns, &settings, &sort,
		&v.CreatedAt, &v.UpdatedAt, &v.CanEdit)
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
	if err := json.Unmarshal(settings, &v.Settings); err != nil {
		return nil, fmt.Errorf("read view %s: %w", v.ID, err)
	}
	if v.Columns == nil {
		v.Columns = []string{}
	}
	v.Dropped = []string{}
	v.Filter = v.Filter.filled()
	return &v, nil
}

// Views answers the reader's own views of one kind of a project's, then those shared with them, each by name, with
// what each names that the project no longer has left out.
func (s *Service) Views(ctx context.Context, reader uuid.UUID, projectKey string, kind ViewKind) ([]View, error) {
	if kind == "" {
		kind = ViewGrid
	}
	if !slices.Contains(ViewKinds, kind) {
		return nil, &FieldError{"kind", "A view is of the grid, the matrix or the chains."}
	}
	out := []View{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectViews+` WHERE v.project_id = $2 AND v.kind = $3
			ORDER BY v.owner_id <> $1, lower(v.name), v.id`, reader, p.ID, kind)
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
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		for i := range out {
			if err := dropLost(ctx, tx, reader, p, &out[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// checkView holds what a view says to the grid's rules, with the project's attributes, and its settings to its kind's.
func checkView(v View, defs []attribute.Def) (string, Filter, error) {
	name, f, columns, sort := v.Name, v.Filter, v.Columns, v.Sort
	if err := checkSettings(v.Kind, columns, v.Settings); err != nil {
		return "", f, err
	}
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
		v := View{Columns: []string{}, Kind: ViewGrid}
		if in.Kind != nil {
			v.Kind = *in.Kind
		}
		apply(&v, in)
		if err := shareable(p, v); err != nil {
			return err
		}
		defs, err := attribute.Defs(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if v.Name, v.Filter, err = checkView(v, defs); err != nil {
			return err
		}
		// A view keeps a query that runs now; one that later names what the project no longer has says so then.
		if _, err := compileQuery(ctx, tx, actor, p, defs, v.Filter.Query); err != nil {
			return err
		}
		filter, settings, sort, err := encodeView(v)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO saved_view (project_id, owner_id, name, shared, kind, filter, columns, settings, sort)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
			p.ID, actor, v.Name, v.Shared, v.Kind, filter, v.Columns, settings, sort).Scan(&id); err != nil {
			return fmt.Errorf("save the view: %w", nameTaken(err))
		}
		if out, err = scanView(tx.QueryRow(ctx, selectViews+` WHERE v.id = $2`, actor, id)); err != nil {
			return err
		}
		return dropLost(ctx, tx, actor, p, out)
	})
	return out, lsn, err
}

// shareable refuses a view that would be shared in a project the actor does not edit: an archived one says so, as
// no right of the actor's would let them change it.
func shareable(p *project.Project, v View) error {
	switch {
	case !v.Shared:
		return nil
	case p.ArchivedAt != nil:
		return project.ErrArchived
	case !p.Can.Edit:
		return ErrShareForbidden
	}
	return nil
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
	if in.Settings != nil {
		v.Settings = *in.Settings
	}
}

func encodeView(v View) (filter, settings, sort []byte, err error) {
	if filter, err = json.Marshal(v.Filter); err != nil {
		return nil, nil, nil, err
	}
	if settings, err = json.Marshal(v.Settings); err != nil {
		return nil, nil, nil, err
	}
	sort, err = json.Marshal(v.Sort)
	return filter, settings, sort, err
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
		if in.Kind != nil && *in.Kind != v.Kind {
			return &FieldError{"kind", "A view keeps the kind it was saved as. Save a new view instead."}
		}
		apply(v, in)
		if err := shareable(p, *v); err != nil {
			if wasShared && errors.Is(err, ErrShareForbidden) {
				return ErrSharedChangeForbidden
			}
			return err
		}
		if wasShared && !v.Shared && v.OwnerID != actor {
			return ErrUnshareForbidden
		}
		defs, err := attribute.Defs(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if v.Name, v.Filter, err = checkView(*v, defs); err != nil {
			return err
		}
		// A view keeps a query that runs now; one that later names what the project no longer has says so then.
		if _, err := compileQuery(ctx, tx, actor, p, defs, v.Filter.Query); err != nil {
			return err
		}
		filter, settings, sort, err := encodeView(*v)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE saved_view SET name = $2, shared = $3, filter = $4, columns = $5, settings = $6, sort = $7 WHERE id = $1`,
			id, v.Name, v.Shared, filter, v.Columns, settings, sort); err != nil {
			return fmt.Errorf("change the view: %w", nameTaken(err))
		}
		if out, err = scanView(tx.QueryRow(ctx, selectViews+` WHERE v.id = $2`, actor, id)); err != nil {
			return err
		}
		return dropLost(ctx, tx, actor, p, out)
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

// ErrViewPrivate is a widget reading a view its owner has not shared, which others of the dashboard could not read.
var ErrViewPrivate = errors.New("the view is not shared")

// SharedGridView reads a shared view of the project's grid as the reader sees it, for a dashboard's widget (#212).
func SharedGridView(ctx context.Context, tx db.DBTX, reader, projectID, id uuid.UUID) (*View, error) {
	v, err := scanView(tx.QueryRow(ctx, selectViews+` WHERE v.id = $2 AND v.project_id = $3 AND v.kind = 'grid'`, reader, id, projectID))
	if err != nil {
		return nil, err
	}
	if !v.Shared {
		return nil, ErrViewPrivate
	}
	return v, nil
}
