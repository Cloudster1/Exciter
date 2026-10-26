//go:build integration

package test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/installation"
)

// Settings for the clusters these tests open themselves: a lag bound small
// enough to cross in a test, and a health loop either quick or out of the way.
const (
	testMaxLag        = 300 * time.Millisecond
	quickHealth       = 100 * time.Millisecond
	neverHealth       = time.Hour
	lagBuildUp        = 3 * testMaxLag
	rotationWait      = 15 * time.Second
	replicaCaughtWait = 15 * time.Second
)

func needReplica(t *testing.T, h *harness) {
	t.Helper()
	if h.replica == nil {
		t.Skipf("%s and EXCITER_DB_REPLICA_URLS are not set; run the suite with make test-integration", envReplicaSuperuser)
	}
}

// openCluster opens a cluster of the test's own with adjusted settings.
func (h *harness) openCluster(t *testing.T, maxLag, health time.Duration) *db.Cluster {
	t.Helper()
	cfg := h.cfg.DB
	cfg.MaxReplicaLag = maxLag
	cfg.HealthInterval = health
	c, err := db.Open(t.Context(), cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("open cluster: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

// rename gives the installation a new name: a write any owner may make, and as
// good a probe of replication as any.
func rename(t *testing.T, c *db.Cluster, m member, name string) db.LSN {
	t.Helper()
	lsn, err := c.Write(m.ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE installation SET name = $1`, name)
		return err
	})
	if err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if lsn == 0 {
		t.Fatal("Write returned a zero position, so nothing can be pinned to it")
	}
	return lsn
}

// named counts whether ctx's organization reads as name, 1 or 0.
func named(t *testing.T, c *db.Cluster, ctx context.Context, name string) int {
	t.Helper()
	var n int
	if err := c.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM installation WHERE name = $1`, name).Scan(&n)
	}); err != nil {
		t.Fatalf("read: %v", err)
	}
	return n
}

// The baseline: without it the tests below would pass on a cluster that never
// used a replica at all.
func TestReadsReachTheReplica(t *testing.T) {
	h := newHarness(t)
	needReplica(t, h)
	m := h.owner(t, "routing")

	before := h.cluster.Stats()
	for range 5 {
		named(t, h.cluster, m.ctx, "none")
	}
	after := h.cluster.Stats()
	if served := after.ReadsToReplica - before.ReadsToReplica; served != 5 {
		t.Errorf("%d of 5 reads went to the replica (primary: %d)", served, after.ReadsToPrimary-before.ReadsToPrimary)
	}
}

// A position read inside the transaction sits before the commit record, and
// a replica there has not replayed the change.
func TestWriteReportsThePositionPastItsCommit(t *testing.T) {
	h := newHarness(t)
	h.keepInstallationName(t)
	m := h.owner(t, "commitlsn")

	var inside db.LSN
	after, err := h.cluster.Write(m.ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `UPDATE installation SET name = 'commit'`); err != nil {
			return err
		}
		var text string
		if err := tx.QueryRow(ctx, `SELECT pg_current_wal_insert_lsn()::text`).Scan(&text); err != nil {
			return err
		}
		var err error
		inside, err = db.ParseLSN(text)
		return err
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if after <= inside {
		t.Fatalf("reported position %v is not past the one inside the transaction %v", after, inside)
	}
}

// With replay paused the standby is behind by construction, rather than by a
// race the test hopes to win.
func TestReadYourWritesWhileReplayIsPaused(t *testing.T) {
	h := newHarness(t)
	h.keepInstallationName(t)
	needReplica(t, h)
	// The health loop is kept out of the way, so what is proven is the check
	// on the connection each read gets.
	c := h.openCluster(t, 0, neverHealth)
	m := h.owner(t, "ryw")

	h.pauseReplay(t)
	lsn := rename(t, c, m, "after-pause")

	t.Run("the writer sees their write, from the primary", func(t *testing.T) {
		before := c.Stats()
		if got := named(t, c, db.PinLSN(m.ctx, lsn), "after-pause"); got != 1 {
			t.Fatalf("a reader pinned to their own write saw %d rows, want 1", got)
		}
		after := c.Stats()
		if after.StaleFallbacks-before.StaleFallbacks != 1 || after.ReadsToPrimary-before.ReadsToPrimary != 1 {
			t.Errorf("the pinned read was not a stale fallback to the primary: %+v then %+v", before, after)
		}
	})

	t.Run("a reader with no write of their own is served the older snapshot", func(t *testing.T) {
		before := c.Stats()
		if got := named(t, c, m.ctx, "after-pause"); got != 0 {
			t.Fatalf("the paused replica answered with %d rows; it should not have the write yet", got)
		}
		if c.Stats().ReadsToReplica-before.ReadsToReplica != 1 {
			t.Error("the unpinned read did not go to the replica")
		}
	})

	h.resumeReplay(t)
	if !h.waitForReplica(t, lsn, replicaCaughtWait) {
		t.Fatal("the replica did not catch up after replay resumed")
	}

	t.Run("once replayed, the pinned read goes back to the replica", func(t *testing.T) {
		before := c.Stats()
		if got := named(t, c, db.PinLSN(m.ctx, lsn), "after-pause"); got != 1 {
			t.Fatalf("after replay resumed the pinned read saw %d rows, want 1", got)
		}
		if c.Stats().ReadsToReplica-before.ReadsToReplica != 1 {
			t.Error("the caught up replica did not serve the pinned read")
		}
	})
}

// A connection further behind than the bound serves nobody, even between two
// passes of the health loop.
func TestALaggingConnectionFallsBackToThePrimary(t *testing.T) {
	h := newHarness(t)
	h.keepInstallationName(t)
	needReplica(t, h)
	c := h.openCluster(t, testMaxLag, neverHealth)
	m := h.owner(t, "lag")

	// Every commit the replica has replayed came before the pause, so from here
	// on it lags at least as long as it has been paused, however slow the host.
	h.pauseReplay(t)
	paused := time.Now()
	lsn := rename(t, c, m, "backlog")
	// A standby that has replayed all it received reports no lag at all.
	if !h.waitForReceipt(t, lsn, replicaCaughtWait) {
		t.Fatal("the replica did not receive the write it is to lag behind")
	}
	time.Sleep(time.Until(paused.Add(lagBuildUp)))

	// The pass at Open judged a replica still replaying the tests before this
	// one, which no bound can promise to pass; the read is what is on trial.
	c.AdmitReplicas()

	before := c.Stats()
	if got := named(t, c, m.ctx, "backlog"); got != 1 {
		t.Fatalf("a read while the replica lags saw %d rows, want the primary's 1", got)
	}
	after := c.Stats()
	if after.LagFallbacks-before.LagFallbacks != 1 || after.ReadsToPrimary-before.ReadsToPrimary != 1 {
		t.Errorf("the read was not a lag fallback to the primary: %+v then %+v", before, after)
	}
}

// The health loop takes a lagging replica out of the rotation altogether and
// puts it back once it has caught up.
func TestALaggingReplicaLeavesTheRotationAndReturns(t *testing.T) {
	h := newHarness(t)
	h.keepInstallationName(t)
	needReplica(t, h)
	c := h.openCluster(t, testMaxLag, quickHealth)
	m := h.owner(t, "rotation")

	h.pauseReplay(t)
	var last db.LSN
	for i := range 3 {
		last = rename(t, c, m, fmt.Sprintf("rotation-%d", i))
		time.Sleep(lagBuildUp / 3)
	}
	waitFor(t, "the replica to leave the rotation", func() bool { return !c.Stats().Replicas[0].Healthy })

	before := c.Stats()
	if got := named(t, c, m.ctx, "rotation-2"); got != 1 {
		t.Fatalf("with the replica out, a read saw %d rows, want the primary's 1", got)
	}
	if c.Stats().NoHealthyReplicaHit-before.NoHealthyReplicaHit != 1 {
		t.Error("the read was not sent to the primary for want of a healthy replica")
	}

	h.resumeReplay(t)
	if !h.waitForReplica(t, last, replicaCaughtWait) {
		t.Fatal("the replica did not catch up after replay resumed")
	}
	waitFor(t, "the replica to rejoin the rotation", func() bool { return c.Stats().Replicas[0].Healthy })
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(rotationWait)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("waited %s for %s", rotationWait, what)
		}
		time.Sleep(quickHealth)
	}
}

// A page is read in many statements, and the replica replaying a write between
// two of them once served a page with an owner newer than its permissions.
func TestAReadSeesOneMomentThroughout(t *testing.T) {
	h := newHarness(t)
	h.keepInstallationName(t)
	needReplica(t, h)
	c := h.openCluster(t, 0, neverHealth)
	m := h.owner(t, "snapshot")

	before := c.Stats()
	err := c.Read(m.ctx, func(ctx context.Context, tx db.DBTX) error {
		count := func() int {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM installation WHERE name = 'mid-read'`).Scan(&n); err != nil {
				t.Fatalf("count: %v", err)
			}
			return n
		}
		if got := count(); got != 0 {
			t.Fatalf("the read began with %d rows, want 0", got)
		}
		lsn := rename(t, c, m, "mid-read")
		if !h.waitForReplica(t, lsn, replicaCaughtWait) {
			t.Fatal("the replica did not replay the write made halfway through the read")
		}
		if got := count(); got != 0 {
			t.Errorf("the read saw %d rows written after it began, want 0", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if c.Stats().ReadsToReplica-before.ReadsToReplica != 1 {
		t.Error("the read did not go to the replica, where replay can land between its statements")
	}
	if got := named(t, c, m.ctx, "mid-read"); got != 1 {
		t.Errorf("a read after it saw %d rows, want 1", got)
	}
}

// The store is shared by every api process, so it, not a process, decides
// that an older position never replaces a newer one.
func TestValkeyKeepsTheNewestPosition(t *testing.T) {
	h := newHarness(t)
	tracker := h.freshness(t)
	ctx := t.Context()
	key := "c:" + uuid.NewString()

	tracker.Note(ctx, key, 0x1_0000_0000)
	tracker.Note(ctx, key, 0xFFFF)
	if got := tracker.Required(ctx, key); got != 0x1_0000_0000 {
		t.Fatalf("Required = %v after an older position arrived, want 1/0", got)
	}
	tracker.Note(ctx, key, 0x1_0000_0001)
	if got := tracker.Required(ctx, key); got != 0x1_0000_0001 {
		t.Fatalf("Required = %v, want the newer 1/1", got)
	}
	if got := tracker.Required(ctx, "c:"+uuid.NewString()); got != 0 {
		t.Fatalf("a key that never wrote requires %v", got)
	}
}

// A read the primary answered while the replica was out holds the reads after it to what it showed: in the same
// request, and through the position it hands on, in the caller's next ones (#374).
func TestAReadDoesNotGoBackOnceThePrimaryAnswered(t *testing.T) {
	h := newHarness(t)
	h.keepInstallationName(t)
	needReplica(t, h)
	c := h.openCluster(t, 0, neverHealth)
	m := h.owner(t, "monotonic")

	h.pauseReplay(t)
	// The worker's write: nobody's own, so no request is pinned to it.
	lsn := rename(t, c, m, "worker-wrote")

	var noted db.LSN
	ctx := db.TrackReads(m.ctx, func(at db.LSN) { noted = at })
	c.ExpelReplicas()
	if got := named(t, c, ctx, "worker-wrote"); got != 1 {
		t.Fatalf("with the replica out the primary showed %d rows, want 1", got)
	}
	if noted < lsn {
		t.Fatalf("the primary's answer was handed on as %v, before the write at %v", noted, lsn)
	}

	c.AdmitReplicas()
	if got := named(t, c, m.ctx, "worker-wrote"); got != 0 {
		t.Fatalf("the paused replica showed %d rows; it should not have the write yet", got)
	}
	before := c.Stats()
	if got := named(t, c, ctx, "worker-wrote"); got != 1 {
		t.Fatalf("a later read of the same request saw %d rows, going back to the replica", got)
	}
	if c.Stats().StaleFallbacks-before.StaleFallbacks != 1 {
		t.Error("the later read was not kept off the replica that is behind")
	}
	if got := named(t, c, db.PinLSN(m.ctx, noted), "worker-wrote"); got != 1 {
		t.Errorf("the next request, pinned to what was handed on, saw %d rows", got)
	}

	var pinned db.LSN
	c.ExpelReplicas()
	named(t, c, db.TrackReads(db.PinPrimary(m.ctx), func(at db.LSN) { pinned = at }), "worker-wrote")
	if pinned != 0 {
		t.Errorf("a read pinned to the primary handed on %v; it chose to see the latest and moves nobody", pinned)
	}

	h.resumeReplay(t)
	if !h.waitForReplica(t, noted, replicaCaughtWait) {
		t.Fatal("the replica did not catch up after replay resumed")
	}
	c.AdmitReplicas()
	before = c.Stats()
	if got := named(t, c, ctx, "worker-wrote"); got != 1 || c.Stats().ReadsToReplica-before.ReadsToReplica != 1 {
		t.Errorf("once caught up the replica did not serve the request again (%d rows)", got)
	}
}

// Through the API: a person who saw the worker's change from the primary still sees it on their next request,
// while the replica is behind (#374).
func TestTheNextRequestShowsWhatThePrimaryShowed(t *testing.T) {
	h := newHarness(t)
	h.keepInstallationName(t)
	needReplica(t, h)
	c := h.openCluster(t, 0, neverHealth)
	api := newAPIServerWith(t, h, func(_ *apiServer, s *httpapi.Server) { s.Installation = installation.NewService(c) })
	m := h.owner(t, "next-request")
	alice := api.as(t, m.user)
	name := func() string {
		return obj(t, want(t, alice.get(t, "/api/v1/installation"), http.StatusOK, "read the installation"), "installation")["name"].(string)
	}

	h.pauseReplay(t)
	rename(t, c, m, "worker-done")
	c.ExpelReplicas()
	if got := name(); got != "worker-done" {
		t.Fatalf("with the replica out the installation is %q, want the primary's", got)
	}
	c.AdmitReplicas()
	if got := named(t, c, m.ctx, "worker-done"); got != 0 {
		t.Fatalf("the paused replica showed %d rows; it should not have the write yet", got)
	}
	if got := name(); got != "worker-done" {
		t.Errorf("the next request read %q from the replica, going back on what the primary showed", got)
	}
}
