package observability

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Each pool reports its own traffic, waiting and fallbacks, so a write pool
// sized too small is told apart from a read pool that is.
func TestEveryPoolReportsItsOwnSeries(t *testing.T) {
	stats := db.Stats{
		ReadsToPrimary: 4, ReadsToReplica: 9, StaleFallbacks: 1, LagFallbacks: 2,
		Replicas: []db.ReplicaStatus{{
			Name: "replica-0", Healthy: true, Lag: 250 * time.Millisecond,
			Fallbacks: map[string]uint64{db.FallbackStale: 1, db.FallbackLagging: 2, db.FallbackUnavailable: 0},
		}},
		Pools: []db.PoolStats{
			{Name: "primary", Max: 20, Acquires: 30, EmptyAcquires: 3, Wait: 1500 * time.Millisecond},
			{Name: "replica-0", Max: 40, Acquires: 12},
		},
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(&ClusterCollector{stats: func() db.Stats { return stats }})

	want := `
# HELP exciter_db_pool_acquires_total Connections taken from a pool, which is one per transaction or statement.
# TYPE exciter_db_pool_acquires_total counter
exciter_db_pool_acquires_total{pool="primary"} 30
exciter_db_pool_acquires_total{pool="replica-0"} 12
# HELP exciter_db_pool_wait_seconds_total Time callers spent waiting for a connection because none was idle.
# TYPE exciter_db_pool_wait_seconds_total counter
exciter_db_pool_wait_seconds_total{pool="primary"} 1.5
exciter_db_pool_wait_seconds_total{pool="replica-0"} 0
# HELP exciter_db_pool_max_connections The most connections a pool may open.
# TYPE exciter_db_pool_max_connections gauge
exciter_db_pool_max_connections{pool="primary"} 20
exciter_db_pool_max_connections{pool="replica-0"} 40
# HELP exciter_db_replica_fallbacks_total Reads a replica pool was picked for and handed to the primary, by reason.
# TYPE exciter_db_replica_fallbacks_total counter
exciter_db_replica_fallbacks_total{pool="replica-0",reason="lagging"} 2
exciter_db_replica_fallbacks_total{pool="replica-0",reason="stale"} 1
exciter_db_replica_fallbacks_total{pool="replica-0",reason="unavailable"} 0
# HELP exciter_db_lag_fallbacks_total Reads sent to the primary because the replica connection lagged past the bound.
# TYPE exciter_db_lag_fallbacks_total counter
exciter_db_lag_fallbacks_total 2
`
	names := []string{
		"exciter_db_pool_acquires_total", "exciter_db_pool_wait_seconds_total", "exciter_db_pool_max_connections",
		"exciter_db_replica_fallbacks_total", "exciter_db_lag_fallbacks_total",
	}
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), names...); err != nil {
		t.Fatal(err)
	}
}
