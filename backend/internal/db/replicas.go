package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cloudster1/exciter/backend/internal/config"
)

// readConn is a replica connection fit to serve a read made with ctx, or nil
// for the primary.
func (c *Cluster) readConn(ctx context.Context) *pgxpool.Conn {
	// Fitness is asked of the connection itself: behind a load balanced read service two connections of one
	// pool can reach replicas at different positions, so no figure about the pool holds for all of them.
	p := pinFrom(ctx)
	r, outcome := pickReplica(c.replicas, int(c.rr.Add(1)), p)
	if outcome != routeReplica {
		c.countFallback(outcome)
		return nil
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		r.unavailableFallbacks.Add(1)
		c.countFallback(routeNoHealthy)
		return nil
	}
	s, err := sampleConn(ctx, conn)
	if err != nil {
		conn.Release()
		r.unavailableFallbacks.Add(1)
		c.countFallback(routeNoHealthy)
		return nil
	}
	if outcome := admit(s, p, c.maxLag); outcome != routeReplica {
		conn.Release()
		if outcome == routeStale {
			r.staleFallbacks.Add(1)
		} else {
			r.lagFallbacks.Add(1)
		}
		c.countFallback(outcome)
		return nil
	}
	c.readsToReplica.Add(1)
	return conn
}

func (c *Cluster) countFallback(outcome routeOutcome) {
	switch outcome {
	case routeStale:
		c.staleFallbacks.Add(1)
	case routeLagging:
		c.lagFallbacks.Add(1)
	case routeNoHealthy:
		c.noHealthyReplica.Add(1)
	}
	c.readsToPrimary.Add(1)
}

// routeOutcome explains why a read was routed where it was.
type routeOutcome int

const (
	routeReplica   routeOutcome = iota // served by a replica
	routePinned                        // caller demanded the primary
	routeStale                         // the replica has not replayed the caller's own write
	routeLagging                       // the replica is further behind than allowed
	routeNoHealthy                     // no replica is currently usable
)

// pickReplica chooses the replica to try for a read, or says why none can
// serve it. Freshness is judged later, on the connection the read gets.
func pickReplica(replicas []*replicaState, start int, p pin) (*replicaState, routeOutcome) {
	if p.forcePrimary {
		return nil, routePinned
	}
	n := len(replicas)
	if start < 0 {
		start = -start
	}
	for i := 0; i < n; i++ {
		if r := replicas[(start+i)%n]; r.healthy.Load() {
			return r, routeReplica
		}
	}
	return nil, routeNoHealthy
}

// connSample is what one connection says about the node it reached.
type connSample struct {
	inRecovery bool
	replayLSN  LSN
	lag        time.Duration
	receiver   string
}

// admit decides whether a replica connection may serve a read. Falling back
// to the primary is always correct, only more expensive.
func admit(s connSample, p pin, maxLag time.Duration) routeOutcome {
	if !s.inRecovery {
		// Promoted since it joined the pool: it has everything it ever saw.
		return routeReplica
	}
	if p.requiredLSN != 0 && s.replayLSN < p.requiredLSN {
		return routeStale
	}
	// A standby whose receiver dropped reports zero lag forever, having replayed all it received; an empty
	// status means this role may not read the view, so the lag figure is trusted instead.
	if s.receiver != "" && s.receiver != "streaming" {
		return routeLagging
	}
	if maxLag > 0 && s.lag > maxLag {
		return routeLagging
	}
	return routeReplica
}

func (s connSample) reason(maxLag time.Duration) string {
	if s.receiver != "" && s.receiver != "streaming" {
		return fmt.Sprintf("wal receiver is %q, not streaming", s.receiver)
	}
	return fmt.Sprintf("replication lag %s exceeds %s", s.lag, maxLag)
}

func (c *Cluster) healthLoop(ctx context.Context, interval time.Duration) {
	defer close(c.stopped)
	if interval <= 0 {
		interval = config.DefaultHealthInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.checkAll(ctx)
		}
	}
}

func (c *Cluster) checkAll(ctx context.Context) {
	for _, r := range c.replicas {
		c.check(ctx, r)
	}
}

// replicaHealthSQL's CASE arms avoid functions that fail on the other kind of node;
// lag is zero once all received WAL is replayed, since an idle standby's clock stops.
const replicaHealthSQL = `
SELECT pg_is_in_recovery(),
       CASE WHEN pg_is_in_recovery()
            THEN COALESCE(pg_last_wal_replay_lsn(), '0/0'::pg_lsn)
            ELSE pg_current_wal_lsn()
       END::text,
       CASE
            WHEN NOT pg_is_in_recovery() THEN 0
            WHEN pg_last_wal_receive_lsn() IS NULL THEN 0
            WHEN pg_last_wal_receive_lsn() = pg_last_wal_replay_lsn() THEN 0
            ELSE GREATEST(0, EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp())))
       END,
       COALESCE((SELECT status FROM pg_stat_wal_receiver LIMIT 1), '')`

// sampleConn asks one connection where its node stands.
func sampleConn(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}) (connSample, error) {
	var (
		s          connSample
		lsnText    string
		lagSeconds float64
	)
	if err := q.QueryRow(ctx, replicaHealthSQL).Scan(&s.inRecovery, &lsnText, &lagSeconds, &s.receiver); err != nil {
		return s, err
	}
	lsn, err := ParseLSN(lsnText)
	if err != nil {
		return s, err
	}
	s.replayLSN = lsn
	// Clock skew between primary and standby can make the figure negative.
	s.lag = max(time.Duration(lagSeconds*float64(time.Second)), 0)
	return s, nil
}

// check samples several connections of a replica's pool at once, so behind a
// load balanced service it sees more than one replica.
func (c *Cluster) check(ctx context.Context, r *replicaState) {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	// The pool leaves the rotation only when no sample is fit to serve, since
	// every read checks its own connection anyway; it reports the worst figures.

	n := min(c.samples, int(r.pool.Config().MaxConns))
	conns := make([]*pgxpool.Conn, 0, n)
	defer func() {
		for _, conn := range conns {
			conn.Release()
		}
	}()
	var lastErr error
	for range n {
		conn, err := r.pool.Acquire(ctx)
		if err != nil {
			lastErr = err
			break
		}
		conns = append(conns, conn)
	}

	var (
		fit, sampled bool
		worst        connSample
		reason       string
	)
	for _, conn := range conns {
		s, err := sampleConn(ctx, conn)
		if err != nil {
			lastErr = err
			continue
		}
		if !sampled || s.replayLSN < worst.replayLSN {
			worst.replayLSN = s.replayLSN
		}
		worst.lag = max(worst.lag, s.lag)
		sampled = true
		if admit(s, pin{}, c.maxLag) == routeReplica {
			fit = true
		} else {
			reason = s.reason(c.maxLag)
		}
	}
	if !sampled {
		msg := "no connection could be sampled"
		if lastErr != nil {
			msg = lastErr.Error()
		}
		c.markUnhealthy(r, msg)
		return
	}
	r.replayLSN.Store(uint64(worst.replayLSN))
	r.lagMillis.Store(worst.lag.Milliseconds())
	if !fit {
		c.markUnhealthy(r, reason)
		return
	}
	if was := r.healthy.Swap(true); !was {
		r.lastErr.Store(nil)
		c.log.Info("replica healthy", "replica", r.name, "lag", worst.lag)
	}
}

func (c *Cluster) markUnhealthy(r *replicaState, reason string) {
	r.lastErr.Store(&reason)
	if was := r.healthy.Swap(false); was {
		c.log.Warn("replica unhealthy", "replica", r.name, "reason", reason)
	}
}
