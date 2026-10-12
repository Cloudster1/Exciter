package observability

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// ClusterCollector reads the cluster's routing counters and pool sizes on each
// scrape, so the numbers /readyz already shows reach a dashboard too.
type ClusterCollector struct {
	stats func() db.Stats
}

// NewClusterCollector collects from the cluster's Stats.
func NewClusterCollector(cluster *db.Cluster) *ClusterCollector {
	return &ClusterCollector{stats: cluster.Stats}
}

var (
	readsDesc     = prometheus.NewDesc("exciter_db_reads_total", "Reads routed, by whether they went to the primary or a replica.", []string{"target"}, nil)
	staleDesc     = prometheus.NewDesc("exciter_db_stale_fallbacks_total", "Reads sent to the primary because no replica had caught up.", nil, nil)
	lagFallDesc   = prometheus.NewDesc("exciter_db_lag_fallbacks_total", "Reads sent to the primary because the replica connection lagged past the bound.", nil, nil)
	noHealthyDesc = prometheus.NewDesc("exciter_db_no_healthy_replica_total", "Reads sent to the primary because no replica was healthy.", nil, nil)
	lagDesc       = prometheus.NewDesc("exciter_db_replica_lag_seconds", "How far behind the primary a replica was at its last check.", []string{"replica"}, nil)
	healthyDesc   = prometheus.NewDesc("exciter_db_replica_healthy", "1 while a replica is taking reads.", []string{"replica"}, nil)
	poolDesc      = prometheus.NewDesc("exciter_db_pool_connections", "Connections in a pool, by state.", []string{"pool", "state"}, nil)
	poolMaxDesc   = prometheus.NewDesc("exciter_db_pool_max_connections", "The most connections a pool may open.", []string{"pool"}, nil)
	acquiresDesc  = prometheus.NewDesc("exciter_db_pool_acquires_total", "Connections taken from a pool, which is one per transaction or statement.", []string{"pool"}, nil)
	emptyDesc     = prometheus.NewDesc("exciter_db_pool_empty_acquires_total", "Connections taken from a pool that had none idle, so the caller waited.", []string{"pool"}, nil)
	waitDesc      = prometheus.NewDesc("exciter_db_pool_wait_seconds_total", "Time callers spent waiting for a connection because none was idle.", []string{"pool"}, nil)
	fallbackDesc  = prometheus.NewDesc("exciter_db_replica_fallbacks_total", "Reads a replica pool was picked for and handed to the primary, by reason.", []string{"pool", "reason"}, nil)
)

func (c *ClusterCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{readsDesc, staleDesc, lagFallDesc, noHealthyDesc, lagDesc, healthyDesc, poolDesc, poolMaxDesc, acquiresDesc, emptyDesc, waitDesc, fallbackDesc} {
		ch <- d
	}
}

func (c *ClusterCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.stats()
	ch <- prometheus.MustNewConstMetric(readsDesc, prometheus.CounterValue, float64(s.ReadsToPrimary), "primary")
	ch <- prometheus.MustNewConstMetric(readsDesc, prometheus.CounterValue, float64(s.ReadsToReplica), "replica")
	ch <- prometheus.MustNewConstMetric(staleDesc, prometheus.CounterValue, float64(s.StaleFallbacks))
	ch <- prometheus.MustNewConstMetric(lagFallDesc, prometheus.CounterValue, float64(s.LagFallbacks))
	ch <- prometheus.MustNewConstMetric(noHealthyDesc, prometheus.CounterValue, float64(s.NoHealthyReplicaHit))
	for _, r := range s.Replicas {
		healthy := 0.0
		if r.Healthy {
			healthy = 1
		}
		ch <- prometheus.MustNewConstMetric(lagDesc, prometheus.GaugeValue, r.Lag.Seconds(), r.Name)
		ch <- prometheus.MustNewConstMetric(healthyDesc, prometheus.GaugeValue, healthy, r.Name)
		for reason, n := range r.Fallbacks {
			ch <- prometheus.MustNewConstMetric(fallbackDesc, prometheus.CounterValue, float64(n), r.Name, reason)
		}
	}
	for _, p := range s.Pools {
		ch <- prometheus.MustNewConstMetric(poolDesc, prometheus.GaugeValue, float64(p.Idle), p.Name, "idle")
		ch <- prometheus.MustNewConstMetric(poolDesc, prometheus.GaugeValue, float64(p.Acquired), p.Name, "acquired")
		ch <- prometheus.MustNewConstMetric(poolDesc, prometheus.GaugeValue, float64(p.Constructing), p.Name, "constructing")
		ch <- prometheus.MustNewConstMetric(poolMaxDesc, prometheus.GaugeValue, float64(p.Max), p.Name)
		ch <- prometheus.MustNewConstMetric(acquiresDesc, prometheus.CounterValue, float64(p.Acquires), p.Name)
		ch <- prometheus.MustNewConstMetric(emptyDesc, prometheus.CounterValue, float64(p.EmptyAcquires), p.Name)
		ch <- prometheus.MustNewConstMetric(waitDesc, prometheus.CounterValue, p.Wait.Seconds(), p.Name)
	}
}
