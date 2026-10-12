package attribute

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
)

const selectType = `
SELECT t.id, t.name, t.description, t.icon, t.colour, t.position, t.retired, t.created_at, t.updated_at,
       (SELECT count(*)::int FROM requirement r WHERE r.org_id = t.org_id AND r.type_id = t.id AND r.trashed_at IS NULL)
FROM requirement_type t`

func scanType(row pgx.CollectableRow) (Type, error) {
	var t Type
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.Icon, &t.Colour, &t.Position, &t.Retired, &t.CreatedAt, &t.UpdatedAt, &t.Requirements)
	return t, err
}

func typesOf(ctx context.Context, tx db.DBTX, projectID uuid.UUID) ([]Type, error) {
	rows, err := tx.Query(ctx, selectType+` WHERE t.project_id = $1 ORDER BY t.position, lower(t.name)`, projectID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanType)
}

func typeByID(ctx context.Context, tx db.DBTX, projectID, id uuid.UUID) (*Type, error) {
	rows, err := tx.Query(ctx, selectType+` WHERE t.project_id = $1 AND t.id = $2`, projectID, id)
	if err != nil {
		return nil, err
	}
	t, err := pgx.CollectExactlyOneRow(rows, scanType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, err
}

// Types answers a project's requirement types in order, retired ones included.
func (s *Service) Types(ctx context.Context, reader uuid.UUID, projectKey string) ([]Type, error) {
	out := []Type{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		types, err := typesOf(ctx, tx, p.ID)
		if types != nil {
			out = types
		}
		return err
	})
	return out, err
}

// applyType reads a type's fields from in onto t, checking each.
func applyType(t *Type, in TypeInput) error {
	var err error
	if in.Name != nil {
		if t.Name, err = checkName(*in.Name); err != nil {
			return err
		}
	}
	if in.Description != nil {
		if t.Description, err = checkDescription(*in.Description); err != nil {
			return err
		}
	}
	if in.Icon != nil {
		if !iconRe.MatchString(*in.Icon) {
			return &FieldError{"icon", "Pick one of the icons offered."}
		}
		t.Icon = *in.Icon
	}
	if in.Colour != nil {
		colour := strings.ToLower(strings.TrimSpace(*in.Colour))
		if !colourRe.MatchString(colour) {
			return &FieldError{"colour", "Give the colour as #rrggbb, such as #4f6bed."}
		}
		t.Colour = colour
	}
	if t.Icon == "" {
		return &FieldError{"icon", "Pick one of the icons offered."}
	}
	if t.Colour == "" {
		return &FieldError{"colour", "Give the colour as #rrggbb, such as #4f6bed."}
	}
	return nil
}

// CreateType makes a requirement type, last in the project's order unless a position says otherwise.
func (s *Service) CreateType(ctx context.Context, actor uuid.UUID, projectKey string, in TypeInput) (*Type, db.LSN, error) {
	var t Type
	if in.Name == nil {
		return nil, 0, &FieldError{"name", "Give it a name."}
	}
	if err := applyType(&t, in); err != nil {
		return nil, 0, err
	}
	var out *Type
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM requirement_type WHERE project_id = $1`, p.ID).Scan(&count); err != nil {
			return err
		}
		if count >= MaxTypes {
			return &FieldError{"name", fmt.Sprintf("A project has at most %d types. Retire or delete one first.", MaxTypes)}
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO requirement_type (org_id, project_id, name, description, icon, colour, position)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6) RETURNING id`, p.ID, t.Name, t.Description, t.Icon, t.Colour, count).Scan(&id)
		if nameTaken(err) {
			return &FieldError{"name", "Another type of this project is called that. Pick another name."}
		}
		if err != nil {
			return err
		}
		if in.Position != nil {
			if err := reorder(ctx, tx, "requirement_type", p.ID, id, *in.Position); err != nil {
				return err
			}
		}
		out, err = typeByID(ctx, tx, p.ID, id)
		return err
	})
	return out, lsn, err
}

// UpdateType changes, moves or retires a type; the project's default type stays in use.
func (s *Service) UpdateType(ctx context.Context, actor uuid.UUID, projectKey string, id uuid.UUID, in TypeInput) (*Type, db.LSN, error) {
	var out *Type
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		t, err := typeByID(ctx, tx, p.ID, id)
		if err != nil {
			return err
		}
		if err := applyType(t, in); err != nil {
			return err
		}
		if in.Retired != nil {
			if *in.Retired && p.Settings.DefaultTypeID != nil && *p.Settings.DefaultTypeID == id {
				return &FieldError{"retired", "This is the project's default type. Make another type the default first."}
			}
			t.Retired = *in.Retired
		}
		_, err = tx.Exec(ctx, `UPDATE requirement_type SET name = $2, description = $3, icon = $4, colour = $5, retired = $6 WHERE id = $1`,
			id, t.Name, t.Description, t.Icon, t.Colour, t.Retired)
		if nameTaken(err) {
			return &FieldError{"name", "Another type of this project is called that. Pick another name."}
		}
		if err != nil {
			return err
		}
		if in.Position != nil {
			if err := reorder(ctx, tx, "requirement_type", p.ID, id, *in.Position); err != nil {
				return err
			}
		}
		out, err = typeByID(ctx, tx, p.ID, id)
		return err
	})
	return out, lsn, err
}

// DeleteType removes a type no requirement and no revision ever had.
func (s *Service) DeleteType(ctx context.Context, actor uuid.UUID, projectKey string, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if _, err := typeByID(ctx, tx, p.ID, id); err != nil {
			return err
		}
		if p.Settings.DefaultTypeID != nil && *p.Settings.DefaultTypeID == id {
			return ErrDefaultType
		}
		// Counted as their owner, since a trashed requirement or a viewer's blind spot still keeps the type.
		var used int
		if err := tx.QueryRow(ctx, `SELECT type_usage($1)`, id).Scan(&used); err != nil {
			return err
		}
		if used > 0 {
			return &InUseError{What: "type", Count: used}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM requirement_type WHERE id = $1`, id); err != nil {
			return err
		}
		return reorder(ctx, tx, "requirement_type", p.ID, uuid.Nil, 0)
	})
}
