package attachment

import (
	"context"
	"log/slog"
	"time"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// reapBatch is how many objects one pass removes before the next look.
const reapBatch = 200

// DefaultReapInterval is how often the worker looks for objects to remove.
const DefaultReapInterval = time.Minute

type writeFunc func(context.Context, func(context.Context, db.DBTX) error) (db.LSN, error)
type readFunc func(context.Context, func(context.Context, db.DBTX) error) error

// reap removes one object and then its tombstone, with whichever transaction the caller has: a tenant's own
// after a delete, the admin's from the reaper.
func (s *Service) reap(ctx context.Context, key string, write writeFunc) error {
	if err := s.store.Delete(ctx, key); err != nil {
		return err
	}
	_, err := write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM attachment_tombstone WHERE object_key = $1`, key)
		return err
	})
	return err
}

// reapBatchWith removes the objects behind up to one batch of the tombstones read sees. A failure on one
// object leaves its tombstone for next time.
func (s *Service) reapBatchWith(ctx context.Context, read readFunc, write writeFunc) (int, error) {
	var keys []string
	err := read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT object_key FROM attachment_tombstone ORDER BY created_at LIMIT $1`, reapBatch)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				return err
			}
			keys = append(keys, key)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, key := range keys {
		if err := s.reap(ctx, key, write); err != nil {
			s.log.Warn("attachment object could not be removed yet", "key", key, "error", err)
			continue
		}
		removed++
	}
	return removed, nil
}

// Reaper removes the bytes behind tombstones across every organization, in the worker: those of purged
// requirements and deleted projects, what a delete missed, and what a deleted organization left.
type Reaper struct {
	service  *Service
	log      *slog.Logger
	interval time.Duration
}

func NewReaper(service *Service, log *slog.Logger, interval time.Duration) *Reaper {
	if interval <= 0 {
		interval = DefaultReapInterval
	}
	return &Reaper{service: service, log: log, interval: interval}
}

// Run reaps every interval until the context ends.
func (r *Reaper) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		if n, err := r.Once(ctx); err != nil {
			r.log.Warn("attachment reaper failed", "error", err)
		} else if n > 0 {
			r.log.Info("attachment objects removed", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Once removes the objects behind up to one batch of tombstones.
func (r *Reaper) Once(ctx context.Context) (int, error) {
	return r.service.reapBatchWith(ctx, r.service.db.ReadAdmin, r.service.db.WriteAdmin)
}
