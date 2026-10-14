//go:build integration

package test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
)

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

// The service refusing is not proof. Straight through SQL as exciter_app, with no help from the Go code, a
// connection acting for nobody reads the installation and its people, which are everyone's (#106), but no project,
// requirement, session, identity, sign-in in flight or anything only an administrator reads; and every way around
// the policies is refused.
func TestRawSQLAsTheAppRoleActingForNobody(t *testing.T) {
	h := newHarness(t)
	home := h.owner(t, "nobody-acting")
	nobodyKey := projectKey(t, "NOBO")
	var project uuid.UUID
	if err := h.super.QueryRow(context.Background(), `INSERT INTO project (org_id, key, name, created_by) VALUES ($1, $2, 'Walled', $3) RETURNING id`,
		home.org, nobodyKey, home.user).Scan(&project); err != nil {
		t.Fatal(err)
	}
	h.settle(t)

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

	t.Run("acting for nobody it reads the installation and its people, and nothing walled", func(t *testing.T) {
		for _, table := range []string{"org", "app_user", "org_member"} {
			if got := countOf(`SELECT count(*) FROM ` + table); got == 0 {
				t.Errorf("a connection acting for nobody reads no %s, want the installation's", table)
			}
		}
		for what, sql := range map[string]string{
			"the project":              `SELECT count(*) FROM project WHERE id = '` + project.String() + `'`,
			"its statuses":             `SELECT count(*) FROM req_status WHERE project_id = '` + project.String() + `'`,
			"sessions":                 `SELECT count(*) FROM user_session`,
			"identities":               `SELECT count(*) FROM user_identity`,
			"the identity provider":    `SELECT count(*) FROM oidc_provider`,
			"people waiting to be let": `SELECT count(*) FROM org_join_request`,
			"the audit log":            `SELECT count(*) FROM audit_log`,
		} {
			if got := countOf(sql); got != 0 {
				t.Errorf("a connection acting for nobody reads %d of %s, want none", got, what)
			}
		}
		if _, err := conn.Exec(ctx, `SELECT count(*) FROM oidc_login`); !isPermissionDenied(err) {
			t.Errorf("the app role reading sign-ins in flight = %v, want permission denied", err)
		}
	})

	for _, attempt := range []struct{ name, sql string }{
		{"turning row security off", `SET row_security = off; SELECT count(*) FROM org`},
		{"disabling the policies", `ALTER TABLE org DISABLE ROW LEVEL SECURITY`},
		{"dropping a policy", `DROP POLICY org_installation ON org`},
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

// A table a migration adds without forcing row level security, or without a permissive policy that lets the admin
// role through, is caught here rather than in production.
func TestEveryTableForcesRowSecurity(t *testing.T) {
	h := newHarness(t)
	rows, err := h.super.Query(context.Background(), `
		SELECT c.relname, c.relrowsecurity AND c.relforcerowsecurity,
		       EXISTS (SELECT 1 FROM pg_policies p
		               WHERE p.schemaname = 'public' AND p.tablename = c.relname AND p.permissive = 'PERMISSIVE'
		                 AND p.qual = 'true' AND ('exciter_admin' = ANY (p.roles) OR 'public' = ANY (p.roles)))
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

// isPermissionDenied recognises the error a privilege the role lacks raises.
func isPermissionDenied(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}
