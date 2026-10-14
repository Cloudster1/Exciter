//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// Where the suite reaches the stack's Keycloak, and as whom it administers the realm.
const (
	envKeycloakURL               = "EXCITER_TEST_KEYCLOAK_URL"
	envKeycloakAdminPassword     = "EXCITER_TEST_KEYCLOAK_ADMIN_PASSWORD"
	defaultKeycloakAdminPassword = "admin"
	realm                        = "suite-dev"
	exciterClient                = "exciter"
	exciterSecret                = "exciter-dev-secret"
)

// The development realm's people, as deploy/keycloak/realm.json has them.
const (
	alicePassword = "alice password"
	bobPassword   = "bob password"
	carolPassword = "carol password"
	davePassword  = "dave password"
	aliceEmail    = "alice@suite.test"
	bobEmail      = "bob@suite.test"
	carolEmail    = "carol@suite.test"
	daveEmail     = "dave@suite.test"
	// appHost is where the API sends the browser after a sign-in; the browser
	// stops there, since no web client is running in the suite.
	appHost = "app.test"
)

const (
	browserTimeout = 30 * time.Second
	dialTimeout    = 5 * time.Second
	testSessionTTL = time.Hour
)

// forgetPerson removes an account a test makes, by address, before and after,
// so a run against a kept stack starts from nothing.
func (h *harness) forgetPerson(t *testing.T, email string) {
	t.Helper()
	clean := func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, email) }
	clean()
	t.Cleanup(clean)
}

// letIn makes the address a member of org with role ahead of its sign-in, the way the seed does, making the account
// if need be; somebody the seed already let in gets the role instead.
func (h *harness) letIn(t *testing.T, org tenant.Org, email, role string) {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	if err := h.super.QueryRow(ctx, `
		INSERT INTO app_user (email, name) VALUES ($1, $2)
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email RETURNING id`, email, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `
		INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET org_role = EXCLUDED.org_role, role_source = 'manual'`, org.ID, id, role); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
}

func (h *harness) userID(t *testing.T, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM app_user WHERE email = $1`, email).Scan(&id); err != nil {
		t.Fatalf("no account for %s: %v", email, err)
	}
	return id
}

// configureProvider points an organization at the realm through the same service the settings page uses, in
// the name of one of its administrators, since only an administrator configures a provider.
func configureProvider(t *testing.T, a *apiServer, org tenant.Org, issuer string, createGroups bool) {
	t.Helper()
	admin := a.h.addPerson(t, org.ID, "admin")
	ctx := withActor(org, admin)
	if _, _, err := a.sso.Save(ctx, oidc.Provider{Issuer: issuer, ClientID: exciterClient, ClientSecret: exciterSecret, CreateGroups: createGroups, Enabled: true}); err != nil {
		t.Fatalf("configure the provider: %v", err)
	}
	a.h.settle(t)
}

// withActor is a context acting for somebody in org, as a request of theirs would be.
func withActor(org tenant.Org, user uuid.UUID) context.Context {
	return db.WithUser(tenant.WithOrg(context.Background(), org), user)
}

// adminOf is a password session of a fresh bootstrap owner of org, and the owner's id.
func adminOf(t *testing.T, h *harness, a *apiServer, org tenant.Org) (*client, uuid.UUID) {
	t.Helper()
	id := h.userID(t, bootstrapAdmin(t, h, a, org.ID))
	return a.as(t, id, org.ID), id
}

// browser is a cookie-keeping client that follows redirects until the API hands it to the web client, and
// remembers every callback it was sent to.
type browser struct {
	*http.Client
	api       *apiServer
	callbacks []string
}

func newBrowser(t *testing.T, a *apiServer) *browser {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	b := &browser{api: a}
	b.Client = &http.Client{Jar: jar, Timeout: browserTimeout, CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		if strings.HasSuffix(req.URL.Path, "/auth/oidc/callback") {
			b.callbacks = append(b.callbacks, req.URL.String())
		}
		if req.URL.Host == appHost {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	return b
}

func (b *browser) get(t *testing.T, target string) (*http.Response, string) {
	t.Helper()
	resp, err := b.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func (b *browser) send(t *testing.T, method, target string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := b.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

// postJSON sends a JSON body the way the web client's fetch does, on the browser's cookies.
func (b *browser) postJSON(t *testing.T, path string, body any) (*http.Response, string) {
	t.Helper()
	encoded, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, b.api.URL+path, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, string(out)
}

var loginFormAction = regexp.MustCompile(`id="kc-form-login"[^>]*action="([^"]+)"`)

// signIn goes the whole way a person does, authorization code with PKCE through Keycloak's own login form, and
// returns where the app was told to land; the replica then holds what the sign-in wrote.
func (b *browser) signIn(t *testing.T, username, password, next string) string {
	t.Helper()
	start := b.api.URL + httpapi.APIPrefix + "/auth/oidc/start"
	if next != "" {
		start += "?next=" + url.QueryEscape(next)
	}
	resp, page := b.get(t, start)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the start did not reach a login form: %s at %s\n%.500s", resp.Status, resp.Request.URL, page)
	}
	if q := resp.Request.URL.Query(); q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("nonce") == "" || q.Get("state") == "" {
		t.Errorf("the provider was asked without state, nonce or PKCE: %s", resp.Request.URL)
	}
	match := loginFormAction.FindStringSubmatch(page)
	if match == nil {
		t.Fatalf("no login form on %s:\n%.1000s", resp.Request.URL, page)
	}
	form := url.Values{"username": {username}, "password": {password}, "credentialId": {""}}
	resp, err := b.PostForm(html.UnescapeString(match[1]), form)
	if err != nil {
		t.Fatalf("submit the login form: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("the login form answered %s at %s, want a redirect into the app:\n%.1000s", resp.Status, resp.Request.URL, body)
	}
	b.api.h.settle(t)
	return resp.Header.Get("Location")
}

// me asks the API who the browser is signed in as.
func (b *browser) me(t *testing.T) (int, map[string]any) {
	t.Helper()
	resp, body := b.get(t, b.api.URL+httpapi.APIPrefix+"/auth/me")
	var out map[string]any
	_ = json.Unmarshal([]byte(body), &out)
	return resp.StatusCode, out
}

// sessionCookie is the session cookie the browser holds for the API.
func (b *browser) sessionCookie(t *testing.T) string {
	t.Helper()
	u, _ := url.Parse(b.api.URL)
	for _, c := range b.Jar.Cookies(u) {
		if c.Name == b.api.h.cfg.Auth.SessionCookie {
			return c.Value
		}
	}
	return ""
}

// roleIn is the role the browser's session acts with.
func (b *browser) roleIn(t *testing.T) string {
	t.Helper()
	status, me := b.me(t)
	if status != http.StatusOK {
		t.Fatalf("/auth/me = %d %v", status, me)
	}
	role, _ := me["role"].(string)
	return role
}

// signedIn signs somebody in through Keycloak in a browser of their own,
// which a previous sign-in's provider session would otherwise skip the form of.
func signedIn(t *testing.T, a *apiServer, username, password string) *browser {
	t.Helper()
	b := newBrowser(t, a)
	if landing := b.signIn(t, username, password, "/"); landing != "http://"+appHost+"/" {
		t.Fatalf("%s landed on %q", username, landing)
	}
	return b
}

// keycloak administers the test realm through Keycloak's admin API.
type keycloak struct {
	base, realm, password string
	client                *http.Client
}

func newKeycloak(t *testing.T) *keycloak {
	t.Helper()
	base := os.Getenv(envKeycloakURL)
	if base == "" {
		t.Fatalf("%s is not set; run the suite with make test-integration, which reaches the stack's Keycloak", envKeycloakURL)
	}
	password := os.Getenv(envKeycloakAdminPassword)
	if password == "" {
		password = defaultKeycloakAdminPassword
	}
	k := &keycloak{base: strings.TrimSuffix(base, "/"), realm: realm, password: password, client: &http.Client{Timeout: browserTimeout}}
	k.reachable(t)
	return k
}

func (k *keycloak) issuer() string { return k.base + "/realms/" + k.realm }

// reachable fails with a sentence rather than a timeout when the stack's Keycloak is not there.
func (k *keycloak) reachable(t *testing.T) {
	t.Helper()
	u, _ := url.Parse(k.base)
	conn, err := net.DialTimeout("tcp", u.Host, dialTimeout)
	if err != nil {
		t.Fatalf("Keycloak at %s is not reachable; start the stack with make stack-up: %v", k.base, err)
	}
	conn.Close()
}

func (k *keycloak) token(t *testing.T) string {
	t.Helper()
	resp, err := k.client.PostForm(k.base+"/realms/master/protocol/openid-connect/token", url.Values{
		"grant_type": {"password"}, "client_id": {"admin-cli"}, "username": {"admin"}, "password": {k.password},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.AccessToken == "" {
		t.Fatalf("no admin token from Keycloak (%s): %v", resp.Status, err)
	}
	return out.AccessToken
}

func (k *keycloak) send(t *testing.T, method, path string, body, into any) {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = bytes.NewReader(encoded)
	}
	req, _ := http.NewRequest(method, k.base+"/admin/realms/"+k.realm+path, payload)
	req.Header.Set("Authorization", "Bearer "+k.token(t))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := k.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s = %s: %s", method, path, resp.Status, body)
	}
	if into != nil {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			t.Fatal(err)
		}
	}
}

func (k *keycloak) idOf(t *testing.T, path string) string {
	t.Helper()
	var found []struct {
		ID string `json:"id"`
	}
	k.send(t, http.MethodGet, path, nil, &found)
	if len(found) != 1 {
		t.Fatalf("%s found %d, want exactly one", path, len(found))
	}
	return found[0].ID
}

// setMembership adds or removes a realm user from a realm group.
func (k *keycloak) setMembership(t *testing.T, username, group string, member bool) {
	t.Helper()
	user := k.idOf(t, "/users?exact=true&username="+url.QueryEscape(username))
	grp := k.idOf(t, "/groups?exact=true&search="+url.QueryEscape(group))
	method := http.MethodDelete
	if member {
		method = http.MethodPut
	}
	k.send(t, method, "/users/"+user+"/groups/"+grp, nil, nil)
}

// newGroup adds a group to the realm that no installation has heard of, removed again when the test ends.
func (k *keycloak) newGroup(t *testing.T, name string) {
	t.Helper()
	k.send(t, http.MethodPost, "/groups", map[string]any{"name": name}, nil)
	t.Cleanup(func() {
		k.send(t, http.MethodDelete, "/groups/"+k.idOf(t, "/groups?exact=true&search="+url.QueryEscape(name)), nil, nil)
	})
}

// newPerson adds somebody to the realm whom Exciter has never seen, in the groups named, removed again here and
// in Keycloak when the test ends. It returns their address.
func (k *keycloak) newPerson(t *testing.T, h *harness, username, password string, groups ...string) string {
	t.Helper()
	email := username + "@suite.test"
	k.send(t, http.MethodPost, "/users", map[string]any{
		"username": username, "email": email, "firstName": "New", "lastName": "Person",
		"enabled": true, "emailVerified": true, "groups": groups,
		"credentials": []map[string]any{{"type": "password", "value": password, "temporary": false}},
	}, nil)
	h.forgetPerson(t, email)
	t.Cleanup(func() {
		k.send(t, http.MethodDelete, "/users/"+k.idOf(t, "/users?exact=true&username="+url.QueryEscape(username)), nil, nil)
	})
	return email
}
