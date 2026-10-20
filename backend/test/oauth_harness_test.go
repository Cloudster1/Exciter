//go:build integration

package test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/oauth"
)

// newOAuthServer is named by its own address, as EXCITER_APP_URL names a real one, so a client's discovery, issuer
// and resource meet; the loopback is its nginx, so each test registers from an address of its own.
func newOAuthServer(t *testing.T, h *harness, fetch ...oauth.Fetcher) *apiServer {
	t.Helper()
	return newAPIServerWith(t, h, func(a *apiServer, s *httpapi.Server) {
		s.AppBaseURL = a.URL
		s.OAuth = oauth.NewService(h.cluster, a.URL, httpapi.APIPrefix, append(fetch, nil)[0]).WithPeople(a.sso)
		s.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}
	})
}

// fromAddress sends every request as if from one address of the test's own, as nginx names the client.
type fromAddress struct {
	ip   string
	next http.RoundTripper
}

func newAddress() fromAddress {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return fromAddress{ip: fmt.Sprintf("10.%d.%d.%d", b[0], b[1], b[2]), next: http.DefaultTransport}
}

func (f fromAddress) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Real-IP", f.ip)
	return f.next.RoundTrip(r)
}

// noFollow is a browser that stops at the first redirect, so the test reads where it would have gone.
var noFollow = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

const callback = "http://127.0.0.1:9/callback"

// handClient is a client the test drives by hand: registered, with a verifier for its next authorization.
type handClient struct {
	a        *apiServer
	id       string
	secret   string
	from     fromAddress
	verifier string
}

// register registers a client; extra fields go into its metadata.
func register(t *testing.T, a *apiServer, from fromAddress, extra map[string]any) (*handClient, *http.Response, map[string]any) {
	t.Helper()
	body := map[string]any{"redirect_uris": []string{callback}, "client_name": "Hand-made assistant"}
	for k, v := range extra {
		body[k] = v
	}
	encoded, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, a.URL+httpapi.APIPrefix+"/oauth/register", strings.NewReader(string(encoded)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Transport: from}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var answer map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&answer)
	id, _ := answer["client_id"].(string)
	secret, _ := answer["client_secret"].(string)
	return &handClient{a: a, id: id, secret: secret, from: from}, resp, answer
}

// authorizeURL is an authorization as the client would send the browser, with what the test changes.
func (c *handClient) authorizeURL(change map[string]string) string {
	c.verifier = oauth2.GenerateVerifier()
	q := url.Values{
		"response_type": {"code"}, "client_id": {c.id}, "redirect_uri": {callback}, "state": {"the-state"},
		"code_challenge": {oauth2.S256ChallengeFromVerifier(c.verifier)}, "code_challenge_method": {"S256"},
		"resource": {c.a.URL + httpapi.APIPrefix + "/mcp"}, "scope": {"mcp"},
	}
	for k, v := range change {
		if v == "" {
			q.Del(k)
		} else {
			q.Set(k, v)
		}
	}
	return c.a.URL + httpapi.APIPrefix + "/oauth/authorize?" + q.Encode()
}

// sentTo is where the browser goes from an address.
func sentTo(t *testing.T, address string) *url.URL {
	t.Helper()
	resp, err := noFollow.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("%s answered %d, want a redirect", address, resp.StatusCode)
	}
	to, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return to
}

// consent follows an authorization as the person's browser would, to the consent page, and answers it there.
func consent(t *testing.T, person *client, address string, answer map[string]any) (*url.URL, error) {
	resp, err := noFollow.Get(address)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	page, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || page.Path != "/oauth/consent" || page.Query().Get("request") == "" {
		return nil, fmt.Errorf("the authorization went to %q, not to the consent page", resp.Header.Get("Location"))
	}
	path := httpapi.APIPrefix + "/oauth/requests/" + page.Query().Get("request")
	if got := person.get(t, path); got.Status != http.StatusOK {
		return nil, fmt.Errorf("the consent page reads %d: %s", got.Status, got.Raw)
	}
	got := person.post(t, path, answer)
	if got.Status != http.StatusOK {
		return nil, fmt.Errorf("the answer is %d: %s", got.Status, got.Raw)
	}
	return url.Parse(got.Body["redirectTo"].(string))
}

// allow connects a hand-made client as the person, and answers the code it is sent back with.
func (c *handClient) allow(t *testing.T, person *client, answer map[string]any) string {
	t.Helper()
	back, err := consent(t, person, c.authorizeURL(nil), answer)
	if err != nil {
		t.Fatal(err)
	}
	if back.Query().Get("state") != "the-state" || back.Query().Get("iss") != c.a.URL || back.Query().Get("code") == "" {
		t.Fatalf("the browser goes back to %s", back)
	}
	return back.Query().Get("code")
}

// token posts a form to the token endpoint as the client, and answers the status and the answer.
func (c *handClient) token(t *testing.T, form url.Values) (int, map[string]any) {
	t.Helper()
	return c.form(t, "/oauth/token", form)
}

func (c *handClient) form(t *testing.T, path string, form url.Values) (int, map[string]any) {
	t.Helper()
	if form.Get("client_id") == "" && c.secret == "" {
		form.Set("client_id", c.id)
	}
	req, _ := http.NewRequest(http.MethodPost, c.a.URL+httpapi.APIPrefix+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.secret != "" {
		req.SetBasicAuth(url.QueryEscape(c.id), url.QueryEscape(c.secret))
	}
	resp, err := (&http.Client{Transport: c.from}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var answer map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&answer)
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("%s answered without Cache-Control: no-store", path)
	}
	return resp.StatusCode, answer
}

// exchange trades a code for tokens, as a client that knows its verifier.
func (c *handClient) exchange(t *testing.T, code string) map[string]any {
	t.Helper()
	status, got := c.token(t, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {callback},
		"code_verifier": {c.verifier}, "resource": {c.a.URL + httpapi.APIPrefix + "/mcp"}})
	if status != http.StatusOK || !strings.HasPrefix(got["access_token"].(string), oauth.AccessTokenPrefix) {
		t.Fatalf("the code was not traded: %d %v", status, got)
	}
	return got
}

// mcpAs calls the MCP endpoint with a bearer, and answers the HTTP response.
func mcpAs(t *testing.T, a *apiServer, bearer, method string, params map[string]any) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, _ := http.NewRequest(http.MethodPost, a.URL+httpapi.APIPrefix+"/mcp", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// rpcResult reads the result of an MCP call that was let in.
func rpcResult(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("MCP answered %d: %s", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	var answer struct {
		Result map[string]any `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil || answer.Result == nil {
		t.Fatalf("MCP answered no result: %v", err)
	}
	return answer.Result
}

// keepOAuthConnections switches OAuth connections back to how they were once the test ends.
func (h *harness) keepOAuthConnections(t *testing.T) {
	t.Helper()
	var on bool
	if err := h.super.QueryRow(context.Background(), `SELECT oauth_connections FROM installation`).Scan(&on); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.cleanupExec(t, h.super, `UPDATE installation SET oauth_connections = $1`, on) })
}

// documents stands in for the web a metadata document is fetched from: address to body, kept for a minute.
func documents(served map[string]string) oauth.Fetcher {
	return func(_ context.Context, address string) ([]byte, time.Duration, error) {
		body, ok := served[address]
		if !ok {
			return nil, 0, fmt.Errorf("the document answered 404 Not Found")
		}
		return []byte(body), time.Minute, nil
	}
}
