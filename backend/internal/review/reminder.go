package review

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
)

// ReminderInterval is how often the worker looks for sessions whose deadline comes within ReminderLead.
const ReminderInterval = time.Minute

// Reminder writes, once per session, the reminder that its deadline is near; the fan-out tells whoever still has
// something to do (#204). The mark is the row's, so several workers never remind twice.
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
			r.log.Warn("review sessions could not be reminded", "error", err)
		} else if n > 0 {
			r.log.Info("review sessions reminded", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Once marks every open session whose deadline is within ReminderLead as reminded, and says how many.
func (r *Reminder) Once(ctx context.Context) (int, error) {
	n := 0
	_, err := r.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			UPDATE review_session SET reminded_at = now()
			WHERE state = 'open' AND reminded_at IS NULL AND deadline IS NOT NULL AND deadline <= now() + $1::interval
			RETURNING id, project_id, moderator_id`, fmt.Sprintf("%d seconds", int(ReminderLead.Seconds())))
		if err != nil {
			return err
		}
		type due struct{ id, project, moderator uuid.UUID }
		var found []due
		for rows.Next() {
			var d due
			var moderator *uuid.UUID
			if err := rows.Scan(&d.id, &d.project, &moderator); err != nil {
				rows.Close()
				return err
			}
			if moderator != nil {
				d.moderator = *moderator
			}
			found = append(found, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, d := range found {
			if err := events.Emit(ctx, tx, TopicSessionReminder, map[string]any{"actorId": d.moderator, "sessionId": d.id, "projectId": d.project}); err != nil {
				return err
			}
		}
		n = len(found)
		return nil
	})
	return n, err
}
