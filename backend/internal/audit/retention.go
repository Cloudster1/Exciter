package audit

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/tenant"
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

// Retention deletes entries older than its window, organization by
// organization, as exciter_admin: the app role may not delete from the log.
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

// Once deletes what is past the window in every organization and says how
// many entries went; one organization failing does not stop the others.
func (r *Retention) Once(ctx context.Context) (int64, error) {
	if r.keep <= 0 {
		return 0, nil
	}
	cutoff := r.now().Add(-r.keep)
	// Finding the organizations is the one step that crosses them; each
	// deletion happens inside its own, as Armature's sweep does.
	var orgs []uuid.UUID
	err := r.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT org_id FROM audit_log WHERE created_at < $1`, cutoff)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			orgs = append(orgs, id)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	var total int64
	var firstErr error
	for _, org := range orgs {
		var n int64
		_, err := r.db.WriteAdmin(tenant.WithOrg(ctx, tenant.Org{ID: org}), func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT audit_log_prune($1)`, cutoff).Scan(&n)
		})
		if err != nil && firstErr == nil {
			firstErr = err
		}
		total += n
	}
	return total, firstErr
}
