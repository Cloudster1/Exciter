//go:build integration

package test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// heldBound is short, so the test waits on the lock it holds only briefly.
var heldBound = bound{deadline: 2 * time.Second, watchdog: 500 * time.Millisecond}

// A statement stuck behind somebody else's lock gives up at its deadline, and
// the report taken then names the session that holds it up (#217).
func TestHarnessGivesUpOnAStatementBehindALock(t *testing.T) {
	h := newHarness(t)
	m := h.makeMember(t, "held")
	ctx := context.Background()

	tx, err := h.super.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	var holder int32
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM org WHERE id = $1 FOR UPDATE`, m.org); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	report, err := h.execBounded(t, h.super, heldBound, `DELETE FROM org WHERE id = $1`, m.org)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a delete behind a held row lock ended with %v, want it to give up at its deadline", err)
	}
	if elapsed > heldBound.deadline+lockReportTimeout {
		t.Errorf("giving up took %s, want about %s", elapsed, heldBound.deadline)
	}
	if !strings.Contains(err.Error(), heldBound.deadline.String()) {
		t.Errorf("the error %q does not say how long it waited", err)
	}
	for _, want := range []string{
		fmt.Sprintf("{%d}", holder),
		fmt.Sprintf("%d %q", holder, superuserAppName),
		"idle in transaction",
		"FOR UPDATE",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the lock report lacks %q:\n%s", want, report)
		}
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := h.execBounded(t, h.super, heldBound, `DELETE FROM org WHERE id = $1`, m.org); err != nil {
		t.Fatalf("the delete still fails once the lock is gone: %v", err)
	}
}
