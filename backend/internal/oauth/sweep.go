package oauth

import (
	"context"
	"log/slog"
	"time"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// SweepInterval is how often the worker removes what has run out; nothing waits on it, since every read checks expiry.
const SweepInterval = 10 * time.Minute

// Sweeper removes requests, codes and tokens that ran out, grants left with neither, and clients nobody holds.
type Sweeper struct {
	db       *db.Cluster
	log      *slog.Logger
	interval time.Duration
}

func NewSweeper(cluster *db.Cluster, log *slog.Logger) *Sweeper {
	return &Sweeper{db: cluster, log: log, interval: SweepInterval}
}

// Run sweeps on start and then every interval until ctx ends.
func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		if err := s.Once(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("what OAuth no longer needs could not all be removed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Once sweeps once, as the admin role, since nobody is signed in to these rows.
func (s *Sweeper) Once(ctx context.Context) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		sweeps := []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM oauth_request WHERE expires_at <= now()`, nil},
			{`DELETE FROM oauth_code WHERE expires_at <= now()`, nil},
			{`DELETE FROM oauth_token WHERE expires_at <= now()`, nil},
			// A grant whose code was never traded, or whose last refresh token ran out, can no longer be used.
			{`DELETE FROM oauth_grant g WHERE NOT EXISTS (SELECT 1 FROM oauth_code c WHERE c.grant_id = g.id)
				AND NOT EXISTS (SELECT 1 FROM oauth_token t WHERE t.grant_id = g.id AND t.kind = 'refresh')`, nil},
			// A client that never led to a grant goes after a day, and one whose last grant ended after RefreshTTL.
			// A partner stays until an administrator removes it, connected or not.
			{`DELETE FROM oauth_client c WHERE c.kind <> 'partner' AND NOT EXISTS (SELECT 1 FROM oauth_grant g WHERE g.client_id = c.id)
				AND NOT EXISTS (SELECT 1 FROM oauth_request r WHERE r.client_id = c.id)
				AND CASE WHEN c.idle_since IS NULL THEN c.created_at < now() - make_interval(secs => $1)
				         ELSE c.idle_since < now() - make_interval(secs => $2) END`,
				[]any{UnusedClientTTL.Seconds(), RefreshTTL.Seconds()}},
		}
		for _, sweep := range sweeps {
			if _, err := tx.Exec(ctx, sweep.sql, sweep.args...); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}
