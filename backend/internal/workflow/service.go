package workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// ErrStatusInUse is a save that drops a status requirements under the workflow are in.
type ErrStatusInUse struct {
	Status string
	Count  int
}

func (e *ErrStatusInUse) Error() string {
	return fmt.Sprintf("%d requirements are in %s", e.Count, e.Status)
}

// remapReason is why a requirement's status changed when its workflow did.
const remapReason = "Its workflow changed."

// Workflows answers a project's workflows, each whole.
func (s *Service) Workflows(ctx context.Context, reader uuid.UUID, projectKey string) ([]Workflow, error) {
	out := []Workflow{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id FROM workflow WHERE project_id = $1 ORDER BY lower(name), id`, p.ID)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, id := range ids {
			w, err := Load(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, *w)
		}
		return nil
	})
	return out, err
}

// inProject reads a workflow of the project.
func inProject(ctx context.Context, tx db.DBTX, projectID, id uuid.UUID) (*Workflow, error) {
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workflow WHERE project_id = $1 AND id = $2)`, projectID, id).Scan(&ok); err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	return Load(ctx, tx, id)
}

// Workflow answers one workflow of a project.
func (s *Service) Workflow(ctx context.Context, reader uuid.UUID, projectKey string, id uuid.UUID) (*Workflow, error) {
	var out *Workflow
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		out, err = inProject(ctx, tx, p.ID, id)
		return err
	})
	return out, err
}

// RuleTypes answers the rules the designer offers.
func (s *Service) RuleTypes() []RuleType { return RuleTypes() }

func statusNames(ctx context.Context, tx db.DBTX, projectID uuid.UUID) (map[uuid.UUID]string, error) {
	statuses, err := statusesOf(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	names := make(map[uuid.UUID]string, len(statuses))
	for _, st := range statuses {
		names[st.ID] = st.Name
	}
	return names, nil
}

func record(ctx context.Context, tx db.DBTX, actor uuid.UUID, action string, target uuid.UUID, data map[string]any) error {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return err
	}
	return audit.Write(ctx, tx, org.ID, audit.Entry{Action: action, TargetType: "workflow", TargetID: &target, Actor: actor, Data: data})
}

// CreateWorkflow makes a workflow from the designer's graph; nobody follows it until it is assigned.
func (s *Service) CreateWorkflow(ctx context.Context, actor uuid.UUID, projectKey string, in GraphInput) (*Workflow, db.LSN, error) {
	var out *Workflow
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		names, err := statusNames(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if err := ValidateGraph(in, names); err != nil {
			return err
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `INSERT INTO workflow (org_id, project_id, name, description) VALUES (current_org_id(), $1, $2, $3) RETURNING id`,
			p.ID, strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)).Scan(&id)
		if taken(err) {
			return &InvalidError{"name", "Another workflow of this project is called that. Pick another name."}
		}
		if err != nil {
			return err
		}
		if err := writeGraph(ctx, tx, id, in); err != nil {
			return err
		}
		if err := record(ctx, tx, actor, audit.ActionWorkflowSaved, id, map[string]any{"name": in.Name, "project": p.Key}); err != nil {
			return err
		}
		out, err = Load(ctx, tx, id)
		return err
	})
	return out, lsn, err
}

// SaveWorkflow replaces a workflow's graph; a transition sent with its id keeps it, and dropping a status that
// requirements under the workflow are in is refused.
func (s *Service) SaveWorkflow(ctx context.Context, actor uuid.UUID, projectKey string, id uuid.UUID, in GraphInput) (*Workflow, db.LSN, error) {
	var out *Workflow
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		was, err := inProject(ctx, tx, p.ID, id)
		if err != nil {
			return err
		}
		names, err := statusNames(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if err := ValidateGraph(in, names); err != nil {
			return err
		}
		kept := map[uuid.UUID]bool{}
		for _, st := range in.Steps {
			kept[st.StatusID] = true
		}
		for _, st := range was.Steps {
			if kept[st.Status.ID] {
				continue
			}
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM requirement r WHERE r.project_id = $1 AND r.status_id = $2
				AND requirement_workflow(r.type_id, r.project_id) = $3`, p.ID, st.Status.ID, id).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return &ErrStatusInUse{Status: st.Status.Name, Count: count}
			}
		}
		_, err = tx.Exec(ctx, `UPDATE workflow SET name = $2, description = $3 WHERE id = $1`, id, strings.TrimSpace(in.Name), strings.TrimSpace(in.Description))
		if taken(err) {
			return &InvalidError{"name", "Another workflow of this project is called that. Pick another name."}
		}
		if err != nil {
			return err
		}
		if err := writeGraph(ctx, tx, id, in); err != nil {
			return err
		}
		if err := record(ctx, tx, actor, audit.ActionWorkflowSaved, id, map[string]any{"name": in.Name, "project": p.Key}); err != nil {
			return err
		}
		out, err = Load(ctx, tx, id)
		return err
	})
	return out, lsn, err
}

// writeGraph puts a checked graph in place: the transitions it does not name go, the steps it does not hold go
// after them, and every rule is written anew.
func writeGraph(ctx context.Context, tx db.DBTX, id uuid.UUID, in GraphInput) error {
	keep := []uuid.UUID{}
	for _, t := range in.Transitions {
		if t.ID != nil {
			keep = append(keep, *t.ID)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workflow_transition WHERE workflow_id = $1 AND NOT (id = ANY ($2))`, id, keep); err != nil {
		return err
	}
	statuses := make([]uuid.UUID, len(in.Steps))
	for i, st := range in.Steps {
		statuses[i] = st.StatusID
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workflow_step WHERE workflow_id = $1 AND NOT (status_id = ANY ($2))`, id, statuses); err != nil {
		return err
	}
	// The one initial step moves without two being initial for a moment.
	if _, err := tx.Exec(ctx, `UPDATE workflow_step SET is_initial = false WHERE workflow_id = $1`, id); err != nil {
		return err
	}
	for _, st := range in.Steps {
		var x, y *int
		if st.Layout != nil {
			x, y = &st.Layout.X, &st.Layout.Y
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO workflow_step (org_id, workflow_id, status_id, is_initial, on_revision_status_id, layout_x, layout_y)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6)
			ON CONFLICT (workflow_id, status_id) DO UPDATE SET is_initial = EXCLUDED.is_initial,
			    on_revision_status_id = EXCLUDED.on_revision_status_id, layout_x = EXCLUDED.layout_x, layout_y = EXCLUDED.layout_y`,
			id, st.StatusID, st.IsInitial, st.OnRevisionStatusID, x, y); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM transition_rule r USING workflow_transition t WHERE r.transition_id = t.id AND t.workflow_id = $1`, id); err != nil {
		return err
	}
	for i, t := range in.Transitions {
		edge := uuid.Nil
		name, description := strings.TrimSpace(t.Name), strings.TrimSpace(t.Description)
		var err error
		if t.ID != nil {
			tag, terr := tx.Exec(ctx, `UPDATE workflow_transition SET name = $3, description = $4, from_status_id = $5, to_status_id = $6, position = $7
				WHERE workflow_id = $1 AND id = $2`, id, *t.ID, name, description, t.FromStatusID, t.ToStatusID, i)
			if terr != nil {
				return terr
			}
			if tag.RowsAffected() == 0 {
				return &InvalidError{"transitions", fmt.Sprintf("%s names a transition this workflow never had. Leave its id out to add it.", name)}
			}
			edge = *t.ID
		} else {
			err = tx.QueryRow(ctx, `INSERT INTO workflow_transition (org_id, workflow_id, name, description, from_status_id, to_status_id, position)
				VALUES (current_org_id(), $1, $2, $3, $4, $5, $6) RETURNING id`, id, name, description, t.FromStatusID, t.ToStatusID, i).Scan(&edge)
			if err != nil {
				return err
			}
		}
		for j, r := range t.Rules {
			config := r.Config
			if len(config) == 0 {
				config = []byte("{}")
			}
			if _, err := tx.Exec(ctx, `INSERT INTO transition_rule (org_id, transition_id, kind, type, config, position)
				VALUES (current_org_id(), $1, $2, $3, $4, $5)`, edge, r.Kind, r.Type, config, j); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteWorkflow removes a workflow nobody follows.
func (s *Service) DeleteWorkflow(ctx context.Context, actor uuid.UUID, projectKey string, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		w, err := inProject(ctx, tx, p.ID, id)
		if err != nil {
			return err
		}
		followers := len(w.TypeIDs)
		if w.ProjectDefault {
			followers++
		}
		if followers > 0 {
			return &InUseError{What: "workflow", Count: followers}
		}
		_, err = tx.Exec(ctx, `DELETE FROM workflow WHERE id = $1`, id)
		return err
	})
}

// Assign makes a workflow the project's, or a type's, and moves each requirement it affects whose status the
// workflow lacks by the mapping given; a status left unmapped is refused by name.
func (s *Service) Assign(ctx context.Context, actor uuid.UUID, projectKey string, in AssignInput) (*Workflow, db.LSN, error) {
	var out *Workflow
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		w, err := inProject(ctx, tx, p.ID, in.WorkflowID)
		if err != nil {
			return err
		}
		if in.TypeID != nil {
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM requirement_type WHERE project_id = $1 AND id = $2)`, p.ID, *in.TypeID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return &InvalidError{"typeId", "That type is not one of this project's. Pick one from its types."}
			}
		}
		// The requirements the assignment affects: the type's, or every one that follows the project's workflow.
		rows, err := tx.Query(ctx, `
			SELECT r.id, r.status_id, s.name FROM requirement r JOIN req_status s ON s.id = r.status_id
			LEFT JOIN requirement_type t ON t.id = r.type_id
			WHERE r.project_id = $1 AND r.kind = 'requirement' AND NOT EXISTS (SELECT 1 FROM workflow_step x WHERE x.workflow_id = $2 AND x.status_id = r.status_id)
			  AND (($3::uuid IS NOT NULL AND r.type_id = $3) OR ($3::uuid IS NULL AND t.workflow_id IS NULL))`, p.ID, w.ID, in.TypeID)
		if err != nil {
			return err
		}
		type stray struct {
			id, status uuid.UUID
			name       string
		}
		strays, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (stray, error) {
			var s stray
			err := row.Scan(&s.id, &s.status, &s.name)
			return s, err
		})
		if err != nil {
			return err
		}
		unmapped := map[string]bool{}
		for _, s := range strays {
			to, ok := in.StatusMapping[s.status.String()]
			if !ok || !w.Has(to) {
				unmapped[s.name] = true
			}
		}
		if len(unmapped) > 0 {
			names := make([]string, 0, len(unmapped))
			for name := range unmapped {
				names = append(names, name)
			}
			sort.Strings(names)
			return &InvalidError{"statusMapping", fmt.Sprintf("Requirements are in %s, which this workflow lacks. Map each to a status it has.", strings.Join(names, ", "))}
		}
		if in.TypeID != nil {
			if _, err := tx.Exec(ctx, `UPDATE requirement_type SET workflow_id = $2 WHERE id = $1`, *in.TypeID, w.ID); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE project SET settings = jsonb_set(settings, '{workflowId}', to_jsonb($2::text)) WHERE id = $1`, p.ID, w.ID.String()); err != nil {
			return err
		}
		for _, s := range strays {
			to := in.StatusMapping[s.status.String()]
			if _, err := tx.Exec(ctx, `SELECT requirement_remap_status($1, $2)`, s.id, to); err != nil {
				return err
			}
			toStatus, _ := w.Status(to)
			if _, err := tx.Exec(ctx, `INSERT INTO requirement_change (org_id, requirement_id, field, from_value, to_value, reason, actor_id)
				VALUES (current_org_id(), $1, 'status', $2, $3, $4, $5)`, s.id, s.name, toStatus.Name, remapReason, actor); err != nil {
				return err
			}
			if err := events.Emit(ctx, tx, "requirement.changed", map[string]any{"actorId": actor, "requirementId": s.id, "field": "status"}); err != nil {
				return err
			}
		}
		if err := record(ctx, tx, actor, audit.ActionWorkflowAssigned, w.ID, map[string]any{"project": p.Key, "typeId": in.TypeID, "moved": len(strays)}); err != nil {
			return err
		}
		out, err = Load(ctx, tx, w.ID)
		return err
	})
	return out, lsn, err
}
