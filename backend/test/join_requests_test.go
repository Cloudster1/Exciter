//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

const joinRequestsPath = "/api/v1/users/requests"

// waitingFor lists the addresses an administrator sees waiting.
func waitingFor(t *testing.T, admin *client) []string {
	t.Helper()
	var emails []string
	for _, each := range list(t, want(t, admin.get(t, joinRequestsPath), http.StatusOK, "list who waits"), "requests") {
		emails = append(emails, each.(map[string]any)["email"].(string))
	}
	return emails
}

// Somebody the provider knows but nobody here has let in is refused, waits for
// an administrator, and is let in or turned away by one, as in Armature.
func TestANewPersonWaitsUntilAnAdministratorLetsThemIn(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	org := h.singleSignOn(t)
	configureProvider(t, a, org, kc.issuer(), true)
	ctx := context.Background()

	suffix := h.suffix()
	const password = "a new person's password"
	carol := kc.newPerson(t, h, "carol-"+suffix, password, "/quality")
	dave := kc.newPerson(t, h, "dave-"+suffix, password)

	t.Run("a first sign-in is refused and noted", func(t *testing.T) {
		b := newBrowser(t, a)
		if landing := b.signIn(t, "carol-"+suffix, password, "/settings"); landing != "http://"+appHost+"/login?sso=not_a_member" {
			t.Fatalf("landed on %q, want the sign-in page saying the request waits", landing)
		}
		if b.sessionCookie(t) != "" {
			t.Fatal("somebody not let in was given a session cookie")
		}
		if status, _ := b.me(t); status != http.StatusUnauthorized {
			t.Fatalf("/auth/me while waiting = %d", status)
		}
	})

	t.Run("straight through SQL there is a request and nothing else", func(t *testing.T) {
		var requests, members, sessions, groups int
		if err := h.super.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM org_join_request r WHERE r.org_id = $1 AND r.user_id = u.id),
			       (SELECT count(*) FROM org_member m WHERE m.user_id = u.id),
			       (SELECT count(*) FROM user_session s WHERE s.user_id = u.id),
			       (SELECT count(*) FROM group_member g WHERE g.user_id = u.id)
			FROM app_user u WHERE u.email = $2`, org.ID, carol).Scan(&requests, &members, &sessions, &groups); err != nil {
			t.Fatal(err)
		}
		if requests != 1 || members != 0 || sessions != 0 || groups != 0 {
			t.Fatalf("carol has %d requests, %d memberships, %d sessions, %d groups; want 1, 0, 0, 0", requests, members, sessions, groups)
		}
		// As the application she is not among the installation's people: app_user shows members only.
		var visible int
		if err := h.appConn(t).QueryRow(ctx, `SELECT count(*) FROM app_user WHERE email = $1`, carol).Scan(&visible); err != nil || visible != 0 {
			t.Fatalf("the installation sees carol among its people: %d (%v)", visible, err)
		}
	})

	newBrowser(t, a).signIn(t, "dave-"+suffix, password, "")
	admin, adminID := adminOf(t, h, a, org)
	carolID, daveID := h.userID(t, carol), h.userID(t, dave)

	t.Run("an administrator sees who waits, oldest first", func(t *testing.T) {
		if emails := waitingFor(t, admin); len(emails) != 2 || emails[0] != carol || emails[1] != dave {
			t.Fatalf("waiting = %v, want carol then dave", emails)
		}
	})

	t.Run("nobody is let in as the owner", func(t *testing.T) {
		got := want(t, admin.post(t, joinRequestsPath+"/"+carolID.String()+"/admit", map[string]string{"role": "owner"}), http.StatusUnprocessableEntity, "admit as owner")
		if obj(t, got, "error", "fields")["role"] == nil {
			t.Fatalf("admitting as owner = %s", got.Raw)
		}
	})

	var carolIn *browser
	t.Run("letting carol in makes her a member who signs in directly", func(t *testing.T) {
		got := want(t, admin.post(t, joinRequestsPath+"/"+carolID.String()+"/admit", map[string]string{"role": "member"}), http.StatusOK, "admit carol")
		if m := obj(t, got, "membership"); m["userId"] != carolID.String() || m["role"] != "member" {
			t.Fatalf("membership = %s", got.Raw)
		}
		// A browser of her own: the first one still holds Keycloak's session, which would skip the login form.
		carolIn = newBrowser(t, a)
		if landing := carolIn.signIn(t, "carol-"+suffix, password, "/settings"); landing != "http://"+appHost+"/settings" {
			t.Fatalf("carol landed on %q after being let in", landing)
		}
		if role := carolIn.roleIn(t); role != "member" {
			t.Fatalf("carol is %q", role)
		}
		if got := h.providerGroups(t, org, carol); len(got) != 1 || got[0] != "quality" {
			t.Fatalf("carol's groups once let in = %q", got)
		}
	})

	t.Run("a member cannot let anybody in", func(t *testing.T) {
		if resp, _ := carolIn.get(t, a.URL+httpapi.APIPrefix+"/users/requests"); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("a member listing requests = %d", resp.StatusCode)
		}
		if resp, _ := carolIn.postJSON(t, httpapi.APIPrefix+"/users/requests/"+daveID.String()+"/admit", map[string]string{"role": "admin"}); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("a member admitting = %d", resp.StatusCode)
		}
	})

	t.Run("turning dave away forgets the request and lets nobody in", func(t *testing.T) {
		want(t, admin.delete(t, joinRequestsPath+"/"+daveID.String()), http.StatusNoContent, "decline dave")
		if emails := waitingFor(t, admin); len(emails) != 0 {
			t.Fatalf("still waiting: %v", emails)
		}
		if n := h.countRows(t, `SELECT count(*) FROM org_member WHERE user_id = $1`, daveID); n != 0 {
			t.Fatal("a person turned away became a member")
		}
		want(t, admin.delete(t, joinRequestsPath+"/"+daveID.String()), http.StatusNotFound, "decline twice")
		want(t, admin.post(t, joinRequestsPath+"/"+daveID.String()+"/admit", map[string]string{"role": "member"}), http.StatusNotFound, "admit after declining")
	})

	t.Run("both answers are on the record", func(t *testing.T) {
		for target, action := range map[uuid.UUID]string{carolID: "member.admitted", daveID: "member.declined"} {
			if n := h.countRows(t, `
				SELECT count(*) FROM audit_log
				WHERE org_id = $1 AND action = $2 AND target_type = 'user' AND target_id = $3 AND actor_user_id = $4 AND ip IS NOT NULL`,
				org.ID, action, target, adminID); n != 1 {
				t.Errorf("%s for %s recorded %d times", action, target, n)
			}
		}
	})
}

// Straight through SQL as exciter_app: waiting people are an administrator's to see and answer.
func TestRawSQLKeepsJoinRequestsToTheirAdministrators(t *testing.T) {
	h := newHarness(t)
	h.singleSignOn(t)
	a := h.owner(t, "omicron")
	ctx := context.Background()
	waiting := h.outsider(t, "rho")
	if _, err := h.super.Exec(ctx, `INSERT INTO org_join_request (org_id, user_id) VALUES ($1, $2)`, a.org, waiting); err != nil {
		t.Fatal(err)
	}
	h.settle(t)

	plain := h.appConn(t, h.addPerson(t, a.org, "member"))
	var n int
	if err := plain.QueryRow(ctx, `SELECT count(*) FROM org_join_request`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a plain member sees %d requests (%v), want none", n, err)
	}
	if tag, err := plain.Exec(ctx, `DELETE FROM org_join_request`); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("a plain member answered %v requests (%v)", tag, err)
	}
	if _, err := plain.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'member')`, a.org, waiting); !isPolicyViolation(err) {
		t.Fatalf("a plain member let somebody in: %v", err)
	}

	owner := h.appConn(t, a.user)
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM org_join_request`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("an owner sees %d requests (%v), want the one", n, err)
	}
	var raw []byte
	if err := owner.QueryRow(ctx, `SELECT coalesce(json_agg(user_id), '[]') FROM org_join_request`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	if err := json.Unmarshal(raw, &ids); err != nil || len(ids) != 1 || ids[0] != waiting {
		t.Fatalf("an owner sees requests %s", raw)
	}
}
