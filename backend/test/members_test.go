//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// An administrator lists, adds, re-roles and removes the organization's people,
// each act recorded once; the owner and the administrator themselves stay as they are.
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

	want(t, admin.delete(t, "/api/v1/users/"+home.user.String()), http.StatusConflict, "remove the owner")
	want(t, admin.delete(t, "/api/v1/users/"+adminID.String()), http.StatusConflict, "remove oneself")
	want(t, admin.delete(t, "/api/v1/users/"+added.String()), http.StatusNoContent, "remove the guest")
	want(t, admin.delete(t, "/api/v1/users/"+added.String()), http.StatusNotFound, "remove them again")

	want(t, member.get(t, "/api/v1/users"), http.StatusForbidden, "a member lists the people")
	want(t, member.delete(t, "/api/v1/users/"+adminID.String()), http.StatusForbidden, "a member removes somebody")
	want(t, member.patch(t, "/api/v1/users/"+adminID.String(), map[string]any{"role": "member"}), http.StatusForbidden, "a member re-roles somebody")
	want(t, member.post(t, "/api/v1/users", map[string]any{"email": "z@example.test", "role": "member"}), http.StatusForbidden, "a member adds somebody")
	want(t, nobody.get(t, "/api/v1/users"), http.StatusUnauthorized, "nobody lists the people")

	for action, n := range map[string]int{audit.ActionMemberCreated: 1, audit.ActionMemberRoleChanged: 1, audit.ActionMemberRemoved: 1} {
		if got := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = $2 AND target_id = $3 AND actor_user_id = $4`, home.org, action, added, adminID); got != n {
			t.Errorf("%s was recorded %d times, want %d", action, got, n)
		}
	}
}

// Straight through SQL as exciter_app: only an administrator changes who
// belongs, nobody is made an owner, and the last owner stays.
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
	if n, err := affected(asMember, `DELETE FROM org_member WHERE user_id = $1`, otherID); err != nil || n != 0 {
		t.Errorf("a member removed somebody: %d rows, %v", n, err)
	}
	newcomer := h.addPerson(t, h.makeMember(t, "members-sql-elsewhere").org, "member")
	_, err := affected(asMember, `INSERT INTO org_member (org_id, user_id, org_role) VALUES (current_org_id(), $1, 'member')`, newcomer)
	permissionDenied(t, "a member letting somebody in", err)

	_, err = affected(home.ctx, `UPDATE org_member SET org_role = 'owner' WHERE user_id = $1`, memberID)
	permissionDenied(t, "the owner making another owner through the app role", err)
	if n, err := affected(home.ctx, `UPDATE org_member SET org_role = 'admin' WHERE user_id = $1`, memberID); err != nil || n != 1 {
		t.Errorf("the owner could not make a member an administrator: %d rows, %v", n, err)
	}

	_, err = h.super.Exec(context.Background(), `DELETE FROM org_member WHERE org_id = $1 AND user_id = $2`, home.org, home.user)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("removing the last owner, even as the superuser, = %v, want refused", err)
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
