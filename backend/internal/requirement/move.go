package requirement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/workflow"
)

// TopicMoved is a requirement moved to another project, with the key it had and the one it has now.
const TopicMoved = "requirement.moved"

// destination reads the project a move or a copy goes to, where the actor must edit.
func destination(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectKey string, here *project.Project) (*project.Project, error) {
	p := here
	if projectKey != "" && NormalizedProjectKey(projectKey) != here.Key {
		var err error
		if p, err = project.Find(ctx, tx, actor, projectKey); err != nil {
			return nil, err
		}
	}
	switch {
	case p.ArchivedAt != nil:
		return nil, project.ErrArchived
	case !p.Can.Edit:
		return nil, ErrForbidden
	}
	return p, nil
}

// NormalizedProjectKey is a project key as a requirement key writes it.
func NormalizedProjectKey(key string) string { return strings.ToUpper(strings.TrimSpace(key)) }

// matcher carries a requirement's type and values into another project: a type by name, else the project's
// default; an attribute by name and kind, an option by name; what matches nothing is left behind.
type matcher struct {
	from, to    []attribute.Def
	types       map[string]uuid.UUID
	defaultType *uuid.UUID
	viewer      attribute.Viewer
}

func newMatcher(ctx context.Context, tx db.DBTX, fromProject uuid.UUID, to *project.Project) (*matcher, error) {
	m := &matcher{types: map[string]uuid.UUID{}, defaultType: to.Settings.DefaultTypeID, viewer: viewerIn(ctx, tx, to.ID)}
	var err error
	if m.from, err = attribute.Defs(ctx, tx, fromProject); err != nil {
		return nil, err
	}
	if m.to, err = attribute.Defs(ctx, tx, to.ID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id, lower(name) FROM requirement_type WHERE project_id = $1 AND NOT retired`, to.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   uuid.UUID
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		m.types[name] = id
	}
	return m, rows.Err()
}

func findDef(defs []attribute.Def, match func(attribute.Def) bool) *attribute.Def {
	for i := range defs {
		if match(defs[i]) {
			return &defs[i]
		}
	}
	return nil
}

// optionByName finds the live option of to named as from's option id is named.
func optionByName(from, to *attribute.Def, id string) (string, bool) {
	for _, o := range from.Options {
		if o.ID.String() != id {
			continue
		}
		for _, t := range to.Options {
			if !t.Retired && strings.EqualFold(t.Name, o.Name) {
				return t.ID.String(), true
			}
		}
	}
	return "", false
}

// carry answers the type and the values a requirement takes into the matcher's project.
func (m *matcher) carry(l *loaded) (*uuid.UUID, map[string]json.RawMessage, error) {
	values := map[string]json.RawMessage{}
	if l.Kind == KindSection {
		return nil, values, nil
	}
	typeID := m.defaultType
	if l.Type != nil {
		if id, ok := m.types[strings.ToLower(l.Type.Name)]; ok {
			typeID = &id
		}
	}
	for key, raw := range l.Attributes {
		from := findDef(m.from, func(d attribute.Def) bool { return d.ID.String() == key })
		if from == nil {
			continue
		}
		to := findDef(m.to, func(d attribute.Def) bool {
			return !d.Retired && d.Kind == from.Kind && strings.EqualFold(d.Name, from.Name)
		})
		if to == nil || !to.Shows(typeID) {
			continue
		}
		switch to.Kind {
		case attribute.Select:
			var id string
			if json.Unmarshal(raw, &id) != nil {
				continue
			}
			if mapped, ok := optionByName(from, to, id); ok {
				values[to.ID.String()], _ = json.Marshal(mapped)
			}
		case attribute.MultiSelect:
			var ids []string
			if json.Unmarshal(raw, &ids) != nil {
				continue
			}
			var mapped []string
			for _, id := range ids {
				if each, ok := optionByName(from, to, id); ok {
					mapped = append(mapped, each)
				}
			}
			if len(mapped) > 0 {
				values[to.ID.String()], _ = json.Marshal(mapped)
			}
		case attribute.User:
			var person uuid.UUID
			if json.Unmarshal(raw, &person) != nil {
				continue
			}
			views, err := m.viewer(person)
			if err != nil {
				return nil, nil, err
			}
			if views {
				values[to.ID.String()] = raw
			}
		default:
			values[to.ID.String()] = raw
		}
	}
	return typeID, values, nil
}

// Move puts a requirement and everything below it elsewhere in its project, or in another, which gives each a
// key from that project's counter and keeps the old one resolving.
func (s *Service) Move(ctx context.Context, actor uuid.UUID, key string, in MoveInput) (*Requirement, db.LSN, error) {
	var out *Requirement
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		cur, err := changing(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		if cur.TrashedAt != nil {
			return ErrTrashed
		}
		here, err := project.Find(ctx, tx, actor, cur.ProjectKey)
		if err != nil {
			return err
		}
		to, err := destination(ctx, tx, actor, in.ProjectKey, here)
		if err != nil {
			return err
		}
		switch {
		case to.ID == here.ID && len(in.StatusMapping) > 0:
			return &FieldError{"statusMapping", "A status mapping is for a move to another project; leave it out within one."}
		case to.ID == here.ID:
			err = s.moveWithin(ctx, tx, actor, cur, in.Place)
		default:
			err = s.moveAcross(ctx, tx, actor, cur, to, in.Place, in.StatusMapping)
		}
		if err != nil {
			return err
		}
		l, err := findByID(ctx, tx, actor, cur.ID)
		if err == nil {
			out = &l.Requirement
		}
		return err
	})
	return out, lsn, err
}

func (s *Service) moveWithin(ctx context.Context, tx db.DBTX, actor uuid.UUID, cur *loaded, place Place) error {
	at, err := rankAt(ctx, tx, cur.projectID, place, &cur.ID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE requirement SET parent_id = $2, rank = $3, updated_by = $4 WHERE id = $1`, cur.ID, place.ParentID, at, actor); err != nil {
		return treeRefusal(err)
	}
	now, err := outline(ctx, tx, cur.projectID, cur.ID)
	if err != nil {
		return err
	}
	if now == cur.Outline {
		return nil
	}
	return logChange(ctx, tx, actor, cur.ID, ChangePlace, cur.Outline, now)
}

// carried is one requirement of a move between projects, read before anything moves, with what it becomes there.
type carried struct {
	l      *loaded
	typeID *uuid.UUID
	values map[string]json.RawMessage
	status *uuid.UUID
}

// statusThere finds the status a requirement takes in another project's workflow: the one of its name, else the
// one the mapping gives for its status; empty when neither does.
func statusThere(w *workflow.Workflow, l *loaded, mapping map[string]uuid.UUID) *uuid.UUID {
	if l.Status == nil {
		return nil
	}
	for _, st := range w.Steps {
		if strings.EqualFold(st.Status.Name, l.Status.Name) {
			return &st.Status.ID
		}
	}
	if to, ok := mapping[l.Status.ID.String()]; ok && w.Has(to) {
		return &to
	}
	return nil
}

func (s *Service) moveAcross(ctx context.Context, tx db.DBTX, actor uuid.UUID, cur *loaded, to *project.Project, place Place, mapping map[string]uuid.UUID) error {
	at, err := rankAt(ctx, tx, to.ID, place, nil)
	if err != nil {
		return err
	}
	match, err := newMatcher(ctx, tx, cur.projectID, to)
	if err != nil {
		return err
	}
	nodes, err := subtree(ctx, tx, cur.projectID, cur.ID)
	if err != nil {
		return err
	}
	// Everything is matched before anything moves, so a status left unmapped refuses the whole move.
	plan := make([]carried, len(nodes))
	unmapped := map[string]bool{}
	for i, id := range nodes {
		l, err := findByID(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		c := carried{l: l}
		if c.typeID, c.values, err = match.carry(l); err != nil {
			return err
		}
		if l.Kind == KindRequirement {
			w, err := workflow.For(ctx, tx, to.ID, c.typeID)
			if err != nil {
				return err
			}
			if c.status = statusThere(w, l, mapping); c.status == nil && l.Status != nil {
				unmapped[l.Status.Name] = true
			}
		}
		plan[i] = c
	}
	if len(unmapped) > 0 {
		names := make([]string, 0, len(unmapped))
		for name := range unmapped {
			names = append(names, name)
		}
		slices.Sort(names)
		return &FieldError{"statusMapping", fmt.Sprintf("%s has no status called %s. Map each to one of its statuses.", to.Name, strings.Join(names, ", "))}
	}
	// In reading order, so each takes its number in the order a reader meets them and finds its parent moved.
	for i, c := range plan {
		l, id, typeID, values := c.l, c.l.ID, c.typeID, c.values
		content := Normative{Title: l.Title, Statement: l.Statement, Rationale: l.Rationale, TypeID: typeID, Attributes: values}
		hash := content.Hash()
		attrs, err := json.Marshal(values)
		if err != nil {
			return err
		}
		parent, rank := l.ParentID, l.rank
		if i == 0 {
			parent, rank = place.ParentID, at
		}
		var number int
		if err := tx.QueryRow(ctx, `
			UPDATE requirement SET project_id = $2, parent_id = $3, rank = $4, type_id = $5, attributes = $6, normative_hash = $7, updated_by = $8
			WHERE id = $1 RETURNING number`, id, to.ID, parent, rank, typeID, attrs, hash, actor).Scan(&number); err != nil {
			return fmt.Errorf("move %s: %w", l.Key, treeRefusal(err))
		}
		moved := Key(to.Key, number)
		if c.status != nil {
			if _, err := tx.Exec(ctx, `SELECT requirement_remap_status($1, $2)`, id, *c.status); err != nil {
				return fmt.Errorf("bring the status of %s along: %w", l.Key, err)
			}
		}
		if hash != l.hash {
			if err := revisionOf(ctx, tx, actor, id, l.Revision+1, content, hash, "Moved from "+l.Key, nil); err != nil {
				return err
			}
			if err := events.Emit(ctx, tx, TopicRevised, map[string]any{"actorId": actor, "requirementId": id, "revision": l.Revision + 1}); err != nil {
				return err
			}
		}
		if err := logChange(ctx, tx, actor, id, ChangeKey, l.Key, moved); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicMoved, map[string]any{"actorId": actor, "requirementId": id, "fromKey": l.Key, "toKey": moved}); err != nil {
			return err
		}
	}
	return nil
}

// revisionOf writes a revision of what content says, as a move, a copy or a restore does without a save behind it.
func revisionOf(ctx context.Context, tx db.DBTX, actor, id uuid.UUID, number int, content Normative, hash, comment string, restoredFrom *int) error {
	attrs, err := json.Marshal(content.Attributes)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO requirement_revision (org_id, requirement_id, number, title, statement, rationale, type_id, attributes, hash, comment, restored_from, created_by)
		VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		id, number, content.Title, content.Statement, content.Rationale, content.TypeID, attrs, hash, comment, restoredFrom, actor); err != nil {
		return fmt.Errorf("write revision %d: %w", number, err)
	}
	return nil
}

// Copy makes new requirements of a requirement, and of what is below it when asked, at their current content
// as revision 1, in its project or another; it answers the copy and how many it made.
func (s *Service) Copy(ctx context.Context, actor uuid.UUID, key string, in CopyInput) (*Requirement, int, db.LSN, error) {
	var (
		out    *Requirement
		copied int
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		src, err := find(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		if src.TrashedAt != nil {
			return ErrTrashed
		}
		here, err := project.Find(ctx, tx, actor, src.ProjectKey)
		if err != nil {
			return err
		}
		to, err := destination(ctx, tx, actor, in.ProjectKey, here)
		if err != nil {
			return err
		}
		nodes := []uuid.UUID{src.ID}
		if in.Children {
			if nodes, err = subtree(ctx, tx, src.projectID, src.ID); err != nil {
				return err
			}
		}
		if len(nodes) > MaxCopy {
			return &FieldError{"children", fmt.Sprintf("A copy makes at most %d requirements, and this one would make %d. Copy a part of the tree.", MaxCopy, len(nodes))}
		}
		at, err := rankAt(ctx, tx, to.ID, in.Place, nil)
		if err != nil {
			return err
		}
		var match *matcher
		if to.ID != here.ID {
			if match, err = newMatcher(ctx, tx, here.ID, to); err != nil {
				return err
			}
		}
		copies := map[uuid.UUID]uuid.UUID{}
		for i, id := range nodes {
			l, err := findByID(ctx, tx, actor, id)
			if err != nil {
				return err
			}
			made, err := copyOne(ctx, tx, actor, l, to, match, i == 0, in.Place.ParentID, at, copies)
			if err != nil {
				return err
			}
			copies[id] = made
		}
		copied = len(nodes)
		l, err := findByID(ctx, tx, actor, copies[src.ID])
		if err == nil {
			out = &l.Requirement
		}
		return err
	})
	return out, copied, lsn, err
}

// copyOne writes one copy: the root of a copy at the place asked for, the rest below the copies of their parents.
func copyOne(ctx context.Context, tx db.DBTX, actor uuid.UUID, l *loaded, to *project.Project, match *matcher, root bool, place *uuid.UUID, at string,
	copies map[uuid.UUID]uuid.UUID) (uuid.UUID, error) {
	typeID, values, owner := l.typeID(), l.Attributes, (*uuid.UUID)(nil)
	if l.Owner != nil {
		owner = &l.Owner.ID
	}
	if match != nil {
		var err error
		if typeID, values, err = match.carry(l); err != nil {
			return uuid.Nil, err
		}
		if owner != nil {
			views, err := match.viewer(*owner)
			if err != nil {
				return uuid.Nil, err
			}
			if !views {
				owner = nil
			}
		}
	}
	parent, rank := place, at
	if !root {
		mapped := copies[*l.ParentID]
		parent, rank = &mapped, l.rank
	}
	content := Normative{Title: l.Title, Statement: l.Statement, Rationale: l.Rationale, TypeID: typeID, Attributes: values}
	hash := content.Hash()
	attrs, err := json.Marshal(values)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO requirement (org_id, project_id, kind, title, statement, rationale, type_id, attributes, normative_hash, owner_id, priority,
		                         parent_id, rank, copied_from, created_by, updated_by)
		VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $14) RETURNING id`,
		to.ID, l.Kind, l.Title, l.Statement, l.Rationale, typeID, attrs, hash, owner, l.Priority, parent, rank, l.ID, actor).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("copy %s: %w", l.Key, treeRefusal(err))
	}
	if err := revisionOf(ctx, tx, actor, id, 1, content, hash, "Copied from "+l.Key, nil); err != nil {
		return uuid.Nil, err
	}
	if err := copyLabels(ctx, tx, actor, l, id); err != nil {
		return uuid.Nil, err
	}
	return id, events.Emit(ctx, tx, TopicCreated, map[string]any{"actorId": actor, "projectId": to.ID, "requirementId": id, "revision": 1})
}
