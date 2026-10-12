package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is what every span from this module is attributed to.
const tracerName = "github.com/cloudster1/exciter/backend"

type spanExporter = sdktrace.SpanExporter

// WithSpanExporter sends spans somewhere other than the collector, for a test
// that wants to read what would have been sent.
func WithSpanExporter(exporter sdktrace.SpanExporter) Option {
	return func(o *setupOptions) { o.exporter = exporter }
}

type tracing struct {
	provider *sdktrace.TracerProvider
}

// setupTracing always installs the W3C propagator, so a traceparent is passed
// on, but a provider only when spans have somewhere to go.
func setupTracing(ctx context.Context, cfg Config, log *slog.Logger, exporter spanExporter) (*tracing, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		// A collector that is down is not this process' problem to shout about.
		log.Debug("telemetry", "error", err)
	}))
	if exporter == nil && cfg.OTLPEndpoint != "" {
		// The endpoint is the whole URL, /v1/traces included, so a collector
		// on another path is one setting away and no gRPC is needed.
		var err error
		exporter, err = otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.OTLPEndpoint))
		if err != nil {
			return nil, err
		}
	}
	if exporter == nil {
		return nil, nil
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.Service),
		semconv.DeploymentEnvironmentNameKey.String(cfg.Env),
	))
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(provider)
	return &tracing{provider: provider}, nil
}

func (t *tracing) shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	return t.provider.Shutdown(ctx)
}

// Tracer is the module's tracer, whichever provider is installed.
func Tracer() trace.Tracer { return otel.Tracer(tracerName) }

// TraceIDFrom is the trace id on ctx as a string, or "" when there is none.
func TraceIDFrom(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
