//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

// standing is somebody's standing, member or guest, straight from the table; empty for somebody who is not in.
func (h *harness) standing(t *testing.T, email string) string {
	t.Helper()
	var role string
	err := h.super.QueryRow(context.Background(), `SELECT role FROM app_user WHERE email = $1 AND role IS NOT NULL`, email).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return role
}

// auditRows lists what the record holds about one person, oldest first.
func (h *harness) auditRows(t *testing.T, action string, target uuid.UUID) []map[string]any {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT data, actor_user_id IS NULL FROM audit_log
		WHERE action = $1 AND target_id = $2 AND created_at >= $3 ORDER BY created_at, id`, action, target, h.started(t))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var raw []byte
		var byNobody bool
		if err := rows.Scan(&raw, &byNobody); err != nil {
			t.Fatal(err)
		}
		var data map[string]any
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		data["byProvider"] = byNobody
		out = append(out, data)
	}
	return out
}

// mapGroup grants a role across the installation to the group carrying the provider's name in its claim, making
// the group if need be, as an administrator does on the Groups and Roles tabs. It answers the grant's id.
func mapGroup(t *testing.T, admin *client, group, role string) string {
	t.Helper()
	got := want(t, admin.post(t, "/api/v1/role-assignments", map[string]any{"role": role, "groupId": providerGroup(t, admin, group)}),
		http.StatusCreated, "grant "+role+" to "+group)
	id, _ := obj(t, got, "assignment")["id"].(string)
	return id
}

// providerGroup is the group carrying the provider's name in its claim, made through the API if there is none yet.
func providerGroup(t *testing.T, admin *client, group string) string {
	t.Helper()
	for _, each := range list(t, want(t, admin.get(t, "/api/v1/groups?limit=50&q="+group), http.StatusOK, "find "+group), "groups") {
		if g := each.(map[string]any); g["externalRef"] == group {
			return g["id"].(string)
		}
	}
	made := want(t, admin.post(t, "/api/v1/groups", map[string]any{"name": group, "externalRef": group}), http.StatusCreated, "make "+group)
	return obj(t, made, "group")["id"].(string)
}

// administers says what the shell is told about administering, which is what decides its Admin entries, and that
// /access/me agrees.
func (b *browser) administers(t *testing.T) bool {
	t.Helper()
	status, me := b.me(t)
	if status != http.StatusOK {
		t.Fatalf("/auth/me = %d %v", status, me)
	}
	resp, body := b.get(t, b.api.URL+httpapi.APIPrefix+"/access/me")
	var access struct {
		Can struct {
			Administer bool `json:"administer"`
		} `json:"can"`
	}
	if err := json.Unmarshal([]byte(body), &access); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("/access/me = %d %s", resp.StatusCode, body)
	}
	if me["administers"] != access.Can.Administer {
		t.Fatalf("/auth/me says administers %v, /access/me %v", me["administers"], access.Can.Administer)
	}
	return access.Can.Administer
}

// Alice is in exciter-administrators and engineering at the provider. A grant to the group exciter-administrators
// makes her an administrator here from her next sign-in, taking it takes that away, and a group the provider no
// longer names at a sign-in takes its grants with it.
func TestAProviderGroupsGrantsReachItsPeopleAndGoWithTheGroup(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, a, kc.issuer(), false)
	h.letIn(t, aliceEmail, "member")
	h.letIn(t, daveEmail, "admin")
	admin, adminID := adminOf(t, h, a)
	aliceID := h.userID(t, aliceEmail)

	t.Run("with no group granted anything alice is a member who administers nothing", func(t *testing.T) {
		b := signedIn(t, a, "alice", alicePassword)
		if role := b.roleIn(t); role != "member" || b.administers(t) {
			t.Fatalf("alice is %q and administers %v", role, b.administers(t))
		}
	})

	var grantID string
	t.Run("a grant to exciter-administrators makes her an administrator", func(t *testing.T) {
		grantID = mapGroup(t, admin, "exciter-administrators", "administrator")
		mapGroup(t, admin, "engineering", "viewer")
		b := signedIn(t, a, "alice", alicePassword)
		if role := b.roleIn(t); role != "member" || !b.administers(t) {
			t.Fatalf("alice is %q and administers %v, want a member who administers", role, b.administers(t))
		}
		if got := h.providerGroups(t, aliceEmail); !slices.Contains(got, "exciter-administrators") || !slices.Contains(got, "engineering") {
			t.Fatalf("alice's provider groups = %q", got)
		}
	})

	t.Run("the list of members says which group the role comes through", func(t *testing.T) {
		got := map[string][]string{}
		for _, each := range list(t, want(t, admin.get(t, "/api/v1/users"), http.StatusOK, "list the members"), "members") {
			m := each.(map[string]any)
			for _, r := range m["roles"].([]any) {
				role := r.(map[string]any)
				via := "by name"
				if name, ok := role["groupName"].(string); ok {
					via = name
				}
				got[m["email"].(string)] = append(got[m["email"].(string)], role["role"].(string)+" through "+via)
			}
		}
		if !slices.Contains(got[aliceEmail], "administrator through exciter-administrators") || !slices.Contains(got[aliceEmail], "viewer through engineering") ||
			!slices.Contains(got[daveEmail], "administrator through by name") {
			t.Fatalf("members hold %v", got)
		}
	})

	t.Run("dave, an administrator by name in no granted group, is left alone", func(t *testing.T) {
		if b := signedIn(t, a, "dave", davePassword); !b.administers(t) {
			t.Fatal("dave no longer administers")
		}
	})

	t.Run("taking the group's grant away takes her administering away", func(t *testing.T) {
		want(t, admin.delete(t, "/api/v1/role-assignments/"+grantID), http.StatusNoContent, "revoke the group's grant")
		refuse(t, "revoke it twice", admin.delete(t, "/api/v1/role-assignments/"+grantID), http.StatusNotFound, "not_found")
		if b := signedIn(t, a, "alice", alicePassword); b.administers(t) || b.roleIn(t) != "member" {
			t.Fatal("alice still administers after the group's grant went")
		}
	})

	t.Run("a group the provider no longer names at a sign-in takes its grants along", func(t *testing.T) {
		key := projectKey(t, "FMR")
		project := obj(t, want(t, admin.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Former"}), http.StatusCreated, "make a project"), "project")
		// Alice is put in a group of the provider's that Keycloak does not name for her, as if she had been in it
		// and was taken out there.
		former := providerGroup(t, admin, "former-team")
		if _, err := h.super.Exec(context.Background(), `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, former, aliceID); err != nil {
			t.Fatal(err)
		}
		want(t, admin.post(t, "/api/v1/role-assignments", map[string]any{"role": "admin", "projectKey": key, "groupId": former}),
			http.StatusCreated, "grant former-team the project")
		h.settle(t)
		holds := func() bool {
			var held bool
			if err := h.super.QueryRow(context.Background(), `SELECT perm_project_holds($1, $2, 'administer')`, project["id"], aliceID).Scan(&held); err != nil {
				t.Fatal(err)
			}
			return held
		}
		if !holds() {
			t.Fatal("alice does not run the project through former-team before her sign-in")
		}
		signedIn(t, a, "alice", alicePassword)
		if got := h.providerGroups(t, aliceEmail); slices.Contains(got, "former-team") {
			t.Fatalf("alice is still in former-team: %q", got)
		}
		if holds() {
			t.Fatal("alice still runs the project through a group the provider took her out of")
		}
	})

	t.Run("every grant the administrator gave and took is on the record, and no standing changed", func(t *testing.T) {
		var added, removed int
		_ = h.super.QueryRow(context.Background(), `
			SELECT count(*) FILTER (WHERE action = 'grant.added'), count(*) FILTER (WHERE action = 'grant.removed')
			FROM audit_log WHERE actor_user_id = $1`, adminID).Scan(&added, &removed)
		if added != 3 || removed != 1 {
			t.Errorf("the administrator's grants recorded %d added and %d removed, want 3 and 1", added, removed)
		}
		if changes := h.auditRows(t, "member.role_changed", aliceID); len(changes) != 0 {
			t.Errorf("signing in changed alice's standing: %v", changes)
		}
	})
}

// A group with a grant is the administrators' yes given in advance, once they let people in by their groups:
// carol, whom nobody let in, joins directly once quality holds a grant. Somebody in no such group still waits.
func TestAMappedGroupLetsSomebodyInWithoutWaiting(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, a, kc.issuer(), true)
	admin, _ := adminOf(t, h, a)

	t.Run("before the grant carol waits like anybody", func(t *testing.T) {
		if landing := newBrowser(t, a).signIn(t, "carol", carolPassword, "/"); landing != "http://"+appHost+"/login?sso=not_a_member" {
			t.Fatalf("carol landed on %q", landing)
		}
		if n := h.countRows(t, `SELECT count(*) FROM join_request r JOIN app_user u ON u.id = r.user_id WHERE u.email = $1`, carolEmail); n != 1 {
			t.Fatalf("carol has %d requests, want 1", n)
		}
	})

	mapGroup(t, admin, "quality", "viewer")
	admitByGroup(t, admin, true)
	carolID := h.userID(t, carolEmail)

	t.Run("once quality holds a grant carol joins directly", func(t *testing.T) {
		b := newBrowser(t, a)
		if landing := b.signIn(t, "carol", carolPassword, "/settings"); landing != "http://"+appHost+"/settings" {
			t.Fatalf("carol landed on %q, want where she was going", landing)
		}
		if role := b.roleIn(t); role != "member" || b.administers(t) {
			t.Fatalf("carol is %q, want a member who administers nothing", role)
		}
		if role := h.standing(t, carolEmail); role != "member" {
			t.Fatalf("carol's membership is %s", role)
		}
		if got := h.providerGroups(t, carolEmail); !slices.Equal(got, carolGroups) {
			t.Fatalf("carol's groups = %q", got)
		}
		if n := h.countRows(t, `SELECT count(*) FROM group_member WHERE user_id = $1 AND group_id = `+everyoneSQL, carolID); n != 1 {
			t.Fatal("carol is not in Everyone, which holds every member")
		}
	})

	t.Run("her request is answered and her joining is on the record", func(t *testing.T) {
		if n := h.countRows(t, `SELECT count(*) FROM join_request WHERE user_id = $1`, carolID); n != 0 {
			t.Fatalf("carol still has %d requests", n)
		}
		joined := h.auditRows(t, "member.joined", carolID)
		if len(joined) != 1 || joined[0]["role"] != "member" || joined[0]["byProvider"] != true {
			t.Fatalf("joining recorded as %v", joined)
		}
		if groups, _ := joined[0]["groups"].([]any); len(groups) != 1 || groups[0] != "quality" {
			t.Fatalf("the join names groups %v", joined[0]["groups"])
		}
	})

	// Somebody of the realm's own, since the seed already let bob in.
	t.Run("somebody in no group with a grant still waits", func(t *testing.T) {
		username := "nobody-" + h.suffix()
		email := kc.newPerson(t, h, username, bobPassword)
		if landing := newBrowser(t, a).signIn(t, username, bobPassword, "/"); landing != "http://"+appHost+"/login?sso=not_a_member" {
			t.Fatalf("%s landed on %q", username, landing)
		}
		if role := h.standing(t, email); role != "" {
			t.Fatalf("%s became %s", username, role)
		}
	})
}

// Administering by name is nobody's group's to take: alice keeps it whatever the provider says of her groups, and
// a sign-in records no change to her standing.
func TestAnAdministratorByNameKeepsItWhateverTheGroups(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, a, kc.issuer(), false)
	h.letIn(t, aliceEmail, "admin")
	admin, _ := adminOf(t, h, a)
	aliceID := h.userID(t, aliceEmail)
	var since time.Time
	if err := h.super.QueryRow(context.Background(), `SELECT now()`).Scan(&since); err != nil {
		t.Fatal(err)
	}

	for _, group := range []string{"engineering", "exciter-administrators"} {
		mapGroup(t, admin, group, "viewer")
		if b := signedIn(t, a, "alice", alicePassword); b.roleIn(t) != "member" || !b.administers(t) {
			t.Fatalf("with %s granted viewer alice no longer administers", group)
		}
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE target_id = $1 AND action LIKE 'member.%' AND created_at >= $2`, aliceID, since); n != 0 {
		t.Fatalf("%d standing changes recorded for an administrator nobody moved", n)
	}
}

// appConnUnscoped is a connection as exciter_app acting for nobody.
func (h *harness) appConnUnscoped(t *testing.T) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), os.Getenv(envPrimary))
	if err != nil {
		t.Fatalf("connect as the app role: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// appConn is a connection as exciter_app, acting for somebody when one is named.
func (h *harness) appConn(t *testing.T, actor ...uuid.UUID) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	conn := h.appConnUnscoped(t)
	for _, id := range actor {
		if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, db.UserVar, id.String()); err != nil {
			t.Fatal(err)
		}
	}
	return conn
}

// Switching somebody off ends their session at once and keeps their standing and provider groups for switching
// them on; nobody switches themselves off.
func TestAnAdministratorSwitchesOffASignedInMember(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, a, kc.issuer(), true)
	h.letIn(t, aliceEmail, "admin")
	h.letIn(t, bobEmail, "member")
	admin, _ := adminOf(t, h, a)
	aliceID, bobID := h.userID(t, aliceEmail), h.userID(t, bobEmail)
	t.Cleanup(func() {
		h.cleanupExec(t, h.super, `UPDATE app_user SET is_active = true WHERE id = ANY($1)`, []uuid.UUID{aliceID, bobID})
	})
	bob := signedIn(t, a, "bob", bobPassword)
	alice := signedIn(t, a, "alice", alicePassword)
	switchOff := func(b *browser, id uuid.UUID) int {
		resp, _ := b.send(t, http.MethodDelete, a.URL+httpapi.APIPrefix+"/users/"+id.String())
		return resp.StatusCode
	}

	if status := switchOff(bob, aliceID); status != http.StatusForbidden {
		t.Fatalf("bob switching alice off = %d", status)
	}
	if status := switchOff(alice, aliceID); status != http.StatusConflict {
		t.Fatalf("alice switching herself off = %d, want 409", status)
	}
	groups := h.providerGroups(t, bobEmail)
	if len(groups) == 0 {
		t.Fatal("bob has no groups to keep; the test proves nothing")
	}
	want(t, admin.delete(t, "/api/v1/users/"+bobID.String()), http.StatusNoContent, "switch bob off")
	if role := h.standing(t, bobEmail); role != "member" {
		t.Fatalf("bob is %q once switched off, want still a member", role)
	}
	if got := h.providerGroups(t, bobEmail); len(got) != len(groups) {
		t.Fatalf("bob has the groups %q once switched off, want %q", got, groups)
	}
	if status, me := bob.me(t); status != http.StatusUnauthorized {
		t.Fatalf("bob's open session goes on after he was switched off: %d %v", status, me)
	}
}
