package attribute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute/formula"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// Agreed limits of the recompute (#242).
const (
	// RecomputeBatch is how many marked requirements one transaction works out.
	RecomputeBatch = 200
	// DefaultRecomputeInterval is how long the worker waits when nothing was marked.
	DefaultRecomputeInterval = 2 * time.Second
)

// Recomputer works out the computed values the database marked, project by project, in the worker.
type Recomputer struct {
	db       *db.Cluster
	log      *slog.Logger
	interval time.Duration
	now      func() time.Time
}

func NewRecomputer(cluster *db.Cluster, log *slog.Logger, interval time.Duration) *Recomputer {
	if interval <= 0 {
		interval = DefaultRecomputeInterval
	}
	return &Recomputer{db: cluster, log: log, interval: interval, now: time.Now}
}

// Run works through what is marked until the context ends, at once while there is more and every interval else.
func (r *Recomputer) Run(ctx context.Context) {
	for {
		n, err := r.Once(ctx)
		if err != nil {
			r.log.Warn("computed attributes could not be worked out", "error", err)
		}
		wait := r.interval
		if n > 0 && err == nil {
			wait = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// Once works out one batch of one project's marked requirements, answering how many.
func (r *Recomputer) Once(ctx context.Context) (int, error) {
	done := 0
	_, err := r.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		project, err := r.claim(ctx, tx)
		if err != nil || project == uuid.Nil {
			return err
		}
		done, err = r.batch(ctx, tx, project)
		return err
	})
	return done, err
}

// claim picks the project marked longest ago that no other worker holds, under a lock until the transaction ends.
func (r *Recomputer) claim(ctx context.Context, tx db.DBTX) (uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT project_id FROM computed_dirty GROUP BY project_id ORDER BY min(since) LIMIT 10`)
	if err != nil {
		return uuid.Nil, err
	}
	projects, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return uuid.Nil, err
	}
	for _, p := range projects {
		var got bool
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('computed ' || $1::text, 0))`, p).Scan(&got); err != nil {
			return uuid.Nil, err
		}
		if got {
			return p, nil
		}
	}
	return uuid.Nil, nil
}

type marked struct {
	id    uuid.UUID
	steps int
}

func (r *Recomputer) batch(ctx context.Context, tx db.DBTX, project uuid.UUID) (int, error) {
	// The marks taken go at once; a change marking a requirement meanwhile appends another, never waiting (#248).
	rows, err := tx.Query(ctx, `
		WITH picked AS (
			SELECT requirement_id FROM computed_dirty WHERE project_id = $1
			GROUP BY requirement_id ORDER BY min(steps), min(id) LIMIT $2)
		DELETE FROM computed_dirty d USING picked p WHERE d.project_id = $1 AND d.requirement_id = p.requirement_id
		RETURNING d.requirement_id, d.steps`, project, RecomputeBatch)
	if err != nil {
		return 0, err
	}
	taken, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (marked, error) {
		var m marked
		return m, row.Scan(&m.id, &m.steps)
	})
	if err != nil || len(taken) == 0 {
		return 0, err
	}
	// A requirement marked more than once is worked out once, as far along as its freshest mark says.
	steps := map[uuid.UUID]int{}
	var ids []uuid.UUID
	for _, m := range taken {
		if was, seen := steps[m.id]; !seen {
			ids, steps[m.id] = append(ids, m.id), m.steps
		} else if m.steps < was {
			steps[m.id] = m.steps
		}
	}
	stopped := map[uuid.UUID]bool{}
	for _, id := range ids {
		stopped[id] = steps[id] >= formula.MaxSteps
	}
	w, err := newWorld(ctx, tx, project, day(r.now()))
	if err != nil {
		return 0, err
	}
	if w == nil {
		_, err := tx.Exec(ctx, `DELETE FROM requirement_computed WHERE project_id = $1`, project)
		return len(ids), err
	}
	results, err := w.work(ctx, ids, stopped)
	if err != nil {
		return 0, err
	}
	var onward []marked
	for _, id := range ids {
		res, ok := results[id]
		if !ok {
			continue
		}
		values, err := json.Marshal(res.values)
		if err != nil {
			return 0, err
		}
		problems, err := json.Marshal(res.errors)
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO requirement_computed (requirement_id, project_id, "values", errors, computed_at) VALUES ($1, $2, $3, $4, now())
			ON CONFLICT (requirement_id) DO UPDATE SET project_id = EXCLUDED.project_id, "values" = EXCLUDED."values",
				errors = EXCLUDED.errors, computed_at = now()`, id, project, values, problems); err != nil {
			return 0, fmt.Errorf("keep the computed values: %w", err)
		}
		// A change of its own fields reaches whoever reads it; a value worked out again only when it moved.
		if !stopped[id] && (steps[id] == 0 || res.changed) {
			onward = append(onward, marked{id, steps[id] + 1})
		}
	}
	if err := r.mark(ctx, tx, w, project, onward); err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `UPDATE attribute_recompute SET finished_at = now()
		WHERE project_id = $1 AND finished_at IS NULL AND NOT EXISTS (SELECT 1 FROM computed_dirty WHERE project_id = $1)`, project)
	return len(ids), err
}

// mark marks again whatever reads the requirements given: their parents, and those whose links lead to them.
func (r *Recomputer) mark(ctx context.Context, tx db.DBTX, w *world, project uuid.UUID, from []marked) error {
	if len(from) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(from))
	steps := make([]int, len(from))
	for i, m := range from {
		ids[i], steps[i] = m.id, m.steps
	}
	reach := `SELECT r.parent_id AS id, f.steps FROM unnest($1::uuid[], $2::int[]) AS f (id, steps) JOIN requirement r ON r.id = f.id
		WHERE r.parent_id IS NOT NULL`
	for _, way := range w.ways {
		// A requirement reading its outgoing links reads their targets, so a target's change reaches the sources.
		reader, read := "source_id", "target_id"
		if !way.out {
			reader, read = "target_id", "source_id"
		}
		reach += fmt.Sprintf(` UNION ALL SELECT l.%s, f.steps FROM unnest($1::uuid[], $2::int[]) AS f (id, steps)
			JOIN trace_link l ON l.%s = f.id AND l.deleted_at IS NULL AND l.type_id = '%s'`, reader, read, way.typeID)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO computed_dirty (requirement_id, project_id, steps)
		SELECT t.id, $3, min(t.steps) FROM (`+reach+`) t JOIN requirement r ON r.id = t.id AND r.project_id = $3
		GROUP BY t.id`, ids, steps, project)
	return err
}

// Values answers a requirement's computed values as the reader sees them, by attribute id.
func Values(ctx context.Context, tx db.DBTX, id uuid.UUID) (map[string]json.RawMessage, error) {
	var out map[string]json.RawMessage
	err := tx.QueryRow(ctx, `SELECT requirement_computed_of($1)`, id).Scan(&out)
	if errors.Is(err, pgx.ErrNoRows) || out == nil {
		return map[string]json.RawMessage{}, nil
	}
	return out, err
}
