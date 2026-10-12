package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Metrics is every series the processes publish, named once here so the
// dashboards and the code cannot disagree about a name.
type Metrics struct {
	HTTPRequests *prometheus.CounterVec
	HTTPDuration *prometheus.HistogramVec
	HTTPInFlight prometheus.Gauge
	DBQuery      *prometheus.HistogramVec
}

// requestBuckets suit a request or statement that usually takes milliseconds,
// and stop where a reader stops caring about the exact value.
var requestBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

// NewMetrics registers the series on reg, along with the Go runtime and the
// process collectors, and returns the handles the code records through.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "exciter_http_requests_total", Help: "Requests answered, by method, route pattern and status."}, []string{"method", "route", "status"}),
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "exciter_http_request_duration_seconds", Help: "How long a request took, by method and route pattern.", Buckets: requestBuckets}, []string{"method", "route"}),
		HTTPInFlight: prometheus.NewGauge(prometheus.GaugeOpts{Name: "exciter_http_in_flight", Help: "Requests being answered right now."}),
		DBQuery:      prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "exciter_db_query_duration_seconds", Help: "How long a statement took, by its first word and the pool it ran on.", Buckets: requestBuckets}, []string{"op", "pool"}),
	}
	reg.MustRegister(
		m.HTTPRequests, m.HTTPDuration, m.HTTPInFlight, m.DBQuery,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return m
}
