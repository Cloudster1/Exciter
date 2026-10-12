package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// ErrNotFound is a status or a workflow the project does not have.
var ErrNotFound = errors.New("status or workflow not found")

const selectStatus = `
SELECT s.id, s.name, s.category, s.description, s.position,
       (SELECT count(*)::int FROM requirement r WHERE r.org_id = s.org_id AND r.status_id = s.id AND r.trashed_at IS NULL)
FROM req_status s`

func scanStatus(row pgx.CollectableRow) (Status, error) {
	var s Status
	err := row.Scan(&s.ID, &s.Name, &s.Category, &s.Description, &s.Position, &s.Requirements)
	return s, err
}

// statusesOf reads a project's statuses in order.
func statusesOf(ctx context.Context, tx db.DBTX, projectID uuid.UUID) ([]Status, error) {
	rows, err := tx.Query(ctx, selectStatus+` WHERE s.project_id = $1 ORDER BY s.position, lower(s.name)`, projectID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanStatus)
}

// Load reads a workflow whole, in the caller's transaction: its steps with their statuses, its transitions with
// their rules, and who follows it.
func Load(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Workflow, error) {
	w := &Workflow{Steps: []Step{}, Transitions: []Transition{}, TypeIDs: []uuid.UUID{}}
	var projectDefault *string
	err := tx.QueryRow(ctx, `
		SELECT w.id, w.name, w.description, w.updated_at, p.settings ->> 'workflowId',
		       COALESCE((SELECT array_agg(t.id ORDER BY t.position) FROM requirement_type t WHERE t.workflow_id = w.id), '{}')
		FROM workflow w JOIN project p ON p.id = w.project_id WHERE w.id = $1`, id).Scan(&w.ID, &w.Name, &w.Description, &w.UpdatedAt, &projectDefault, &w.TypeIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	w.ProjectDefault = projectDefault != nil && *projectDefault == id.String()
	rows, err := tx.Query(ctx, `
		SELECT s.id, s.name, s.category, s.description, s.position,
		       (SELECT count(*)::int FROM requirement r WHERE r.org_id = s.org_id AND r.status_id = s.id AND r.trashed_at IS NULL),
		       st.is_initial, st.on_revision_status_id, st.layout_x, st.layout_y
		FROM workflow_step st JOIN req_status s ON s.id = st.status_id
		WHERE st.workflow_id = $1 ORDER BY s.position, lower(s.name)`, id)
	if err != nil {
		return nil, err
	}
	if w.Steps, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Step, error) {
		var (
			st   Step
			x, y *int
		)
		err := row.Scan(&st.Status.ID, &st.Status.Name, &st.Status.Category, &st.Status.Description, &st.Status.Position, &st.Status.Requirements,
			&st.IsInitial, &st.OnRevisionStatusID, &x, &y)
		if x != nil && y != nil {
			st.Layout = &Point{X: *x, Y: *y}
		}
		return st, err
	}); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `
		SELECT t.id, t.name, t.from_status_id, t.to_status_id, t.description, t.position
		FROM workflow_transition t WHERE t.workflow_id = $1 ORDER BY t.position, t.id`, id)
	if err != nil {
		return nil, err
	}
	if w.Transitions, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Transition, error) {
		t := Transition{Rules: []Rule{}}
		err := row.Scan(&t.ID, &t.Name, &t.FromStatusID, &t.ToStatusID, &t.Description, &t.Position)
		return t, err
	}); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `
		SELECT r.transition_id, r.id, r.kind, r.type, r.config, r.position
		FROM transition_rule r JOIN workflow_transition t ON t.id = r.transition_id WHERE t.workflow_id = $1 ORDER BY r.position, r.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			edge   uuid.UUID
			r      Rule
			config []byte
		)
		if err := rows.Scan(&edge, &r.ID, &r.Kind, &r.Type, &config, &r.Position); err != nil {
			return nil, err
		}
		r.Config = json.RawMessage(config)
		for i := range w.Transitions {
			if w.Transitions[i].ID == edge {
				w.Transitions[i].Rules = append(w.Transitions[i].Rules, r)
			}
		}
	}
	return w, rows.Err()
}

// For reads the workflow a requirement of a type follows in a project: the type's own, else the project's.
func For(ctx context.Context, tx db.DBTX, projectID uuid.UUID, typeID *uuid.UUID) (*Workflow, error) {
	var id *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT requirement_workflow($1, $2)`, typeID, projectID).Scan(&id); err != nil {
		return nil, fmt.Errorf("find the workflow: %w", err)
	}
	if id == nil {
		return nil, ErrNotFound
	}
	return Load(ctx, tx, *id)
}

// ProjectRoles reads the project roles a person holds, which a role condition asks for.
func ProjectRoles(ctx context.Context, tx db.DBTX, projectID, person uuid.UUID) ([]string, error) {
	var roles []string
	err := tx.QueryRow(ctx, `SELECT perm_project_roles($1, $2)`, projectID, person).Scan(&roles)
	return roles, err
}

// pgxOne reads the one status rows hold, ErrNotFound when they hold none.
func pgxOne(rows pgx.Rows) (*Status, error) {
	st, err := pgx.CollectExactlyOneRow(rows, scanStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}
