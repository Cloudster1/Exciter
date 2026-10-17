//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

// An assistant given Exciter's address alone connects as the official Go SDK does it: it meets the 401, reads the
// metadata, registers, sends its person to consent, trades the code and works as them (#188).
func TestAnAssistantConnectsThroughOAuth(t *testing.T) {
	key := projectKey(t, "OAU")
	h := newHarness(t)
	a := newOAuthServer(t, h)
	person := a.as(t, h.owner(t, "oauth").user)
	want(t, person.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	from := newAddress()
	handler, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			RedirectURIs: []string{callback}, ClientName: "Integration assistant", GrantTypes: []string{"authorization_code", "refresh_token"}}},
		RedirectURL: callback,
		AuthorizationCodeFetcher: func(_ context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			back, err := consent(t, person, args.URL, map[string]any{"allow": true})
			if err != nil {
				return nil, err
			}
			q := back.Query()
			return &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}, nil
		},
		Client: &http.Client{Transport: from},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "exciter-oauth", Version: "1"}, nil).Connect(ctx,
		&mcp.StreamableClientTransport{Endpoint: a.URL + httpapi.APIPrefix + "/mcp", HTTPClient: &http.Client{Transport: from}, OAuthHandler: handler}, nil)
	if err != nil {
		t.Fatalf("the client does not connect: %v", err)
	}
	defer session.Close()
	who, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
	if err != nil || who.IsError {
		t.Fatalf("whoami: %v %+v", err, who)
	}
	token := who.StructuredContent.(map[string]any)["token"].(map[string]any)
	if token["kind"] != "oauth" || token["name"] != "Integration assistant" || token["allProjects"] != true {
		t.Errorf("the assistant is connected as %v", token)
	}
	made, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "create_requirement", Arguments: map[string]any{"projectKey": key, "title": "Written through OAuth"}})
	if err != nil || made.IsError {
		t.Errorf("a full grant cannot write: %v %+v", err, made)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'oauth.granted' AND actor_user_id = $1 AND data->>'client' = 'Integration assistant'`,
		person.user); n != 1 {
		t.Errorf("%d audit entries for the grant, want 1", n)
	}
}

// What a client reads before it is let in: the resource at both addresses, the server, and a 401 that points there.
func TestTheMetadataSaysHowToConnect(t *testing.T) {
	h := newHarness(t)
	a := newOAuthServer(t, h)
	anyone := a.anonymous()
	resource := a.URL + httpapi.APIPrefix + "/mcp"
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/api/v1/mcp"} {
		got := want(t, anyone.get(t, path), http.StatusOK, "read "+path)
		if got.Body["resource"] != resource || got.Body["authorization_servers"].([]any)[0] != a.URL {
			t.Errorf("%s reads %v", path, got.Body)
		}
	}
	server := want(t, anyone.get(t, "/.well-known/oauth-authorization-server"), http.StatusOK, "read the server").Body
	if server["issuer"] != a.URL || server["token_endpoint"] != a.URL+"/api/v1/oauth/token" || server["client_id_metadata_document_supported"] != true ||
		server["authorization_response_iss_parameter_supported"] != true || server["code_challenge_methods_supported"].([]any)[0] != "S256" {
		t.Errorf("the server reads %v", server)
	}
	resp := mcpAs(t, a, "", "tools/list", nil)
	if challenge := resp.Header.Get("WWW-Authenticate"); resp.StatusCode != http.StatusUnauthorized ||
		!strings.Contains(challenge, `resource_metadata="`+a.URL+`/.well-known/oauth-protected-resource/api/v1/mcp"`) || strings.Contains(challenge, "invalid_token") {
		t.Errorf("an anonymous call is told %d %q", resp.StatusCode, challenge)
	}
	if challenge := mcpAs(t, a, "exciter_oat_nothing", "tools/list", nil).Header.Get("WWW-Authenticate"); !strings.Contains(challenge, `error="invalid_token"`) {
		t.Errorf("a refused token is told %q", challenge)
	}
}

// Each refusal is the error the RFCs name, with a sentence; a code or refresh token used twice ends its grant, and
// an access token is for MCP alone.
func TestOAuthRefusesWhatTheRFCsRefuse(t *testing.T) {
	h := newHarness(t)
	a := newOAuthServer(t, h)
	person := a.as(t, h.owner(t, "rfc").user)
	c, resp, _ := register(t, a, newAddress(), nil)
	if resp.StatusCode != http.StatusCreated || c.id == "" || c.secret != "" {
		t.Fatalf("a client keeping no secret registers: %d %q", resp.StatusCode, c.secret)
	}

	t.Run("a client or redirect it does not know is never sent back to", func(t *testing.T) {
		for change, reason := range map[string]string{"client_id": "invalid_client", "redirect_uri": "invalid_redirect"} {
			to := sentTo(t, c.authorizeURL(map[string]string{change: "https://elsewhere.example/" + change}))
			if to.Path != "/oauth/consent" || to.Query().Get("error") != reason {
				t.Errorf("an unknown %s goes to %s", change, to)
			}
		}
	})

	t.Run("anything else goes back with the error, the state and the issuer", func(t *testing.T) {
		for name, tc := range map[string]struct {
			change map[string]string
			code   string
		}{
			"another response type": {map[string]string{"response_type": "token"}, "unsupported_response_type"},
			"no challenge":          {map[string]string{"code_challenge": ""}, "invalid_request"},
			"the plain method":      {map[string]string{"code_challenge_method": "plain"}, "invalid_request"},
			"no state":              {map[string]string{"state": ""}, "invalid_request"},
			"another resource":      {map[string]string{"resource": "https://elsewhere.example/mcp"}, "invalid_target"},
			"another scope":         {map[string]string{"scope": "admin"}, "invalid_scope"},
		} {
			to := sentTo(t, c.authorizeURL(tc.change))
			q := to.Query()
			if to.Host != "127.0.0.1:9" || q.Get("error") != tc.code || q.Get("error_description") == "" || q.Get("iss") != a.URL {
				t.Errorf("%s goes back to %s", name, to)
			}
		}
	})

	t.Run("a code is traded once, with its verifier and redirect", func(t *testing.T) {
		code := c.allow(t, person, map[string]any{"allow": true})
		good := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {callback}, "code_verifier": {c.verifier}}
		for name, change := range map[string][2]string{
			"a wrong verifier":    {"code_verifier", "a-verifier-that-is-long-enough-but-not-the-one-it-was-made-from"},
			"another redirect":    {"redirect_uri", "http://127.0.0.1:9/other"},
			"another resource":    {"resource", "https://elsewhere.example/mcp"},
			"an unknown grant":    {"grant_type", "password"},
			"another client's id": {"client_id", "nobody"},
		} {
			form := url.Values{}
			for k, v := range good {
				form[k] = v
			}
			form.Set(change[0], change[1])
			if status, got := c.token(t, form); status/100 != 4 || got["error"] == nil || got["error_description"] == nil {
				t.Errorf("%s answers %d %v", name, status, got)
			}
		}
		tokens := c.exchange(t, code)
		access := tokens["access_token"].(string)
		if tokens["token_type"] != "Bearer" || tokens["expires_in"] != float64(3600) || tokens["scope"] != "mcp" {
			t.Errorf("the tokens read %v", tokens)
		}
		rpcResult(t, mcpAs(t, a, access, "tools/list", nil))
		if status, got := c.token(t, good); status != http.StatusBadRequest || got["error"] != "invalid_grant" {
			t.Errorf("a code traded twice answers %d %v", status, got)
		}
		if resp := mcpAs(t, a, access, "tools/list", nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("the grant a code traded twice made still works: %d", resp.StatusCode)
		}
	})

	t.Run("a refresh token turns over, and one used twice ends the grant", func(t *testing.T) {
		first := c.exchange(t, c.allow(t, person, map[string]any{"allow": true}))
		refresh := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first["refresh_token"].(string)}}
		status, second := c.token(t, refresh)
		if status != http.StatusOK || second["refresh_token"] == first["refresh_token"] {
			t.Fatalf("a refresh answers %d %v", status, second)
		}
		rpcResult(t, mcpAs(t, a, second["access_token"].(string), "tools/list", nil))
		if status, got := c.token(t, refresh); status != http.StatusBadRequest || got["error"] != "invalid_grant" {
			t.Errorf("a refresh token used twice answers %d %v", status, got)
		}
		if resp := mcpAs(t, a, second["access_token"].(string), "tools/list", nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("the grant a stolen refresh token reached still works: %d", resp.StatusCode)
		}
	})

	t.Run("an access token is for MCP alone, and revoking one ends it", func(t *testing.T) {
		tokens := c.exchange(t, c.allow(t, person, map[string]any{"allow": true}))
		access := tokens["access_token"].(string)
		script := a.withToken(person.user, access)
		outside := script.get(t, "/api/v1/auth/me")
		if refuse(t, "an access token outside MCP", outside, http.StatusUnauthorized, "invalid_token"); !strings.Contains(string(outside.Raw), "MCP alone") {
			t.Errorf("the refusal reads %s", outside.Raw)
		}
		if status, _ := c.form(t, "/oauth/revoke", url.Values{"token": {access}}); status != http.StatusOK {
			t.Errorf("a revocation answers %d", status)
		}
		if resp := mcpAs(t, a, access, "tools/list", nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("a revoked access token still works: %d", resp.StatusCode)
		}
		if status, _ := c.form(t, "/oauth/revoke", url.Values{"token": {"exciter_oat_unknown"}}); status != http.StatusOK {
			t.Errorf("revoking a token nobody knows answers %d", status)
		}
		if status, got := c.form(t, "/oauth/revoke", url.Values{"token": {access}, "client_id": {"nobody"}}); status != http.StatusUnauthorized || got["error"] != "invalid_client" {
			t.Errorf("a client nobody knows revoking answers %d %v", status, got)
		}
		rpcResult(t, mcpAs(t, a, c.exchange(t, c.allow(t, person, map[string]any{"allow": true}))["access_token"].(string), "tools/list", nil))
	})

	t.Run("a client with a secret is held to it", func(t *testing.T) {
		secret, resp, made := register(t, a, newAddress(), map[string]any{"token_endpoint_auth_method": "client_secret_basic"})
		if resp.StatusCode != http.StatusCreated || !strings.HasPrefix(secret.secret, "exciter_ocs_") || made["token_endpoint_auth_method"] != "client_secret_basic" {
			t.Fatalf("a confidential client registers: %d %v", resp.StatusCode, made)
		}
		code := secret.allow(t, person, map[string]any{"allow": true})
		wrong := *secret
		wrong.secret = "exciter_ocs_wrong"
		if status, got := wrong.token(t, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {callback},
			"code_verifier": {secret.verifier}}); status != http.StatusUnauthorized || got["error"] != "invalid_client" {
			t.Errorf("a wrong secret answers %d %v", status, got)
		}
		secret.exchange(t, code)
	})

	t.Run("the consent is a person's at a keyboard, and an address registers ten an hour", func(t *testing.T) {
		_, pat := makeToken(t, person, map[string]any{"name": "script"})
		h.settle(t)
		page := sentTo(t, c.authorizeURL(nil))
		refuse(t, "a token consenting", a.withToken(person.user, pat).get(t, httpapi.APIPrefix+"/oauth/requests/"+page.Query().Get("request")),
			http.StatusForbidden, "session_only")
		from := newAddress()
		for i := range 10 {
			if _, resp, _ := register(t, a, from, nil); resp.StatusCode != http.StatusCreated {
				t.Fatalf("registration %d answers %d", i+1, resp.StatusCode)
			}
		}
		if _, resp, got := register(t, a, from, nil); resp.StatusCode != http.StatusTooManyRequests || got["error"] != "temporarily_unavailable" {
			t.Errorf("the eleventh registration answers %d %v", resp.StatusCode, got)
		}
		if _, resp, got := register(t, a, newAddress(), map[string]any{"redirect_uris": []string{"http://elsewhere.example/cb"}}); resp.StatusCode != http.StatusBadRequest ||
			got["error"] != "invalid_redirect_uri" {
			t.Errorf("a plain http redirect registers: %d %v", resp.StatusCode, got)
		}
	})
}

// A client whose client_id is the address of its metadata document is known by reading it, held to the rules a
// registration is, and refused when the document is not its own.
func TestAClientIsKnownByItsMetadataDocument(t *testing.T) {
	const (
		good     = "https://assistant.example/oauth/client.json"
		elsewise = "https://assistant.example/oauth/other.json"
		secretly = "https://assistant.example/oauth/secret.json"
	)
	doc := func(id, extra string) string {
		return `{"client_id":"` + id + `","client_name":"Document assistant","redirect_uris":["` + callback + `"]` + extra + `}`
	}
	h := newHarness(t)
	a := newOAuthServer(t, h, documents(map[string]string{
		good: doc(good, ""), elsewise: doc(good, ""), secretly: doc(secretly, `,"token_endpoint_auth_method":"client_secret_basic"`),
	}))
	person := a.as(t, h.owner(t, "document").user)
	c := &handClient{a: a, id: good, from: newAddress()}
	page := sentTo(t, c.authorizeURL(nil))
	got := obj(t, want(t, person.get(t, httpapi.APIPrefix+"/oauth/requests/"+page.Query().Get("request")), http.StatusOK, "read the request"), "request")
	if client := got["client"].(map[string]any); client["kind"] != "metadata" || client["name"] != "Document assistant" || client["id"] != good {
		t.Errorf("the consent page shows %v", client)
	}
	c.exchange(t, c.allow(t, person, map[string]any{"allow": true}))
	for _, id := range []string{elsewise, secretly, "https://assistant.example/oauth/missing.json"} {
		other := &handClient{a: a, id: id, from: newAddress()}
		if to := sentTo(t, other.authorizeURL(nil)); to.Query().Get("error") != "invalid_client" {
			t.Errorf("%s goes to %s", id, to)
		}
	}
}
