//go:build integration

package test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/installation"
	"github.com/cloudster1/exciter/backend/internal/seed"
	"github.com/cloudster1/exciter/backend/migrations"
)

const (
	// beforeTheGuard is the last migration before 00400, which refuses several organizations.
	beforeTheGuard = 310
	theGuard       = 400
)

// scratchDatabase is a database of its own on the stack's cluster, dropped afterwards, with goose and a superuser
// on it, and the way to name it in another address, for what the shared installation cannot show: being empty,
// holding several organizations, or having a single administrator.
func scratchDatabase(t *testing.T, h *harness, name string) (*goose.Provider, *pgxpool.Pool, func(string) string) {
	t.Helper()
	ctx := context.Background()
	// A run that died leaves its database behind; nothing else connects to it.
	drop := func() { h.cleanupExec(t, h.super, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)") }
	drop()
	if _, err := h.super.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	t.Cleanup(drop)

	at := func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse a database address: %v", err)
		}
		u.Path = "/" + name
		return u.String()
	}
	super, err := pgxpool.New(ctx, at(os.Getenv(envSuperuser)))
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	t.Cleanup(super.Close)
	sqlDB := stdlib.OpenDBFromPool(super)
	t.Cleanup(func() { _ = sqlDB.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS, goose.WithAllowOutofOrder(true))
	if err != nil {
		t.Fatalf("goose on %s: %v", name, err)
	}
	return provider, super, at
}

// The guard counts organizations as the migration runs, and with more than one stops, naming them and saying what
// to do, and leaves the database where it was.
func TestTheUpgradeRefusesSeveralOrganizations(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	provider, super, _ := scratchDatabase(t, h, "exciter_one_installation_test")
	const (
		// One more than the refusal lists, so the count of the rest shows.
		organizations = 11
		listed        = 10
	)
	if _, err := provider.UpTo(ctx, beforeTheGuard); err != nil {
		t.Fatalf("migrate to %d: %v", beforeTheGuard, err)
	}
	insertOrg := func(t *testing.T, slug, name string) {
		t.Helper()
		if _, err := super.Exec(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2)`, slug, name); err != nil {
			t.Fatalf("insert organization %s: %v", slug, err)
		}
	}
	version := func(t *testing.T) int64 {
		t.Helper()
		v, err := provider.GetDBVersion(ctx)
		if err != nil {
			t.Fatalf("read the version: %v", err)
		}
		return v
	}
	passes := func(t *testing.T, what string) {
		t.Helper()
		if _, err := provider.ApplyVersion(ctx, theGuard, true); err != nil {
			t.Fatalf("the guard refused %s: %v", what, err)
		}
		if _, err := provider.ApplyVersion(ctx, theGuard, false); err != nil {
			t.Fatalf("take the guard back after %s: %v", what, err)
		}
	}

	t.Run("an empty database passes", func(t *testing.T) {
		passes(t, "a database with no organization")
	})

	t.Run("one organization passes", func(t *testing.T) {
		insertOrg(t, "acme", "Acme Corp")
		passes(t, "one organization")
	})

	t.Run("several are refused, by name", func(t *testing.T) {
		insertOrg(t, "beta", "Beta Works")
		for i := 3; i <= organizations; i++ {
			insertOrg(t, fmt.Sprintf("extra-%02d", i), fmt.Sprintf("Extra %d", i))
		}
		_, err := provider.ApplyVersion(ctx, theGuard, true)
		if err == nil {
			t.Fatal("the upgrade ran over eleven organizations")
		}
		for _, want := range []string{
			fmt.Sprintf("%d organizations", organizations), "acme (Acme Corp)", "beta (Beta Works)",
			fmt.Sprintf("and %d more", organizations-listed), "DELETE FROM org WHERE slug", "then migrate again.",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal does not say %q: %v", want, err)
			}
		}
		if v := version(t); v != beforeTheGuard {
			t.Errorf("the database is at %d after the refusal, want it left at %d", v, beforeTheGuard)
		}
		var forced bool
		if err := super.QueryRow(ctx, `SELECT relforcerowsecurity FROM pg_class WHERE oid = 'org'::regclass`).Scan(&forced); err != nil || !forced {
			t.Errorf("the refusal left org's row security lifted: forced=%v err=%v", forced, err)
		}
	})

	t.Run("with the others deleted, the upgrade runs to the end", func(t *testing.T) {
		if _, err := super.Exec(ctx, `DELETE FROM org WHERE slug <> 'acme'`); err != nil {
			t.Fatal(err)
		}
		if _, err := provider.Up(ctx); err != nil {
			t.Fatalf("migrate the rest: %v", err)
		}
		all := provider.ListSources()
		if latest := all[len(all)-1].Version; version(t) != latest {
			t.Fatalf("the database is at %d, the migrations go to %d", version(t), latest)
		}
	})
}

// The seed runs on every start: the first makes the one organization under EXCITER_NAME, and every later one finds
// it, whatever an administrator renamed it to, and makes no second.
func TestAFirstStartMakesTheOneOrganization(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	provider, super, at := scratchDatabase(t, h, "exciter_first_start_test")
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := h.cfg.DB
	cfg.PrimaryURL, cfg.AdminURL, cfg.ReplicaURLs = at(cfg.PrimaryURL), at(cfg.AdminURL), nil
	cluster, err := db.Open(ctx, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("open the scratch database: %v", err)
	}
	t.Cleanup(cluster.Close)

	first, err := seed.Ensure(ctx, cluster, "Acme requirements")
	if err != nil || !first.OrgCreated || first.Org.Slug != installation.Slug {
		t.Fatalf("the first start = %+v, %v; want the organization made", first, err)
	}
	if _, err := super.Exec(ctx, `UPDATE org SET name = 'Renamed'`); err != nil {
		t.Fatal(err)
	}
	again, err := seed.Ensure(ctx, cluster, "Something else")
	if err != nil || again.OrgCreated || again.Org != first.Org {
		t.Fatalf("the second start = %+v, %v; want the first one found", again, err)
	}
	var count int
	var name string
	if err := super.QueryRow(ctx, `SELECT count(*), min(name) FROM org`).Scan(&count, &name); err != nil || count != 1 || name != "Renamed" {
		t.Errorf("after two starts there are %d organizations called %q (%v), want the one renamed", count, name, err)
	}
}

// An administrator renames the installation, which everybody signed in reads and the audit log keeps; a member is
// refused, and straight through SQL as exciter_app the database refuses them too.
func TestTheInstallationIsRenamedByAnAdministrator(t *testing.T) {
	h := newHarness(t)
	h.keepInstallationName(t)
	a := newAPIServer(t, h)
	home := h.owner(t, "installation")
	memberID := h.addPerson(t, home.org, "member")
	owner, member := a.as(t, home.user, home.org), a.as(t, memberID, home.org)

	renamed := want(t, owner.put(t, "/api/v1/installation", map[string]any{"name": "  Acme requirements "}), http.StatusOK, "rename")
	if obj(t, renamed, "installation")["name"] != "Acme requirements" {
		t.Errorf("the rename answers %s", renamed.Raw)
	}
	if got := obj(t, want(t, member.get(t, "/api/v1/installation"), http.StatusOK, "a member reads it"), "installation"); got["name"] != "Acme requirements" {
		t.Errorf("a member reads %v", got)
	}
	if inst := obj(t, want(t, member.get(t, "/api/v1/auth/me"), http.StatusOK, "who I am"), "installation"); inst["name"] != "Acme requirements" {
		t.Errorf("the shell reads the installation as %v", inst)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = 'installation.renamed' AND actor_user_id = $2
		AND data ->> 'to' = 'Acme requirements'`, home.org, home.user); n != 1 {
		t.Errorf("the rename is recorded %d times, want once", n)
	}

	for _, blank := range []string{"", "   "} {
		if f := fieldError(t, want(t, owner.put(t, "/api/v1/installation", map[string]any{"name": blank}), http.StatusUnprocessableEntity, "a blank name"), "name"); f == "" {
			t.Errorf("a blank name is not refused beside its field")
		}
	}
	tooLong := strings.Repeat("x", installation.MaxNameLength+1)
	want(t, owner.put(t, "/api/v1/installation", map[string]any{"name": tooLong}), http.StatusUnprocessableEntity, "a long name")
	if code := errorCode(t, want(t, member.put(t, "/api/v1/installation", map[string]any{"name": "Mine"}), http.StatusForbidden, "a member renames")); code != "forbidden" {
		t.Errorf("a member renaming answers %s", code)
	}

	touchesNothing(t, h.appConn(t, home.org, memberID), "a member renaming through SQL", `UPDATE org SET name = 'Mine' WHERE id = $1`, home.org)
	if n := h.countRows(t, `SELECT count(*) FROM org WHERE id = $1 AND name = 'Acme requirements'`, home.org); n != 1 {
		t.Error("a member's statement changed the name")
	}
}

// Single sign-on names no organization: it starts with the installation's, which the seed made.
func TestSingleSignOnIsTheInstallations(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	org, err := a.accounts.InstallationOrg(context.Background())
	if err != nil || org.Slug != installation.Slug {
		t.Fatalf("single sign-on starts with %+v (%v), want the installation's organization %s", org, err, installation.Slug)
	}
}
