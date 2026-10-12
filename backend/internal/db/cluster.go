// Package db owns the Postgres connection topology: one writable primary and a
// rotation of read replicas, routed so a user's own writes stay visible to them.
package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cloudster1/exciter/backend/internal/config"
)

// Timeouts for the cluster's own round trips, which must not hang a boot or a
// health pass on a database that has gone quiet.
const (
	pingTimeout        = 5 * time.Second
	healthCheckTimeout = 3 * time.Second
)

// DBTX is the query surface shared by pgxpool.Pool and pgx.Tx, so the same
// repository code runs on the primary or a replica, in a transaction or not.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// replicaState tracks one read replica's health and replay position. Request
// goroutines read it while the health loop writes it, so every field is atomic.
type replicaState struct {
	name string
	pool *pgxpool.Pool

	healthy   atomic.Bool
	replayLSN atomic.Uint64
	lagMillis atomic.Int64
	lastErr   atomic.Pointer[string]

	// Reads this replica was chosen for and handed to the primary, by why.
	staleFallbacks       atomic.Uint64
	lagFallbacks         atomic.Uint64
	unavailableFallbacks atomic.Uint64
}

func (r *replicaState) snapshot() ReplicaStatus {
	var errStr string
	if p := r.lastErr.Load(); p != nil {
		errStr = *p
	}
	return ReplicaStatus{
		Name:      r.name,
		Healthy:   r.healthy.Load(),
		ReplayLSN: LSN(r.replayLSN.Load()),
		Lag:       time.Duration(r.lagMillis.Load()) * time.Millisecond,
		LastError: errStr,
		Fallbacks: map[string]uint64{
			FallbackStale:       r.staleFallbacks.Load(),
			FallbackLagging:     r.lagFallbacks.Load(),
			FallbackUnavailable: r.unavailableFallbacks.Load(),
		},
	}
}

// ReplicaStatus is a point in time view of one replica, for health endpoints.
type ReplicaStatus struct {
	Name      string        `json:"name"`
	Healthy   bool          `json:"healthy"`
	ReplayLSN LSN           `json:"-"`
	Lag       time.Duration `json:"-"`
	LastError string        `json:"lastError,omitempty"`
	// Fallbacks counts the reads this replica was picked for and handed to
	// the primary, by reason.
	Fallbacks map[string]uint64 `json:"-"`
}

// Why a read a replica was picked for went to the primary instead.
const (
	FallbackStale       = "stale"
	FallbackLagging     = "lagging"
	FallbackUnavailable = "unavailable"
)

// Cluster is the primary plus its replica rotation.
type Cluster struct {
	primary  *pgxpool.Pool
	admin    *pgxpool.Pool
	replicas []*replicaState

	maxLag  time.Duration
	samples int
	rr      atomic.Uint64
	log     *slog.Logger
	stop    context.CancelFunc
	stopped chan struct{}

	readsToPrimary   atomic.Uint64
	readsToReplica   atomic.Uint64
	staleFallbacks   atomic.Uint64
	lagFallbacks     atomic.Uint64
	noHealthyReplica atomic.Uint64
}

// Option adjusts how the cluster is opened.
type Option func(*options)

type options struct {
	tracer func(pool string) pgx.QueryTracer
}

// WithQueryTracer gives every pool a tracer, made per pool so a span can say
// which one it ran on. The pools are named primary, admin and replica-<n>.
func WithQueryTracer(make func(pool string) pgx.QueryTracer) Option {
	return func(o *options) { o.tracer = make }
}

// Open connects the primary and every configured replica, then starts the
// health loop. The caller must Close the returned cluster.
func Open(ctx context.Context, cfg config.DB, log *slog.Logger, opts ...Option) (*Cluster, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	tracerFor := func(pool string) pgx.QueryTracer {
		if o.tracer == nil {
			return nil
		}
		return o.tracer(pool)
	}

	primary, err := openPool(ctx, cfg, cfg.PrimaryURL, cfg.PrimaryMaxConns, tracerFor("primary"))
	if err != nil {
		return nil, fmt.Errorf("connect to the primary database: %w", err)
	}

	admin := primary
	if cfg.AdminURL != "" && cfg.AdminURL != cfg.PrimaryURL {
		admin, err = openPool(ctx, cfg, cfg.AdminURL, cfg.PrimaryMaxConns, tracerFor("admin"))
		if err != nil {
			primary.Close()
			return nil, fmt.Errorf("connect to the database as the admin role: %w", err)
		}
	}

	c := &Cluster{
		primary: primary,
		admin:   admin,
		maxLag:  cfg.MaxReplicaLag,
		samples: max(cfg.ReplicaLagSamples, 1),
		log:     log,
		stopped: make(chan struct{}),
	}
	// Start the rotation at a random offset so that many api processes booting
	// at once do not all send their first read to the same replica.
	c.rr.Store(rand.Uint64())

	for i, url := range cfg.ReplicaURLs {
		name := fmt.Sprintf("replica-%d", i)
		pool, err := openPool(ctx, cfg, url, cfg.ReplicaMaxConns, tracerFor(name))
		if err != nil {
			// A replica that is down at boot must not stop the process: the
			// health loop will pick it up when it returns.
			log.Warn("replica unavailable at startup", "replica", i, "error", err)
			continue
		}
		c.replicas = append(c.replicas, &replicaState{name: name, pool: pool})
	}

	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	c.stop = cancel
	c.checkAll(loopCtx) // seed health before serving the first request
	go c.healthLoop(loopCtx, cfg.HealthInterval)

	log.Info("database cluster ready", "replicas", len(c.replicas), "max_lag", cfg.MaxReplicaLag)
	return c, nil
}

func openPool(ctx context.Context, cfg config.DB, url string, maxConns int32, tracer pgx.QueryTracer) (*pgxpool.Pool, error) {
	pcfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	pcfg.MaxConns = maxConns
	pcfg.MinConns = min(cfg.MinConns, maxConns)
	pcfg.MaxConnLifetime = cfg.ConnMaxLifetime
	if tracer != nil {
		pcfg.ConnConfig.Tracer = tracer
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// Close shuts down the health loop and every pool.
func (c *Cluster) Close() {
	if c.stop != nil {
		c.stop()
		<-c.stopped
	}
	for _, r := range c.replicas {
		r.pool.Close()
	}
	if c.admin != c.primary {
		c.admin.Close()
	}
	c.primary.Close()
}

// Primary returns the writable pool. Prefer Write for anything that mutates.
func (c *Cluster) Primary() *pgxpool.Pool { return c.primary }

// Stats reports routing counters and replica health for the metrics endpoint.
type Stats struct {
	Replicas            []ReplicaStatus `json:"replicas"`
	ReadsToPrimary      uint64          `json:"readsToPrimary"`
	ReadsToReplica      uint64          `json:"readsToReplica"`
	StaleFallbacks      uint64          `json:"staleFallbacks"`
	LagFallbacks        uint64          `json:"lagFallbacks"`
	NoHealthyReplicaHit uint64          `json:"noHealthyReplicaHits"`
	// Pools is how full each connection pool is, which is what a saturated
	// api looks like from the outside.
	Pools []PoolStats `json:"-"`
}

// PoolStats is a point in time view of one pool: its connections, and how
// often and how long callers waited for one.
type PoolStats struct {
	Name         string
	Idle         int32
	Acquired     int32
	Constructing int32
	Max          int32

	Acquires      int64
	EmptyAcquires int64
	// Wait is the time spent waiting because no connection was idle, which
	// is what a pool sized too small costs.
	Wait time.Duration
}

// Stats is a snapshot of the routing counters, replica health and pool sizes.
func (c *Cluster) Stats() Stats {
	s := Stats{
		ReadsToPrimary:      c.readsToPrimary.Load(),
		ReadsToReplica:      c.readsToReplica.Load(),
		StaleFallbacks:      c.staleFallbacks.Load(),
		LagFallbacks:        c.lagFallbacks.Load(),
		NoHealthyReplicaHit: c.noHealthyReplica.Load(),
	}
	type named struct {
		name string
		pool *pgxpool.Pool
	}
	pools := []named{{"primary", c.primary}}
	if c.admin != c.primary {
		pools = append(pools, named{"admin", c.admin})
	}
	for _, r := range c.replicas {
		s.Replicas = append(s.Replicas, r.snapshot())
		pools = append(pools, named{r.name, r.pool})
	}
	for _, p := range pools {
		stat := p.pool.Stat()
		s.Pools = append(s.Pools, PoolStats{
			Name: p.name, Idle: stat.IdleConns(), Acquired: stat.AcquiredConns(), Constructing: stat.ConstructingConns(), Max: stat.MaxConns(),
			Acquires: stat.AcquireCount(), EmptyAcquires: stat.EmptyAcquireCount(), Wait: stat.EmptyAcquireWaitTime(),
		})
	}
	return s
}

// ErrPrivilegedRole is returned when the application connects as a role that
// row level security does not apply to.
var ErrPrivilegedRole = errors.New("the database connection uses a role that bypasses row level security; connect as exciter_app instead")

// RefuseSuperuser fails when this connects as a role that ignores row level
// security, which would make every policy in the schema decoration.
func (c *Cluster) RefuseSuperuser(ctx context.Context) error {
	var privileged bool
	err := c.primary.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&privileged)
	if err != nil {
		return fmt.Errorf("ask which role this connects as: %w", err)
	}
	if privileged {
		return ErrPrivilegedRole
	}
	return nil
}

// Ping verifies the primary is reachable.
func (c *Cluster) Ping(ctx context.Context) error {
	if err := c.primary.Ping(ctx); err != nil {
		return fmt.Errorf("primary: %w", err)
	}
	return nil
}
