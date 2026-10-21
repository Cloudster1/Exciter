//go:build integration

package test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/oauth"
)

// narrowAs exchanges an access token of the client's own grant for a narrower one, with the form Rotor sends.
func (c *handClient) narrowAs(t *testing.T, subject string, extra url.Values) (int, map[string]any) {
	t.Helper()
	form := url.Values{"grant_type": {oauth.GrantTokenExchange}, "subject_token": {subject}, "subject_token_type": {oauth.TokenTypeAccess},
		"requested_token_type": {oauth.TokenTypeAccess}, "resource": {c.a.URL + httpapi.APIPrefix + "/mcp"}}
	for k, v := range extra {
		form[k] = v
	}
	return c.token(t, form)
}

// wantRefused holds a token endpoint answer to a refusal with its RFC code and a sentence.
func wantRefused(t *testing.T, what string, status int, got map[string]any, code string) {
	t.Helper()
	description, _ := got["error_description"].(string)
	if status != http.StatusBadRequest || got["error"] != code || !strings.HasSuffix(description, ".") {
		t.Errorf("%s answers %d %v, want 400 %s with a sentence", what, status, got, code)
	}
}

// writes calls create_requirement with a token, and answers whether it wrote.
func writes(t *testing.T, a *apiServer, bearer, key string) bool {
	t.Helper()
	called := rpcResult(t, mcpAs(t, a, bearer, "tools/call", map[string]any{"name": "create_requirement",
		"arguments": map[string]any{"projectKey": key, "title": "Through a narrower token"}}))
	return called["isError"] != true
}

// A client trades an access token of its own grant for a narrower, shorter one for one run, and the grant keeps
// what its person allowed (#235).
func TestANarrowerTokenLeavesItsGrantAsItWas(t *testing.T) {
	inKey, outKey := projectKey(t, "NIN"), projectKey(t, "NOU")
	h := newHarness(t)
	a := newOAuthServer(t, h)
	owner := a.as(t, h.owner(t, "narrow").user)
	for _, key := range []string{inKey, outKey} {
		want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
		want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "In " + key}), http.StatusCreated, "write in "+key)
	}
	c, resp, registered := register(t, a, newAddress(), map[string]any{
		"grant_types": []string{oauth.GrantAuthorizationCode, oauth.GrantRefreshToken, oauth.GrantTokenExchange}})
	if resp.StatusCode != http.StatusCreated || !slices.Contains(registered["grant_types"].([]any), any(oauth.GrantTokenExchange)) {
		t.Fatalf("a client registering for the exchange answers %d %v", resp.StatusCode, registered)
	}
	h.settle(t)
	tokens := c.exchange(t, c.allow(t, owner, map[string]any{"allow": true}))
	subject, refresh := tokens["access_token"].(string), tokens["refresh_token"].(string)

	t.Run("a token narrowed to reading writes nothing, and the grant still writes", func(t *testing.T) {
		status, got := c.narrowAs(t, subject, url.Values{"scope": {oauth.ScopeMCPRead}, oauth.ParamExpiresIn: {"900"}})
		if status != http.StatusOK || got["scope"] != oauth.ScopeMCPRead || got["issued_token_type"] != oauth.TokenTypeAccess ||
			got["expires_in"] != float64(900) || got["token_type"] != "Bearer" {
			t.Fatalf("the exchange answers %d %v", status, got)
		}
		if _, ok := got["refresh_token"]; ok {
			t.Errorf("an exchange issued a refresh token: %v", got)
		}
		read := got["access_token"].(string)
		if !strings.HasPrefix(read, oauth.AccessTokenPrefix) || read == subject {
			t.Fatalf("the narrower token is %q", read)
		}
		if writes(t, a, read, inKey) {
			t.Error("a token narrowed to reading wrote")
		}
		if !writes(t, a, subject, inKey) {
			t.Error("the grant no longer writes after an exchange narrowed a token of it")
		}
		status, got = c.narrowAs(t, read, nil)
		wantRefused(t, "a narrower token as the subject", status, got, oauth.ErrInvalidGrant)
	})

	t.Run("an exchange never widens what the grant holds", func(t *testing.T) {
		status, got := c.narrowAs(t, subject, nil)
		if left := got["expires_in"].(float64); status != http.StatusOK || got["scope"] != oauth.ScopeMCP || left > 3600 || left < 3000 {
			t.Errorf("an exchange asking nothing answers %d %v", status, got)
		}
		status, got = c.narrowAs(t, subject, url.Values{oauth.ParamExpiresIn: {"3601"}})
		wantRefused(t, "a lifetime past an hour", status, got, oauth.ErrInvalidRequest)
		status, got = c.narrowAs(t, subject, url.Values{"scope": {oauth.ScopeEvidence}})
		wantRefused(t, "evidence from a grant that is not", status, got, oauth.ErrInvalidScope)
		status, got = c.narrowAs(t, subject, url.Values{"resource": {a.URL + httpapi.APIPrefix}})
		wantRefused(t, "another resource", status, got, oauth.ErrInvalidTarget)

		reader, _, _ := register(t, a, newAddress(), nil)
		held := reader.exchange(t, reader.allow(t, owner, map[string]any{"allow": true, "readOnly": true}))["access_token"].(string)
		status, got = reader.narrowAs(t, held, url.Values{"scope": {oauth.ScopeMCP}})
		wantRefused(t, "writing from a grant that reads", status, got, oauth.ErrInvalidScope)
		status, got = reader.narrowAs(t, subject, nil)
		wantRefused(t, "another client's token as the subject", status, got, oauth.ErrInvalidGrant)
	})

	t.Run("a narrower token reaches only the projects it names", func(t *testing.T) {
		status, got := c.narrowAs(t, subject, url.Values{oauth.ParamProjects: {strings.ToLower(inKey)}})
		if status != http.StatusOK {
			t.Fatalf("an exchange to one project answers %d %v", status, got)
		}
		limited := got["access_token"].(string)
		listed := rpcResult(t, mcpAs(t, a, limited, "tools/call", map[string]any{"name": "list_projects", "arguments": map[string]any{}}))
		var keys []string
		for _, p := range listed["structuredContent"].(map[string]any)["projects"].([]any) {
			keys = append(keys, p.(map[string]any)["key"].(string))
		}
		if len(keys) != 1 || keys[0] != inKey {
			t.Errorf("the narrower token lists %v", keys)
		}
		if got := rpcResult(t, mcpAs(t, a, limited, "tools/call", map[string]any{"name": "get_requirement",
			"arguments": map[string]any{"reqKey": outKey + "-R1"}})); got["isError"] != true {
			t.Errorf("the narrower token reads outside its project: %v", got)
		}
		status, got = c.narrowAs(t, subject, url.Values{oauth.ParamProjects: {"NOSUCH" + strings.ToUpper(uuid.NewString()[:4])}})
		wantRefused(t, "a project nobody has", status, got, oauth.ErrInvalidScope)
	})

	t.Run("straight through SQL as exciter_app, a narrower token's projects are out of reach", func(t *testing.T) {
		mine := h.appConn(t, owner.user)
		refusedOutright(t, mine, "reading oauth_token_project", `SELECT count(*) FROM oauth_token_project`)
		refusedOutright(t, mine, "adding a project to a narrower token", `INSERT INTO oauth_token_project (token_hash, project_id)
			SELECT token_hash, $1 FROM oauth_token LIMIT 1`, uuid.New())
	})

	t.Run("revoking a narrower token leaves the grant, and ending the grant ends them", func(t *testing.T) {
		_, first := c.narrowAs(t, subject, nil)
		_, second := c.narrowAs(t, subject, nil)
		if status, _ := c.form(t, "/oauth/revoke", url.Values{"token": {first["access_token"].(string)}}); status != http.StatusOK {
			t.Fatalf("revoking a narrower token answers %d", status)
		}
		if resp := mcpAs(t, a, first["access_token"].(string), "tools/list", nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("a revoked narrower token answers %d", resp.StatusCode)
		}
		rpcResult(t, mcpAs(t, a, subject, "tools/list", nil))
		rpcResult(t, mcpAs(t, a, second["access_token"].(string), "tools/list", nil))
		if status, _ := c.form(t, "/oauth/revoke", url.Values{"token": {refresh}}); status != http.StatusOK {
			t.Fatalf("revoking the grant's refresh token answers %d", status)
		}
		if resp := mcpAs(t, a, second["access_token"].(string), "tools/list", nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("a narrower token outlives its grant: %d", resp.StatusCode)
		}
	})
}
