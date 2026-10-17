package requirement

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/workflow"
)

// fields is what a requirement holds of the fields its project's rules weigh (#206).
type fields struct {
	title                string
	statement, rationale json.RawMessage
	typeID, owner        *uuid.UUID
	priority             *Priority
	attributes           map[string]json.RawMessage
}

func fieldsOf(l *loaded) fields {
	f := fields{title: l.Title, statement: l.Statement, rationale: l.Rationale, typeID: l.typeID(), priority: l.Priority, attributes: l.Attributes}
	if l.Owner != nil {
		f.owner = &l.Owner.ID
	}
	return f
}

// sameJSON compares two documents by what they say, not how they are spaced.
func sameJSON(a, b json.RawMessage) bool {
	var x, y bytes.Buffer
	if json.Compact(&x, a) != nil || json.Compact(&y, b) != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}

func samePriority(a, b *Priority) bool { return priorityText(a) == priorityText(b) }

// changed names the fields that differ, fixed ones by their rule's name and attributes by their id.
func changed(before, after fields) []string {
	var out []string
	if before.title != after.title {
		out = append(out, "title")
	}
	if !sameJSON(before.statement, after.statement) {
		out = append(out, "statement")
	}
	if (len(before.rationale) == 0) != (len(after.rationale) == 0) || !sameJSON(before.rationale, after.rationale) {
		out = append(out, "rationale")
	}
	if !samePerson(before.typeID, after.typeID) {
		out = append(out, "type")
	}
	if !samePerson(before.owner, after.owner) {
		out = append(out, "owner")
	}
	if !samePriority(before.priority, after.priority) {
		out = append(out, "priority")
	}
	keys := slices.Sorted(maps.Keys(before.attributes))
	for key := range after.attributes {
		if _, ok := before.attributes[key]; !ok {
			keys = append(keys, key)
		}
	}
	for _, key := range keys {
		if !sameJSON(before.attributes[key], after.attributes[key]) {
			out = append(out, key)
		}
	}
	return out
}

// inputOf is the request field a rule's field is refused on.
func inputOf(field string) string {
	if input, ok := attribute.Inputs[field]; ok {
		return input
	}
	return "attributes." + field
}

// holdFieldRules refuses a change of a field the actor's roles may not change in the requirement's status, and a
// requirement left without a field its status asks for.
func holdFieldRules(ctx context.Context, tx db.DBTX, actor uuid.UUID, l *loaded, after fields) error {
	if l.Kind == KindSection {
		return nil
	}
	moved := changed(fieldsOf(l), after)
	if len(moved) == 0 {
		return nil
	}
	rules, err := attribute.LoadRules(ctx, tx, l.projectID)
	if err != nil {
		return err
	}
	roles, err := workflow.ProjectRoles(ctx, tx, l.projectID, actor)
	if err != nil {
		return err
	}
	status := statusID(l)
	for _, field := range moved {
		if ok, why := rules.Editable(field, status, roles); !ok {
			return &FieldError{inputOf(field), why}
		}
	}
	return rules.MissingIn(status, stateOf(after))
}

// requiredIn refuses a requirement that lacks a field the status asks for, as one entering it would.
func requiredIn(ctx context.Context, tx db.DBTX, l *loaded, status *uuid.UUID) error {
	if l.Kind == KindSection {
		return nil
	}
	rules, err := attribute.LoadRules(ctx, tx, l.projectID)
	if err != nil {
		return err
	}
	return rules.MissingIn(status, stateOf(fieldsOf(l)))
}

func stateOf(f fields) attribute.State {
	return attribute.State{Rationale: len(f.rationale) > 0 && !isNull(f.rationale), Owner: f.owner != nil, Priority: f.priority != nil,
		TypeID: f.typeID, Attributes: f.attributes}
}

func statusID(l *loaded) *uuid.UUID {
	if l.Status == nil {
		return nil
	}
	return &l.Status.ID
}

// ownerAfter and priorityAfter read what a save asks of the owner and the priority, the current one where it asks
// nothing or asks it badly, which the change itself then refuses.
func ownerAfter(cur *loaded, raw json.RawMessage) *uuid.UUID {
	var was *uuid.UUID
	if cur.Owner != nil {
		was = &cur.Owner.ID
	}
	if len(raw) == 0 {
		return was
	}
	if isNull(raw) {
		return nil
	}
	var owner uuid.UUID
	if json.Unmarshal(raw, &owner) != nil {
		return was
	}
	return &owner
}

func priorityAfter(cur *loaded, raw json.RawMessage) *Priority {
	if len(raw) == 0 {
		return cur.Priority
	}
	if isNull(raw) {
		return nil
	}
	var p Priority
	if json.Unmarshal(raw, &p) != nil {
		return cur.Priority
	}
	return &p
}

// Coverage judges one requirement in each coverage dimension, as the coverage report does; trace gives it, which
// reads requirements itself.
type Coverage interface {
	Judge(ctx context.Context, tx db.DBTX, projectID, id uuid.UUID, typeID *uuid.UUID) (map[string]string, error)
}

// WithCoverage lets a transition ask for full coverage (#206); without it no requirement is covered.
func (s *Service) WithCoverage(c Coverage) *Service {
	s.coverage = c
	return s
}

// traceOf reads what the trace conditions of a transition weigh, as the taker sees the requirement's links.
func (s *Service) traceOf(ctx context.Context, tx db.DBTX, l *loaded, subject *workflow.Subject) error {
	subject.Key, subject.OpenThreads = l.Key, l.Comments.Open
	rows, err := tx.Query(ctx, `
		SELECT lt.id, lt.name, count(t.id) FILTER (WHERE t.source_id = $1)::int, count(t.id) FILTER (WHERE t.target_id = $1)::int
		FROM link_type lt
		LEFT JOIN trace_link t ON t.type_id = lt.id AND t.deleted_at IS NULL AND (t.source_id = $1 OR t.target_id = $1)
		GROUP BY lt.id, lt.name`, l.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c workflow.LinkCount
		if err := rows.Scan(&c.TypeID, &c.TypeName, &c.Outgoing, &c.Incoming); err != nil {
			return err
		}
		subject.Links = append(subject.Links, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT `+SuspectLinks+` FROM requirement r WHERE r.id = $1`, l.ID).Scan(&subject.Suspects); err != nil {
		return err
	}
	subject.Coverage = map[string]string{}
	if s.coverage != nil {
		subject.Coverage, err = s.coverage.Judge(ctx, tx, l.projectID, l.ID, l.typeID())
	}
	return err
}
