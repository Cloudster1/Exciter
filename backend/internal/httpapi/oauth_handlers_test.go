package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A client's form reaches the token endpoint whatever cookie the browser adds, since the endpoint never reads the
// session; anywhere else the same form is still refused.
func TestTheClientEndpointsAreLeftOutOfTheSameSiteCheck(t *testing.T) {
	h := newServer(t).Routes(nil)
	for _, path := range []string{"/oauth/token", "/oauth/revoke", "/oauth/register"} {
		req := httptest.NewRequest(http.MethodPost, APIPrefix+path, strings.NewReader("grant_type=refresh_token"))
		req.AddCookie(&http.Cookie{Name: "exciter_session", Value: "whatever"})
		req.Header.Set("Origin", "http://evil.test")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		// The server is built without OAuth, so getting past the edge is answered by oauthOn.
		if resp, body := serve(t, h, req); resp.StatusCode != http.StatusServiceUnavailable || errorOf(t, body)["code"] != "oauth_unavailable" {
			t.Errorf("%s answers %d %v", path, resp.StatusCode, body)
		}
	}
	if !isClientEndpoint(APIPrefix+"/oauth/token/") || isClientEndpoint(APIPrefix+"/oauth/requests/x") || isClientEndpoint(APIPrefix+"/tokens") {
		t.Error("the client endpoints are told apart wrongly")
	}
}

// Without OAuth the endpoint's 401 points nowhere, as it does while OAuth connections are off.
func TestAnMCPRefusalNamesNoMetadataWithoutOAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := serve(t, newServer(t).Routes(nil), req)
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") != "" {
		t.Errorf("an anonymous call answers %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
}
