package testexec

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

const selectPlan = `
SELECT pl.id, p.key, pl.project_id, pl.title, pl.description, pl.scope, pl.query, pl.created_by, COALESCE(u.name, ''),
       pl.created_at, pl.updated_at, perm_project_holds(pl.project_id, $1::uuid, 'edit') AND project_open(pl.project_id)
FROM test_plan pl
JOIN project p ON p.id = pl.project_id
LEFT JOIN app_user u ON u.id = pl.created_by`

type planRow struct {
	Plan
	projectID uuid.UUID
}

func scanPlan(row pgx.Row) (*planRow, error) {
	var (
		p  planRow
		by *uuid.UUID
	)
	err := row.Scan(&p.ID, &p.ProjectKey, &p.projectID, &p.Title, &p.Description, &p.Scope, &p.Query, &by, &p.CreatedBy.Name,
		&p.CreatedAt, &p.UpdatedAt, &p.Can.Edit)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if by != nil {
		p.CreatedBy.ID = *by
	}
	p.Cases = []CaseRef{}
	return &p, err
}

// planCases reads a plan's cases: those picked in order, or what its query finds now as the reader sees it.
func planCases(ctx context.Context, tx db.DBTX, reader uuid.UUID, p *planRow) ([]CaseRef, error) {
	if p.Scope == ScopeQuery {
		proj, err := project.Find(ctx, tx, reader, p.ProjectKey)
		if err != nil {
			return nil, err
		}
		kept, err := search.Keep(ctx, tx, reader, proj, search.Filter{Query: p.Query}, search.Sort{})
		if err != nil {
			return nil, err
		}
		var ids []uuid.UUID
		for _, k := range kept {
			ids = append(ids, k.Ref.ID)
		}
		rows, err := tx.Query(ctx, `
			SELECT r.id, r.number, r.title, false FROM unnest($1::uuid[]) WITH ORDINALITY AS k (id, n)
			JOIN requirement r ON r.id = k.id JOIN requirement_type t ON t.id = r.type_id AND t.role = 'testCase'
			ORDER BY k.n`, ids)
		return cases(rows, p.ProjectKey, err)
	}
	rows, err := tx.Query(ctx, `
		SELECT r.id, r.number, r.title, r.trashed_at IS NOT NULL FROM test_plan_case c JOIN requirement r ON r.id = c.requirement_id
		WHERE c.plan_id = $1 ORDER BY c.position`, p.ID)
	return cases(rows, p.ProjectKey, err)
}

func cases(rows pgx.Rows, projectKey string, err error) ([]CaseRef, error) {
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CaseRef, error) {
		var (
			c      CaseRef
			number int
		)
		err := row.Scan(&c.ID, &number, &c.Title, &c.Trashed)
		c.Key = requirement.Key(projectKey, number)
		return c, err
	})
	if out == nil {
		out = []CaseRef{}
	}
	return out, err
}

// Plans answers a project's test plans by title, without their cases.
func (s *Service) Plans(ctx context.Context, reader uuid.UUID, projectKey string) ([]Plan, error) {
	out := []Plan{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectPlan+` WHERE pl.project_id = $2 ORDER BY lower(pl.title), pl.id`, reader, p.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			pl, err := scanPlan(rows)
			if err != nil {
				return err
			}
			out = append(out, pl.Plan)
		}
		return rows.Err()
	})
	return out, err
}

func readPlan(ctx context.Context, tx db.DBTX, reader, id uuid.UUID) (*planRow, error) {
	p, err := scanPlan(tx.QueryRow(ctx, selectPlan+` WHERE pl.id = $2`, reader, id))
	if err != nil {
		return nil, err
	}
	p.Cases, err = planCases(ctx, tx, reader, p)
	return p, err
}

// Plan answers a plan and its cases.
func (s *Service) Plan(ctx context.Context, reader, id uuid.UUID) (*Plan, error) {
	var out *Plan
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := readPlan(ctx, tx, reader, id)
		if p != nil {
			out = &p.Plan
		}
		return err
	})
	return out, err
}

func checkPlan(title, description string) (string, string, error) {
	title, err := checkText("title", title, MaxTitleLength, true, fmt.Sprintf("Name the plan in at most %d characters, such as Release 2.0 regression.", MaxTitleLength))
	if err != nil {
		return "", "", err
	}
	description, err = checkText("description", description, MaxDescriptionLength, false, fmt.Sprintf("Keep the description to %d characters.", MaxDescriptionLength))
	return title, description, err
}

// checkQuery compiles a plan's query as the reader would run it, so a query that does not compile is refused now.
func checkQuery(ctx context.Context, tx db.DBTX, reader uuid.UUID, p *project.Project, query string) error {
	if query == "" {
		return &FieldError{"query", "Write the query that finds the plan's test cases, such as type = \"Test case\"."}
	}
	_, err := search.Keep(ctx, tx, reader, p, search.Filter{Query: query}, search.Sort{})
	return err
}

// setCases replaces a plan's picked cases, in order.
func setCases(ctx context.Context, tx db.DBTX, plan uuid.UUID, ids []uuid.UUID) error {
	if len(ids) > MaxPlanCases {
		return &FieldError{"cases", fmt.Sprintf("A plan holds at most %d test cases. Split it into more plans.", MaxPlanCases)}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM test_plan_case WHERE plan_id = $1`, plan); err != nil {
		return refusal(err)
	}
	for i, id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO test_plan_case (plan_id, requirement_id, position) VALUES ($1, $2, $3)`, plan, id, i); err != nil {
			return refusal(err)
		}
	}
	return nil
}

// CreatePlan makes a test plan of picked cases or of a query, for whoever edits the project.
func (s *Service) CreatePlan(ctx context.Context, actor uuid.UUID, projectKey string, in PlanInput) (*Plan, db.LSN, error) {
	title, description, err := checkPlan(in.Title, in.Description)
	if err != nil {
		return nil, 0, err
	}
	if in.Scope != ScopeCases && in.Scope != ScopeQuery {
		return nil, 0, &FieldError{"scope", "A plan holds cases picked in order, or those a query finds."}
	}
	var out *Plan
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := editing(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		query := ""
		if in.Scope == ScopeQuery {
			query = in.Query
			if err := checkQuery(ctx, tx, actor, p, query); err != nil {
				return err
			}
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO test_plan (project_id, title, description, scope, query, created_by) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			p.ID, title, description, in.Scope, query, actor).Scan(&id); err != nil {
			return refusal(err)
		}
		if in.Scope == ScopeCases {
			ids, err := caseIDs(ctx, tx, p.ID, p.Key, in.Cases)
			if err != nil {
				return err
			}
			if err := setCases(ctx, tx, id, ids); err != nil {
				return err
			}
		}
		pl, err := readPlan(ctx, tx, actor, id)
		if pl != nil {
			out = &pl.Plan
		}
		return err
	})
	return out, lsn, err
}

// changingPlan reads a plan the caller may change.
func changingPlan(ctx context.Context, tx db.DBTX, actor, id uuid.UUID) (*planRow, error) {
	p, err := scanPlan(tx.QueryRow(ctx, selectPlan+` WHERE pl.id = $2`, actor, id))
	if err != nil {
		return nil, err
	}
	if !p.Can.Edit {
		return nil, ErrForbidden
	}
	return p, nil
}

// UpdatePlan renames a plan, describes it, or changes its query.
func (s *Service) UpdatePlan(ctx context.Context, actor, id uuid.UUID, in PlanPatch) (*Plan, db.LSN, error) {
	var out *Plan
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := changingPlan(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		title, description, query := p.Title, p.Description, p.Query
		if in.Title != nil {
			title = *in.Title
		}
		if in.Description != nil {
			description = *in.Description
		}
		if title, description, err = checkPlan(title, description); err != nil {
			return err
		}
		if in.Query != nil {
			if p.Scope != ScopeQuery {
				return &FieldError{"query", "This plan holds cases picked by hand. Set its cases instead."}
			}
			proj, err := project.Find(ctx, tx, actor, p.ProjectKey)
			if err != nil {
				return err
			}
			if err := checkQuery(ctx, tx, actor, proj, *in.Query); err != nil {
				return err
			}
			query = *in.Query
		}
		if _, err := tx.Exec(ctx, `UPDATE test_plan SET title = $2, description = $3, query = $4, updated_at = now() WHERE id = $1`,
			id, title, description, query); err != nil {
			return refusal(err)
		}
		pl, err := readPlan(ctx, tx, actor, id)
		if pl != nil {
			out = &pl.Plan
		}
		return err
	})
	return out, lsn, err
}

// SetPlanCases sets a picked plan's cases, in order.
func (s *Service) SetPlanCases(ctx context.Context, actor, id uuid.UUID, in CasesInput) (*Plan, db.LSN, error) {
	var out *Plan
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := changingPlan(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if p.Scope != ScopeCases {
			return &FieldError{"cases", "This plan finds its cases by its query. Change the query instead."}
		}
		ids, err := caseIDs(ctx, tx, p.projectID, p.ProjectKey, in.Cases)
		if err != nil {
			return err
		}
		if err := setCases(ctx, tx, id, ids); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE test_plan SET updated_at = now() WHERE id = $1`, id); err != nil {
			return refusal(err)
		}
		pl, err := readPlan(ctx, tx, actor, id)
		if pl != nil {
			out = &pl.Plan
		}
		return err
	})
	return out, lsn, err
}

// DeletePlan deletes a plan; the executions it started keep its title.
func (s *Service) DeletePlan(ctx context.Context, actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := changingPlan(ctx, tx, actor, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM test_plan WHERE id = $1`, id)
		return refusal(err)
	})
}
