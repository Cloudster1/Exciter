//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

// toolNames are the tools an MCP answer lists.
func toolNames(result map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, each := range result["tools"].([]any) {
		out[each.(map[string]any)["name"].(string)] = true
	}
	return out
}

// A grant holds as a personal token does: reading only when the person said so, and the projects they named,
// which the database holds too (#188).
func TestAGrantHoldsReadingAndItsProjects(t *testing.T) {
	inKey, outKey := projectKey(t, "GIN"), projectKey(t, "GOU")
	h := newHarness(t)
	a := newOAuthServer(t, h)
	owner := a.as(t, h.owner(t, "grant").user)
	for _, key := range []string{inKey, outKey} {
		want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
		want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "In " + key}), http.StatusCreated, "write in "+key)
	}
	c, _, _ := register(t, a, newAddress(), nil)
	// An assistant reads as nobody who just wrote, so what the test wrote reaches the replicas first.
	h.settle(t)

	t.Run("a grant made to read lists and does no writing", func(t *testing.T) {
		tokens := c.exchange(t, c.allow(t, owner, map[string]any{"allow": true, "readOnly": true}))
		if tokens["scope"] != "mcp:read" {
			t.Errorf("a reading grant's scope is %v", tokens["scope"])
		}
		access := tokens["access_token"].(string)
		if names := toolNames(rpcResult(t, mcpAs(t, a, access, "tools/list", nil))); names["create_requirement"] || !names["get_requirement"] {
			t.Errorf("a reading grant is offered %v", names)
		}
		called := rpcResult(t, mcpAs(t, a, access, "tools/call", map[string]any{"name": "create_requirement",
			"arguments": map[string]any{"projectKey": inKey, "title": "Never"}}))
		if called["isError"] != true || !strings.Contains(called["content"].([]any)[0].(map[string]any)["text"].(string), "can only read") {
			t.Errorf("a reading grant writes: %v", called)
		}
	})

	t.Run("a client that asked to read is granted no more", func(t *testing.T) {
		page := sentTo(t, c.authorizeURL(map[string]string{"scope": "mcp:read"}))
		path := httpapi.APIPrefix + "/oauth/requests/" + page.Query().Get("request")
		if got := want(t, owner.get(t, path), http.StatusOK, "read the request"); obj(t, got, "request")["readOnly"] != true {
			t.Errorf("the request reads %s", got.Raw)
		}
		refuse422(t, "everything for a client that asked to read", owner.post(t, path, map[string]any{"allow": true}), "readOnly")
		refuse422(t, "a project the person cannot see", owner.post(t, path, map[string]any{"allow": true, "readOnly": true,
			"projects": []string{"NOSUCH" + strings.ToUpper(uuid.NewString()[:4])}}), "projects")
	})

	var grant uuid.UUID
	t.Run("a grant limited to a project reaches it alone", func(t *testing.T) {
		access := c.exchange(t, c.allow(t, owner, map[string]any{"allow": true, "projects": []string{inKey}}))["access_token"].(string)
		listed := rpcResult(t, mcpAs(t, a, access, "tools/call", map[string]any{"name": "list_projects", "arguments": map[string]any{}}))
		var keys []string
		for _, p := range listed["structuredContent"].(map[string]any)["projects"].([]any) {
			keys = append(keys, p.(map[string]any)["key"].(string))
		}
		if len(keys) != 1 || keys[0] != inKey {
			t.Errorf("the limited grant lists %v", keys)
		}
		if got := rpcResult(t, mcpAs(t, a, access, "tools/call", map[string]any{"name": "get_requirement",
			"arguments": map[string]any{"reqKey": outKey + "-R1"}})); got["isError"] != true {
			t.Errorf("the limited grant reads outside its project: %v", got)
		}
		if err := h.super.QueryRow(context.Background(), `SELECT id FROM oauth_grant WHERE user_id = $1`, owner.user).Scan(&grant); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("straight through SQL as exciter_app, the grant holds", func(t *testing.T) {
		ctx := context.Background()
		var in uuid.UUID
		if err := h.super.QueryRow(ctx, `SELECT project_id FROM oauth_grant_project WHERE grant_id = $1`, grant).Scan(&in); err != nil {
			t.Fatal(err)
		}
		limited := h.appConn(t, owner.user)
		limitTo(t, limited, in)
		count := `SELECT count(*) FROM requirement r JOIN project p ON p.id = r.project_id WHERE p.key = $1`
		if seesRows(t, limited, count, inKey) != 1 || seesRows(t, limited, count, outKey) != 0 {
			t.Error("acting with the grant's projects reaches another project")
		}
		if seesRows(t, limited, `SELECT count(*) FROM oauth_grant`) != 0 {
			t.Error("a limited grant reads grants, which concern the whole installation")
		}
		mine := h.appConn(t, owner.user)
		member := h.addPerson(t, "member")
		other := h.appConn(t, member)
		if seesRows(t, other, `SELECT count(*) FROM oauth_grant WHERE id = $1`, grant) != 0 || seesRows(t, mine, `SELECT count(*) FROM oauth_grant WHERE id = $1`, grant) != 1 {
			t.Error("a grant is not kept to its owner")
		}
		refusedOutright(t, other, "a grant made in somebody else's name",
			`INSERT INTO oauth_grant (user_id, client_id, redirect_uri, read_only) VALUES ($1, $2, $3, false)`, owner.user, c.id, callback)
		refusedOutright(t, mine, "a grant widened", `UPDATE oauth_grant SET read_only = false, projects_only = false WHERE id = $1`, grant)
		refusedOutright(t, mine, "a project added to an older grant", `INSERT INTO oauth_grant_project (grant_id, project_id)
			SELECT $1, id FROM project WHERE key = $2`, grant, outKey)
		for _, table := range []string{"oauth_token", "oauth_code", "oauth_request"} {
			refusedOutright(t, mine, "reading "+table, `SELECT count(*) FROM `+table)
		}
		touchesNothing(t, other, "ending somebody else's grant", `DELETE FROM oauth_grant WHERE id = $1`, grant)
	})

	t.Run("switching somebody off ends their grants", func(t *testing.T) {
		member := a.as(t, h.addPerson(t, "member"))
		access := c.exchange(t, c.allow(t, member, map[string]any{"allow": true}))["access_token"].(string)
		rpcResult(t, mcpAs(t, a, access, "tools/list", nil))
		want(t, owner.delete(t, "/api/v1/users/"+member.user.String()), http.StatusNoContent, "switch the member off")
		if n := h.countRows(t, `SELECT count(*) FROM oauth_grant WHERE user_id = $1`, member.user); n != 0 {
			t.Errorf("%d grants outlive their person", n)
		}
		if resp := mcpAs(t, a, access, "tools/list", nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("a switched off person's assistant still works: %d", resp.StatusCode)
		}
	})
}

// Administrators switch OAuth connections off for everyone: discovery goes, tokens are refused without pointing
// anywhere, and the grants come back when switched on.
func TestOAuthConnectionsAreSwitchedOffAndOn(t *testing.T) {
	h := newHarness(t)
	h.keepOAuthConnections(t)
	a := newOAuthServer(t, h)
	owner := a.as(t, h.owner(t, "switch").user)
	c, _, _ := register(t, a, newAddress(), nil)
	tokens := c.exchange(t, c.allow(t, owner, map[string]any{"allow": true}))
	access := tokens["access_token"].(string)

	refuse(t, "a member switching", a.as(t, h.addPerson(t, "member")).put(t, "/api/v1/installation/oauth-connections", map[string]any{"enabled": false}),
		http.StatusForbidden, "forbidden")
	got := want(t, owner.put(t, "/api/v1/installation/oauth-connections", map[string]any{"enabled": false}), http.StatusOK, "switch off")
	if obj(t, got, "installation")["oauthConnections"] != false {
		t.Errorf("switching off answers %s", got.Raw)
	}
	refuse(t, "the resource while off", a.anonymous().get(t, "/.well-known/oauth-protected-resource"), http.StatusNotFound, "not_found")
	refuse(t, "the server while off", a.anonymous().get(t, "/.well-known/oauth-authorization-server"), http.StatusNotFound, "not_found")
	refuse(t, "the resource at its own address while off", a.anonymous().get(t, "/.well-known/oauth-protected-resource/api/v1/mcp"), http.StatusNotFound, "not_found")
	resp := mcpAs(t, a, access, "tools/list", nil)
	if resp.StatusCode != http.StatusUnauthorized || strings.Contains(resp.Header.Get("WWW-Authenticate"), "resource_metadata") {
		t.Errorf("a token while off answers %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	if status, answer := c.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens["refresh_token"].(string)}}); status != http.StatusBadRequest ||
		answer["error"] != "temporarily_unavailable" {
		t.Errorf("a refresh while off answers %d %v", status, answer)
	}
	if to := sentTo(t, c.authorizeURL(nil)); to.Query().Get("error") != "switched_off" {
		t.Errorf("an authorization while off goes to %s", to)
	}
	if _, resp, answer := register(t, a, newAddress(), nil); resp.StatusCode != http.StatusBadRequest || answer["error"] != "temporarily_unavailable" {
		t.Errorf("a registration while off answers %d %v", resp.StatusCode, answer)
	}

	want(t, owner.put(t, "/api/v1/installation/oauth-connections", map[string]any{"enabled": true}), http.StatusOK, "switch on")
	rpcResult(t, mcpAs(t, a, access, "tools/list", nil))
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'installation.oauth_switched' AND actor_user_id = $1`, owner.user); n != 2 {
		t.Errorf("%d audit entries for switching, want 2", n)
	}
}
