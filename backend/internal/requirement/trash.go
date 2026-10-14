package requirement

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
)

// trashedNote is how the activity log words a requirement going to the trash and coming back.
const (
	trashedNote  = "trashed"
	restoredNote = "restored"
)

// Trash moves a requirement to its project's trash; one already there stays as it is.
func (s *Service) Trash(ctx context.Context, actor uuid.UUID, key string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		cur, err := changing(ctx, tx, actor, key)
		if err != nil || cur.TrashedAt != nil {
			return err
		}
		// Everything below it goes along as one item, keeping its parent and rank for the restore.
		below, err := subtree(ctx, tx, cur.projectID, cur.ID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE requirement SET trashed_at = now(), trashed_by = $2, trash_id = $3, updated_by = $2 WHERE id = ANY ($1)`, below, actor, cur.ID); err != nil {
			return fmt.Errorf("trash %s: %w", cur.Key, err)
		}
		if err := logChange(ctx, tx, actor, cur.ID, ChangeTrash, "", trashedNote); err != nil {
			return err
		}
		return events.Emit(ctx, tx, TopicTrashed, map[string]any{"actorId": actor, "requirementId": cur.ID})
	})
}

// trashOf reads a project for its trash, which its editors open and its administrators empty.
func trashOf(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectKey string, purge bool) (*project.Project, error) {
	p, err := project.Find(ctx, tx, actor, projectKey)
	switch {
	case err != nil:
		return nil, err
	case p.ArchivedAt != nil:
		return nil, project.ErrArchived
	case purge && !p.Can.Administer:
		return nil, ErrNotAdministrator
	case !p.Can.Edit:
		return nil, ErrForbidden
	}
	return p, nil
}

// TrashItems lists what went to a project's trash, the latest first, each with what went with it.
func (s *Service) TrashItems(ctx context.Context, actor uuid.UUID, projectKey string) ([]TrashItem, error) {
	out := []TrashItem{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := trashOf(ctx, tx, actor, projectKey, false)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT r.id, r.number, r.kind, r.title, r.trashed_at, COALESCE(u.name, ''),
			       r.parent_id IS NULL OR EXISTS (SELECT 1 FROM requirement x WHERE x.id = r.parent_id AND x.trashed_at IS NULL),
			       (SELECT count(*)::int FROM requirement x WHERE x.trash_id = r.id)
			FROM requirement r LEFT JOIN app_user u ON u.id = r.trashed_by
			WHERE r.project_id = $1 AND r.trash_id = r.id
			ORDER BY r.trashed_at DESC, r.number DESC`, p.ID)
		if err != nil {
			return err
		}
		items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (TrashItem, error) {
			var (
				item   TrashItem
				number int
			)
			err := row.Scan(&item.ID, &number, &item.Kind, &item.Title, &item.TrashedAt, &item.TrashedByName, &item.ParentInTree, &item.Requirements)
			item.Key = Key(p.Key, number)
			return item, err
		})
		if items != nil {
			out = items
		}
		return err
	})
	return out, err
}

// Restore puts a trashed requirement, and what went with it, back.
func (s *Service) Restore(ctx context.Context, actor uuid.UUID, projectKey string, id uuid.UUID) (*Requirement, db.LSN, error) {
	var out *Requirement
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := trashOf(ctx, tx, actor, projectKey, false)
		if err != nil {
			return err
		}
		// Back under its parent, or at the top when the parent went to the trash on its own or for good.
		var parentGone bool
		if err := tx.QueryRow(ctx, `
			SELECT r.parent_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM requirement x WHERE x.id = r.parent_id AND x.trashed_at IS NULL)
			FROM requirement r WHERE r.project_id = $1 AND r.id = $2 AND r.trash_id = r.id`, p.ID, id).Scan(&parentGone); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if parentGone {
			at, err := lastAtTop(ctx, tx, p.ID)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE requirement SET parent_id = NULL, rank = $2 WHERE id = $1`, id, at); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `
			UPDATE requirement SET trashed_at = NULL, trashed_by = NULL, trash_id = NULL, updated_by = $3
			WHERE project_id = $1 AND trash_id = $2 RETURNING id`, p.ID, id, actor)
		if err != nil {
			return err
		}
		restored, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		if len(restored) == 0 {
			return ErrNotFound
		}
		for _, each := range restored {
			if err := logChange(ctx, tx, actor, each, ChangeTrash, trashedNote, restoredNote); err != nil {
				return err
			}
			if err := events.Emit(ctx, tx, TopicRestored, map[string]any{"actorId": actor, "requirementId": each}); err != nil {
				return err
			}
		}
		l, err := findByID(ctx, tx, actor, id)
		if err == nil {
			out = &l.Requirement
		}
		return err
	})
	return out, lsn, err
}

// Purge deletes a trashed requirement, what went with it and their history for good.
func (s *Service) Purge(ctx context.Context, actor uuid.UUID, projectKey string, id uuid.UUID) (db.LSN, error) {
	return s.purge(ctx, actor, projectKey, &id)
}

// EmptyTrash deletes everything in a project's trash for good.
func (s *Service) EmptyTrash(ctx context.Context, actor uuid.UUID, projectKey string) (db.LSN, error) {
	return s.purge(ctx, actor, projectKey, nil)
}

// purge deletes one item of the trash, or all of it when item is nil, and keeps in the audit log which keys
// went, since their history goes with them.
func (s *Service) purge(ctx context.Context, actor uuid.UUID, projectKey string, item *uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := trashOf(ctx, tx, actor, projectKey, true)
		if err != nil {
			return err
		}
		// What went with a root goes by the cascade on trash_id; the count is taken first, to say it.
		var gone int
		if err := tx.QueryRow(ctx, `
			SELECT count(*)::int FROM requirement x JOIN requirement r ON r.id = x.trash_id
			WHERE r.project_id = $1 AND r.trash_id = r.id AND ($2::uuid IS NULL OR r.id = $2)`,
			p.ID, item).Scan(&gone); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			DELETE FROM requirement WHERE project_id = $1 AND trash_id = id AND ($2::uuid IS NULL OR id = $2)
			RETURNING number`, p.ID, item)
		if err != nil {
			return err
		}
		numbers, err := pgx.CollectRows(rows, pgx.RowTo[int])
		if err != nil {
			return err
		}
		if item != nil && len(numbers) == 0 {
			return ErrNotFound
		}
		slices.Sort(numbers)
		keys := make([]string, 0, len(numbers))
		for _, n := range numbers {
			keys = append(keys, Key(p.Key, n))
		}
		action := audit.ActionTrashEmptied
		target := p.ID
		targetType := "project"
		if item != nil {
			action, target, targetType = audit.ActionRequirementPurged, *item, "requirement"
		}
		return audit.Write(ctx, tx, audit.Entry{Action: action, TargetType: targetType, TargetID: &target, Actor: actor,
			Data: map[string]any{"project": p.Key, "keys": keys, "requirements": gone}})
	})
}
