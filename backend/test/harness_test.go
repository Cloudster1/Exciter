//go:build integration

// Package test holds the integration suite. It runs against a real Postgres,
// because row level security is a property of the database that cannot be faked.
package test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/freshness"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// Environment the harness needs, set by make test-integration.
const (
	envPrimary          = "EXCITER_DB_PRIMARY_URL"
	envSuperuser        = "EXCITER_TEST_SUPERUSER_URL"
	envReplicaSuperuser = "EXCITER_TEST_REPLICA_SUPERUSER_URL"
)

// replicaCatchUpWait bounds how long arranging a test waits for the replica
// to replay what was arranged behind the policies' back.
const replicaCatchUpWait = 15 * time.Second

// replicaPollInterval is how often a wait on the replica asks it again.
const replicaPollInterval = 20 * time.Millisecond

// Bounds on the harness's own statements, so one stuck behind a lock fails its
// test with a report of who holds what instead of hanging the whole package.
const (
	// statementDeadline is how long one cleanup statement may run.
	statementDeadline = 30 * time.Second
	// statementWatchdog is how long one runs before the locks are reported.
	statementWatchdog = 10 * time.Second
	// testWatchdog is how long a test runs before the locks are reported.
	testWatchdog = 2 * time.Minute
	// lockReportTimeout bounds taking the report itself.
	lockReportTimeout = 5 * time.Second
	// superuserAppName marks the harness's sessions in pg_stat_activity.
	superuserAppName = "exciter-test-superuser"
)

// harness bundles the cluster under test, connected as exciter_app and
// exciter_admin, with a superuser pool used only to arrange and clean up.
type harness struct {
	cfg     config.Config
	cluster *db.Cluster
	super   *pgxpool.Pool
	// replica is the superuser on the standby, nil when none is configured;
	// only a superuser may pause and resume its replay.
	replica *pgxpool.Pool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	if os.Getenv(envPrimary) == "" || os.Getenv(envSuperuser) == "" {
		t.Skipf("%s and %s are not set; run the suite with make test-integration", envPrimary, envSuperuser)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cluster, err := db.Open(ctx, cfg.DB, log)
	if err != nil {
		t.Fatalf("open cluster: %v", err)
	}
	superCfg, err := pgxpool.ParseConfig(os.Getenv(envSuperuser))
	if err != nil {
		cluster.Close()
		t.Fatalf("parse %s: %v", envSuperuser, err)
	}
	superCfg.ConnConfig.RuntimeParams["application_name"] = superuserAppName
	super, err := pgxpool.NewWithConfig(ctx, superCfg)
	if err != nil {
		cluster.Close()
		t.Fatalf("connect superuser: %v", err)
	}
	h := &harness{cfg: cfg, cluster: cluster, super: super}
	if url := os.Getenv(envReplicaSuperuser); url != "" && len(cfg.DB.ReplicaURLs) > 0 {
		h.replica, err = pgxpool.New(ctx, url)
		if err != nil {
			t.Fatalf("connect superuser on the replica: %v", err)
		}
	}
	// Registered before the test's own cleanups, so it also watches them.
	watchdog := time.AfterFunc(testWatchdog, func() {
		// Straight to stderr: a test that hangs until go test gives up never
		// gets to print its log.
		fmt.Fprintf(os.Stderr, "%s has run for %s; what the database is doing:\n%s\n", t.Name(), testWatchdog, h.lockReport())
	})
	t.Cleanup(func() {
		watchdog.Stop()
		if h.replica != nil {
			h.replica.Close()
		}
		super.Close()
		cluster.Close()
	})
	return h
}

// bound is how long a harness statement may run, and when it is reported.
type bound struct {
	deadline time.Duration
	watchdog time.Duration
}

var statementBound = bound{deadline: statementDeadline, watchdog: statementWatchdog}

// execBounded runs one statement within b: past b.watchdog it logs a lock
// report, past b.deadline it gives up and returns the report taken then.
func (h *harness) execBounded(t *testing.T, pool *pgxpool.Pool, b bound, sql string, args ...any) (string, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watched := make(chan struct{})
	watchdog := time.AfterFunc(b.watchdog, func() {
		defer close(watched)
		t.Logf("%q is still running after %s; what the database is doing:\n%s", sql, b.watchdog, h.lockReport())
	})
	var report string
	gaveUp := make(chan struct{})
	// The report is taken before the cancel: once the statement is cancelled,
	// it no longer shows who it was waiting for.
	deadline := time.AfterFunc(b.deadline, func() {
		report = h.lockReport()
		close(gaveUp)
		cancel()
	})
	_, err := pool.Exec(ctx, sql, args...)
	if !watchdog.Stop() {
		<-watched
	}
	if !deadline.Stop() {
		<-gaveUp
		if err != nil {
			return report, fmt.Errorf("%q did not finish within %s: %w", sql, b.deadline, context.DeadlineExceeded)
		}
	}
	return "", err
}

// cleanupExec runs a cleanup statement within statementBound, and fails the
// test with a lock report when it does not finish in time.
func (h *harness) cleanupExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	report, err := h.execBounded(t, pool, statementBound, sql, args...)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a cleanup gave up: %v. Find the session that blocks it below and make it finish:\n%s", err, report)
	}
}

// lockReport lists the sessions that are not idle, who blocks whom, and the
// locks involved, on a connection of its own so a full pool cannot stop it.
func (h *harness) lockReport() string {
	ctx, cancel := context.WithTimeout(context.Background(), lockReportTimeout)
	defer cancel()
	var b strings.Builder
	stat := h.super.Stat()
	fmt.Fprintf(&b, "superuser pool: %d of %d connections in use\n", stat.AcquiredConns(), stat.MaxConns())
	conn, err := pgx.Connect(ctx, os.Getenv(envSuperuser))
	if err != nil {
		fmt.Fprintf(&b, "no lock report: connect: %v\n", err)
		return b.String()
	}
	defer func() { _ = conn.Close(context.Background()) }()

	rows, err := conn.Query(ctx, `
		SELECT a.pid, COALESCE(a.application_name, ''), COALESCE(a.usename::text, ''),
		       COALESCE(host(a.client_addr), 'local'), COALESCE(a.state, ''),
		       COALESCE(a.wait_event_type || ':' || a.wait_event, '-'),
		       COALESCE(to_char(now() - a.xact_start, 'HH24:MI:SS.MS'), '-'),
		       pg_blocking_pids(a.pid)::text,
		       left(regexp_replace(COALESCE(a.query, ''), '\s+', ' ', 'g'), 300)
		FROM pg_stat_activity a
		WHERE a.backend_type = 'client backend' AND a.pid <> pg_backend_pid()
		  AND a.state IS DISTINCT FROM 'idle'
		ORDER BY a.xact_start NULLS LAST`)
	if err != nil {
		fmt.Fprintf(&b, "no lock report: pg_stat_activity: %v\n", err)
		return b.String()
	}
	b.WriteString("sessions (pid, application, user, client, state, wait, transaction age, blocked by, query):\n")
	for rows.Next() {
		var (
			pid                                                  int32
			app, user, client, state, wait, age, blockers, query string
		)
		if err := rows.Scan(&pid, &app, &user, &client, &state, &wait, &age, &blockers, &query); err != nil {
			fmt.Fprintf(&b, "  scan: %v\n", err)
			break
		}
		fmt.Fprintf(&b, "  %d %q %s %s %q %s %s %s %s\n", pid, app, user, client, state, wait, age, blockers, query)
	}
	rows.Close()

	rows, err = conn.Query(ctx, `
		WITH involved AS (
		    SELECT unnest(pg_blocking_pids(pid)) AS pid FROM pg_stat_activity
		    UNION SELECT pid FROM pg_locks WHERE NOT granted
		)
		SELECT l.pid, l.locktype, COALESCE(l.relation::regclass::text, '-'), l.mode, l.granted
		FROM pg_locks l
		WHERE l.pid IN (SELECT pid FROM involved) AND l.locktype <> 'virtualxid'
		  -- Every write holds these on each table it touches; they block
		  -- nothing but DDL and would bury the locks that matter.
		  AND NOT (l.granted AND l.locktype = 'relation'
		           AND l.mode IN ('AccessShareLock', 'RowShareLock', 'RowExclusiveLock'))
		ORDER BY l.pid, l.granted, l.locktype, 3`)
	if err != nil {
		fmt.Fprintf(&b, "no lock report: pg_locks: %v\n", err)
		return b.String()
	}
	b.WriteString("locks of blocked and blocking sessions (pid, type, relation, mode, granted):\n")
	for rows.Next() {
		var (
			pid             int32
			kind, rel, mode string
			granted         bool
		)
		if err := rows.Scan(&pid, &kind, &rel, &mode, &granted); err != nil {
			fmt.Fprintf(&b, "  scan: %v\n", err)
			break
		}
		fmt.Fprintf(&b, "  %d %s %s %s %t\n", pid, kind, rel, mode, granted)
	}
	rows.Close()
	return b.String()
}

// settle waits until the replica has replayed everything written so far, for
// rows a test arranged as the superuser, which no read is pinned to.
func (h *harness) settle(t *testing.T) {
	t.Helper()
	if h.replica == nil {
		return
	}
	// With synchronous_commit off a commit can sit unsent in the WAL buffers; a message emitted with flush
	// writes out everything before it, so the replica need not wait for the WAL writer.
	var text string
	if err := h.super.QueryRow(context.Background(), `SELECT pg_logical_emit_message(false, 'exciter-settle', '', true)::text`).Scan(&text); err != nil {
		t.Fatalf("flush the primary's WAL: %v", err)
	}
	lsn, err := db.ParseLSN(text)
	if err != nil {
		t.Fatal(err)
	}
	if !h.waitForReplica(t, lsn, replicaCatchUpWait) {
		t.Fatalf("the replica did not replay %s within %s", lsn, replicaCatchUpWait)
	}
}

// waitForReplica reports whether the replica replays lsn within the wait.
func (h *harness) waitForReplica(t *testing.T, lsn db.LSN, wait time.Duration) bool {
	t.Helper()
	return h.waitForPosition(t, `SELECT COALESCE(pg_last_wal_replay_lsn(), '0/0')::text`, lsn, wait)
}

// waitForReceipt reports whether the replica receives lsn within the wait,
// whether or not it replays it.
func (h *harness) waitForReceipt(t *testing.T, lsn db.LSN, wait time.Duration) bool {
	t.Helper()
	return h.waitForPosition(t, `SELECT COALESCE(pg_last_wal_receive_lsn(), '0/0')::text`, lsn, wait)
}

func (h *harness) waitForPosition(t *testing.T, positionSQL string, lsn db.LSN, wait time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(wait)
	for {
		var text string
		if err := h.replica.QueryRow(context.Background(), positionSQL).Scan(&text); err != nil {
			t.Fatalf("read the replica's position: %v", err)
		}
		if at, err := db.ParseLSN(text); err == nil && at >= lsn {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(replicaPollInterval)
	}
}

// pauseReplay freezes the replica where it is until the test ends or
// resumeReplay is called, so it is behind by construction rather than by luck.
func (h *harness) pauseReplay(t *testing.T) {
	t.Helper()
	if _, err := h.replica.Exec(context.Background(), `SELECT pg_wal_replay_pause()`); err != nil {
		t.Fatalf("pause replay: %v", err)
	}
	t.Cleanup(func() { h.cleanupExec(t, h.replica, `SELECT pg_wal_replay_resume()`) })
	// The call only asks for a pause; until replay has stopped it may still
	// apply the write a test makes next, which then would not be held back.
	deadline := time.Now().Add(replicaCatchUpWait)
	for {
		var state string
		if err := h.replica.QueryRow(context.Background(), `SELECT pg_get_wal_replay_pause_state()`).Scan(&state); err != nil {
			t.Fatalf("read the replay pause state: %v", err)
		}
		if state == "paused" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("replay was still %s after %s", state, replicaCatchUpWait)
		}
		time.Sleep(replicaPollInterval)
	}
}

func (h *harness) resumeReplay(t *testing.T) {
	t.Helper()
	if _, err := h.replica.Exec(context.Background(), `SELECT pg_wal_replay_resume()`); err != nil {
		t.Fatalf("resume replay: %v", err)
	}
}

// member is one organization with one person in it, made behind the policies'
// back so that what a test observes is only the policies' doing.
type member struct {
	org  uuid.UUID
	user uuid.UUID
	ctx  context.Context
}

// suffix is unique per call, so repeated runs against a kept database do not collide.
func (h *harness) suffix() string { return uuid.NewString()[:8] }

func (h *harness) makeMember(t *testing.T, slug string) member {
	t.Helper()
	ctx := context.Background()
	full := fmt.Sprintf("%s-%s", slug, h.suffix())

	var m member
	if err := h.super.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2) RETURNING id`, full, slug).Scan(&m.org); err != nil {
		t.Fatalf("create org %s: %v", full, err)
	}
	if err := h.super.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ($1, $2) RETURNING id`,
		full+"@example.test", "Person of "+slug).Scan(&m.user); err != nil {
		t.Fatalf("create user in %s: %v", full, err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'owner')`, m.org, m.user); err != nil {
		t.Fatalf("make the user a member of %s: %v", full, err)
	}
	t.Cleanup(func() {
		h.cleanupExec(t, h.super, `DELETE FROM org WHERE id = $1`, m.org)
		h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE id = $1`, m.user)
	})
	m.ctx = db.WithUser(tenant.WithOrg(ctx, tenant.Org{ID: m.org, Slug: full}), m.user)
	h.settle(t)
	return m
}

// count runs a count(*) query in ctx's tenant scope on the primary.
func (h *harness) count(t *testing.T, ctx context.Context, sql string, args ...any) int {
	t.Helper()
	var n int
	err := h.cluster.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, sql, args...).Scan(&n)
	})
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func isNoTenant(err error) bool { return errors.Is(err, tenant.ErrNoTenant) }

// freshness is the Valkey tracker the running api would use, under a prefix of
// the suite's own so a test never reads the stack's positions.
func (h *harness) freshness(t *testing.T) *freshness.ValkeyTracker {
	t.Helper()
	if h.cfg.Valkey.URL == "" {
		t.Fatal("EXCITER_VALKEY_URL is not set; run the suite with make test-integration against the running stack")
	}
	opts, err := redis.ParseURL(h.cfg.Valkey.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	tracker := freshness.NewValkeyTracker(client, h.cfg.DB.ReadYourWritesTTL, "exciter-test:ryw:")
	if err := tracker.Ping(t.Context()); err != nil {
		t.Fatalf("reach Valkey: %v", err)
	}
	return tracker
}
