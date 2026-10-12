package observability

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Timeouts for the metrics listener, which serves one scraper and nobody else.
const (
	metricsReadHeaderTimeout = 10 * time.Second
	metricsShutdownTimeout   = 5 * time.Second
)

// Config is what a process needs to say how it is doing.
type Config struct {
	// Service names the binary in a trace and on a dashboard: exciter-api or exciter-worker.
	Service string
	// Env is the deployment, kept on every span.
	Env string
	// MetricsAddr serves /metrics on its own listener; blank serves none.
	MetricsAddr string
	// OTLPEndpoint is where traces go over HTTP; blank keeps them.
	OTLPEndpoint string
	// SampleRatio is the share of new traces kept.
	SampleRatio float64
}

// Telemetry is the process' metrics and its tracer, made once at startup.
type Telemetry struct {
	Metrics  *Metrics
	Registry *prometheus.Registry
	addr     string
	log      *slog.Logger
	tracing  *tracing
}

// Option adjusts Setup, for tests that want to see what would have been sent.
type Option func(*setupOptions)

type setupOptions struct {
	exporter spanExporter
}

// Setup builds the metrics registry and, when a collector is configured, the
// tracer. It is called once per process.
func Setup(ctx context.Context, cfg Config, log *slog.Logger, opts ...Option) (*Telemetry, error) {
	var o setupOptions
	for _, opt := range opts {
		opt(&o)
	}
	registry := prometheus.NewRegistry()
	t := &Telemetry{Metrics: NewMetrics(registry), Registry: registry, addr: cfg.MetricsAddr, log: log}
	tr, err := setupTracing(ctx, cfg, log, o.exporter)
	if err != nil {
		return nil, err
	}
	t.tracing = tr
	return t, nil
}

// Handler serves the registry in the exposition format Prometheus scrapes.
func (t *Telemetry) Handler() http.Handler {
	return promhttp.HandlerFor(t.Registry, promhttp.HandlerOpts{})
}

// Register adds a collector that reads its values on each scrape.
func (t *Telemetry) Register(c prometheus.Collector) error {
	return t.Registry.Register(c)
}

// Mux is what the metrics listener serves: /metrics, and a /healthz of its own
// so a probe of that port needs nothing from the API.
func (t *Telemetry) Mux() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", t.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return mux
}

// Serve listens on the metrics address until ctx ends. It returns at once when
// there is no address, so a caller can always run it.
func (t *Telemetry) Serve(ctx context.Context) error {
	if t.addr == "" {
		return nil
	}
	srv := &http.Server{Addr: t.addr, Handler: t.Mux(), ReadHeaderTimeout: metricsReadHeaderTimeout}
	done := make(chan error, 1)
	go func() {
		t.log.Info("metrics listening", "addr", t.addr)
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), metricsShutdownTimeout)
		defer cancel()
		return srv.Shutdown(stopCtx)
	}
}

// Shutdown flushes what the tracer still holds.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t.tracing == nil {
		return nil
	}
	return t.tracing.shutdown(ctx)
}
