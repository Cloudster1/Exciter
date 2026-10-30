//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// adminPassword is the bootstrap administrator's in these tests; long enough for the rule.
const adminPassword = "a long enough password"

// bootstrapAdmin makes a local owner of the installation, as seed bootstrap does, and returns the address.
func bootstrapAdmin(t *testing.T, h *harness, a *apiServer) string {
	t.Helper()
	email := "owner-" + h.suffix() + "@example.test"
	ctx := context.Background()
	if _, err := a.accounts.EnsureAdmin(ctx, email, "", adminPassword); err != nil {
		t.Fatalf("make the bootstrap administrator: %v", err)
	}
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, email) })
	h.settle(t)
	return email
}

// A person signs in with a password, reads who they are and where, and signs out,
// the way the web client does.
func TestASessionOverTheAPI(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	email := bootstrapAdmin(t, h, a)

	b := a.anonymous()
	for _, wrong := range []map[string]any{
		{"email": email, "password": "not the password"},
		{"email": "nobody-" + h.suffix() + "@example.test", "password": adminPassword},
	} {
		if code := errorCode(t, want(t, b.post(t, "/api/v1/auth/login", wrong), http.StatusUnauthorized, "a wrong sign-in")); code != "invalid_credentials" {
			t.Errorf("a wrong sign-in answers %s, want invalid_credentials whether or not the address exists", code)
		}
	}
	if got := want(t, b.post(t, "/api/v1/auth/login", map[string]any{"email": "", "password": ""}), http.StatusUnprocessableEntity, "an empty form"); len(got.Body["error"].(map[string]any)["fields"].(map[string]any)) != 2 {
		t.Errorf("an empty form names %s", got.Raw)
	}

	signedIn := want(t, b.post(t, "/api/v1/auth/login", map[string]any{"email": email, "password": adminPassword}), http.StatusOK, "sign in")
	if signedIn.Body["role"] != "member" || signedIn.Body["administers"] != true || obj(t, signedIn, "installation")["name"] == "" {
		t.Errorf("the session lands as %s, want a member who administers the installation", signedIn.Raw)
	}
	if _, listed := signedIn.Body["organizations"]; listed {
		t.Errorf("the sign-in still lists organizations: %s", signedIn.Raw)
	}
	cookie := signedIn.Header.Get("Set-Cookie")
	for _, attr := range []string{"HttpOnly", "SameSite=Lax", "Path=/"} {
		if !contains(cookie, attr) {
			t.Errorf("the session cookie %q lacks %s", cookie, attr)
		}
	}

	want(t, b.patch(t, "/api/v1/auth/me", map[string]any{"locale": "de"}), http.StatusOK, "choose German")
	if me := want(t, b.get(t, "/api/v1/auth/me"), http.StatusOK, "read who I am"); obj(t, me, "user")["locale"] != "de" {
		t.Errorf("the language did not stick: %s", me.Raw)
	}
	if code := errorCode(t, want(t, b.patch(t, "/api/v1/auth/me", map[string]any{"locale": "fr"}), http.StatusUnprocessableEntity, "French")); code != "validation_failed" {
		t.Errorf("an unknown language answers %s", code)
	}

	want(t, b.post(t, "/api/v1/auth/logout", nil), http.StatusNoContent, "sign out")
	want(t, b.get(t, "/api/v1/auth/me"), http.StatusUnauthorized, "read who I am after signing out")
	want(t, a.anonymous().post(t, "/api/v1/auth/logout", nil), http.StatusUnauthorized, "sign out without a session")
}

// Turning somebody's account off refuses their open session; letting them go ends it, so the browser goes back
// to sign in, where a password is refused with what to do.
func TestASessionReachesOnlyWhatItsPersonStillHas(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	memberID := h.addPerson(t, "member")
	c := a.as(t, memberID)
	if me := want(t, c.get(t, "/api/v1/auth/me"), http.StatusOK, "read who I am"); me.Body["role"] != "member" {
		t.Fatalf("the member does not stand as a member: %s", me.Raw)
	}

	setActive := func(active bool) {
		t.Helper()
		if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET is_active = $2 WHERE id = $1`, memberID, active); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
	}
	setActive(false)
	if code := errorCode(t, want(t, c.get(t, "/api/v1/auth/me"), http.StatusForbidden, "a switched-off account")); code != "account_switched_off" {
		t.Errorf("a switched-off account answers %s", code)
	}
	setActive(true)

	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET role = NULL WHERE id = $1`, memberID); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	want(t, c.get(t, "/api/v1/auth/me"), http.StatusUnauthorized, "read who I am once let go")
	if n := h.countRows(t, `SELECT count(*) FROM user_session WHERE user_id = $1`, memberID); n != 0 {
		t.Errorf("letting the member go left %d sessions, want them ended", n)
	}

	email := bootstrapAdmin(t, h, a)
	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET role = NULL WHERE email = $1`, email); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	refused := want(t, a.anonymous().post(t, "/api/v1/auth/login", map[string]any{"email": email, "password": adminPassword}), http.StatusForbidden, "sign in once let go")
	if code := errorCode(t, refused); code != "account_switched_off" {
		t.Errorf("somebody let go signing in answers %s", code)
	}
	if msg := refused.Body["error"].(map[string]any)["message"]; msg != "Your account has been switched off. Ask an administrator." {
		t.Errorf("somebody let go is told %q", msg)
	}
}

// Straight through SQL: the app role reads no session, and a session keeps how it was proven and where it opened,
// which the trigger refuses to change before anything asks whether the place exists.
func TestSessionsAreTheAdminRolesAndStayWhereTheyWereProven(t *testing.T) {
	h := newHarness(t)
	home := h.owner(t, "session-sql-home")
	var sessionID string
	_, err := h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		id, _, err := auth.OpenSession(ctx, tx, home.user, auth.ProofOIDC, time.Now().Add(time.Hour), "", "")
		sessionID = id.String()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	h.settle(t)

	if n := h.count(t, home.ctx, `SELECT count(*) FROM user_session`); n != 0 {
		t.Errorf("the app role reads %d sessions", n)
	}
	for what, sql := range map[string]string{
		"changing how a session was proven": `UPDATE user_session SET proof = 'password' WHERE id = '` + sessionID + `'`,
	} {
		_, err := h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, sql)
			return err
		})
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("%s = %v, want refused by the database", what, err)
		}
	}
}

// The bootstrap makes its owner once; run again it changes nothing but a password
// that was changed in the configuration.
func TestTheBootstrapIsIdempotent(t *testing.T) {
	h := newHarness(t)
	accounts := auth.NewService(h.cluster, cheapPasswords(), time.Hour)
	h.singleSignOn(t)
	email := "boot-" + h.suffix() + "@example.test"
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, email) })
	ctx := context.Background()
	for i, pw := range []string{adminPassword, adminPassword, "another long password"} {
		made, err := accounts.EnsureAdmin(ctx, email, "", pw)
		if err != nil || made != (i == 0) {
			t.Fatalf("run %d = made %v, %v", i, made, err)
		}
		if _, err := accounts.Login(context.Background(), email, pw, "", ""); err != nil {
			t.Fatalf("run %d: the password does not sign in: %v", i, err)
		}
	}
	if _, err := accounts.Login(context.Background(), email, adminPassword, "", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("the replaced password still signs in: %v", err)
	}
	var role string
	var administers bool
	if err := h.super.QueryRow(context.Background(), `
		SELECT role, perm_is_administrator(id) FROM app_user WHERE email = $1 AND role IS NOT NULL`, email).Scan(&role, &administers); errors.Is(err, pgx.ErrNoRows) || role != "member" || !administers {
		t.Errorf("the bootstrapped account is %q and administers %v (%v), want a member who administers", role, administers, err)
	}
	if _, err := accounts.EnsureAdmin(ctx, email, "", "short"); !errors.Is(err, auth.ErrPasswordTooShort) {
		t.Errorf("a short password was accepted: %v", err)
	}
}
