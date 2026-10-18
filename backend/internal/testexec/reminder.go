package testexec

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
)

// ReminderInterval is how often the worker looks for executions due within ReminderLead.
const (
	ReminderInterval = time.Minute
	ReminderLead     = 24 * time.Hour
)

// Reminder writes, once per execution, the reminder that it is due soon; the fan-out tells its testers. The mark is
// the row's, so several workers never remind twice.
type Reminder struct {
	db  *db.Cluster
	log *slog.Logger
}

func NewReminder(cluster *db.Cluster, log *slog.Logger) *Reminder {
	return &Reminder{db: cluster, log: log}
}

// Run reminds what is due every ReminderInterval until ctx ends.
func (r *Reminder) Run(ctx context.Context) {
	ticker := time.NewTicker(ReminderInterval)
	defer ticker.Stop()
	for {
		if n, err := r.Once(ctx); err != nil && ctx.Err() == nil {
			r.log.Warn("test executions could not be reminded", "error", err)
		} else if n > 0 {
			r.log.Info("test executions reminded", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Once marks every open execution due within ReminderLead as reminded, and says how many.
func (r *Reminder) Once(ctx context.Context) (int, error) {
	n := 0
	_, err := r.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			UPDATE test_execution SET reminded_at = now()
			WHERE state = 'open' AND reminded_at IS NULL AND due_at IS NOT NULL AND due_at <= now() + $1::interval
			RETURNING id, project_id`, fmt.Sprintf("%d seconds", int(ReminderLead.Seconds())))
		if err != nil {
			return err
		}
		type due struct{ id, project uuid.UUID }
		var found []due
		for rows.Next() {
			var d due
			if err := rows.Scan(&d.id, &d.project); err != nil {
				rows.Close()
				return err
			}
			found = append(found, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, d := range found {
			if err := events.Emit(ctx, tx, TopicReminder, map[string]any{"executionId": d.id, "projectId": d.project}); err != nil {
				return err
			}
		}
		n = len(found)
		return nil
	})
	return n, err
}
