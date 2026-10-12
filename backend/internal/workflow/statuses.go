package workflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
)

// MaxDescriptionLength bounds a status's and a workflow's description.
const MaxDescriptionLength = 500

var (
	// ErrForbidden is somebody who may view a project shaping its statuses and workflows, its administrators' to shape.
	ErrForbidden = errors.New("only the project's administrators change its statuses and workflows")
)

// InUseError refuses deleting what is still used, saying by how many.
type InUseError struct {
	What  string
	Count int
}

func (e *InUseError) Error() string { return fmt.Sprintf("the %s is used %d times", e.What, e.Count) }

// Service keeps a project's statuses and workflows.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// shaping reads a project for a change its administrators make, while it is not archived.
func shaping(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectKey string) (*project.Project, error) {
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

func taken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Statuses answers a project's statuses in order.
func (s *Service) Statuses(ctx context.Context, reader uuid.UUID, projectKey string) ([]Status, error) {
	out := []Status{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		statuses, err := statusesOf(ctx, tx, p.ID)
		if statuses != nil {
			out = statuses
		}
		return err
	})
	return out, err
}

func applyStatus(st *Status, in StatusInput) error {
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return &InvalidError{"name", "Give the status a name."}
		}
		if utf8.RuneCountInString(name) > MaxNameLength {
			return &InvalidError{"name", fmt.Sprintf("Keep the name to %d characters.", MaxNameLength)}
		}
		st.Name = name
	}
	if in.Category != nil {
		if !slices.Contains(Categories, *in.Category) {
			return &InvalidError{"category", "Pick draft, review, approved, done or closed."}
		}
		st.Category = *in.Category
	}
	if in.Description != nil {
		description := strings.TrimSpace(*in.Description)
		if utf8.RuneCountInString(description) > MaxDescriptionLength {
			return &InvalidError{"description", fmt.Sprintf("Keep the description to %d characters.", MaxDescriptionLength)}
		}
		st.Description = description
	}
	if in.Position != nil {
		st.Position = max(*in.Position, 0)
	}
	return nil
}

func statusByID(ctx context.Context, tx db.DBTX, projectID, id uuid.UUID) (*Status, error) {
	rows, err := tx.Query(ctx, selectStatus+` WHERE s.project_id = $1 AND s.id = $2`, projectID, id)
	if err != nil {
		return nil, err
	}
	found, err := pgxOne(rows)
	if err != nil {
		return nil, err
	}
	return found, nil
}

// CreateStatus makes a status of the project, which a workflow then takes in.
func (s *Service) CreateStatus(ctx context.Context, actor uuid.UUID, projectKey string, in StatusInput) (*Status, db.LSN, error) {
	st := Status{Category: CategoryDraft}
	if in.Name == nil {
		return nil, 0, &InvalidError{"name", "Give the status a name."}
	}
	if err := applyStatus(&st, in); err != nil {
		return nil, 0, err
	}
	var out *Status
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if in.Position == nil {
			if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM req_status WHERE project_id = $1`, p.ID).Scan(&st.Position); err != nil {
				return err
			}
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `INSERT INTO req_status (org_id, project_id, name, category, description, position)
			VALUES (current_org_id(), $1, $2, $3, $4, $5) RETURNING id`, p.ID, st.Name, st.Category, st.Description, st.Position).Scan(&id)
		if taken(err) {
			return &InvalidError{"name", "Another status of this project is called that. Pick another name."}
		}
		if err != nil {
			return err
		}
		out, err = statusByID(ctx, tx, p.ID, id)
		return err
	})
	return out, lsn, err
}

// UpdateStatus renames a status, or changes its category, description or place in the list.
func (s *Service) UpdateStatus(ctx context.Context, actor uuid.UUID, projectKey string, id uuid.UUID, in StatusInput) (*Status, db.LSN, error) {
	var out *Status
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		st, err := statusByID(ctx, tx, p.ID, id)
		if err != nil {
			return err
		}
		if err := applyStatus(st, in); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE req_status SET name = $2, category = $3, description = $4, position = $5 WHERE id = $1`,
			id, st.Name, st.Category, st.Description, st.Position)
		if taken(err) {
			return &InvalidError{"name", "Another status of this project is called that. Pick another name."}
		}
		if err != nil {
			return err
		}
		out, err = statusByID(ctx, tx, p.ID, id)
		return err
	})
	return out, lsn, err
}

// DeleteStatus removes a status no workflow and no requirement uses.
func (s *Service) DeleteStatus(ctx context.Context, actor uuid.UUID, projectKey string, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if _, err := statusByID(ctx, tx, p.ID, id); err != nil {
			return err
		}
		var used int
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM workflow_step WHERE status_id = $1 OR on_revision_status_id = $1)::int
			+ (SELECT count(*) FROM requirement WHERE status_id = $1)::int`, id).Scan(&used); err != nil {
			return err
		}
		if used > 0 {
			return &InUseError{What: "status", Count: used}
		}
		_, err = tx.Exec(ctx, `DELETE FROM req_status WHERE id = $1`, id)
		return err
	})
}
