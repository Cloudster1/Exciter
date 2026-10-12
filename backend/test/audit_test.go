//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/keyset"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// plantEntry writes an entry behind the policies' back, at a time of the test's choosing.
func (h *harness) plantEntry(t *testing.T, org uuid.UUID, action string, actor *uuid.UUID, targetType string, target *uuid.UUID, at time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `
		INSERT INTO audit_log (org_id, actor_user_id, action, target_type, target_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, org, actor, action, targetType, target, at).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func permissionDenied(t *testing.T, what string, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Errorf("%s = %v, want permission denied", what, err)
	}
}

// Straight through SQL: only an organization's owners and administrators read
// its record, and nobody rewrites, deletes or forges an entry.
func TestTheAuditLogIsTheAdministratorsAndOnlyEverAddedTo(t *testing.T) {
	h := newHarness(t)
	home := h.makeMember(t, "audit-read")
	memberID := h.addPerson(t, home.org, "member")
	adminID := h.addPerson(t, home.org, "admin")
	h.plantEntry(t, home.org, audit.ActionEventDeadLettered, nil, "event", nil, time.Now())
	h.settle(t)

	as := func(who uuid.UUID) context.Context {
		return db.WithUser(tenant.WithOrg(context.Background(), tenant.Org{ID: home.org}), who)
	}
	for who, want := range map[uuid.UUID]bool{home.user: true, adminID: true, memberID: false} {
		if n := h.count(t, as(who), `SELECT count(*) FROM audit_log`); (n > 0) != want {
			t.Errorf("%s reads %d entries, want any: %v", who, n, want)
		}
	}
	if n := h.count(t, tenant.WithOrg(context.Background(), tenant.Org{ID: home.org}), `SELECT count(*) FROM audit_log`); n != 0 {
		t.Errorf("a transaction acting for nobody reads %d entries", n)
	}

	ctx := as(home.user)
	for _, sql := range []string{`UPDATE audit_log SET action = 'nothing.done'`, `DELETE FROM audit_log`, `SELECT audit_log_prune(now() - interval '400 days')`} {
		_, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, sql)
			return err
		})
		permissionDenied(t, sql+" as the app role", err)
		if strings.Contains(sql, "audit_log_prune") {
			continue
		}
		_, err = h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, sql)
			return err
		})
		permissionDenied(t, sql+" as the admin role", err)
	}
	_, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		return audit.Write(ctx, tx, home.org, audit.Entry{Action: audit.ActionAuditExported, TargetType: "org", Actor: memberID})
	})
	permissionDenied(t, "an entry naming somebody else", err)
	_, err = h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `SELECT audit_log_prune(now() - interval '400 days')`)
		return err
	})
	permissionDenied(t, "pruning outside an organization", err)
	_, err = h.cluster.WriteAdmin(tenant.WithOrg(context.Background(), tenant.Org{ID: home.org}), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `SELECT audit_log_prune(now() - interval '23 hours')`)
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "22023" {
		t.Errorf("pruning the last day = %v, want refused", err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1`, home.org); n == 0 {
		t.Error("the record lost its entries")
	}
}

// An entry is written in the transaction of its act: a rolled back act leaves
// none, a committed one always has one.
func TestAnEntryCommitsAndRollsBackWithItsAct(t *testing.T) {
	h := newHarness(t)
	home := h.makeMember(t, "audit-tx")
	marker := uuid.New()
	refused := errors.New("the act failed after its entry was written")
	write := func(fail bool) error {
		_, err := h.cluster.Write(home.ctx, func(ctx context.Context, tx db.DBTX) error {
			if err := audit.Write(ctx, tx, home.org, audit.Entry{Action: audit.ActionAuditExported, TargetType: "org", TargetID: &marker, Actor: home.user}); err != nil {
				return err
			}
			if fail {
				return refused
			}
			return nil
		})
		return err
	}
	if err := write(true); !errors.Is(err, refused) {
		t.Fatalf("the act = %v", err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE target_id = $1`, marker); n != 0 {
		t.Errorf("a rolled back act left %d entries", n)
	}
	if err := write(false); err != nil {
		t.Fatal(err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE target_id = $1 AND actor_user_id = $2`, marker, home.user); n != 1 {
		t.Errorf("a committed act left %d entries, want 1", n)
	}
}

// actionsOf walks a filtered log a window of two at a time and lists the
// actions it met, newest first.
func actionsOf(t *testing.T, s *audit.Service, ctx context.Context, f audit.Filter) []string {
	t.Helper()
	var out []string
	var after *keyset.Cursor
	for range 50 {
		entries, next, err := s.List(ctx, f, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			out = append(out, e.Action)
		}
		if next == nil {
			return out
		}
		cursor, err := keyset.Decode(*next)
		if err != nil {
			t.Fatalf("the cursor %q does not decode: %v", *next, err)
		}
		after = cursor
	}
	t.Fatal("the walk through the log did not end")
	return nil
}

// The log is read newest first a window at a time, and narrowed by action,
// actor, target and time; the export holds what the filter holds.
func TestTheAuditLogIsWalkedAndNarrowed(t *testing.T) {
	h := newHarness(t)
	home := h.makeMember(t, "audit-walk")
	annID := h.namedPerson(t, home.org, "Ann Walker")
	event, project := uuid.New(), uuid.New()
	day := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -10)
	at := func(days int) time.Time { return day.AddDate(0, 0, days).Add(12 * time.Hour) }
	h.plantEntry(t, home.org, "project.created", &home.user, "project", &project, at(0))
	h.plantEntry(t, home.org, "project.updated", &annID, "project", &project, at(1))
	h.plantEntry(t, home.org, audit.ActionAuditExported, &annID, "org", nil, at(2))
	// Two at one instant: the id keeps them in order across a window's edge.
	h.plantEntry(t, home.org, "member.admitted", &home.user, "user", &annID, at(3))
	h.plantEntry(t, home.org, "member.role_changed", &home.user, "user", &annID, at(3))
	h.plantEntry(t, home.org, audit.ActionEventDeadLettered, nil, "event", &event, at(4))
	h.settle(t)

	s := audit.NewService(h.cluster)
	ctx := home.ctx
	all := actionsOf(t, s, ctx, audit.Filter{})
	if len(all) != 6 || all[0] != audit.ActionEventDeadLettered || all[5] != "project.created" {
		t.Errorf("the whole log reads %v", all)
	}
	from, to := at(1).Truncate(24*time.Hour), at(3).Truncate(24*time.Hour)
	sameList(t, "by action", actionsOf(t, s, ctx, audit.Filter{Action: "project.updated"}), "project.updated")
	sameList(t, "by actor", actionsOf(t, s, ctx, audit.Filter{ActorID: &annID}), audit.ActionAuditExported, "project.updated")
	sameList(t, "by target", actionsOf(t, s, ctx, audit.Filter{TargetID: &project}), "project.updated", "project.created")
	sameList(t, "by kind of target", actionsOf(t, s, ctx, audit.Filter{TargetType: "user"}), "member.role_changed", "member.admitted")
	sameList(t, "by time", actionsOf(t, s, ctx, audit.Filter{From: &from, To: &to}), audit.ActionAuditExported, "project.updated")

	facets, err := s.Facets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(facets.Actions) != 6 || len(facets.Actors) != 2 || len(facets.TargetTypes) != 4 {
		t.Errorf("the facets read %+v", facets)
	}

	file, n, err := s.CSV(ctx, audit.Filter{ActorID: &annID})
	if err != nil || n != 2 {
		t.Fatalf("the export = %d entries, %v", n, err)
	}
	lines, err := csv.NewReader(bytes.NewReader(file)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 || strings.Join(lines[0], ",") != strings.Join(audit.CSVHeader, ",") || lines[1][1] != audit.ActionAuditExported || lines[1][3] != "Ann Walker" || lines[2][5] != project.String() {
		t.Errorf("the export reads %q", lines)
	}

	// A member reads nothing of it, by the database's policy rather than the service's.
	member := db.WithUser(tenant.WithOrg(context.Background(), tenant.Org{ID: home.org}), annID)
	if got := actionsOf(t, s, member, audit.Filter{}); len(got) != 0 {
		t.Errorf("a member walked %d entries", len(got))
	}
}

// Retention deletes what is past its window, in every organization, and nothing younger.
func TestRetentionTakesOnlyTheOld(t *testing.T) {
	h := newHarness(t)
	a, b := h.makeMember(t, "audit-keep-a"), h.makeMember(t, "audit-keep-b")
	now := time.Now()
	old := h.plantEntry(t, a.org, audit.ActionAuditExported, &a.user, "org", nil, now.AddDate(0, 0, -40))
	oldB := h.plantEntry(t, b.org, audit.ActionAuditExported, &b.user, "org", nil, now.AddDate(0, 0, -31))
	young := h.plantEntry(t, a.org, audit.ActionAuditExported, &a.user, "org", nil, now.AddDate(0, 0, -29))
	fresh := h.plantEntry(t, b.org, audit.ActionAuditExported, &b.user, "org", nil, now)

	n, err := audit.NewRetention(h.cluster, 30*24*time.Hour, discard()).Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Errorf("retention took %d entries, want at least the two old ones", n)
	}
	for id, kept := range map[uuid.UUID]bool{old: false, oldB: false, young: true, fresh: true} {
		if got := h.countRows(t, `SELECT count(*) FROM audit_log WHERE id = $1`, id) == 1; got != kept {
			t.Errorf("entry %s kept %v, want %v", id, got, kept)
		}
	}
	if n, err := audit.NewRetention(h.cluster, 0, discard()).Once(context.Background()); n != 0 || err != nil {
		t.Errorf("keeping forever took %d entries, %v", n, err)
	}
}
