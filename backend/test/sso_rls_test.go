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
	if err := h.super.QueryRow(ctx, `INSERT INTO groups (org_id, name, source, external_ref) VALUES ($1, $2, 'oidc', $2) RETURNING id`, m.org, name).Scan(&group); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO group_member (org_id, group_id, user_id) VALUES ($1, $2, $3)`, []any{m.org, group, m.user}},
		{`INSERT INTO oidc_provider (org_id, issuer, client_id, client_secret) VALUES ($1, 'https://id.test', 'exciter', '\x01'::bytea)`, []any{m.org}},
		{`INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, $2, 'member')`, []any{m.org, name}},
		{`INSERT INTO oidc_login (state, org_id, nonce, code_verifier, expires_at) VALUES (gen_random_uuid()::text, $1, 'n', 'v', now() + interval '1 hour')`, []any{m.org}},
		{`INSERT INTO org_join_request (org_id, user_id) VALUES ($1, $2)`, []any{m.org, m.user}},
		{`INSERT INTO user_identity (issuer, subject, user_id) VALUES ('https://id.test', gen_random_uuid()::text, $1)`, []any{m.user}},
		{`INSERT INTO user_session (user_id, token_hash, current_org_id, proof, expires_at) VALUES ($1, $2, $3, 'oidc', $4)`, []any{m.user, digest, m.org, time.Now().Add(time.Hour)}},
	} {
		if _, err := h.super.Exec(ctx, sql.query, sql.args...); err != nil {
			t.Fatalf("%s: %v", sql.query, err)
		}
	}
	h.settle(t)
	return group
}

// Straight through SQL as exciter_app: the provider, its mapping and the waiting people show only to an
// administrator, nothing shows to a connection without the installation, and the tables that span it nothing.
func TestRawSQLKeepsSignInToItsAdministrators(t *testing.T) {
	h := newHarness(t)
	h.singleSignOn(t)
	a := h.owner(t, "iota")
	h.signInRows(t, a)
	ctx := context.Background()
	tenantTables := []string{"groups", "group_member", "oidc_provider", "oidc_group_role", "org_join_request"}
	adminTables := []string{"oidc_provider", "oidc_group_role", "org_join_request"}
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
			if got, there := countOf(t, `SELECT count(*) FROM `+table), h.countRows(t, `SELECT count(*) FROM `+table+` WHERE org_id = $1`, a.org); got != there || got == 0 {
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
		plain := h.appConn(t, h.addPerson(t, a.org, "member"))
		for _, table := range adminTables {
			var n int
			if err := plain.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
				t.Errorf("a plain member sees %d rows in %s (%v), want none", n, table, err)
			}
		}
		if tag, err := plain.Exec(ctx, `UPDATE oidc_provider SET issuer = 'https://evil.test'`); err != nil || tag.RowsAffected() != 0 {
			t.Errorf("a plain member repointed the provider: %v %v", tag, err)
		}
		if _, err := plain.Exec(ctx, `INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, 'mine', 'admin')`, a.org); !isPolicyViolation(err) {
			t.Errorf("a plain member mapped a group: %v", err)
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
		_, err := tx.Exec(ctx, `INSERT INTO group_member (org_id, group_id, user_id) VALUES ($1, $2, $3)`, a.org, group, stranger)
		return err
	})
	if !isCheckViolation(err) {
		t.Fatalf("the admin role put somebody who is no member into a group: %v", err)
	}
}
