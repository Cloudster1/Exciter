package attribute

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
)

// MaxPermits bounds the role and status pairs that say who changes one field (#206).
const MaxPermits = 20

// FixedFields are a requirement's own fields that take rules, beside its attributes.
var FixedFields = []string{"title", "statement", "rationale", "type", "owner", "priority"}

// Permit lets the holders of any of Roles change a field while the requirement is in one of Statuses, every status
// when empty.
type Permit struct {
	Roles    []string    `json:"roles"`
	Statuses []uuid.UUID `json:"statuses"`
}

// Dependency narrows a select attribute's options by the option another select attribute holds; an option of the
// other that Choices does not name allows every option.
type Dependency struct {
	AttributeID uuid.UUID              `json:"attributeId"`
	Choices     map[string][]uuid.UUID `json:"choices"`
}

// FieldRule is when a fixed field is required and who changes it.
type FieldRule struct {
	Field string `json:"field"`
	// RequiredFrom is the status from which on it needs a value, in the project's status order; null for never.
	RequiredFrom *uuid.UUID `json:"requiredFrom"`
	// EditableBy empty leaves the field to whoever may edit the requirement.
	EditableBy []Permit `json:"editableBy"`
}

// FieldRuleInput sets a fixed field's rule whole.
type FieldRuleInput struct {
	RequiredFrom *uuid.UUID `json:"requiredFrom"`
	EditableBy   []Permit   `json:"editableBy"`
}

// permitRoles are the project roles a permit names (#12).
var permitRoles = []string{"viewer", "reviewer", "editor", "admin"}

// checkPermits holds permits to the project's roles and statuses, each permit naming at least one role.
func checkPermits(ctx context.Context, tx db.DBTX, projectID uuid.UUID, permits []Permit) error {
	if len(permits) > MaxPermits {
		return &FieldError{"editableBy", fmt.Sprintf("Name at most %d pairs of roles and statuses.", MaxPermits)}
	}
	for i := range permits {
		p := &permits[i]
		if p.Statuses == nil {
			p.Statuses = []uuid.UUID{}
		}
		if len(p.Roles) == 0 {
			return &FieldError{"editableBy", "Each line names at least one role who changes the field."}
		}
		for _, r := range p.Roles {
			if !slices.Contains(permitRoles, r) {
				return &FieldError{"editableBy", fmt.Sprintf("%q is not a project role; pick viewer, reviewer, editor or admin.", r)}
			}
		}
		if len(p.Statuses) == 0 {
			continue
		}
		var mine int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM req_status WHERE project_id = $1 AND id = ANY($2)`, projectID, p.Statuses).Scan(&mine); err != nil {
			return err
		}
		if mine != len(p.Statuses) {
			return &FieldError{"editableBy", "Pick statuses of this project."}
		}
	}
	return nil
}

// statusFrom reads the status a field is required from: absent leaves it, null clears it.
func statusFrom(ctx context.Context, tx db.DBTX, projectID uuid.UUID, raw json.RawMessage, current *uuid.UUID) (*uuid.UUID, error) {
	if len(raw) == 0 {
		return current, nil
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var id uuid.UUID
	if err := json.Unmarshal(raw, &id); err != nil {
		return nil, &FieldError{"requiredFrom", "Pick a status of this project, or none."}
	}
	var mine bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM req_status WHERE project_id = $1 AND id = $2)`, projectID, id).Scan(&mine); err != nil {
		return nil, err
	}
	if !mine {
		return nil, &FieldError{"requiredFrom", "Pick a status of this project, or none."}
	}
	return &id, nil
}

// dependency checks what an attribute depends on: another single select of the project, never back round to it, its
// choices keyed by that one's options and naming this one's.
func dependency(ctx context.Context, tx db.DBTX, projectID uuid.UUID, d *Def, raw json.RawMessage) (*Dependency, error) {
	if len(raw) == 0 {
		return d.DependsOn, nil
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var dep Dependency
	if err := json.Unmarshal(raw, &dep); err != nil {
		return nil, &FieldError{"dependsOn", "Name the attribute it depends on and the options each of its options allows."}
	}
	if d.Kind != Select && d.Kind != MultiSelect {
		return nil, &FieldError{"dependsOn", "Only a select or multi-select attribute depends on another."}
	}
	defs, err := Defs(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]Def{}
	for _, each := range defs {
		byID[each.ID] = each
	}
	parent, ok := byID[dep.AttributeID]
	if !ok || parent.ID == d.ID || parent.Kind != Select {
		return nil, &FieldError{"dependsOn", "Pick another select attribute of this project to depend on."}
	}
	for at := parent; at.DependsOn != nil; at = byID[at.DependsOn.AttributeID] {
		if at.DependsOn.AttributeID == d.ID {
			return nil, &FieldError{"dependsOn", fmt.Sprintf("%s depends on %s already, so it cannot depend on it in turn.", parent.Name, d.Name)}
		}
	}
	has := func(of Def, id string) bool {
		return slices.ContainsFunc(of.Options, func(o Option) bool { return o.ID.String() == id })
	}
	if dep.Choices == nil {
		dep.Choices = map[string][]uuid.UUID{}
	}
	for key, allowed := range dep.Choices {
		if !has(parent, key) {
			return nil, &FieldError{"dependsOn", fmt.Sprintf("The choices name an option %s does not have.", parent.Name)}
		}
		for _, id := range allowed {
			if !has(*d, id.String()) {
				return nil, &FieldError{"dependsOn", fmt.Sprintf("The choices name an option %s does not have.", d.Name)}
			}
		}
	}
	return &dep, nil
}

// setRules writes what an attribute's input says of its field rules, and audits a change of them.
func setRules(ctx context.Context, tx db.DBTX, actor, projectID uuid.UUID, projectKey string, d *Def, in DefInput) error {
	if len(in.RequiredFrom) == 0 && len(in.DependsOn) == 0 && in.EditableBy == nil {
		return nil
	}
	from, err := statusFrom(ctx, tx, projectID, in.RequiredFrom, d.RequiredFrom)
	if err != nil {
		return err
	}
	dep, err := dependency(ctx, tx, projectID, d, in.DependsOn)
	if err != nil {
		return err
	}
	permits := d.EditableBy
	if in.EditableBy != nil {
		permits = *in.EditableBy
		if err := checkPermits(ctx, tx, projectID, permits); err != nil {
			return err
		}
	}
	if permits == nil {
		permits = []Permit{}
	}
	var dependsOn *uuid.UUID
	choices := map[string][]uuid.UUID{}
	if dep != nil {
		dependsOn, choices = &dep.AttributeID, dep.Choices
	}
	encodedChoices, err := json.Marshal(choices)
	if err != nil {
		return err
	}
	encodedPermits, err := json.Marshal(permits)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE attribute_def SET required_from = $2, depends_on = $3, choices = $4, editable_by = $5 WHERE id = $1`,
		d.ID, from, dependsOn, encodedChoices, encodedPermits); err != nil {
		return fmt.Errorf("set the rules of %s: %w", d.Name, err)
	}
	return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionFieldRuleSaved, TargetType: "attribute", TargetID: &d.ID, Actor: actor,
		Data: map[string]any{"project": projectKey, "field": d.Name, "requiredFrom": from, "dependsOn": dependsOn, "permits": len(permits)}})
}

// FieldRules answers the rules of every fixed field of a project, a field without one as having none.
func (s *Service) FieldRules(ctx context.Context, reader uuid.UUID, projectKey string) ([]FieldRule, error) {
	var out []FieldRule
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		out, err = everyFixed(ctx, tx, p.ID)
		return err
	})
	return out, err
}

func everyFixed(ctx context.Context, tx db.DBTX, projectID uuid.UUID) ([]FieldRule, error) {
	saved, err := fieldRules(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]FieldRule, len(FixedFields))
	for i, field := range FixedFields {
		out[i] = FieldRule{Field: field, EditableBy: []Permit{}}
		for _, f := range saved {
			if f.Field == field {
				out[i] = f
			}
		}
	}
	return out, nil
}

// SaveFieldRule sets a fixed field's rule whole; the project's administrators'.
func (s *Service) SaveFieldRule(ctx context.Context, actor uuid.UUID, projectKey, field string, in FieldRuleInput) (*FieldRule, db.LSN, error) {
	if !slices.Contains(FixedFields, field) {
		return nil, 0, ErrNotFound
	}
	var out *FieldRule
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if in.RequiredFrom != nil && (field == "title" || field == "statement") {
			return &FieldError{"requiredFrom", fmt.Sprintf("%s is required always.", strings.ToUpper(field[:1])+field[1:])}
		}
		if in.RequiredFrom != nil {
			raw, _ := json.Marshal(in.RequiredFrom)
			if _, err := statusFrom(ctx, tx, p.ID, raw, nil); err != nil {
				return err
			}
		}
		permits := in.EditableBy
		if permits == nil {
			permits = []Permit{}
		}
		if err := checkPermits(ctx, tx, p.ID, permits); err != nil {
			return err
		}
		encoded, err := json.Marshal(permits)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO field_rule (project_id, field, required_from, editable_by, updated_by) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (project_id, field) DO UPDATE
			SET required_from = EXCLUDED.required_from, editable_by = EXCLUDED.editable_by, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			p.ID, field, in.RequiredFrom, encoded, actor); err != nil {
			return fmt.Errorf("save the rule of %s: %w", field, err)
		}
		all, err := everyFixed(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		for i := range all {
			if all[i].Field == field {
				out = &all[i]
			}
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionFieldRuleSaved, TargetType: "project", TargetID: &p.ID, Actor: actor,
			Data: map[string]any{"project": p.Key, "field": field, "requiredFrom": in.RequiredFrom, "permits": len(permits)}})
	})
	return out, lsn, err
}
