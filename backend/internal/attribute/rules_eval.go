package attribute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Rules are a project's field rules as one save or transition weighs them: its attributes, its fixed fields' rules
// and its statuses in order.
type Rules struct {
	Defs   []Def
	Fixed  map[string]FieldRule
	order  map[uuid.UUID]int
	status map[uuid.UUID]string
}

// LoadRules reads a project's field rules.
func LoadRules(ctx context.Context, tx db.DBTX, projectID uuid.UUID) (*Rules, error) {
	defs, err := Defs(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	fixed, err := fieldRules(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	r := &Rules{Defs: defs, Fixed: map[string]FieldRule{}, order: map[uuid.UUID]int{}, status: map[uuid.UUID]string{}}
	for _, f := range fixed {
		r.Fixed[f.Field] = f
	}
	rows, err := tx.Query(ctx, `SELECT id, name, position FROM req_status WHERE project_id = $1`, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the statuses: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   uuid.UUID
			name string
			at   int
		)
		if err := rows.Scan(&id, &name, &at); err != nil {
			return nil, err
		}
		r.order[id], r.status[id] = at, name
	}
	return r, rows.Err()
}

// from says whether a field required from a status is required in another: that one, or any after it.
func (r *Rules) from(required, status *uuid.UUID) bool {
	if required == nil || status == nil {
		return false
	}
	at, ok := r.order[*required]
	now, known := r.order[*status]
	return ok && known && now >= at
}

// StatusName is a status's name, for a sentence.
func (r *Rules) StatusName(status *uuid.UUID) string {
	if status == nil {
		return ""
	}
	return r.status[*status]
}

// State is what a requirement has of the fixed fields a status may ask for.
type State struct {
	Rationale, Owner, Priority bool
	TypeID                     *uuid.UUID
	Attributes                 map[string]json.RawMessage
}

// FixedLabels name the fixed fields in a sentence, and Inputs the request field each is refused on.
var (
	FixedLabels = map[string]string{"title": "the title", "statement": "the statement", "rationale": "the rationale", "type": "the type",
		"owner": "the owner", "priority": "the priority"}
	Inputs = map[string]string{"title": "title", "statement": "statement", "rationale": "rationale", "type": "typeId", "owner": "ownerId",
		"priority": "priority"}
)

// MissingIn refuses a requirement in a status that lacks a field required from that status on, naming it.
func (r *Rules) MissingIn(status *uuid.UUID, s State) error {
	name := r.StatusName(status)
	for _, field := range []string{"rationale", "owner", "priority"} {
		f, ok := r.Fixed[field]
		has := map[string]bool{"rationale": s.Rationale, "owner": s.Owner, "priority": s.Priority}[field]
		if ok && !has && r.from(f.RequiredFrom, status) {
			return &FieldError{Inputs[field], fmt.Sprintf("Give %s a value; requirements in %s need it.", FixedLabels[field], name)}
		}
	}
	for _, d := range r.Defs {
		if d.Retired || !d.Shows(s.TypeID) || !r.from(d.RequiredFrom, status) {
			continue
		}
		if _, ok := s.Attributes[d.ID.String()]; !ok {
			return valueError(d, "Give %s a value; requirements in %s need it.", d.Name, name)
		}
	}
	return nil
}

// Editable says whether holders of roles change a field in a status, and if not, who does: a fixed field by its
// name, an attribute by its id.
func (r *Rules) Editable(field string, status *uuid.UUID, roles []string) (bool, string) {
	var permits []Permit
	label := FixedLabels[field]
	if f, ok := r.Fixed[field]; ok {
		permits = f.EditableBy
	} else {
		for _, d := range r.Defs {
			if d.ID.String() == field {
				permits, label = d.EditableBy, d.Name
			}
		}
	}
	return Permitted(permits, status, roles, label, r.StatusName(status))
}

// Permitted weighs permits for holders of roles in a status; why names who may, for a field called label.
func Permitted(permits []Permit, status *uuid.UUID, roles []string, label, statusName string) (bool, string) {
	if len(permits) == 0 {
		return true, ""
	}
	var who []string
	for _, p := range permits {
		if len(p.Statuses) > 0 && (status == nil || !slices.Contains(p.Statuses, *status)) {
			continue
		}
		for _, role := range p.Roles {
			if slices.Contains(roles, role) {
				return true, ""
			}
			if !slices.Contains(who, role) {
				who = append(who, role)
			}
		}
	}
	if len(who) == 0 {
		return false, fmt.Sprintf("Nobody changes %s in %s.", label, statusName)
	}
	return false, fmt.Sprintf("Only %s change %s in %s.", joinRoles(who), label, statusName)
}

func joinRoles(roles []string) string {
	names := make([]string, len(roles))
	for i, r := range roles {
		names[i] = r + "s"
	}
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// Allowed is the options a dependent attribute may hold while the one it depends on holds what values say; nil for
// every option.
func Allowed(defs []Def, d Def, values map[string]json.RawMessage) []uuid.UUID {
	if d.DependsOn == nil {
		return nil
	}
	var parent string
	if raw, ok := values[d.DependsOn.AttributeID.String()]; ok {
		_ = json.Unmarshal(raw, &parent)
	}
	allowed, ok := d.DependsOn.Choices[parent]
	if !ok {
		return nil
	}
	return allowed
}

// CheckChoices refuses a dependent attribute holding an option the one it depends on does not allow, where either
// changed from before.
func CheckChoices(defs []Def, before, after map[string]json.RawMessage) error {
	for _, d := range defs {
		if d.DependsOn == nil {
			continue
		}
		key, parentKey := d.ID.String(), d.DependsOn.AttributeID.String()
		raw, has := after[key]
		if !has || (string(before[key]) == string(raw) && string(before[parentKey]) == string(after[parentKey])) {
			continue
		}
		allowed := Allowed(defs, d, after)
		if allowed == nil {
			continue
		}
		for _, id := range chosen(raw) {
			if !slices.ContainsFunc(allowed, func(a uuid.UUID) bool { return a.String() == id }) {
				return valueError(d, "%s is not a choice of %s while %s is %s. Pick one of: %s.", optionName(d, id), d.Name,
					parentName(defs, d), optionOf(defs, d.DependsOn.AttributeID, after[parentKey]), optionNames(d, allowed))
			}
		}
	}
	return nil
}

func optionName(d Def, id string) string {
	for _, o := range d.Options {
		if o.ID.String() == id {
			return o.Name
		}
	}
	return id
}

func optionNames(d Def, ids []uuid.UUID) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, optionName(d, id.String()))
	}
	return strings.Join(names, ", ")
}

func parentName(defs []Def, d Def) string {
	for _, p := range defs {
		if p.ID == d.DependsOn.AttributeID {
			return p.Name
		}
	}
	return "the attribute it depends on"
}

func optionOf(defs []Def, id uuid.UUID, raw json.RawMessage) string {
	var chosen string
	_ = json.Unmarshal(raw, &chosen)
	for _, p := range defs {
		if p.ID == id {
			return optionName(p, chosen)
		}
	}
	return chosen
}

// fieldRules reads a project's fixed fields' rules.
func fieldRules(ctx context.Context, tx db.DBTX, projectID uuid.UUID) ([]FieldRule, error) {
	rows, err := tx.Query(ctx, `SELECT field, required_from, editable_by FROM field_rule WHERE project_id = $1 ORDER BY field`, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the field rules: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (FieldRule, error) {
		var (
			f       FieldRule
			permits []byte
		)
		if err := row.Scan(&f.Field, &f.RequiredFrom, &permits); err != nil {
			return f, err
		}
		if err := json.Unmarshal(permits, &f.EditableBy); err != nil {
			return f, errors.New("a field rule's permits are not a list")
		}
		if f.EditableBy == nil {
			f.EditableBy = []Permit{}
		}
		return f, nil
	})
}
