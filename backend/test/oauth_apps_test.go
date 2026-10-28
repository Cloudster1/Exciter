//go:build integration

package test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
)

// grantsIn are the grants a list answers, by the client they connect.
func grantsIn(t *testing.T, r response) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, each := range list(t, r, "grants") {
		g := each.(map[string]any)
		out[g["client"].(map[string]any)["name"].(string)] = g
	}
	return out
}

// endedBy counts the records of a grant ended, by who ended it.
func endedBy(t *testing.T, h *harness, grant any, by string) int {
	t.Helper()
	return h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'oauth.revoked' AND target_id = $1 AND data->>'by' = $2`, grant, by)
}

// A person sees the apps they connected and ends one, which stops its tokens at once; an administrator sees and
// ends anybody's, and nobody else sees another's (#189).
func TestAConnectedAppIsListedAndEnded(t *testing.T) {
	h := newHarness(t)
	a := newOAuthServer(t, h)
	admin := a.as(t, h.owner(t, "apps").user)
	member := a.as(t, h.addPerson(t, "member"))
	other := a.as(t, h.addPerson(t, "member"))
	desk, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Desk assistant"})
	phone, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Phone assistant"})
	mine := desk.exchange(t, desk.allow(t, member, map[string]any{"allow": true, "readOnly": true}))
	theirs := phone.exchange(t, phone.allow(t, other, map[string]any{"allow": true}))
	rpcResult(t, mcpAs(t, a, mine["access_token"].(string), "tools/list", nil))
	h.settle(t)

	t.Run("a person lists their own and nobody else's", func(t *testing.T) {
		listed := grantsIn(t, want(t, member.get(t, "/api/v1/oauth/grants"), http.StatusOK, "list my apps"))
		g, ok := listed["Desk assistant"]
		if len(listed) != 1 || !ok || g["redirectHost"] != "127.0.0.1:9" || g["scopes"].([]any)[0] != "read" || g["allProjects"] != true ||
			g["lastUsedAt"] == nil || g["createdAt"] == nil {
			t.Errorf("the member's apps read %v", listed)
		}
		refuse(t, "a member listing everybody's", member.get(t, "/api/v1/oauth/grants/everyone"), http.StatusForbidden, "forbidden")
		refuse(t, "nobody listing", a.anonymous().get(t, "/api/v1/oauth/grants"), http.StatusUnauthorized, "unauthorized")
		theirsID := grantsIn(t, want(t, other.get(t, "/api/v1/oauth/grants"), http.StatusOK, "list theirs"))["Phone assistant"]["id"].(string)
		refuse(t, "ending somebody else's", member.delete(t, "/api/v1/oauth/grants/"+theirsID), http.StatusNotFound, "not_found")
		refuse(t, "ending one that is no id", member.delete(t, "/api/v1/oauth/grants/nothing"), http.StatusBadRequest, "bad_request")
	})

	t.Run("ending one stops its tokens at once, and only from a session", func(t *testing.T) {
		id := grantsIn(t, want(t, member.get(t, "/api/v1/oauth/grants"), http.StatusOK, "list my apps"))["Desk assistant"]["id"].(string)
		_, pat := makeToken(t, member, map[string]any{"name": "script"})
		h.settle(t)
		refuse(t, "a token ending a connection", a.withToken(member.user, pat).delete(t, "/api/v1/oauth/grants/"+id), http.StatusForbidden, "session_only")
		want(t, member.delete(t, "/api/v1/oauth/grants/"+id), http.StatusNoContent, "end my app")
		if resp := mcpAs(t, a, mine["access_token"].(string), "tools/list", nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("an ended app's access token still works: %d", resp.StatusCode)
		}
		if status, got := desk.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {mine["refresh_token"].(string)}}); status != http.StatusBadRequest ||
			got["error"] != "invalid_grant" {
			t.Errorf("an ended app's refresh token answers %d %v", status, got)
		}
		if endedBy(t, h, id, "person") != 1 {
			t.Error("ending one's own app is not recorded")
		}
		refuse(t, "ending it again", member.delete(t, "/api/v1/oauth/grants/"+id), http.StatusNotFound, "not_found")
	})

	t.Run("an administrator sees and ends anybody's", func(t *testing.T) {
		everyone := grantsIn(t, want(t, admin.get(t, "/api/v1/oauth/grants/everyone"), http.StatusOK, "list everybody's"))
		g, ok := everyone["Phone assistant"]
		if !ok || g["owner"].(map[string]any)["id"] != other.user.String() {
			t.Fatalf("everybody's apps read %v", everyone)
		}
		refuse(t, "a member ending anybody's", member.delete(t, "/api/v1/oauth/grants/everyone/"+g["id"].(string)), http.StatusForbidden, "forbidden")
		refuse(t, "ending nobody's", admin.delete(t, "/api/v1/oauth/grants/everyone/"+uuid.NewString()), http.StatusNotFound, "not_found")
		refuse(t, "ending one that is no id", admin.delete(t, "/api/v1/oauth/grants/everyone/nothing"), http.StatusBadRequest, "bad_request")
		if _, kept := grantsIn(t, want(t, admin.get(t, "/api/v1/oauth/grants/everyone"), http.StatusOK, "list everybody's again"))["Phone assistant"]; !kept ||
			endedBy(t, h, g["id"], "administrator") != 0 {
			t.Fatal("a refused end ended the app")
		}
		want(t, admin.delete(t, "/api/v1/oauth/grants/everyone/"+g["id"].(string)), http.StatusNoContent, "end their app")
		if resp := mcpAs(t, a, theirs["access_token"].(string), "tools/list", nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("an app an administrator ended still works: %d", resp.StatusCode)
		}
		if endedBy(t, h, g["id"], "administrator") != 1 {
			t.Error("an administrator ending an app is not recorded")
		}
	})

	t.Run("the client ending its refresh token, or a reused one, is recorded too", func(t *testing.T) {
		first := desk.exchange(t, desk.allow(t, member, map[string]any{"allow": true}))
		var grant uuid.UUID
		if err := h.super.QueryRow(t.Context(), `SELECT id FROM oauth_grant WHERE user_id = $1`, member.user).Scan(&grant); err != nil {
			t.Fatal(err)
		}
		if status, _ := desk.form(t, "/oauth/revoke", url.Values{"token": {first["refresh_token"].(string)}}); status != http.StatusOK || endedBy(t, h, grant, "client") != 1 {
			t.Errorf("the client's revocation answers %d and is recorded %d times", status, endedBy(t, h, grant, "client"))
		}
		again := desk.exchange(t, desk.allow(t, member, map[string]any{"allow": true}))
		if err := h.super.QueryRow(t.Context(), `SELECT id FROM oauth_grant WHERE user_id = $1`, member.user).Scan(&grant); err != nil {
			t.Fatal(err)
		}
		refresh := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {again["refresh_token"].(string)}}
		desk.token(t, refresh)
		desk.token(t, refresh)
		if endedBy(t, h, grant, "reused_token") != 1 {
			t.Error("a reused refresh token ending its grant is not recorded")
		}
	})
}
