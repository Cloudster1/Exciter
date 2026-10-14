package audit

import (
	"context"
	"log/slog"
	"time"

	"github.com/cloudster1/exciter/backend/internal/db"
)

const (
	// MinRetention is the least the log keeps; audit_log_prune refuses a
	// cutoff younger than this, whatever the configuration says.
	MinRetention = 24 * time.Hour
	// DefaultRetention is a year, as Armature keeps its log.
	DefaultRetention = 365 * 24 * time.Hour
	// RetentionInterval is how often the worker prunes.
	RetentionInterval = 24 * time.Hour
)

// Retention deletes entries older than its window, as exciter_admin: the
// app role may not delete from the log.
type Retention struct {
	db       *db.Cluster
	keep     time.Duration
	log      *slog.Logger
	interval time.Duration
	now      func() time.Time
}

// NewRetention prunes entries older than keep; zero keeps them forever.
func NewRetention(cluster *db.Cluster, keep time.Duration, log *slog.Logger) *Retention {
	return &Retention{db: cluster, keep: keep, log: log, interval: RetentionInterval, now: time.Now}
}

// Run prunes on start and then every interval until ctx ends.
func (r *Retention) Run(ctx context.Context) {
	if r.keep <= 0 {
		r.log.Info("the audit log is kept forever: EXCITER_RETAIN_AUDIT is 0")
		return
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		n, err := r.Once(ctx)
		if err != nil && ctx.Err() == nil {
			r.log.Warn("old audit entries could not all be pruned", "error", err)
		}
		if n > 0 {
			r.log.Info("old audit entries pruned", "count", n, "keep", r.keep)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Once deletes what is past the window and says how many entries went.
func (r *Retention) Once(ctx context.Context) (int64, error) {
	if r.keep <= 0 {
		return 0, nil
	}
	cutoff := r.now().Add(-r.keep)
	var n int64
	_, err := r.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT audit_log_prune($1)`, cutoff).Scan(&n)
	})
	return n, err
}
