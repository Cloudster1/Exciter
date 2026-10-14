package events

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/observability"
)

const (
	// DefaultBatch is how many events one pass claims.
	DefaultBatch = 20
	// DefaultIdle is how long the worker waits when nothing is waiting.
	DefaultIdle = time.Second
	// MaxAttempts is how often an event is tried before it is marked done
	// with its last error, so one bad event cannot block the rest.
	MaxAttempts = 10
	// retryStep is how much longer each failed attempt waits than the last.
	retryStep = 5 * time.Second
	// Retention is how long a done event is kept, to look into what went out.
	Retention = 7 * 24 * time.Hour
	// pruneEvery is how often the done events past Retention are deleted.
	pruneEvery = time.Hour
	// ClaimLease is how long a claimed event is kept from other passes; a
	// worker that dies holding one delays it by at most this much.
	ClaimLease = 5 * time.Minute
	// releaseTimeout bounds handing back a stopped pass's leases.
	releaseTimeout = 5 * time.Second
)

// Handler acts on one event, at least once, so a second run with the same
// event must change nothing.
type Handler interface {
	Handle(ctx context.Context, e Event) error
}

// HandlerFunc is a function as a Handler.
type HandlerFunc func(ctx context.Context, e Event) error

func (f HandlerFunc) Handle(ctx context.Context, e Event) error { return f(ctx, e) }

// Worker drains the outbox. Any number may run and restart at any time: an
// event is leased with SKIP LOCKED, and handled once that claim is committed.
type Worker struct {
	db      *db.Cluster
	handler Handler
	log     *slog.Logger
	batch   int
	idle    time.Duration
	pruned  time.Time
}

func NewWorker(cluster *db.Cluster, handler Handler, log *slog.Logger) *Worker {
	return &Worker{db: cluster, handler: handler, log: log, batch: DefaultBatch, idle: DefaultIdle}
}

// WithBatch sets how many events one pass claims.
func (w *Worker) WithBatch(n int) *Worker {
	w.batch = max(n, 1)
	return w
}

// WithIdle sets how long the worker waits when nothing is waiting.
func (w *Worker) WithIdle(d time.Duration) *Worker {
	w.idle = d
	return w
}

// Run drains until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	w.log.Info("outbox worker started", "batch", w.batch)
	for {
		n, err := w.Once(ctx)
		if ctx.Err() != nil {
			w.log.Info("outbox worker stopped")
			return
		}
		if err != nil {
			w.log.Error("outbox pass failed", "error", err)
		}
		if time.Since(w.pruned) > pruneEvery {
			w.prune(ctx)
		}
		// A full batch means more is probably waiting, so go straight round.
		if n < w.batch || err != nil {
			select {
			case <-ctx.Done():
				w.log.Info("outbox worker stopped")
				return
			case <-time.After(w.idle):
			}
		}
	}
}

// claim is an event this worker has leased, with the attempts made before.
type claim struct {
	Event
	attempts int
}

// Once claims up to one batch, hands each event to the handler and marks it
// done, and returns how many it claimed.
func (w *Worker) Once(ctx context.Context) (int, error) {
	batch, err := w.claim(ctx)
	if err != nil {
		return 0, err
	}
	for i, c := range batch {
		var herr error
		if ctx.Err() == nil {
			herr = w.handle(ctx, c.Event)
		}
		if ctx.Err() != nil {
			w.release(ctx, batch[i:])
			return len(batch), ctx.Err()
		}
		if err := w.settle(ctx, c, herr); err != nil {
			w.release(ctx, batch[i:])
			return len(batch), err
		}
	}
	return len(batch), nil
}

// claim leases a batch in a transaction of its own: holding the rows' locks while handlers run deadlocks unseen,
// since a handler's write can wait on a project's delete, which waits on those locks.
func (w *Worker) claim(ctx context.Context) ([]claim, error) {
	var batch []claim
	_, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			UPDATE outbox_event e
			SET available_at = now() + make_interval(secs => $2)
			FROM (
			    SELECT id FROM outbox_event
			    WHERE processed_at IS NULL AND available_at <= now()
			    ORDER BY created_at, id
			    LIMIT $1
			    FOR UPDATE SKIP LOCKED
			) due
			WHERE e.id = due.id
			RETURNING e.id, e.topic, e.payload, e.created_at, COALESCE(e.trace_parent, ''), e.attempts`,
			w.batch, ClaimLease.Seconds())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c claim
			if err := rows.Scan(&c.ID, &c.Topic, &c.Payload, &c.CreatedAt, &c.Trace, &c.attempts); err != nil {
				return err
			}
			batch = append(batch, c)
		}
		return rows.Err()
	})
	// RETURNING keeps no order, and the oldest event goes first.
	slices.SortFunc(batch, func(a, b claim) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return bytes.Compare(a.ID[:], b.ID[:])
	})
	return batch, err
}

// settle marks a handled event done, or due again later after a failure.
func (w *Worker) settle(ctx context.Context, c claim, herr error) error {
	_, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if herr == nil {
			_, err := tx.Exec(ctx, `UPDATE outbox_event SET processed_at = now(), attempts = attempts + 1 WHERE id = $1`, c.ID)
			return err
		}
		attempts := c.attempts + 1
		giveUp := attempts >= MaxAttempts
		w.log.Warn("an event could not be delivered yet", "event", c.ID, "topic", c.Topic, "attempt", attempts, "given_up", giveUp, "error", herr)
		if _, err := tx.Exec(ctx, `
			UPDATE outbox_event
			SET attempts = $2, last_error = $3,
			    available_at = now() + make_interval(secs => $4),
			    processed_at = CASE WHEN $5 THEN now() END
			WHERE id = $1`, c.ID, attempts, herr.Error(), retryDelay(attempts).Seconds(), giveUp); err != nil {
			return err
		}
		if !giveUp {
			return nil
		}
		// The installation's administrators read the audit log anyway, so it is the record of what was set aside.
		return audit.Write(ctx, tx, audit.Entry{
			Action: audit.ActionEventDeadLettered, TargetType: "event", TargetID: &c.ID,
			Data: map[string]any{"topic": c.Topic, "attempts": attempts, "lastError": herr.Error()},
		})
	})
	return err
}

// retryDelay is how long an event waits after its nth failed attempt: a step
// longer each time, so a failing dependency is not hammered.
func retryDelay(attempts int) time.Duration {
	return time.Duration(max(attempts, 1)) * retryStep
}

// release hands back what was leased and not handled, so the next pass need
// not wait out the lease.
func (w *Worker) release(ctx context.Context, rest []claim) {
	if len(rest) == 0 {
		return
	}
	ids := make([]uuid.UUID, len(rest))
	for i, c := range rest {
		ids[i] = c.ID
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()
	if _, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE outbox_event SET available_at = now() WHERE id = ANY($1) AND processed_at IS NULL`, ids)
		return err
	}); err != nil {
		w.log.Warn("leased events were not handed back and wait out their lease", "events", len(ids), "error", err)
	}
}

func (w *Worker) handle(ctx context.Context, e Event) (err error) {
	ctx, span := observability.Tracer().Start(continueTrace(ctx, e), "events "+e.Topic,
		trace.WithAttributes(attribute.String("exciter.event_id", e.ID.String()), attribute.String("exciter.topic", e.Topic)))
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("the handler panicked")
			w.log.Error("an event's handler panicked", "event", e.ID, "panic", r)
		}
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	return w.handler.Handle(ctx, e)
}

// prune deletes done events past Retention.
func (w *Worker) prune(ctx context.Context) {
	w.pruned = time.Now()
	if _, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM outbox_event WHERE processed_at < now() - make_interval(secs => $1)`, Retention.Seconds())
		return err
	}); err != nil && ctx.Err() == nil {
		w.log.Warn("old outbox events could not be pruned", "error", err)
	}
}

// Chain hands each event to every handler in order and stops at the first
// that fails, so put first the ones a second run leaves unchanged.
type Chain []Handler

func (c Chain) Handle(ctx context.Context, e Event) error {
	for _, h := range c {
		if h == nil {
			continue
		}
		if err := h.Handle(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

// Mux hands each event to the handler of its topic and any other to the
// fallback, so a topic that fails is retried without running the others again.
type Mux struct {
	routes   map[string]Handler
	fallback Handler
}

// NewMux routes every topic not named to fallback, which may be nil.
func NewMux(fallback Handler) *Mux {
	return &Mux{routes: map[string]Handler{}, fallback: fallback}
}

// Route sends a topic's events to h.
func (m *Mux) Route(topic string, h Handler) *Mux {
	m.routes[topic] = h
	return m
}

func (m *Mux) Handle(ctx context.Context, e Event) error {
	if h, ok := m.routes[e.Topic]; ok {
		return h.Handle(ctx, e)
	}
	if m.fallback == nil {
		return nil
	}
	return m.fallback.Handle(ctx, e)
}
