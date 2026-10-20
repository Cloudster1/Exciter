//go:build integration

package test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/seed"
)

const (
	// claimWait is how long a planted event waits for the suite's worker to take it.
	claimWait = 10 * time.Second
	// testTopic is a topic no handler of the product knows.
	testTopic = "test.probe"
	// manyEvents is how many events two workers race for.
	manyEvents = 200
)

// deleteBound is how long deleting the event may take while the handler runs; it waits on nothing.
var deleteBound = bound{deadline: 10 * time.Second, watchdog: 2 * time.Second}

// ownOutbox is the harness on a database of its own, migrated and seeded, whose outbox only the test's workers
// drain. On the stack's database its worker takes any event as soon as it is due, so a test there cannot count
// on its own worker getting one (#279).
func ownOutbox(t *testing.T, h *harness, name string) *harness {
	t.Helper()
	ctx := context.Background()
	provider, super, at := scratchDatabase(t, h, name)
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate %s: %v", name, err)
	}
	cfg := h.cfg
	cfg.DB.PrimaryURL, cfg.DB.AdminURL, cfg.DB.ReplicaURLs = at(cfg.DB.PrimaryURL), at(cfg.DB.AdminURL), nil
	cluster, err := db.Open(ctx, cfg.DB, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(cluster.Close)
	if _, err := seed.Ensure(ctx, cluster, "Outbox"); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
	return &harness{cfg: cfg, cluster: cluster, super: super}
}

// plantEvent adds an event behind the policies' back, already tried attempts times.
func (h *harness) plantEvent(t *testing.T, attempts int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(),
		`INSERT INTO outbox_event (topic, payload, attempts) VALUES ($1, '{}', $2) RETURNING id`,
		testTopic, attempts).Scan(&id); err != nil {
		t.Fatalf("add an event: %v", err)
	}
	return id
}

// runWorker runs a worker of the suite with handler until the test ends.
func (h *harness) runWorker(t *testing.T, handler events.Handler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		events.NewWorker(h.cluster, handler, discard()).WithBatch(1).WithIdle(workerIdle).Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// An event is written in the transaction of its change: a rolled back change
// leaves none, a committed one always has one.
func TestAnEventCommitsAndRollsBackWithItsChange(t *testing.T) {
	h := newHarness(t)
	m := h.owner(t, "outbox-tx")
	marker := uuid.NewString()
	refused := errors.New("the change failed after its event was written")

	_, err := h.cluster.Write(m.ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := events.Emit(ctx, tx, testTopic, map[string]any{"actorId": m.user, "marker": marker}); err != nil {
			return err
		}
		return refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("the change = %v", err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE payload ->> 'marker' = $1`, marker); n != 0 {
		t.Errorf("a rolled back change left %d events", n)
	}
	if _, err := h.cluster.Write(m.ctx, func(ctx context.Context, tx db.DBTX) error {
		return events.Emit(ctx, tx, testTopic, map[string]any{"actorId": m.user, "marker": marker})
	}); err != nil {
		t.Fatal(err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE payload ->> 'marker' = $1`, marker); n != 1 {
		t.Errorf("a committed change left %d events in its organization, want 1", n)
	}
}

// Straight through SQL as exciter_app: an event may only be added, and only in
// the name of the person the transaction acts for.
func TestTheAppRoleOnlyAddsEventsInItsOwnName(t *testing.T) {
	h := newHarness(t)
	m := h.owner(t, "outbox-own")
	other := h.addPerson(t, "member")
	h.plantEvent(t, 0)

	denied := func(what string, err error) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Errorf("%s = %v, want permission denied", what, err)
		}
	}
	write := func(sql string, args ...any) error {
		_, err := h.cluster.Write(m.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
		return err
	}
	denied("an event in somebody else's name", write(
		`INSERT INTO outbox_event (topic, payload) VALUES ($1, jsonb_build_object('actorId', $2::text))`, testTopic, other))
	denied("an event in nobody's name", write(`INSERT INTO outbox_event (topic, payload) VALUES ($1, '{}')`, testTopic))
	denied("reading the outbox", write(`SELECT count(*) FROM outbox_event`))
	denied("marking events done", write(`UPDATE outbox_event SET processed_at = now()`))
	denied("deleting events", write(`DELETE FROM outbox_event`))
	if err := write(`INSERT INTO outbox_event (topic, payload) VALUES ($1, jsonb_build_object('actorId', $2::text))`, testTopic, m.user); err != nil {
		t.Errorf("an event in the caller's own name was refused: %v", err)
	}
}

// Any number of workers may run at once: each event is handled by one of them,
// never by two, and every one is handled.
func TestTwoWorkersNeverHandleOneEvent(t *testing.T) {
	h := ownOutbox(t, newHarness(t), "exciter_outbox_two_workers_test")

	var mu sync.Mutex
	handledBy := map[uuid.UUID][]int{}
	worker := func(n int) events.Handler {
		return events.HandlerFunc(func(_ context.Context, e events.Event) error {
			mu.Lock()
			handledBy[e.ID] = append(handledBy[e.ID], n)
			mu.Unlock()
			return nil
		})
	}
	h.runWorker(t, worker(1))
	h.runWorker(t, worker(2))
	if _, err := h.super.Exec(context.Background(),
		`INSERT INTO outbox_event (topic, payload) SELECT $1, '{}' FROM generate_series(1, $2)`, testTopic, manyEvents); err != nil {
		t.Fatalf("add the events: %v", err)
	}

	waitFor(t, "every planted event to be done", func() bool {
		return h.countRows(t, `SELECT count(*) FROM outbox_event WHERE processed_at IS NULL`) == 0
	})
	mu.Lock()
	defer mu.Unlock()
	for id, by := range handledBy {
		if len(by) > 1 {
			t.Errorf("event %s was handled %d times, by workers %v", id, len(by), by)
		}
	}
	if len(handledBy) != manyEvents {
		t.Errorf("the workers handled %d of the %d events", len(handledBy), manyEvents)
	}
}

// offerer runs one worker of the suite that fails every event it is offered.
type offerer struct {
	h       *harness
	claimed chan uuid.UUID
}

func newOfferer(t *testing.T, h *harness) *offerer {
	t.Helper()
	o := &offerer{h: h, claimed: make(chan uuid.UUID, 64)}
	h.runWorker(t, events.HandlerFunc(func(_ context.Context, e events.Event) error {
		o.claimed <- e.ID
		return errors.New("the dependency is down")
	}))
	return o
}

// offer adds an event tried attempts times and waits for the worker to claim it. An earlier event of this test
// coming due again is no answer: the worker takes the oldest first, so it may come before this one.
func (o *offerer) offer(t *testing.T, attempts int) uuid.UUID {
	t.Helper()
	id := o.h.plantEvent(t, attempts)
	deadline := time.After(claimWait)
	for {
		select {
		case got := <-o.claimed:
			if got == id {
				return id
			}
		case <-deadline:
			t.Fatalf("the worker did not take event %s within %s", id, claimWait)
		}
	}
}

// A failed event is tried again later, each time later still, and after the
// last attempt it is set aside and recorded in the audit log.
func TestAFailedEventWaitsAndAGivenUpOneIsAudited(t *testing.T) {
	h := ownOutbox(t, newHarness(t), "exciter_outbox_failed_event_test")

	o := newOfferer(t, h)
	first := o.offer(t, 0)
	waitFor(t, "the first failure to be written", func() bool {
		return h.countRows(t, `SELECT count(*) FROM outbox_event WHERE id = $1 AND attempts = 1`, first) == 1
	})
	var waits time.Duration
	var lastError string
	if err := h.super.QueryRow(context.Background(), `
		SELECT available_at - now(), last_error FROM outbox_event WHERE id = $1 AND processed_at IS NULL`, first).Scan(&waits, &lastError); err != nil {
		t.Fatalf("read the failed event: %v", err)
	}
	if waits <= 0 || lastError != "the dependency is down" {
		t.Errorf("a failed event is due again in %s with error %q, want later and the handler's words", waits, lastError)
	}

	last := o.offer(t, events.MaxAttempts-1)
	waitFor(t, "the last attempt to be written", func() bool {
		return h.countRows(t, `SELECT count(*) FROM outbox_event WHERE id = $1 AND processed_at IS NOT NULL`, last) == 1
	})
	var topic string
	var actor *uuid.UUID
	err := h.super.QueryRow(context.Background(), `
		SELECT data ->> 'topic', actor_user_id FROM audit_log
		WHERE action = $1 AND target_id = $2`, audit.ActionEventDeadLettered, last).Scan(&topic, &actor)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("an event given up on left no audit entry")
	}
	if err != nil || topic != testTopic || actor != nil {
		t.Errorf("the entry reads topic %q actor %v (%v), want the event's topic and nobody", topic, actor, err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = $1 AND target_id IN ($2, $3)`, audit.ActionEventDeadLettered, first, last); n != 1 {
		t.Errorf("%d events were recorded as given up, want only the last", n)
	}
}

// While a handler runs, the worker holds no lock on its event: a handler's write may wait on a delete that
// reaches the event, which would otherwise wait on the handler.
func TestTheWorkerHoldsNoLockWhileItHandles(t *testing.T) {
	h := ownOutbox(t, newHarness(t), "exciter_outbox_no_lock_test")
	started := make(chan uuid.UUID, 1)
	release := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(release) }) }
	t.Cleanup(stop)
	h.runWorker(t, events.HandlerFunc(func(ctx context.Context, e events.Event) error {
		started <- e.ID
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}))

	planted := h.plantEvent(t, 0)
	var handling uuid.UUID
	select {
	case handling = <-started:
	case <-time.After(claimWait):
		t.Fatalf("the worker did not take event %s within %s", planted, claimWait)
	}
	if report, err := h.execBounded(t, h.super, deleteBound, `DELETE FROM outbox_event WHERE id = $1`, handling); err != nil {
		t.Fatalf("deleting the event while it is handled: %v\n%s", err, report)
	}
	stop()
	if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE id = $1`, handling); n != 0 {
		t.Error("the event handled while it was deleted is still there")
	}
}
