//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// An administrator lists, adds, re-roles and switches off and on the installation's people, each act recorded once;
// the owner and the administrator themselves stay as they are.
func TestMembersOverTheAPI(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	home := h.makeMember(t, "members")
	adminID := h.addPerson(t, home.org, "admin")
	memberID := h.addPerson(t, home.org, "member")
	admin, member, nobody := a.as(t, adminID, home.org), a.as(t, memberID, home.org), a.anonymous()

	people := list(t, want(t, admin.get(t, "/api/v1/users"), http.StatusOK, "list the people"), "members")
	if len(people) != 3 || people[0].(map[string]any)["role"] != "owner" {
		t.Errorf("the people read %v, want three, the owner first", people)
	}

	email := "added-" + h.suffix() + "@example.test"
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, email) })
	made := obj(t, want(t, admin.post(t, "/api/v1/users", map[string]any{"email": email, "role": "member"}), http.StatusCreated, "add a person"), "member")
	password, _ := made["password"].(string)
	if made["newAccount"] != true || len(password) < 12 {
		t.Fatalf("a new person reads %v, want a made password shown once", made)
	}
	if _, err := a.accounts.Login(context.Background(), email, password, "", ""); err != nil {
		t.Errorf("the made password does not sign in: %v", err)
	}
	if code := errorCode(t, want(t, admin.post(t, "/api/v1/users", map[string]any{"email": email, "role": "member"}), http.StatusConflict, "add them again")); code != "conflict" {
		t.Errorf("adding a member twice answers %s", code)
	}
	for _, bad := range []map[string]any{
		{"email": "not an address", "role": "member"},
		{"email": "x-" + h.suffix() + "@example.test", "role": "owner"},
		{"email": "y-" + h.suffix() + "@example.test", "role": "member", "password": "short"},
	} {
		want(t, admin.post(t, "/api/v1/users", bad), http.StatusUnprocessableEntity, "add with a bad field")
	}
	added := uuid.MustParse(made["userId"].(string))

	changed := obj(t, want(t, admin.patch(t, "/api/v1/users/"+added.String(), map[string]any{"role": "guest"}), http.StatusOK, "make a guest"), "member")
	if changed["role"] != "guest" {
		t.Errorf("the role reads %v", changed)
	}
	for what, req := range map[string]struct {
		id   uuid.UUID
		role string
		code int
	}{
		"make an owner":        {added, "owner", http.StatusUnprocessableEntity},
		"change the owner":     {home.user, "member", http.StatusConflict},
		"change oneself":       {adminID, "member", http.StatusConflict},
		"change somebody gone": {uuid.New(), "member", http.StatusNotFound},
	} {
		want(t, admin.patch(t, "/api/v1/users/"+req.id.String(), map[string]any{"role": req.role}), req.code, what)
	}

	want(t, admin.patch(t, "/api/v1/users/"+added.String(), map[string]any{}), http.StatusUnprocessableEntity, "change nothing")
	if code := errorCode(t, want(t, admin.delete(t, "/api/v1/users/"+home.user.String()), http.StatusForbidden, "switch the owner off")); code != "forbidden" {
		t.Errorf("an administrator switching the owner off answers %s", code)
	}
	want(t, admin.delete(t, "/api/v1/users/"+adminID.String()), http.StatusConflict, "switch oneself off")
	want(t, admin.delete(t, "/api/v1/users/"+uuid.New().String()), http.StatusNotFound, "switch somebody gone off")

	// The guest signs in and makes a token, both of which switching them off ends.
	guest := a.anonymous()
	want(t, guest.post(t, "/api/v1/auth/login", map[string]any{"email": email, "password": password}), http.StatusOK, "the guest signs in")
	want(t, guest.post(t, "/api/v1/tokens", map[string]any{"name": "nightly"}), http.StatusCreated, "the guest makes a token")
	want(t, admin.delete(t, "/api/v1/users/"+added.String()), http.StatusNoContent, "switch the guest off")
	want(t, admin.delete(t, "/api/v1/users/"+added.String()), http.StatusNoContent, "switch them off again")
	h.settle(t)
	want(t, guest.get(t, "/api/v1/auth/me"), http.StatusUnauthorized, "the guest's session once switched off")
	for what, sql := range map[string]string{
		"sessions": `SELECT count(*) FROM user_session WHERE user_id = $1`,
		"tokens":   `SELECT count(*) FROM api_token WHERE user_id = $1`,
	} {
		if n := h.countRows(t, sql, added); n != 0 {
			t.Errorf("switching the guest off left %d %s", n, what)
		}
	}
	if code := errorCode(t, want(t, a.anonymous().post(t, "/api/v1/auth/login", map[string]any{"email": email, "password": password}), http.StatusForbidden, "sign in switched off")); code != "account_switched_off" {
		t.Errorf("a switched-off sign-in answers %s", code)
	}
	people = list(t, want(t, admin.get(t, "/api/v1/users"), http.StatusOK, "list the people"), "members")
	if last := people[len(people)-1].(map[string]any); last["userId"] != added.String() || last["active"] != false || last["role"] != "guest" {
		t.Errorf("the people read %v, want the guest last, switched off and still a guest", people)
	}

	back := obj(t, want(t, admin.patch(t, "/api/v1/users/"+added.String(), map[string]any{"active": true}), http.StatusOK, "switch the guest on"), "member")
	if back["active"] != true {
		t.Errorf("the guest reads %v once switched on", back)
	}
	h.settle(t)
	want(t, a.anonymous().post(t, "/api/v1/auth/login", map[string]any{"email": email, "password": password}), http.StatusOK, "sign in switched on")

	// Only an owner lets another owner go.
	second := h.addPerson(t, home.org, "owner")
	want(t, admin.delete(t, "/api/v1/users/"+second.String()), http.StatusForbidden, "an administrator switches an owner off")
	want(t, a.as(t, home.user, home.org).delete(t, "/api/v1/users/"+second.String()), http.StatusNoContent, "an owner switches another off")

	want(t, member.get(t, "/api/v1/users"), http.StatusForbidden, "a member lists the people")
	want(t, member.delete(t, "/api/v1/users/"+adminID.String()), http.StatusForbidden, "a member switches somebody off")
	want(t, member.patch(t, "/api/v1/users/"+adminID.String(), map[string]any{"role": "member"}), http.StatusForbidden, "a member re-roles somebody")
	want(t, member.post(t, "/api/v1/users", map[string]any{"email": "z@example.test", "role": "member"}), http.StatusForbidden, "a member adds somebody")
	want(t, nobody.get(t, "/api/v1/users"), http.StatusUnauthorized, "nobody lists the people")

	for action, n := range map[string]int{audit.ActionMemberCreated: 1, audit.ActionMemberRoleChanged: 1, audit.ActionMemberSwitchedOff: 1, audit.ActionMemberSwitchedOn: 1} {
		if got := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = $2 AND target_id = $3 AND actor_user_id = $4`, home.org, action, added, adminID); got != n {
			t.Errorf("%s was recorded %d times, want %d", action, got, n)
		}
	}
}

// Straight through SQL as exciter_app: only an administrator changes a role or switches somebody off, nobody is
// made an owner or loses one's standing, nobody leaves, and the installation keeps an active administrator.
func TestMembershipIsTheAdministratorsThroughSQL(t *testing.T) {
	h := newHarness(t)
	home := h.makeMember(t, "members-sql")
	memberID := h.addPerson(t, home.org, "member")
	otherID := h.addPerson(t, home.org, "member")
	asMember := db.WithUser(homeOrg(home), memberID)

	affected := func(ctx context.Context, sql string, args ...any) (int64, error) {
		var n int64
		_, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, sql, args...)
			n = tag.RowsAffected()
			return err
		})
		return n, err
	}
	if n, err := affected(asMember, `UPDATE org_member SET org_role = 'admin' WHERE user_id = $1`, memberID); err != nil || n != 0 {
		t.Errorf("a member raised themselves: %d rows, %v", n, err)
	}
	if n, err := affected(asMember, `UPDATE app_user SET is_active = false WHERE id = $1`, otherID); err != nil || n != 0 {
		t.Errorf("a member switched somebody off: %d rows, %v", n, err)
	}
	_, err := affected(asMember, `DELETE FROM org_member WHERE user_id = $1`, otherID)
	permissionDenied(t, "a member removing somebody", err)
	_, err = affected(home.ctx, `DELETE FROM org_member WHERE user_id = $1`, otherID)
	permissionDenied(t, "the owner removing somebody rather than switching them off", err)
	newcomer := h.addPerson(t, h.makeMember(t, "members-sql-elsewhere").org, "member")
	_, err = affected(asMember, `INSERT INTO org_member (org_id, user_id, org_role) VALUES (current_org_id(), $1, 'member')`, newcomer)
	permissionDenied(t, "a member letting somebody in", err)

	_, err = affected(home.ctx, `UPDATE org_member SET org_role = 'owner' WHERE user_id = $1`, memberID)
	permissionDenied(t, "the owner making another owner through the app role", err)
	if n, err := affected(home.ctx, `UPDATE org_member SET org_role = 'admin' WHERE user_id = $1`, memberID); err != nil || n != 1 {
		t.Errorf("the owner could not make a member an administrator: %d rows, %v", n, err)
	}
	asAdmin := db.WithUser(homeOrg(home), memberID)
	_, err = affected(asAdmin, `UPDATE org_member SET org_role = 'admin' WHERE user_id = $1`, home.user)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("an administrator changing the owner's role = %v, want refused", err)
	}
	if n, err := affected(asAdmin, `UPDATE app_user SET is_active = false WHERE id = $1`, otherID); err != nil || n != 1 {
		t.Errorf("an administrator could not switch a member off: %d rows, %v", n, err)
	}

	// The owner and the new administrator are the two left; switching one off leaves the other the last.
	if n, err := affected(home.ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, memberID); err != nil || n != 1 {
		t.Fatalf("the owner could not switch the other administrator off: %d rows, %v", n, err)
	}
	for what, sql := range map[string]string{
		"switching the last active administrator off": `UPDATE app_user SET is_active = false WHERE id = $1`,
		"making them a member":                        `UPDATE org_member SET org_role = 'member' WHERE user_id = $1`,
		"taking their membership away":                `DELETE FROM org_member WHERE user_id = $1`,
	} {
		_, err := h.super.Exec(context.Background(), sql, home.user)
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("%s, even as the superuser, = %v, want refused", what, err)
		}
	}
	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET is_active = true WHERE id = $1`, memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET is_active = false WHERE id = $1`, home.user); err != nil {
		t.Errorf("switching the owner off beside an active administrator = %v, want allowed", err)
	}
	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET is_active = true WHERE id = $1`, home.user); err != nil {
		t.Fatal(err)
	}
}

// Two administrators switching each other off at once do not leave the installation without one: the second waits
// for the first and is refused.
func TestTwoAdministratorsCannotSwitchEachOtherOff(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	home := h.makeMember(t, "members-race")
	one, other := h.addPerson(t, home.org, "admin"), h.addPerson(t, home.org, "admin")
	// With the owner switched off, the two are the only active administrators.
	if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, home.user); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.cleanupExec(t, h.super, `UPDATE app_user SET is_active = true WHERE id = ANY($1)`, []uuid.UUID{home.user, one, other})
	})

	first, err := h.super.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback(ctx) }()
	if _, err := first.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, other); err != nil {
		t.Fatalf("the first switch-off = %v", err)
	}
	second := make(chan error, 1)
	// Spelled out, so the wait below finds this statement and no other test's.
	switchOff := `UPDATE app_user SET is_active = false WHERE id = '` + one.String() + `'`
	go func() {
		_, err := h.super.Exec(ctx, switchOff)
		second <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for h.countRows(t, `SELECT count(*) FROM pg_stat_activity WHERE wait_event = 'advisory' AND query = $1`, switchOff) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the second switch-off never waited for the first")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var pgErr *pgconn.PgError
	if err := <-second; !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("the second switch-off = %v, want refused once the first committed", err)
	}
}

// The audit log's endpoints answer administrators, refuse members and nobody,
// and an export is itself recorded.
func TestTheAuditLogOverTheAPI(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	home := h.makeMember(t, "audit-api")
	adminID := h.addPerson(t, home.org, "admin")
	memberID := h.addPerson(t, home.org, "member")
	admin, member, nobody := a.as(t, adminID, home.org), a.as(t, memberID, home.org), a.anonymous()
	want(t, admin.patch(t, "/api/v1/users/"+memberID.String(), map[string]any{"role": "guest"}), http.StatusOK, "make an entry")

	entries := list(t, want(t, admin.get(t, "/api/v1/audit"), http.StatusOK, "read the log"), "entries")
	if len(entries) != 1 || entries[0].(map[string]any)["action"] != audit.ActionMemberRoleChanged || entries[0].(map[string]any)["actorName"] == "" {
		t.Errorf("the log reads %v", entries)
	}
	facets := obj(t, want(t, admin.get(t, "/api/v1/audit/facets"), http.StatusOK, "facets"), "facets")
	if facets["retentionDays"] != float64(365) || len(facets["actions"].([]any)) != 1 {
		t.Errorf("the facets read %v", facets)
	}
	for query, field := range map[string]string{"action=member.danced": "action", "actor=ann": "actor", "from=2026-13-01": "from", "cursor=nonsense": "cursor", "limit=0": "limit"} {
		got := want(t, admin.get(t, "/api/v1/audit?"+query), http.StatusUnprocessableEntity, query)
		if got.Body["error"].(map[string]any)["fields"].(map[string]any)[field] == nil {
			t.Errorf("%s does not name %s: %s", query, field, got.Raw)
		}
	}
	want(t, admin.get(t, "/api/v1/audit/export?from=2026-02-01&to=2026-01-01"), http.StatusUnprocessableEntity, "an export of a range ending before it starts")

	file := want(t, admin.get(t, "/api/v1/audit/export"), http.StatusOK, "export the log")
	if ct := file.Header.Get("Content-Type"); ct != "text/csv; charset=utf-8" || !contains(file.Header.Get("Content-Disposition"), "audit-log.csv") {
		t.Errorf("the export is served as %q, %q", ct, file.Header.Get("Content-Disposition"))
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = $2 AND actor_user_id = $3`, home.org, audit.ActionAuditExported, adminID); n != 1 {
		t.Errorf("the export was recorded %d times, want once", n)
	}

	for _, path := range []string{"/api/v1/audit", "/api/v1/audit/facets", "/api/v1/audit/export"} {
		if code := errorCode(t, want(t, member.get(t, path), http.StatusForbidden, "a member reads "+path)); code != "forbidden" {
			t.Errorf("%s refuses a member with %s", path, code)
		}
		want(t, nobody.get(t, path), http.StatusUnauthorized, "nobody reads "+path)
	}
}
