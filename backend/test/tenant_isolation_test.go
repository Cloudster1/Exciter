//go:build integration

package test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// Every tenant table must be invisible across organizations even when a query
// deliberately omits the tenant filter, the mistake row level security survives.
func TestTenantIsolation(t *testing.T) {
	h := newHarness(t)
	a := h.makeMember(t, "alpha")
	b := h.makeMember(t, "beta")

	for _, table := range []string{"org", "app_user", "org_member"} {
		t.Run(table+" shows a tenant only its own rows", func(t *testing.T) {
			if got := h.count(t, a.ctx, `SELECT count(*) FROM `+table); got != 1 {
				t.Errorf("org A sees %d rows in %s, want only its own 1", got, table)
			}
			if got := h.count(t, b.ctx, `SELECT count(*) FROM `+table); got != 1 {
				t.Errorf("org B sees %d rows in %s, want only its own 1", got, table)
			}
		})
	}

	t.Run("another tenant's rows are not found by id", func(t *testing.T) {
		if got := h.count(t, a.ctx, `SELECT count(*) FROM org WHERE id = $1`, b.org); got != 0 {
			t.Errorf("org A finds org B by id: %d", got)
		}
		if got := h.count(t, a.ctx, `SELECT count(*) FROM app_user WHERE id = $1`, b.user); got != 0 {
			t.Errorf("org A finds org B's person by id: %d", got)
		}
	})

	t.Run("joining another tenant is refused", func(t *testing.T) {
		_, err := h.cluster.Write(a.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'admin')`, b.org, a.user)
			return err
		})
		if !isPolicyViolation(err) {
			t.Fatalf("org A wrote a membership into org B: %v", err)
		}
	})

	t.Run("updating or deleting another tenant affects nothing", func(t *testing.T) {
		var updated, deleted int64
		_, err := h.cluster.Write(a.ctx, func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, `UPDATE org SET name = 'tampered' WHERE id = $1`, b.org)
			if err != nil {
				return err
			}
			updated = tag.RowsAffected()
			tag, err = tx.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, b.user)
			deleted = tag.RowsAffected()
			return err
		})
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		if updated != 0 || deleted != 0 {
			t.Errorf("org A updated %d and deleted %d of org B's rows, want none", updated, deleted)
		}
		var name string
		if err := h.super.QueryRow(context.Background(), `SELECT name FROM org WHERE id = $1`, b.org).Scan(&name); err != nil || name != "beta" {
			t.Errorf("org B's name is %q (%v), want it untouched", name, err)
		}
	})
}

// The belt as well as the braces: a query with no organization at all is
// refused before it reaches the database.
func TestUnscopedAccessIsRefused(t *testing.T) {
	h := newHarness(t)

	err := h.cluster.Read(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		var n int
		return tx.QueryRow(ctx, `SELECT count(*) FROM org`).Scan(&n)
	})
	if !isNoTenant(err) {
		t.Fatalf("a read with no organization = %v, want tenant.ErrNoTenant", err)
	}
	_, err = h.cluster.Write(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO org (slug, name) VALUES ('nobody', 'Nobody')`)
		return err
	})
	if !isNoTenant(err) {
		t.Fatalf("a write with no organization = %v, want tenant.ErrNoTenant", err)
	}
}

// The service refusing is not proof. Straight through SQL as exciter_app, with
// no help from the Go code, the database has to refuse on its own.
func TestRawSQLAsTheAppRoleCannotCrossTenants(t *testing.T) {
	h := newHarness(t)
	a := h.makeMember(t, "gamma")
	b := h.makeMember(t, "delta")

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv(envPrimary))
	if err != nil {
		t.Fatalf("connect as the app role: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	var role string
	var privileged bool
	if err := conn.QueryRow(ctx, `SELECT current_user, rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&role, &privileged); err != nil {
		t.Fatal(err)
	}
	if role != "exciter_app" || privileged {
		t.Fatalf("connected as %s (privileged %v), want the unprivileged exciter_app", role, privileged)
	}

	countOf := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}

	t.Run("with no tenant set nothing is visible", func(t *testing.T) {
		for _, table := range []string{"org", "app_user", "org_member"} {
			if got := countOf(`SELECT count(*) FROM ` + table); got != 0 {
				t.Errorf("an unscoped connection sees %d rows in %s, want none", got, table)
			}
		}
	})

	t.Run("scoped to one tenant the other stays invisible", func(t *testing.T) {
		if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, tenant.PostgresVar, a.org.String()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = conn.Exec(context.Background(), `RESET `+tenant.PostgresVar) })
		if got := countOf(`SELECT count(*) FROM org WHERE id = $1`, b.org); got != 0 {
			t.Errorf("org B is visible from org A's scope")
		}
		if got := countOf(`SELECT count(*) FROM app_user WHERE id = $1`, b.user); got != 0 {
			t.Errorf("org B's person is visible from org A's scope")
		}
		if got := countOf(`SELECT count(*) FROM org WHERE id = $1`, a.org); got != 1 {
			t.Errorf("org A does not see itself: %d", got)
		}
		_, err := conn.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'owner')`, b.org, a.user)
		if !isPolicyViolation(err) {
			t.Errorf("a membership in org B was written from org A's scope: %v", err)
		}
	})

	for _, attempt := range []struct{ name, sql string }{
		{"turning row security off", `SET row_security = off; SELECT count(*) FROM org`},
		{"disabling the policies", `ALTER TABLE org DISABLE ROW LEVEL SECURITY`},
		{"dropping a policy", `DROP POLICY org_tenant_isolation ON org`},
		{"becoming the exempt role", `SET ROLE exciter_admin`},
		{"making an organization", `INSERT INTO org (slug, name) VALUES ('sneaky', 'Sneaky')`},
	} {
		t.Run(attempt.name+" is refused", func(t *testing.T) {
			if _, err := conn.Exec(ctx, `RESET ALL`); err != nil {
				t.Fatal(err)
			}
			_, err := conn.Exec(ctx, attempt.sql)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("%s succeeded (err = %v)", attempt.sql, err)
			}
			if !strings.HasPrefix(pgErr.Code, "42") {
				t.Errorf("%s failed for the wrong reason: %s %s", attempt.sql, pgErr.Code, pgErr.Message)
			}
		})
	}
}

// Signup and login run before a tenant is known; the admin role is exempt by
// an explicit policy, and only the admin transactions use it.
func TestTheAdminRoleSeesAcrossTenants(t *testing.T) {
	h := newHarness(t)
	a := h.makeMember(t, "epsilon")
	b := h.makeMember(t, "zeta")

	var role string
	var n int
	err := h.cluster.ReadAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM org WHERE id IN ($1, $2)`, a.org, b.org).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	if role != "exciter_admin" || n != 2 {
		t.Fatalf("the admin pool is %s and sees %d of the two organizations", role, n)
	}
}

// Both runtime roles leave row level security in force, and a connection that
// would ignore it is refused at startup.
func TestPrivilegedRolesAreRefused(t *testing.T) {
	h := newHarness(t)
	if err := h.cluster.RefuseSuperuser(context.Background()); err != nil {
		t.Fatalf("exciter_app was taken for a privileged role: %v", err)
	}

	cfg := h.cfg.DB
	cfg.PrimaryURL = os.Getenv(envSuperuser)
	cfg.AdminURL = cfg.PrimaryURL
	cfg.ReplicaURLs = nil
	super, err := db.Open(context.Background(), cfg, discard())
	if err != nil {
		t.Fatal(err)
	}
	defer super.Close()
	if err := super.RefuseSuperuser(context.Background()); !errors.Is(err, db.ErrPrivilegedRole) {
		t.Fatalf("a superuser connection = %v, want db.ErrPrivilegedRole", err)
	}
}

// A table a migration adds without forcing row level security, or without the
// admin's bypass, is caught here rather than in production.
func TestEveryTableForcesRowSecurity(t *testing.T) {
	h := newHarness(t)
	rows, err := h.super.Query(context.Background(), `
		SELECT c.relname, c.relrowsecurity AND c.relforcerowsecurity,
		       EXISTS (SELECT 1 FROM pg_policies p
		               WHERE p.schemaname = 'public' AND p.tablename = c.relname AND 'exciter_admin' = ANY (p.roles))
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relname <> 'goose_db_version'
		ORDER BY c.relname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var name string
		var forced, bypass bool
		if err := rows.Scan(&name, &forced, &bypass); err != nil {
			t.Fatal(err)
		}
		seen++
		if !forced {
			t.Errorf("%s does not force row level security", name)
		}
		if !bypass {
			t.Errorf("%s has no policy letting exciter_admin through", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// A run that found no tables would pass for the wrong reason.
	if seen < 3 {
		t.Fatalf("only %d tables found; the schema is not migrated", seen)
	}
}

// isPolicyViolation recognises the error a WITH CHECK clause raises.
func isPolicyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}
