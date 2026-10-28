//go:build integration

package test

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/review"
)

// leaseSlack is how far a lease read back may fall short of ClaimLease, for the time between the claim and the read.
const leaseSlack = 30 * time.Second

// plantedEvent is one row planted in an outbox of the test's own, aged as the test needs.
type plantedEvent struct {
	created, processed, available string
}

// plant adds an event of testTopic whose times are SQL expressions; an empty processed leaves it unprocessed.
func (h *harness) plant(t *testing.T, e plantedEvent) uuid.UUID {
	t.Helper()
	processed := "NULL"
	if e.processed != "" {
		processed = e.processed
	}
	available := "now()"
	if e.available != "" {
		available = e.available
	}
	created := "now()"
	if e.created != "" {
		created = e.created
	}
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `
		INSERT INTO outbox_event (topic, payload, created_at, processed_at, available_at)
		VALUES ($1, '{}', `+created+`, `+processed+`, `+available+`) RETURNING id`, testTopic).Scan(&id); err != nil {
		t.Fatalf("plant an event: %v", err)
	}
	return id
}

// eventRow is what the worker leaves on an event.
type eventRow struct {
	attempts  int
	lastError *string
	processed bool
	dueNow    bool
	leasedFor time.Duration
}

func (h *harness) eventRow(t *testing.T, id uuid.UUID) eventRow {
	t.Helper()
	var r eventRow
	if err := h.super.QueryRow(context.Background(), `
		SELECT attempts, last_error, processed_at IS NOT NULL, available_at <= now(), GREATEST(available_at - now(), '0'::interval)
		FROM outbox_event WHERE id = $1`, id).Scan(&r.attempts, &r.lastError, &r.processed, &r.dueNow, &r.leasedFor); err != nil {
		t.Fatalf("read event %s: %v", id, err)
	}
	return r
}

// recorder is a handler that keeps the events it was handed, in order.
type recorder struct {
	mu      sync.Mutex
	handled []uuid.UUID
}

func (r *recorder) Handle(_ context.Context, e events.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handled = append(r.handled, e.ID)
	return nil
}

func (r *recorder) ids() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uuid.UUID(nil), r.handled...)
}

func sameIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A session whose deadline comes within ReminderLead is reminded once, in its moderator's name; every other session
// is left alone, and a second pass, or two at once, remind nobody again.
func TestTheReviewReminderRemindsEachDueSessionOnce(t *testing.T) {
	h := ownOutbox(t, newHarness(t), "exciter_review_reminder_test")
	ctx := context.Background()
	var moderator, project uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO app_user (email, name, role) VALUES ($1, 'A moderator', 'owner') RETURNING id`,
		"moderator-"+uuid.NewString()+"@example.test").Scan(&moderator); err != nil {
		t.Fatalf("make the moderator: %v", err)
	}
	if err := h.super.QueryRow(ctx, `INSERT INTO project (key, name, created_by) VALUES ('REMIND', 'Reminded', $1) RETURNING id`,
		moderator).Scan(&project); err != nil {
		t.Fatalf("make the project: %v", err)
	}
	session := func(title, deadline string, closed bool) uuid.UUID {
		t.Helper()
		state, closedAt := "open", "NULL"
		if closed {
			state, closedAt = "closed", "now()"
		}
		var id uuid.UUID
		if err := h.super.QueryRow(ctx, `
			INSERT INTO review_session (project_id, title, scope_kind, moderator_id, deadline, quorum, state, closed_at)
			VALUES ($1, $2, 'query', $3, `+deadline+`, 1, '`+state+`', `+closedAt+`) RETURNING id`,
			project, title, moderator).Scan(&id); err != nil {
			t.Fatalf("make the session %s: %v", title, err)
		}
		return id
	}
	due := session("Due within the lead", "now() + interval '1 hour'", false)
	closed := session("Closed", "now() + interval '1 hour'", true)
	open := session("No deadline", "NULL", false)
	later := session("Due in three days", "now() + interval '3 days'", false)

	reminders := func(id uuid.UUID) []map[string]string {
		t.Helper()
		rows, err := h.super.Query(ctx, `SELECT payload FROM outbox_event WHERE topic = $1 AND payload ->> 'sessionId' = $2`,
			review.TopicSessionReminder, id.String())
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []map[string]string
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var p map[string]string
			if err := json.Unmarshal(raw, &p); err != nil {
				t.Fatal(err)
			}
			out = append(out, p)
		}
		return out
	}
	reminded := func(id uuid.UUID) bool {
		t.Helper()
		return h.countRows(t, `SELECT count(*) FROM review_session WHERE id = $1 AND reminded_at IS NOT NULL`, id) == 1
	}

	r := review.NewReminder(h.cluster, discard())
	n, err := r.Once(ctx)
	if err != nil || n != 1 {
		t.Fatalf("the first pass reminded %d sessions (%v), want the one due", n, err)
	}
	if !reminded(due) {
		t.Error("the session due within the lead has no reminded_at")
	}
	got := reminders(due)
	if len(got) != 1 {
		t.Fatalf("the due session has %d reminder events, want 1", len(got))
	}
	if got[0]["actorId"] != moderator.String() || got[0]["projectId"] != project.String() {
		t.Errorf("the reminder is %v, want the moderator %s as its actor and the project %s", got[0], moderator, project)
	}
	for name, id := range map[string]uuid.UUID{"closed": closed, "without a deadline": open, "due in three days": later} {
		if reminded(id) || len(reminders(id)) != 0 {
			t.Errorf("the session %s was reminded", name)
		}
	}

	if n, err := r.Once(ctx); err != nil || n != 0 {
		t.Errorf("a second pass reminded %d sessions (%v), want none", n, err)
	}
	if got := reminders(due); len(got) != 1 {
		t.Errorf("after a second pass the due session has %d reminder events, want 1", len(got))
	}

	both := session("Due while two workers run", "now() + interval '2 hours'", false)
	var wg sync.WaitGroup
	counts := make([]int, 2)
	errs := make([]error, 2)
	for i := range counts {
		wg.Go(func() { counts[i], errs[i] = review.NewReminder(h.cluster, discard()).Once(ctx) })
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || counts[0]+counts[1] != 1 {
		t.Errorf("two passes side by side reminded %v (%v, %v), want one session between them", counts, errs[0], errs[1])
	}
	if got := reminders(both); len(got) != 1 {
		t.Errorf("two passes side by side wrote %d reminder events, want 1", len(got))
	}
}

// The worker's first pass also prunes: a done event past Retention goes, a recent one and one still waiting stay.
func TestTheWorkerPrunesOnlyDoneEventsPastRetention(t *testing.T) {
	h := ownOutbox(t, newHarness(t), "exciter_outbox_prune_test")
	pastRetention := "now() - interval '8 days'"
	old := h.plant(t, plantedEvent{created: pastRetention, processed: pastRetention})
	recent := h.plant(t, plantedEvent{created: "now() - interval '1 hour'", processed: "now() - interval '1 hour'"})
	// Not yet due, so the worker leaves it waiting rather than handling it.
	waiting := h.plant(t, plantedEvent{created: pastRetention, available: "now() + interval '1 day'"})
	h.runWorker(t, &recorder{})

	waitFor(t, "the done event past Retention to be pruned", func() bool {
		return h.countRows(t, `SELECT count(*) FROM outbox_event WHERE id = $1`, old) == 0
	})
	for name, id := range map[string]uuid.UUID{"the event done an hour ago": recent, "the event still waiting": waiting} {
		if h.countRows(t, `SELECT count(*) FROM outbox_event WHERE id = $1`, id) != 1 {
			t.Errorf("%s was pruned", name)
		}
	}
	if r := h.eventRow(t, waiting); r.processed {
		t.Error("the event still waiting was marked done")
	}
}

// A handler that panics fails only its own event, which waits with the reason, and the rest of the batch goes on.
func TestAPanickingHandlerFailsOnlyItsEvent(t *testing.T) {
	h := ownOutbox(t, newHarness(t), "exciter_outbox_panic_test")
	bad := h.plant(t, plantedEvent{created: "now() - interval '1 second'"})
	good := h.plant(t, plantedEvent{})
	var handled []uuid.UUID
	handler := events.HandlerFunc(func(_ context.Context, e events.Event) error {
		if e.ID == bad {
			panic("the handler broke")
		}
		handled = append(handled, e.ID)
		return nil
	})

	n, err := events.NewWorker(h.cluster, handler, discard()).WithBatch(2).Once(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("the pass claimed %d (%v), want both events and no error", n, err)
	}
	r := h.eventRow(t, bad)
	if r.attempts != 1 || r.lastError == nil || *r.lastError != "the handler panicked" || r.processed || r.dueNow {
		t.Errorf("the event whose handler panicked reads %+v, want one attempt, the error \"the handler panicked\" and a wait", r)
	}
	if !sameIDs(handled, []uuid.UUID{good}) {
		t.Errorf("the handler was given %v after the panic, want the next event %s", handled, good)
	}
	if r := h.eventRow(t, good); !r.processed || r.attempts != 1 || r.lastError != nil {
		t.Errorf("the next event of the batch reads %+v, want it done at the first attempt", r)
	}
}

// A worker stopped mid batch hands back what it has not settled, so another takes it at once instead of after the
// lease; the event it was handling comes again too, since handlers run at least once.
func TestAWorkerStoppedMidBatchHandsTheRestBack(t *testing.T) {
	h := ownOutbox(t, newHarness(t), "exciter_outbox_stop_test")
	first := h.plant(t, plantedEvent{created: "now() - interval '2 seconds'"})
	second := h.plant(t, plantedEvent{created: "now() - interval '1 second'"})
	third := h.plant(t, plantedEvent{})
	all := []uuid.UUID{first, second, third}

	ctx, stop := context.WithCancel(context.Background())
	var stopped []uuid.UUID
	n, err := events.NewWorker(h.cluster, events.HandlerFunc(func(_ context.Context, e events.Event) error {
		stopped = append(stopped, e.ID)
		stop()
		return nil
	}), discard()).WithBatch(len(all)).Once(ctx)
	if n != len(all) || err == nil {
		t.Fatalf("the stopped pass claimed %d (%v), want all %d and the stop as its error", n, err, len(all))
	}
	if !sameIDs(stopped, []uuid.UUID{first}) {
		t.Errorf("the stopped worker handled %v, want only the first event", stopped)
	}
	for _, id := range all {
		if r := h.eventRow(t, id); r.processed || !r.dueNow || r.attempts != 0 {
			t.Errorf("event %s reads %+v after the stop, want it due now, not done and not counted as tried", id, r)
		}
	}

	next := &recorder{}
	if n, err := events.NewWorker(h.cluster, next, discard()).WithBatch(len(all)).Once(context.Background()); err != nil || n != len(all) {
		t.Fatalf("the next worker claimed %d (%v), want all %d at once", n, err, len(all))
	}
	if got := next.ids(); !sameIDs(got, all) {
		t.Errorf("the next worker handled %v, want %v oldest first", got, all)
	}
	for _, id := range all {
		if r := h.eventRow(t, id); !r.processed {
			t.Errorf("event %s is not done after the next worker", id)
		}
	}
}

// A worker that dies holding a claim neither settles nor hands it back: the event waits out ClaimLease, and then
// another worker takes it.
func TestAnEventLeftClaimedIsTakenOnceItsLeaseRunsOut(t *testing.T) {
	h := ownOutbox(t, newHarness(t), "exciter_outbox_lease_test")
	id := h.plant(t, plantedEvent{})

	died := make(chan struct{})
	go func() {
		defer close(died)
		// Goexit ends the worker's goroutine mid handler as a crash would: no settle, no release.
		_, _ = events.NewWorker(h.cluster, events.HandlerFunc(func(context.Context, events.Event) error {
			runtime.Goexit()
			return nil
		}), discard()).Once(context.Background())
	}()
	<-died

	r := h.eventRow(t, id)
	if r.processed || r.dueNow || r.attempts != 0 || r.leasedFor < events.ClaimLease-leaseSlack || r.leasedFor > events.ClaimLease {
		t.Fatalf("the event its worker died holding reads %+v, want it leased for about %s", r, events.ClaimLease)
	}
	next := &recorder{}
	if n, err := events.NewWorker(h.cluster, next, discard()).Once(context.Background()); err != nil || n != 0 {
		t.Fatalf("another worker claimed %d (%v) while the lease runs, want nothing", n, err)
	}

	// The lease running out, without waiting five minutes for it.
	if _, err := h.super.Exec(context.Background(), `UPDATE outbox_event SET available_at = now() - interval '1 second' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if n, err := events.NewWorker(h.cluster, next, discard()).Once(context.Background()); err != nil || n != 1 {
		t.Fatalf("another worker claimed %d (%v) once the lease ran out, want the event", n, err)
	}
	if !sameIDs(next.ids(), []uuid.UUID{id}) {
		t.Errorf("the other worker handled %v, want %s", next.ids(), id)
	}
	if r := h.eventRow(t, id); !r.processed || r.attempts != 1 {
		t.Errorf("the event reads %+v after the other worker, want it done at its first counted attempt", r)
	}
}
