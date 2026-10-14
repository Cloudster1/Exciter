package baseline

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// side is everything one side of a comparison holds.
type side struct {
	items    []held
	links    []heldLink
	external []heldExternal
}

func scanHeld(row pgx.CollectableRow) (held, error) {
	var (
		h        held
		statusID *uuid.UUID
		status   requirement.StatusRef
	)
	err := row.Scan(&h.ID, &h.Number, &h.Revision, &h.Title, &h.Path, &h.ParentID, &h.Rank, &statusID, &status.Name, &status.Category)
	if statusID != nil {
		status.ID = *statusID
		h.Status = &status
	}
	return h, err
}

// baselineSide reads what a baseline holds, each requirement titled as its revision was.
func baselineSide(ctx context.Context, tx db.DBTX, id uuid.UUID) (*side, error) {
	rows, err := tx.Query(ctx, `
		SELECT i.requirement_id, i.number, i.revision, COALESCE(v.title, ''), i.path, i.parent_id, i.rank, i.status_id,
		       COALESCE(i.status_name, ''), COALESCE(i.status_category, '')
		FROM baseline_item i
		LEFT JOIN requirement_revision v ON v.requirement_id = i.requirement_id AND v.number = i.revision
		WHERE i.baseline_id = $1 ORDER BY i.path`, id)
	if err != nil {
		return nil, fmt.Errorf("read the baseline's items: %w", err)
	}
	s := &side{}
	if s.items, err = pgx.CollectRows(rows, scanHeld); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `SELECT type_id, source_id, target_id, type_name FROM baseline_link WHERE baseline_id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("read the baseline's links: %w", err)
	}
	if s.links, err = pgx.CollectRows(rows, pgx.RowToStructByPos[heldLink]); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `SELECT requirement_id, issue_key, relation FROM baseline_external_link WHERE baseline_id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("read the baseline's Armature links: %w", err)
	}
	s.external, err = pgx.CollectRows(rows, pgx.RowToStructByPos[heldExternal])
	return s, err
}

// nowSide reads what a baseline of the same scope would hold now: the project out of the trash, or what is below
// the baseline's section while that is.
func nowSide(ctx context.Context, tx db.DBTX, of *found) (*side, error) {
	var root *uuid.UUID
	if of.Root != nil {
		root = &of.Root.ID
	}
	rows, err := tx.Query(ctx, requirement.WalkTree+`, scope AS (
			SELECT id FROM requirement WHERE id = $2::uuid AND trashed_at IS NULL
			UNION ALL
			SELECT c.id FROM requirement c JOIN scope s ON c.parent_id = s.id WHERE c.trashed_at IS NULL
		)
		SELECT r.id, r.number, r.revision, r.title, w.path, r.parent_id, r.rank, r.status_id, COALESCE(st.name, ''), COALESCE(st.category, '')
		FROM walk w JOIN requirement r ON r.id = w.id LEFT JOIN req_status st ON st.id = r.status_id
		WHERE $2::uuid IS NULL OR w.id IN (SELECT id FROM scope)
		ORDER BY w.path`, of.projectID, root)
	if err != nil {
		return nil, fmt.Errorf("read the project now: %w", err)
	}
	s := &side{}
	if s.items, err = pgx.CollectRows(rows, scanHeld); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(s.items))
	for i, h := range s.items {
		ids[i] = h.ID
	}
	rows, err = tx.Query(ctx, `
		SELECT l.type_id, l.source_id, l.target_id, lt.name FROM trace_link l JOIN link_type lt ON lt.id = l.type_id
		WHERE l.deleted_at IS NULL AND l.source_id = ANY($1) AND l.target_id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("read the links now: %w", err)
	}
	if s.links, err = pgx.CollectRows(rows, pgx.RowToStructByPos[heldLink]); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `SELECT requirement_id, issue_key, relation FROM armature_issue_link WHERE requirement_id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("read the Armature links now: %w", err)
	}
	s.external, err = pgx.CollectRows(rows, pgx.RowToStructByPos[heldExternal])
	return s, err
}
