//go:build integration

package test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// signInRows gives one member every kind of row the sign-in tables hold, under a group of the test's own, written
// as the superuser so the policies play no part in making them; the installation needs no provider yet.
func (h *harness) signInRows(t *testing.T, m member) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	_, digest, err := auth.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	var group uuid.UUID
	name := "staff-" + h.suffix()
	if err := h.super.QueryRow(ctx, `INSERT INTO groups (name, source, external_ref) VALUES ($1, 'oidc', $1) RETURNING id`, name).Scan(&group); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, []any{group, m.user}},
		{`INSERT INTO oidc_provider (issuer, client_id, client_secret) VALUES ('https://id.test', 'exciter', '\x01'::bytea)`, nil},
		{`INSERT INTO role_assignment (role, group_id) VALUES ('viewer', $1)`, []any{group}},
		{`INSERT INTO oidc_login (state, nonce, code_verifier, expires_at, browser_hash) VALUES (gen_random_uuid()::text, 'n', 'v', now() + interval '1 hour', sha256('b'))`, nil},
		{`INSERT INTO join_request (user_id) VALUES ($1)`, []any{m.user}},
		{`INSERT INTO user_identity (issuer, subject, user_id) VALUES ('https://id.test', gen_random_uuid()::text, $1)`, []any{m.user}},
		{`INSERT INTO user_session (user_id, token_hash, proof, expires_at) VALUES ($1, $2, 'oidc', $3)`, []any{m.user, digest, time.Now().Add(time.Hour)}},
	} {
		if _, err := h.super.Exec(ctx, sql.query, sql.args...); err != nil {
			t.Fatalf("%s: %v", sql.query, err)
		}
	}
	h.settle(t)
	return group
}

// Straight through SQL as exciter_app: the provider, the grants and the waiting people show only to an
// administrator, groups and their members change only for one, nothing shows to a connection without the
// installation, and the tables that span it nothing.
func TestRawSQLKeepsSignInToItsAdministrators(t *testing.T) {
	h := newHarness(t)
	h.singleSignOn(t)
	a := h.owner(t, "iota")
	h.signInRows(t, a)
	ctx := context.Background()
	tenantTables := []string{"groups", "group_member", "oidc_provider", "role_assignment", "join_request"}
	adminTables := []string{"oidc_provider", "role_assignment", "join_request"}
	// Everybody reads these, since grants, pickers and reviews name groups; only administrators write them.
	adminWrittenTables := []string{"groups", "group_member"}
	globalTables := []string{"user_session", "user_identity"}

	t.Run("acting for nobody, the administrators' rows and the tables that span the installation stay hidden", func(t *testing.T) {
		unscoped := h.appConnUnscoped(t)
		for _, table := range append(adminTables, globalTables...) {
			var n int
			if err := unscoped.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
				t.Errorf("a connection acting for nobody sees %d rows in %s (%v)", n, table, err)
			}
		}
		// A sign-in in flight, with its nonce and verifier, is the admin role's alone, whoever acts.
		for who, conn := range map[string]*pgx.Conn{"nobody": unscoped, "an owner": h.appConn(t, a.user)} {
			if _, err := conn.Exec(ctx, `SELECT count(*) FROM oidc_login`); !isPermissionDenied(err) {
				t.Errorf("the app role acting for %s reading sign-ins in flight = %v, want permission denied", who, err)
			}
		}
	})

	owner := h.appConn(t, a.user)
	countOf := func(t *testing.T, sql string, args ...any) int {
		t.Helper()
		var n int
		if err := owner.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}

	t.Run("an owner sees the installation's rows, and none of the tables that span it", func(t *testing.T) {
		for _, table := range tenantTables {
			if got, there := countOf(t, `SELECT count(*) FROM `+table), h.countRows(t, `SELECT count(*) FROM `+table+``); got != there || got == 0 {
				t.Errorf("an owner sees %d rows in %s, want the installation's %d", got, table, there)
			}
		}
		for _, table := range globalTables {
			if got := countOf(t, `SELECT count(*) FROM `+table); got != 0 {
				t.Errorf("the app role sees %d rows in %s, want none", got, table)
			}
		}
	})

	t.Run("a plain member sees none of the administrators' rows", func(t *testing.T) {
		plain := h.appConn(t, h.addPerson(t, "member"))
		for _, table := range adminTables {
			var n int
			if err := plain.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
				t.Errorf("a plain member sees %d rows in %s (%v), want none", n, table, err)
			}
		}
		if tag, err := plain.Exec(ctx, `UPDATE oidc_provider SET issuer = 'https://evil.test'`); err != nil || tag.RowsAffected() != 0 {
			t.Errorf("a plain member repointed the provider: %v %v", tag, err)
		}
		if _, err := plain.Exec(ctx, `INSERT INTO role_assignment (role, group_id) SELECT 'administrator', id FROM groups WHERE name LIKE 'staff-%' LIMIT 1`); !isPolicyViolation(err) {
			t.Errorf("a plain member granted a group administering: %v", err)
		}
	})

	t.Run("a plain member reads the groups and changes none of them", func(t *testing.T) {
		plain := h.appConn(t, h.addPerson(t, "member"))
		for _, table := range adminWrittenTables {
			var n int
			if err := plain.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != h.countRows(t, `SELECT count(*) FROM `+table) {
				t.Errorf("a plain member sees %d rows in %s (%v), want all of them", n, table, err)
			}
			touchesNothing(t, plain, "a plain member changing "+table, `UPDATE `+table+` SET created_at = created_at`)
			touchesNothing(t, plain, "a plain member emptying "+table, `DELETE FROM `+table)
		}
	})

	for _, attempt := range []struct {
		name string
		sql  string
		args []any
	}{
		{"forging a session", `INSERT INTO user_session (user_id, token_hash, proof, expires_at) VALUES ($1, decode(repeat('00', 32), 'hex'), 'password', now() + interval '1 day')`, []any{a.user}},
		{"claiming an identity", `INSERT INTO user_identity (issuer, subject, user_id) VALUES ('https://evil.test', 'x', $1)`, []any{a.user}},
	} {
		t.Run(attempt.name+" is refused", func(t *testing.T) {
			_, err := owner.Exec(ctx, attempt.sql, attempt.args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || (pgErr.Code != "42501" && pgErr.Code != "23514") {
				t.Fatalf("%s = %v, want a policy or check violation", attempt.sql, err)
			}
		})
	}

	t.Run("the app role ends no session", func(t *testing.T) {
		if tag, err := owner.Exec(ctx, `DELETE FROM user_session`); err != nil || tag.RowsAffected() != 0 {
			t.Errorf("the app role deleted %v sessions (%v)", tag, err)
		}
	})
}

// The admin role is exempt from the policies, so the trigger is what keeps a group's members people of the
// installation there.
func TestAGroupKeepsItsMembersInItsOrganization(t *testing.T) {
	h := newHarness(t)
	h.singleSignOn(t)
	a := h.owner(t, "lambda")
	group := h.signInRows(t, a)
	stranger := h.outsider(t, "mu")
	_, err := h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, group, stranger)
		return err
	})
	if !isCheckViolation(err) {
		t.Fatalf("the admin role put somebody who is no member into a group: %v", err)
	}
}
