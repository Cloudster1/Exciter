//go:build integration

package test

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/oauth"
)

// jsonRPCInvalidRequest is the JSON-RPC 2.0 code of a request that is not one.
const jsonRPCInvalidRequest = -32600

// tokenIDs are the ids of a person's own tokens, as their list answers them.
func tokenIDs(t *testing.T, c *client) []string {
	t.Helper()
	var ids []string
	for _, each := range list(t, want(t, c.get(t, "/api/v1/tokens"), http.StatusOK, "list the tokens"), "tokens") {
		ids = append(ids, each.(map[string]any)["id"].(string))
	}
	return ids
}

// The token and OAuth writes never seen refused, each refused with its status and code and nothing changed: a
// revocation by a held-back token, a script's consent, a malformed registration, revocation or MCP body (#329).
func TestTokenAndOAuthWritesRefuseWhatTheyMayNot(t *testing.T) {
	key := projectKey(t, "TOR")
	h := newHarness(t)
	a := newOAuthServer(t, h)
	person := a.as(t, h.owner(t, "token-refusals").user)
	member := a.as(t, h.addPerson(t, "member"))
	want(t, person.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	victim, victimSecret := makeToken(t, person, map[string]any{"name": "nightly"})
	_, readSecret := makeToken(t, person, map[string]any{"name": "reader", "scopes": []string{"read"}})
	_, limitedSecret := makeToken(t, person, map[string]any{"name": "limited", "projects": []string{key}})
	h.settle(t)

	t.Run("a token is revoked by neither a read-only nor a project token, nor by a member as everybody's", func(t *testing.T) {
		refuse(t, "a read-only token revoking", a.withToken(person.user, readSecret).delete(t, "/api/v1/tokens/"+victim), http.StatusForbidden, "read_only_token")
		refuse(t, "a project token revoking", a.withToken(person.user, limitedSecret).delete(t, "/api/v1/tokens/"+victim), http.StatusForbidden, "projects_token")
		refuse(t, "a member revoking it as everybody's", member.delete(t, "/api/v1/tokens/everyone/"+victim), http.StatusForbidden, "forbidden")
		refuse(t, "a token id that is none", person.delete(t, "/api/v1/tokens/nothing"), http.StatusBadRequest, "bad_request")
		refuse(t, "everybody's token by an id that is none", person.delete(t, "/api/v1/tokens/everyone/nothing"), http.StatusBadRequest, "bad_request")
		if ids := tokenIDs(t, person); !slices.Contains(ids, victim) {
			t.Errorf("a refused revocation took the token away: %v", ids)
		}
		want(t, a.withToken(person.user, victimSecret).get(t, "/api/v1/auth/me"), http.StatusOK, "the token still works")
	})

	t.Run("a consent is no script's to answer", func(t *testing.T) {
		c, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Scripted assistant"})
		path := c.pendingRequest(t)
		refuse(t, "a token allowing", a.withToken(person.user, victimSecret).post(t, path, map[string]any{"allow": true}), http.StatusForbidden, "session_only")
		refuse(t, "a token denying", a.withToken(person.user, victimSecret).post(t, path, map[string]any{"allow": false}), http.StatusForbidden, "session_only")
		if n := h.countRows(t, `SELECT count(*) FROM oauth_grant WHERE client_id = $1`, c.id); n != 0 {
			t.Errorf("a token's consent made %d grants", n)
		}
		want(t, person.get(t, path), http.StatusOK, "the request still waits for its person")
	})

	t.Run("a registration that does not decode registers nothing", func(t *testing.T) {
		name := "Malformed assistant " + h.suffix()
		_, resp, got := register(t, a, newAddress(), map[string]any{"client_name": name, "redirect_uris": callback})
		if resp.StatusCode != http.StatusBadRequest || got["error"] != oauth.ErrInvalidClientMetadata || got["error_description"] == "" {
			t.Errorf("redirect_uris as a string answers %d %v, want 400 invalid_client_metadata", resp.StatusCode, got)
		}
		if n := h.countRows(t, `SELECT count(*) FROM oauth_client WHERE name = $1`, name); n != 0 {
			t.Errorf("a malformed registration made %d clients", n)
		}
	})

	t.Run("a revocation without a token or by a client nobody knows ends nothing", func(t *testing.T) {
		c, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Revoking assistant"})
		access := c.exchange(t, c.allow(t, person, map[string]any{"allow": true}))["access_token"].(string)
		if status, got := c.form(t, "/oauth/revoke", url.Values{}); status != http.StatusBadRequest || got["error"] != oauth.ErrInvalidRequest {
			t.Errorf("a revocation without a token answers %d %v, want 400 invalid_request", status, got)
		}
		if status, got := c.form(t, "/oauth/revoke", url.Values{"token": {access}, "client_id": {"nobody"}}); status != http.StatusUnauthorized ||
			got["error"] != oauth.ErrInvalidClient {
			t.Errorf("a client nobody knows revoking answers %d %v, want 401 invalid_client", status, got)
		}
		rpcResult(t, mcpAs(t, a, access, "tools/list", nil))
	})

	t.Run("an MCP body that is no JSON-RPC request is 400, not 422", func(t *testing.T) {
		script := a.withToken(person.user, victimSecret)
		refuse(t, "a string for a body", script.post(t, "/api/v1/mcp", "tools/list"), http.StatusBadRequest, "bad_request")
		got := want(t, script.post(t, "/api/v1/mcp", map[string]any{"id": 1, "method": "tools/list"}), http.StatusOK, "a request without jsonrpc")
		if e, _ := got.Body["error"].(map[string]any); e == nil || e["code"] != float64(jsonRPCInvalidRequest) {
			t.Errorf("a request without jsonrpc answers %s, want the JSON-RPC invalid request", got.Raw)
		}
	})
}
