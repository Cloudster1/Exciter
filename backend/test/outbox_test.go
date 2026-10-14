//go:build integration

package test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
)

const (
	// claimTries is how often a test offers an event before giving up: the stack's own worker may take one first.
	claimTries = 5
	// claimWait is how long one offer waits for the suite's worker to take it.
	claimWait = 5 * time.Second
	// testTopic is a topic no handler of the product knows.
	testTopic = "test.probe"
	// manyEvents is how many events two workers race for.
	manyEvents = 200
)

// errNotThisTest leaves another test's event to the stack's worker.
var errNotThisTest = errors.New("not this test's event")

// deleteBound is how long deleting the event may take while the handler runs; it waits on nothing.
var deleteBound = bound{deadline: 10 * time.Second, watchdog: 2 * time.Second}

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
	h := newHarness(t)
	race := uuid.NewString()

	var mu sync.Mutex
	handledBy := map[uuid.UUID][]int{}
	worker := func(n int) events.Handler {
		return events.HandlerFunc(func(_ context.Context, e events.Event) error {
			if !bytes.Contains(e.Payload, []byte(race)) {
				return errNotThisTest
			}
			mu.Lock()
			handledBy[e.ID] = append(handledBy[e.ID], n)
			mu.Unlock()
			return nil
		})
	}
	// Running before the events exist, so the stack's worker does not have them all to itself.
	h.runWorker(t, worker(1))
	h.runWorker(t, worker(2))
	if _, err := h.super.Exec(context.Background(),
		`INSERT INTO outbox_event (topic, payload) SELECT $1, jsonb_build_object('race', $2::text) FROM generate_series(1, $3)`,
		testTopic, race, manyEvents); err != nil {
		t.Fatalf("add the events: %v", err)
	}

	waitFor(t, "every planted event to be done", func() bool {
		return h.countRows(t, `SELECT count(*) FROM outbox_event WHERE payload ->> 'race' = $1 AND processed_at IS NULL`, race) == 0
	})
	mu.Lock()
	defer mu.Unlock()
	// The stack's own worker may take some of them; whoever took one, nobody else did.
	for id, by := range handledBy {
		if len(by) > 1 {
			t.Errorf("event %s was handled %d times, by workers %v", id, len(by), by)
		}
	}
	if len(handledBy) == 0 {
		t.Error("the suite's workers handled none of the events, so nothing was raced for")
	}
}

// offerer runs one worker of the suite that fails every event it was offered, and
// offers events until that worker, rather than the stack's, claims one.
type offerer struct {
	h       *harness
	mu      sync.Mutex
	ours    map[uuid.UUID]bool
	claimed chan uuid.UUID
}

func newOfferer(t *testing.T, h *harness) *offerer {
	t.Helper()
	o := &offerer{h: h, ours: map[uuid.UUID]bool{}, claimed: make(chan uuid.UUID, claimTries)}
	h.runWorker(t, events.HandlerFunc(func(_ context.Context, e events.Event) error {
		o.mu.Lock()
		mine := o.ours[e.ID]
		o.mu.Unlock()
		if !mine {
			return errNotThisTest
		}
		o.claimed <- e.ID
		return errors.New("the dependency is down")
	}))
	return o
}

// offer adds events tried attempts times until the worker claims one, and returns it.
func (o *offerer) offer(t *testing.T, attempts int) uuid.UUID {
	t.Helper()
	for range claimTries {
		id := o.h.plantEvent(t, attempts)
		o.mu.Lock()
		o.ours[id] = true
		o.mu.Unlock()
		if o.awaitClaim(t, id) {
			return id
		}
	}
	t.Fatalf("the stack's worker took every one of %d events first", claimTries)
	return uuid.Nil
}

// awaitClaim waits for the worker to take id, and says false when the stack's worker took it instead. An earlier
// event of this test coming due again is no answer: the worker takes the oldest first, so it may come before id.
func (o *offerer) awaitClaim(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	deadline := time.After(claimWait)
	for {
		select {
		case got := <-o.claimed:
			if got == id {
				return true
			}
		case <-deadline:
			if o.h.countRows(t, `SELECT count(*) FROM outbox_event WHERE id = $1 AND processed_at IS NULL`, id) > 0 {
				t.Fatalf("the worker did not take event %s within %s", id, claimWait)
			}
			return false
		}
	}
}

// A failed event is tried again later, each time later still, and after the
// last attempt it is set aside and recorded in the audit log.
func TestAFailedEventWaitsAndAGivenUpOneIsAudited(t *testing.T) {
	h := newHarness(t)

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
	h := newHarness(t)
	var mu sync.Mutex
	ours := map[uuid.UUID]bool{}
	started := make(chan uuid.UUID, 1)
	release := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(release) }) }
	t.Cleanup(stop)
	h.runWorker(t, events.HandlerFunc(func(ctx context.Context, e events.Event) error {
		mu.Lock()
		mine := ours[e.ID]
		mu.Unlock()
		if !mine {
			return errNotThisTest
		}
		started <- e.ID
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}))

	handling := uuid.Nil
	for try := 0; try < claimTries && handling == uuid.Nil; try++ {
		id := h.plantEvent(t, 0)
		mu.Lock()
		ours[id] = true
		mu.Unlock()
		select {
		case handling = <-started:
		case <-time.After(claimWait):
		}
	}
	if handling == uuid.Nil {
		t.Fatalf("the stack's worker took every one of %d events first", claimTries)
	}
	if report, err := h.execBounded(t, h.super, deleteBound, `DELETE FROM outbox_event WHERE id = $1`, handling); err != nil {
		t.Fatalf("deleting the event while it is handled: %v\n%s", err, report)
	}
	stop()
	if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE id = $1`, handling); n != 0 {
		t.Error("the event handled while it was deleted is still there")
	}
}
