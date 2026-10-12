package requirement

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
)

// Create writes a requirement or a section as revision 1.
func (s *Service) Create(ctx context.Context, actor uuid.UUID, projectKey string, in CreateInput) (*Requirement, db.LSN, error) {
	kind := in.Kind
	if kind == "" {
		kind = KindRequirement
	}
	if !slices.Contains(Kinds, kind) {
		return nil, 0, &FieldError{"kind", "Make a requirement or a section."}
	}
	title, err := checkTitle(in.Title)
	if err != nil {
		return nil, 0, err
	}
	statement := document.Empty
	if len(in.Statement) > 0 && !isNull(in.Statement) {
		if statement, err = checkDocument("statement", in.Statement); err != nil {
			return nil, 0, err
		}
	}
	var rationale json.RawMessage
	if len(in.Rationale) > 0 && !isNull(in.Rationale) {
		if kind == KindSection {
			return nil, 0, &FieldError{"rationale", "A section has no rationale. Put its words in the statement."}
		}
		if rationale, err = checkDocument("rationale", in.Rationale); err != nil {
			return nil, 0, err
		}
	}
	if err := untilLater(in.Labels, in.Place); err != nil {
		return nil, 0, err
	}
	if kind == KindSection && (in.TypeID != nil || len(in.Attributes) > 0) {
		return nil, 0, &FieldError{"typeId", "A section has no type or attributes. Give them to the requirements in it."}
	}
	if kind == KindSection && (in.OwnerID != nil || in.Priority != nil) {
		return nil, 0, &FieldError{"ownerId", "A section has no owner or priority. Give them to the requirements in it."}
	}
	if in.Priority != nil {
		if err := checkPriority(*in.Priority); err != nil {
			return nil, 0, err
		}
	}
	comment, err := checkComment(in.Comment)
	if err != nil {
		return nil, 0, err
	}
	var out *Requirement
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if p.ArchivedAt != nil {
			return project.ErrArchived
		}
		if !p.Can.Edit {
			return ErrForbidden
		}
		if in.OwnerID != nil {
			if err := checkOwner(ctx, tx, p.ID, *in.OwnerID); err != nil {
				return err
			}
		}
		content := Normative{Title: title, Statement: statement, Rationale: rationale, Attributes: map[string]json.RawMessage{}}
		if kind == KindRequirement {
			if content.TypeID = in.TypeID; content.TypeID == nil {
				content.TypeID = p.Settings.DefaultTypeID
			}
			if err := checkType(ctx, tx, p.ID, content.TypeID, nil); err != nil {
				return err
			}
			defs, err := attribute.Defs(ctx, tx, p.ID)
			if err != nil {
				return err
			}
			if content.Attributes, err = attribute.Merge(defs, content.TypeID, false, nil, in.Attributes, viewerIn(ctx, tx, p.ID)); err != nil {
				return err
			}
			if err := attribute.Missing(defs, content.TypeID, content.Attributes); err != nil {
				return err
			}
		}
		hash := content.Hash()
		attrs, err := json.Marshal(content.Attributes)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO requirement (org_id, project_id, kind, title, statement, rationale, type_id, attributes, normative_hash, owner_id, priority, created_by, updated_by)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11) RETURNING id`,
			p.ID, kind, title, statement, rationale, content.TypeID, attrs, hash, in.OwnerID, in.Priority, actor).Scan(&id); err != nil {
			return fmt.Errorf("write the requirement: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO requirement_revision (org_id, requirement_id, number, title, statement, rationale, type_id, attributes, hash, comment, created_by)
			VALUES (current_org_id(), $1, 1, $2, $3, $4, $5, $6, $7, $8, $9)`, id, title, statement, rationale, content.TypeID, attrs, hash, comment, actor); err != nil {
			return fmt.Errorf("write revision 1: %w", err)
		}
		if err := events.Emit(ctx, tx, TopicCreated, map[string]any{"actorId": actor, "projectId": p.ID, "requirementId": id, "revision": 1}); err != nil {
			return err
		}
		l, err := findByID(ctx, tx, actor, id)
		if err == nil {
			out = &l.Requirement
		}
		return err
	})
	return out, lsn, err
}

// changing reads a requirement for a change its project's editors make, and holds its row. The lock is taken
// once the reader may change it, since a row they may not change is one the lock cannot see.
func changing(ctx context.Context, tx db.DBTX, actor uuid.UUID, key string) (*loaded, error) {
	l, err := find(ctx, tx, actor, key)
	if err != nil {
		return nil, err
	}
	switch {
	case l.archived:
		return nil, project.ErrArchived
	case !l.editor:
		return nil, ErrForbidden
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM requirement WHERE id = $1 FOR UPDATE`, l.ID); err != nil {
		return nil, fmt.Errorf("hold %s: %w", l.Key, err)
	}
	// Read again under the lock, so a save that landed meanwhile is the one this change is decided on.
	return findByID(ctx, tx, actor, l.ID)
}

// logChange notes a change that writes no revision.
func logChange(ctx context.Context, tx db.DBTX, actor, id uuid.UUID, field ChangeField, from, to string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO requirement_change (org_id, requirement_id, field, from_value, to_value, actor_id)
		VALUES (current_org_id(), $1, $2, $3, $4, $5)`, id, field, from, to, actor); err != nil {
		return fmt.Errorf("log the %s change: %w", field, err)
	}
	return events.Emit(ctx, tx, TopicChanged, map[string]any{"actorId": actor, "requirementId": id, "field": field})
}

func personName(ctx context.Context, tx db.DBTX, id *uuid.UUID) (string, error) {
	if id == nil {
		return "", nil
	}
	var name string
	err := tx.QueryRow(ctx, `SELECT name FROM app_user WHERE id = $1`, *id).Scan(&name)
	return name, err
}

func priorityText(p *Priority) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

// Update changes what in names. A change to the normative content writes the next revision over baseRevision;
// the owner and the priority are logged, and the last save of them wins.
func (s *Service) Update(ctx context.Context, actor uuid.UUID, key string, in UpdateInput) (*Requirement, db.LSN, error) {
	var out *Requirement
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		cur, err := changing(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		if cur.TrashedAt != nil {
			return ErrTrashed
		}
		next := Normative{Title: cur.Title, Statement: cur.Statement, Rationale: cur.Rationale, TypeID: cur.typeID(), Attributes: cur.Attributes}
		if in.Title != nil {
			if next.Title, err = checkTitle(*in.Title); err != nil {
				return err
			}
		}
		if len(in.Statement) > 0 {
			if isNull(in.Statement) {
				return &FieldError{"statement", "A requirement always has a statement. Send an empty document to clear it."}
			}
			if next.Statement, err = checkDocument("statement", in.Statement); err != nil {
				return err
			}
		}
		if len(in.Rationale) > 0 {
			switch {
			case isNull(in.Rationale):
				next.Rationale = nil
			case cur.Kind == KindSection:
				return &FieldError{"rationale", "A section has no rationale. Put its words in the statement."}
			default:
				if next.Rationale, err = checkDocument("rationale", in.Rationale); err != nil {
					return err
				}
			}
		}
		var defs []attribute.Def
		if in.TypeID != nil || len(in.Attributes) > 0 {
			if cur.Kind == KindSection {
				return &FieldError{"typeId", "A section has no type or attributes. Give them to the requirements in it."}
			}
			typeChanged := in.TypeID != nil && (next.TypeID == nil || *in.TypeID != *next.TypeID)
			if typeChanged {
				if err := checkType(ctx, tx, cur.projectID, in.TypeID, next.TypeID); err != nil {
					return err
				}
				next.TypeID = in.TypeID
			}
			if defs, err = attribute.Defs(ctx, tx, cur.projectID); err != nil {
				return err
			}
			if next.Attributes, err = attribute.Merge(defs, next.TypeID, typeChanged, cur.Attributes, in.Attributes, viewerIn(ctx, tx, cur.projectID)); err != nil {
				return err
			}
		}
		if hash := next.Hash(); hash != cur.hash {
			if cur.Kind == KindRequirement {
				if defs == nil {
					if defs, err = attribute.Defs(ctx, tx, cur.projectID); err != nil {
						return err
					}
				}
				if err := attribute.Missing(defs, next.TypeID, next.Attributes); err != nil {
					return err
				}
			}
			if err := s.revise(ctx, tx, actor, cur, next, hash, in); err != nil {
				return err
			}
		}
		if err := s.changeOwner(ctx, tx, actor, cur, in.OwnerID); err != nil {
			return err
		}
		if err := s.changePriority(ctx, tx, actor, cur, in.Priority); err != nil {
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

// revise writes the next revision of cur with the content next, which differs from it.
func (s *Service) revise(ctx context.Context, tx db.DBTX, actor uuid.UUID, cur *loaded, next Normative, hash string, in UpdateInput) error {
	if in.BaseRevision == nil {
		return &FieldError{"baseRevision", "Say which revision you changed, as baseRevision, so a newer one is not overwritten."}
	}
	if *in.BaseRevision != cur.Revision {
		return &ConflictError{Key: cur.Key, Revision: cur.Revision}
	}
	comment, err := checkComment(in.Comment)
	if err != nil {
		return err
	}
	if comment == "" && cur.requireComment {
		return &FieldError{"comment", "This project asks for a comment on every new revision. Say what changed and why."}
	}
	attrs, err := json.Marshal(next.Attributes)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE requirement SET title = $2, statement = $3, rationale = $4, type_id = $5, attributes = $6, normative_hash = $7, updated_by = $8
		WHERE id = $1`, cur.ID, next.Title, next.Statement, next.Rationale, next.TypeID, attrs, hash, actor); err != nil {
		return fmt.Errorf("change %s: %w", cur.Key, err)
	}
	number := cur.Revision + 1
	if _, err := tx.Exec(ctx, `
		INSERT INTO requirement_revision (org_id, requirement_id, number, title, statement, rationale, type_id, attributes, hash, comment, created_by)
		VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		cur.ID, number, next.Title, next.Statement, next.Rationale, next.TypeID, attrs, hash, comment, actor); err != nil {
		return fmt.Errorf("write revision %d of %s: %w", number, cur.Key, err)
	}
	return events.Emit(ctx, tx, TopicRevised, map[string]any{"actorId": actor, "requirementId": cur.ID, "revision": number})
}

func (s *Service) changeOwner(ctx context.Context, tx db.DBTX, actor uuid.UUID, cur *loaded, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var owner *uuid.UUID
	if !isNull(raw) {
		if err := json.Unmarshal(raw, &owner); err != nil {
			return &FieldError{"ownerId", "Pick the owner from the list of people."}
		}
	}
	was := (*uuid.UUID)(nil)
	if cur.Owner != nil {
		was = &cur.Owner.ID
	}
	if (owner == nil) == (was == nil) && (owner == nil || *owner == *was) {
		return nil
	}
	if owner != nil {
		if cur.Kind == KindSection {
			return &FieldError{"ownerId", "A section has no owner. Give one to the requirements in it."}
		}
		if err := checkOwner(ctx, tx, cur.projectID, *owner); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE requirement SET owner_id = $2, updated_by = $3 WHERE id = $1`, cur.ID, owner, actor); err != nil {
		return fmt.Errorf("change the owner of %s: %w", cur.Key, err)
	}
	from, err := personName(ctx, tx, was)
	if err != nil {
		return err
	}
	to, err := personName(ctx, tx, owner)
	if err != nil {
		return err
	}
	return logChange(ctx, tx, actor, cur.ID, ChangeOwner, from, to)
}

func (s *Service) changePriority(ctx context.Context, tx db.DBTX, actor uuid.UUID, cur *loaded, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var priority *Priority
	if !isNull(raw) {
		if err := json.Unmarshal(raw, &priority); err != nil {
			return checkPriority("")
		}
		if err := checkPriority(*priority); err != nil {
			return err
		}
		if cur.Kind == KindSection {
			return &FieldError{"priority", "A section has no priority. Give one to the requirements in it."}
		}
	}
	if priorityText(priority) == priorityText(cur.Priority) {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE requirement SET priority = $2, updated_by = $3 WHERE id = $1`, cur.ID, priority, actor); err != nil {
		return fmt.Errorf("change the priority of %s: %w", cur.Key, err)
	}
	return logChange(ctx, tx, actor, cur.ID, ChangePriority, priorityText(cur.Priority), priorityText(priority))
}
