//go:build integration

package test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudster1/exciter/backend/internal/comment"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/notify"
	"github.com/cloudster1/exciter/backend/internal/observability"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

const (
	// incomingTrace and incomingSpan are the caller's, sampled, as a W3C traceparent carries them.
	incomingTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
	incomingSpan  = "00f067aa0ba902b7"
	// incomingRequestID is the caller's own request id, which the access log repeats.
	incomingRequestID = "trace-through-the-outbox"
)

// syncBuffer collects log lines written from the server's goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// lines are the JSON log lines written so far.
func (b *syncBuffer) lines(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(b.buf.Bytes()))
	for scanner.Scan() {
		line := map[string]any{}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("a log line is not JSON: %q", scanner.Text())
		}
		out = append(out, line)
	}
	return out
}

// keptSpans keeps what it is handed past the provider's shutdown, which empties tracetest's in-memory exporter.
type keptSpans struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

func (k *keptSpans) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.spans = append(k.spans, spans...)
	return nil
}

func (k *keptSpans) Shutdown(context.Context) error { return nil }

func (k *keptSpans) all() []sdktrace.ReadOnlySpan {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]sdktrace.ReadOnlySpan(nil), k.spans...)
}

// carryOver plants an event of the stack's outbox in o's, as it was written, for a worker of the test's own: the
// stack's worker drains its outbox as soon as an event is due, so a test cannot count on handling one there (#279).
func carryOver(t *testing.T, h, o *harness, topic, about, value string) uuid.UUID {
	t.Helper()
	var payload []byte
	var traceParent *string
	if err := h.super.QueryRow(context.Background(), `
		SELECT payload, trace_parent FROM outbox_event WHERE topic = $1 AND payload ->> $2 = $3
		ORDER BY created_at DESC, id DESC LIMIT 1`, topic, about, value).Scan(&payload, &traceParent); err != nil {
		t.Fatalf("no %s event with %s %s: %v", topic, about, value, err)
	}
	var id uuid.UUID
	if err := o.super.QueryRow(context.Background(), `INSERT INTO outbox_event (topic, payload, trace_parent) VALUES ($1, $2, $3) RETURNING id`,
		topic, payload, traceParent).Scan(&id); err != nil {
		t.Fatalf("carry the %s event over: %v", topic, err)
	}
	return id
}

// A request's trace goes into the event it emits and on into the worker's span for it, and its access line names
// both the caller's request id and that trace.
func TestATraceGoesThroughTheOutbox(t *testing.T) {
	h := newHarness(t)
	o := ownOutbox(t, h, "exciter_outbox_trace_test")
	exporter := &keptSpans{}
	previous := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	tel, err := observability.Setup(t.Context(), observability.Config{Service: "exciter-test", SampleRatio: 1}, discard(),
		observability.WithSpanExporter(exporter))
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	api := newAPIServerWith(t, h, func(_ *apiServer, s *httpapi.Server) {
		s.Telemetry = tel
		s.Log = slog.New(slog.NewJSONHandler(logs, nil))
	})
	key := projectKey(t, "TRCE")
	owner := api.as(t, h.owner(t, "trace").user)
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Traced"}), http.StatusCreated, "make a project")

	body := strings.NewReader(`{"title": "Every request is traced"}`)
	req, err := http.NewRequest(http.MethodPost, api.URL+httpapi.APIPrefix+"/projects/"+key+"/requirements", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("traceparent", "00-"+incomingTrace+"-"+incomingSpan+"-01")
	req.Header.Set("X-Request-Id", incomingRequestID)
	resp, err := owner.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || resp.Header.Get("X-Request-Id") != incomingRequestID {
		t.Fatalf("the traced request answered %s with X-Request-Id %q, want 201 and the caller's id", resp.Status, resp.Header.Get("X-Request-Id"))
	}

	var access map[string]any
	for _, line := range logs.lines(t) {
		if line["msg"] == "request" && line["method"] == http.MethodPost && strings.HasSuffix(line["path"].(string), "/requirements") {
			access = line
		}
	}
	if access == nil || access["request_id"] != incomingRequestID || access["trace_id"] != incomingTrace {
		t.Errorf("the access line is %v, want request_id %q and trace_id %q", access, incomingRequestID, incomingTrace)
	}

	var reqID string
	if err := h.super.QueryRow(t.Context(), `SELECT r.id FROM requirement r JOIN project p ON p.id = r.project_id WHERE p.key = $1 AND r.number = 1`,
		key).Scan(&reqID); err != nil {
		t.Fatalf("find the requirement: %v", err)
	}
	var traceParent string
	if err := h.super.QueryRow(t.Context(), `SELECT COALESCE(trace_parent, '') FROM outbox_event WHERE topic = $1 AND payload ->> 'requirementId' = $2`,
		requirement.TopicCreated, reqID).Scan(&traceParent); err != nil {
		t.Fatalf("read the event's trace: %v", err)
	}
	parts := strings.Split(traceParent, "-")
	if len(parts) != 4 || parts[1] != incomingTrace || parts[2] == incomingSpan {
		t.Fatalf("the event's trace_parent is %q, want the incoming trace %s under a span of the api's own", traceParent, incomingTrace)
	}

	event := carryOver(t, h, o, requirement.TopicCreated, "requirementId", reqID)
	var handledIn trace.SpanContext
	if n, err := events.NewWorker(o.cluster, events.HandlerFunc(func(ctx context.Context, _ events.Event) error {
		handledIn = trace.SpanContextFromContext(ctx)
		return nil
	}), discard()).Once(t.Context()); err != nil || n != 1 {
		t.Fatalf("the worker claimed %d (%v), want the carried event", n, err)
	}
	if handledIn.TraceID().String() != incomingTrace {
		t.Errorf("the handler ran in trace %s, want %s", handledIn.TraceID(), incomingTrace)
	}
	if err := tel.Shutdown(t.Context()); err != nil {
		t.Fatalf("flush the spans: %v", err)
	}
	var worker sdktrace.ReadOnlySpan
	for _, span := range exporter.all() {
		if span.Name() == "events "+requirement.TopicCreated && span.SpanContext().TraceID().String() == incomingTrace {
			worker = span
		}
	}
	if worker == nil {
		t.Fatalf("no span \"events %s\" in trace %s among %d exported", requirement.TopicCreated, incomingTrace, len(exporter.all()))
	}
	if worker.Parent().SpanID().String() != parts[2] {
		t.Errorf("the worker's span hangs under %s, want the span the event recorded, %s", worker.Parent().SpanID(), parts[2])
	}
	if !hasAttribute(worker, "exciter.event_id", event.String()) {
		t.Errorf("the worker's span does not name the event %s: %v", event, worker.Attributes())
	}
}

func hasAttribute(span sdktrace.ReadOnlySpan, key, value string) bool {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key && kv.Value.AsString() == value {
			return true
		}
	}
	return false
}

// Without EXCITER_SMTP_ADDR the worker's fan-out has no mailer: a mention still lands in the app, the event is
// done at its first attempt, and nothing waits for a mail.
func TestAMentionWithoutMailStillReachesTheApp(t *testing.T) {
	w := newCommentWorld(t, "NOML")
	o := ownOutbox(t, w.h, "exciter_outbox_no_mail_test")
	thread := obj(t, want(t, w.dave.post(t, "/api/v1/requirements/"+w.reqKey+"/comments", commentBody("Have a look, ", w.bobID)),
		http.StatusCreated, "dave mentions bob"), "thread")
	event := carryOver(t, w.h, o, comment.TopicCreated, "threadId", thread["id"].(string))

	if n, err := events.NewWorker(o.cluster, notify.NewFanOut(w.h.cluster, nil, testAppURL(t), discard()), discard()).Once(t.Context()); err != nil || n != 1 {
		t.Fatalf("the worker claimed %d (%v), want the mention", n, err)
	}
	if r := o.eventRow(t, event); !r.processed || r.attempts != 1 || r.lastError != nil {
		t.Errorf("the mention's event reads %+v, want it done at the first attempt with no error", r)
	}
	if n := w.h.countRows(t, `SELECT count(*) FROM notification WHERE event_id = $1 AND user_id = $2 AND kind = $3`,
		event, w.bobID, notify.KindMentioned); n != 1 {
		t.Errorf("bob has %d mention notifications of the event, want 1", n)
	}
	if n := w.h.countRows(t, `SELECT count(*) FROM notification_digest q JOIN notification n ON n.id = q.notification_id WHERE n.event_id = $1`,
		event); n != 0 {
		t.Errorf("%d notifications of the event wait for a mail that cannot go", n)
	}
}
