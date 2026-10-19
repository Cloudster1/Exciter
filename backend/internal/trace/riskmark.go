package trace

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// riskMarks reads the mark of each of the requirements given that is a risk (#261): the level it stands at and its
// state, as risk_view answers them.
func riskMarks(ctx context.Context, tx db.DBTX, ids []uuid.UUID) (map[uuid.UUID]*requirement.RiskMark, error) {
	out := map[uuid.UUID]*requirement.RiskMark{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT k.id, m FROM unnest($1::uuid[]) k(id) CROSS JOIN LATERAL risk_mark(k.id) m WHERE m IS NOT NULL`, ids)
	if err != nil {
		return nil, fmt.Errorf("read the risks' marks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id  uuid.UUID
			raw []byte
		)
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var m requirement.RiskMark
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		out[id] = &m
	}
	return out, rows.Err()
}

// risks marks each risk the walk reached with its level and state.
func (d *dbGraph) risks(ctx context.Context, b *graphBuilder) error {
	var ids []uuid.UUID
	for _, n := range b.g.Nodes {
		if n.Requirement != nil && n.Requirement.Kind == requirement.KindRequirement {
			ids = append(ids, n.Requirement.ID)
		}
	}
	marks, err := riskMarks(ctx, d.tx, ids)
	if err != nil {
		return err
	}
	for id, m := range marks {
		b.g.Nodes[b.at[id.String()]].Risk = m
	}
	return nil
}
