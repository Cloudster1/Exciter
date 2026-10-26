package attribute

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// DefaultDayTurnInterval is how often the worker looks whether the day has moved on (#360).
const DefaultDayTurnInterval = time.Minute

// DayTurner marks again, once a day, the requirements whose computed attributes call today(), since nothing they
// read changes when the day does.
type DayTurner struct {
	db       *db.Cluster
	log      *slog.Logger
	interval time.Duration
	now      func() time.Time
}

// NewDayTurner reads the day from now, or from the clock when now is nil.
func NewDayTurner(cluster *db.Cluster, log *slog.Logger, interval time.Duration, now func() time.Time) *DayTurner {
	if interval <= 0 {
		interval = DefaultDayTurnInterval
	}
	if now == nil {
		now = time.Now
	}
	return &DayTurner{db: cluster, log: log, interval: interval, now: now}
}

// Run turns the projects to the new day every interval until the context ends.
func (d *DayTurner) Run(ctx context.Context) {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		if n, err := d.Once(ctx); err != nil && ctx.Err() == nil {
			d.log.Warn("computed attributes could not be turned to the new day", "error", err)
		} else if n > 0 {
			d.log.Info("computed attributes turned to the new day", "projects", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Once marks what today() reads in every project not yet turned to today, answering how many projects it marked.
func (d *DayTurner) Once(ctx context.Context) (int, error) {
	today := day(d.now())
	turned := 0
	_, err := d.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		// Only a formula that names today can call it; the rest are never compiled here.
		rows, err := tx.Query(ctx, `
			SELECT DISTINCT a.project_id FROM attribute_def a
			WHERE a.formula ILIKE '%today%'
			  AND NOT EXISTS (SELECT 1 FROM computed_day c WHERE c.project_id = a.project_id AND c.day >= $1)`, today)
		if err != nil {
			return err
		}
		projects, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, project := range projects {
			marked, err := d.turn(ctx, tx, project, today)
			if err != nil {
				return err
			}
			if marked {
				turned++
			}
		}
		return nil
	})
	return turned, err
}

// turn keeps the project's new day and marks the requirements its today() formulas show on; false when another
// worker turned it already or none of its formulas calls today().
func (d *DayTurner) turn(ctx context.Context, tx db.DBTX, project uuid.UUID, today time.Time) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO computed_day (project_id, day) VALUES ($1, $2)
		ON CONFLICT (project_id) DO UPDATE SET day = EXCLUDED.day WHERE computed_day.day < EXCLUDED.day`, project, today)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	defs, err := Defs(ctx, tx, project)
	if err != nil {
		return false, err
	}
	links, err := linkTypesOf(ctx, tx, project)
	if err != nil {
		return false, err
	}
	marked := false
	// What reads such a value on another requirement is marked by the recompute once the value is worked out.
	for id, p := range programs(defs, links) {
		if !p.Reads.Today {
			continue
		}
		def, _ := findDef(defs, id)
		if _, err := tx.Exec(ctx, `
			INSERT INTO computed_dirty (requirement_id, project_id)
			SELECT r.id, r.project_id FROM requirement r
			WHERE r.project_id = $1 AND r.trashed_at IS NULL
			  AND ((r.kind = 'section' AND $2) OR (r.kind <> 'section' AND (coalesce(cardinality($3::uuid[]), 0) = 0 OR r.type_id = ANY ($3))))`,
			project, def.OnSections, def.TypeIDs); err != nil {
			return false, err
		}
		marked = true
	}
	return marked, nil
}
