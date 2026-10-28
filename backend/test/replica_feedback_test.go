//go:build integration

package test

import (
	"context"
	"testing"
	"time"
)

// feedbackWait bounds how long the standby takes to tell the primary about a snapshot, which it does on its
// status interval, ten seconds by default.
const feedbackWait = 20 * time.Second

// A standby tells the primary which rows a read on it still sees, so the primary keeps them and replay never has to
// cancel the read for a conflict with recovery, which answered a long document read 500 (#435).
func TestTheReplicaKeepsWhatItsReadsSee(t *testing.T) {
	h := newHarness(t)
	if h.replica == nil {
		t.Fatalf("%s is not set; run the suite with make test-integration against the running stack", envReplicaSuperuser)
	}
	ctx := context.Background()
	var feedback string
	if err := h.replica.QueryRow(ctx, `SHOW hot_standby_feedback`).Scan(&feedback); err != nil || feedback != "on" {
		t.Fatalf("the standby runs with hot_standby_feedback %q: %v", feedback, err)
	}

	tx, err := h.replica.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	var xmin string
	if err := tx.QueryRow(ctx, `SELECT pg_snapshot_xmin(pg_current_snapshot())::text`).Scan(&xmin); err != nil {
		t.Fatalf("hold a snapshot on the standby: %v", err)
	}
	deadline := time.Now().Add(feedbackWait)
	for {
		// The standby streams through a slot, which is where the primary keeps what the standby told it.
		var held bool
		if err := h.super.QueryRow(ctx, `SELECT COALESCE(bool_or(xmin IS NOT NULL AND xmin::text::bigint <= $1::bigint), false)
			FROM pg_replication_slots WHERE slot_type = 'physical'`, xmin).Scan(&held); err != nil {
			t.Fatal(err)
		}
		if held {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("within %s the primary heard of no snapshot the standby holds (its xmin %s)", feedbackWait, xmin)
		}
		time.Sleep(replicaPollInterval)
	}
}
