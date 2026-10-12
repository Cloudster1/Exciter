package observability

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// QueryTracer gives a pool a tracer that spans each statement, under a caller's
// span only, so health checks are not roots; the histogram is fed either way.
func (t *Telemetry) QueryTracer(pool string) pgx.QueryTracer {
	return &queryTracer{pool: pool, duration: t.Metrics.DBQuery}
}

// statementLimit is how much of a statement a span keeps.
const statementLimit = 512

type queryTracer struct {
	pool     string
	duration *prometheus.HistogramVec
}

type queryKey struct{}

type queryStart struct {
	op      string
	started time.Time
	span    trace.Span
}

func (q *queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	start := &queryStart{op: firstWord(data.SQL), started: time.Now()}
	if trace.SpanFromContext(ctx).IsRecording() {
		statement := data.SQL
		if len(statement) > statementLimit {
			statement = statement[:statementLimit]
		}
		ctx, start.span = Tracer().Start(ctx, "db."+start.op, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
			semconv.DBSystemNamePostgreSQL,
			semconv.DBOperationName(start.op),
			semconv.DBQueryText(statement),
			attribute.String("exciter.pool", q.pool),
		))
	}
	return context.WithValue(ctx, queryKey{}, start)
}

func (q *queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	start, ok := ctx.Value(queryKey{}).(*queryStart)
	if !ok {
		return
	}
	q.duration.WithLabelValues(start.op, q.pool).Observe(time.Since(start.started).Seconds())
	if start.span == nil {
		return
	}
	// No rows is an answer, not a failure.
	if data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows) {
		start.span.RecordError(data.Err)
		start.span.SetStatus(codes.Error, data.Err.Error())
	}
	start.span.End()
}

// firstWord names a statement by its verb, lowercased, or "other", so the
// histogram has a handful of series rather than one per statement.
func firstWord(sql string) string {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "other"
	}
	switch op := strings.ToLower(fields[0]); op {
	case "select", "insert", "update", "delete", "with", "begin", "commit", "rollback":
		return op
	}
	return "other"
}
