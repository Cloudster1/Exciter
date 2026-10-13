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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// standing is somebody's role in org and who decided it, straight from the table.
func (h *harness) standing(t *testing.T, org tenant.Org, email string) (string, string) {
	t.Helper()
	var role, source string
	err := h.super.QueryRow(context.Background(), `
		SELECT m.org_role, m.role_source FROM org_member m JOIN app_user u ON u.id = m.user_id
		WHERE m.org_id = $1 AND u.email = $2`, org.ID, email).Scan(&role, &source)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return role, source
}

// auditRows lists what the record holds about one person, oldest first.
func (h *harness) auditRows(t *testing.T, org tenant.Org, action string, target uuid.UUID) []map[string]any {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT data, actor_user_id IS NULL FROM audit_log
		WHERE org_id = $1 AND action = $2 AND target_id = $3 ORDER BY created_at, id`, org.ID, action, target)
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

func mapGroup(t *testing.T, admin *client, group, role string) string {
	t.Helper()
	got := want(t, admin.post(t, groupRolesPath, map[string]string{"group": group, "role": role}), http.StatusOK, "map "+group+" to "+role)
	id, _ := obj(t, got, "groupRole")["id"].(string)
	return id
}

// Alice is in exciter-administrators at the provider. Mapping that group makes
// her an administrator here at her next sign-in, and unmapping it takes it back.
func TestAMappedGroupGrantsItsRoleAndTakesItBack(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	org := h.makeOrg(t, "mapped")
	configureProvider(t, a, org, kc.issuer(), false)
	h.letIn(t, org, aliceEmail, "member")
	h.letIn(t, org, bobEmail, "admin")
	h.letIn(t, org, daveEmail, "admin")
	admin, adminID := adminOf(t, h, a, org)
	aliceID := h.userID(t, aliceEmail)

	t.Run("with nothing mapped alice keeps the role she was given", func(t *testing.T) {
		if role := signedIn(t, a, org, "alice", alicePassword).roleIn(t); role != "member" {
			t.Fatalf("alice is %q, want member", role)
		}
	})

	var mappingID string
	t.Run("mapping exciter-administrators makes her an admin", func(t *testing.T) {
		mappingID = mapGroup(t, admin, "exciter-administrators", "admin")
		mapGroup(t, admin, "engineering", "member")
		if role := signedIn(t, a, org, "alice", alicePassword).roleIn(t); role != "admin" {
			t.Fatalf("alice is %q after the mapping, want admin, the higher of her two groups", role)
		}
		if role, source := h.standing(t, org, aliceEmail); role != "admin" || source != "oidc" {
			t.Fatalf("alice's membership is %s from %s, want admin from oidc", role, source)
		}
	})

	t.Run("the administrator sees both mappings", func(t *testing.T) {
		got := map[string]string{}
		for _, each := range list(t, want(t, admin.get(t, groupRolesPath), http.StatusOK, "list the mapping"), "groupRoles") {
			m := each.(map[string]any)
			got[m["group"].(string)] = m["role"].(string)
		}
		if len(got) != 2 || got["exciter-administrators"] != "admin" || got["engineering"] != "member" {
			t.Fatalf("group roles = %v", got)
		}
	})

	t.Run("the list of members says the role comes from the provider", func(t *testing.T) {
		got := map[string]string{}
		for _, each := range list(t, want(t, admin.get(t, "/api/v1/users"), http.StatusOK, "list the members"), "members") {
			m := each.(map[string]any)
			got[m["email"].(string)] = m["role"].(string) + "/" + m["roleSource"].(string)
		}
		if got[aliceEmail] != "admin/oidc" || got[bobEmail] != "admin/manual" || got[daveEmail] != "admin/manual" {
			t.Fatalf("members = %v", got)
		}
	})

	t.Run("bob, an admin by hand in a group mapped to member, follows the group", func(t *testing.T) {
		if role := signedIn(t, a, org, "bob", bobPassword).roleIn(t); role != "member" {
			t.Fatalf("bob is %q, want the member engineering grants", role)
		}
		if role, source := h.standing(t, org, bobEmail); role != "member" || source != "oidc" {
			t.Fatalf("bob's membership is %s from %s", role, source)
		}
	})

	t.Run("dave, an admin by hand in no mapped group, is left alone", func(t *testing.T) {
		if role := signedIn(t, a, org, "dave", davePassword).roleIn(t); role != "admin" {
			t.Fatalf("dave is %q, want the admin somebody made him", role)
		}
		if role, source := h.standing(t, org, daveEmail); role != "admin" || source != "manual" {
			t.Fatalf("dave's membership is %s from %s", role, source)
		}
	})

	t.Run("unmapping the group makes her a member again at her next sign-in", func(t *testing.T) {
		want(t, admin.delete(t, groupRolesPath+"/"+mappingID), http.StatusNoContent, "unmap exciter-administrators")
		want(t, admin.delete(t, groupRolesPath+"/"+mappingID), http.StatusNotFound, "unmap it twice")
		if role := signedIn(t, a, org, "alice", alicePassword).roleIn(t); role != "member" {
			t.Fatalf("alice is %q after the unmapping, want member", role)
		}
		if role, source := h.standing(t, org, aliceEmail); role != "member" || source != "oidc" {
			t.Fatalf("alice's membership is %s from %s, want member from oidc through engineering", role, source)
		}
	})

	t.Run("with no mapped group left she stays a member", func(t *testing.T) {
		var engineering string
		_ = h.super.QueryRow(context.Background(), `SELECT id FROM oidc_group_role WHERE org_id = $1 AND group_ref = 'engineering'`, org.ID).Scan(&engineering)
		want(t, admin.delete(t, groupRolesPath+"/"+engineering), http.StatusNoContent, "unmap engineering")
		if role := signedIn(t, a, org, "alice", alicePassword).roleIn(t); role != "member" {
			t.Fatalf("alice is %q, want member: the floor while she is still a member", role)
		}
		if role, source := h.standing(t, org, aliceEmail); role != "member" || source != "manual" {
			t.Fatalf("alice's membership is %s from %s, want member from manual", role, source)
		}
	})

	t.Run("every role the mapping changed is on the record, by nobody here", func(t *testing.T) {
		changes := h.auditRows(t, org, "member.role_changed", aliceID)
		wantChanges := [][2]string{{"member", "admin"}, {"admin", "member"}}
		if len(changes) != len(wantChanges) {
			t.Fatalf("recorded changes = %v, want %v", changes, wantChanges)
		}
		for i, change := range changes {
			if change["from"] != wantChanges[i][0] || change["to"] != wantChanges[i][1] || change["byProvider"] != true {
				t.Errorf("change %d = %v, want %s to %s by the provider", i, change, wantChanges[i][0], wantChanges[i][1])
			}
		}
		if groups, _ := changes[0]["groups"].([]any); len(groups) != 1 || groups[0] != "exciter-administrators" {
			t.Errorf("the promotion names groups %v", changes[0]["groups"])
		}
		var set, removed int
		_ = h.super.QueryRow(context.Background(), `
			SELECT count(*) FILTER (WHERE action = 'sso.group_role_set'), count(*) FILTER (WHERE action = 'sso.group_role_removed')
			FROM audit_log WHERE org_id = $1 AND actor_user_id = $2`, org.ID, adminID).Scan(&set, &removed)
		if set != 2 || removed != 2 {
			t.Errorf("the administrator's mapping changes recorded %d sets and %d removals, want 2 and 2", set, removed)
		}
	})
}

// A mapped group is the administrators' yes given in advance: carol, whom
// nobody let in, joins directly once quality is mapped. Bob still waits.
func TestAMappedGroupLetsSomebodyInWithoutWaiting(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	org := h.makeOrg(t, "welcome")
	configureProvider(t, a, org, kc.issuer(), true)
	admin, _ := adminOf(t, h, a, org)

	t.Run("before the mapping carol waits like anybody", func(t *testing.T) {
		if landing := newBrowser(t, a).signIn(t, org, "carol", carolPassword, "/"); landing != "http://"+appHost+"/login?sso=not_a_member" {
			t.Fatalf("carol landed on %q", landing)
		}
		if n := h.countRows(t, `SELECT count(*) FROM org_join_request r JOIN app_user u ON u.id = r.user_id WHERE r.org_id = $1 AND u.email = $2`, org.ID, carolEmail); n != 1 {
			t.Fatalf("carol has %d requests, want 1", n)
		}
	})

	mapGroup(t, admin, "quality", "member")
	carolID := h.userID(t, carolEmail)

	t.Run("once quality is mapped carol joins directly", func(t *testing.T) {
		b := newBrowser(t, a)
		if landing := b.signIn(t, org, "carol", carolPassword, "/settings"); landing != "http://"+appHost+"/settings" {
			t.Fatalf("carol landed on %q, want where she was going", landing)
		}
		if role := b.roleIn(t); role != "member" {
			t.Fatalf("carol is %q, want member", role)
		}
		if role, source := h.standing(t, org, carolEmail); role != "member" || source != "oidc" {
			t.Fatalf("carol's membership is %s from %s", role, source)
		}
		if got := h.providerGroups(t, org, carolEmail); !slices.Equal(got, []string{"quality"}) {
			t.Fatalf("carol's groups = %q", got)
		}
	})

	t.Run("her request is answered and her joining is on the record", func(t *testing.T) {
		if n := h.countRows(t, `SELECT count(*) FROM org_join_request WHERE org_id = $1 AND user_id = $2`, org.ID, carolID); n != 0 {
			t.Fatalf("carol still has %d requests", n)
		}
		joined := h.auditRows(t, org, "member.joined", carolID)
		if len(joined) != 1 || joined[0]["role"] != "member" || joined[0]["byProvider"] != true {
			t.Fatalf("joining recorded as %v", joined)
		}
		if groups, _ := joined[0]["groups"].([]any); len(groups) != 1 || groups[0] != "quality" {
			t.Fatalf("the join names groups %v", joined[0]["groups"])
		}
	})

	t.Run("bob, in no mapped group, still waits", func(t *testing.T) {
		if landing := newBrowser(t, a).signIn(t, org, "bob", bobPassword, "/"); landing != "http://"+appHost+"/login?sso=not_a_member" {
			t.Fatalf("bob landed on %q", landing)
		}
		if role, _ := h.standing(t, org, bobEmail); role != "" {
			t.Fatalf("bob became %s", role)
		}
	})
}

// The owner is how an organization is always entered again, so no mapping
// moves them, and the database refuses to let a later code path try.
func TestTheOwnerIsNeverDemotedByAMapping(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	org := h.makeOrg(t, "keep")
	configureProvider(t, a, org, kc.issuer(), false)
	h.letIn(t, org, aliceEmail, "owner")
	admin, adminID := adminOf(t, h, a, org)

	for _, mapping := range [][2]string{{"engineering", "member"}, {"exciter-administrators", "admin"}} {
		mapGroup(t, admin, mapping[0], mapping[1])
		if role := signedIn(t, a, org, "alice", alicePassword).roleIn(t); role != "owner" {
			t.Fatalf("with %s mapped to %s alice is %q, want owner", mapping[0], mapping[1], role)
		}
		if role, source := h.standing(t, org, aliceEmail); role != "owner" || source != "manual" {
			t.Fatalf("alice's membership is %s from %s", role, source)
		}
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action LIKE 'member.%'`, org.ID); n != 0 {
		t.Fatalf("%d role changes recorded for an owner nobody moved", n)
	}

	aliceID := h.userID(t, aliceEmail)
	handOver := []string{
		`UPDATE org_member SET org_role = 'member', role_source = 'oidc' WHERE user_id = $1`,
		`UPDATE org_member SET role_source = 'oidc' WHERE user_id = $1`,
	}
	t.Run("straight through SQL an administrator cannot hand an owner to the provider", func(t *testing.T) {
		conn := h.appConn(t, org.ID, adminID)
		for _, sql := range handOver {
			_, err := conn.Exec(context.Background(), sql, aliceID)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || (pgErr.Code != "23514" && pgErr.Code != "42501") {
				t.Errorf("%s = %v, want refused by the database", sql, err)
			}
		}
	})
	t.Run("nor can the admin role, past every policy", func(t *testing.T) {
		for _, sql := range handOver {
			_, err := h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
				_, err := tx.Exec(ctx, sql, aliceID)
				return err
			})
			if !isCheckViolation(err) {
				t.Errorf("%s = %v, want a check violation", sql, err)
			}
		}
	})
}

// appConnUnscoped is a connection as exciter_app that names no organization.
func (h *harness) appConnUnscoped(t *testing.T) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), os.Getenv(envPrimary))
	if err != nil {
		t.Fatalf("connect as the app role: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// appConn is a connection as exciter_app scoped to one organization, acting for somebody when one is named.
func (h *harness) appConn(t *testing.T, org uuid.UUID, actor ...uuid.UUID) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	conn := h.appConnUnscoped(t)
	if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, tenant.PostgresVar, org.String()); err != nil {
		t.Fatal(err)
	}
	for _, id := range actor {
		if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, db.UserVar, id.String()); err != nil {
			t.Fatal(err)
		}
	}
	return conn
}

// Straight through SQL as exciter_app, one organization's mapping is out of another's reach, out of a plain
// member's reach in its own, and a mapping cannot grant ownership.
func TestRawSQLCannotReachAnotherTenantsGroupRoles(t *testing.T) {
	h := newHarness(t)
	a := h.makeMember(t, "nu")
	b := h.makeMember(t, "xi")
	ctx := context.Background()
	for _, m := range []member{a, b} {
		if _, err := h.super.Exec(ctx, `INSERT INTO oidc_provider (org_id, issuer, client_id) VALUES ($1, 'https://id.test', 'exciter')`, m.org); err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(ctx, `INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, 'staff', 'admin')`, m.org); err != nil {
			t.Fatal(err)
		}
	}

	var n int
	if err := h.appConnUnscoped(t).QueryRow(ctx, `SELECT count(*) FROM oidc_group_role`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("an unscoped connection sees %d mappings (%v)", n, err)
	}

	plain := h.appConn(t, a.org, h.addPerson(t, a.org, "member"))
	if err := plain.QueryRow(ctx, `SELECT count(*) FROM oidc_group_role`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a plain member of org A sees %d mappings (%v), want none", n, err)
	}
	if _, err := plain.Exec(ctx, `INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, 'mine', 'admin')`, a.org); !isPolicyViolation(err) {
		t.Fatalf("a plain member mapped a group: %v", err)
	}

	conn := h.appConn(t, a.org, a.user)
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM oidc_group_role`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("org A's owner sees %d mappings, want its own one (%v)", n, err)
	}
	tag, err := conn.Exec(ctx, `UPDATE oidc_group_role SET org_role = 'member' WHERE org_id = $1`, b.org)
	if err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("org A changed org B's mapping: %v %v", tag, err)
	}
	tag, err = conn.Exec(ctx, `DELETE FROM oidc_group_role WHERE org_id = $1`, b.org)
	if err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("org A deleted org B's mapping: %v %v", tag, err)
	}
	for name, attempt := range map[string]struct {
		sql  string
		args []any
		code string
	}{
		"mapping a group for another tenant": {`INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, 'evil', 'admin')`, []any{b.org}, "42501"},
		"granting ownership through a group": {`INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, 'founders', 'owner')`, []any{a.org}, "23514"},
		"mapping the same group twice":       {`INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, 'staff', 'member')`, []any{a.org}, "23505"},
		"mapping a blank group":              {`INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, '  ', 'member')`, []any{a.org}, "23514"},
	} {
		t.Run(name+" is refused", func(t *testing.T) {
			_, err := conn.Exec(ctx, attempt.sql, attempt.args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != attempt.code {
				t.Fatalf("%s = %v, want SQLSTATE %s", attempt.sql, err, attempt.code)
			}
		})
	}
	if n := h.countRows(t, `SELECT count(*) FROM oidc_group_role WHERE org_id = $1 AND org_role = 'admin'`, b.org); n != 1 {
		t.Fatal("org B's mapping did not survive org A")
	}
}

// Taking somebody out ends their reach and their provider groups at once; the owner and the caller stay.
func TestAnAdministratorRemovesASignedInMember(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	org := h.makeOrg(t, "leaving")
	configureProvider(t, a, org, kc.issuer(), true)
	h.letIn(t, org, aliceEmail, "admin")
	h.letIn(t, org, bobEmail, "member")
	admin, adminID := adminOf(t, h, a, org)
	aliceID, bobID := h.userID(t, aliceEmail), h.userID(t, bobEmail)
	bob := signedIn(t, a, org, "bob", bobPassword)
	alice := signedIn(t, a, org, "alice", alicePassword)
	remove := func(b *browser, id uuid.UUID) int {
		resp, _ := b.send(t, http.MethodDelete, a.URL+httpapi.APIPrefix+"/users/"+id.String())
		return resp.StatusCode
	}

	if status := remove(bob, aliceID); status != http.StatusForbidden {
		t.Fatalf("bob removing alice = %d", status)
	}
	if status := remove(alice, adminID); status != http.StatusConflict {
		t.Fatalf("alice removing the owner = %d, want 409", status)
	}
	if len(h.providerGroups(t, org, bobEmail)) == 0 {
		t.Fatal("bob has no groups to lose; the test proves nothing")
	}
	want(t, admin.delete(t, "/api/v1/users/"+bobID.String()), http.StatusNoContent, "remove bob")
	if role, _ := h.standing(t, org, bobEmail); role != "" {
		t.Fatalf("bob is still %s", role)
	}
	if got := h.providerGroups(t, org, bobEmail); len(got) != 0 {
		t.Fatalf("bob kept the groups %q", got)
	}
	if status, me := bob.me(t); status != http.StatusUnauthorized {
		t.Fatalf("bob's open session goes on after he was let go: %d %v", status, me)
	}
}
