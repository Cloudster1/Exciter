//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/oauth"
)

// The realm's partner clients, whose audience mapper puts Exciter's client into their access tokens.
const (
	statorClient   = "stator"
	statorSecret   = "stator-dev-secret"
	armatureClient = "armature"
)

// realmToken is a person's access token from the realm, issued to a client as its sign-in would get one.
func (k *keycloak) realmToken(t *testing.T, client, secret, username, password string) string {
	t.Helper()
	resp, err := k.client.PostForm(k.issuer()+"/protocol/openid-connect/token", url.Values{
		"grant_type": {"password"}, "client_id": {client}, "client_secret": {secret},
		"username": {username}, "password": {password}, "scope": {"openid"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.AccessToken == "" {
		t.Fatalf("no access token for %s from %s (%s): %v", username, client, resp.Status, err)
	}
	return out.AccessToken
}

// partnerApp is an app an administrator registered, with the secret it was shown once.
type partnerApp struct {
	id, secret string
}

func registerPartner(t *testing.T, admin *client, body map[string]any) partnerApp {
	t.Helper()
	got := obj(t, want(t, admin.post(t, "/api/v1/oauth/clients", body), http.StatusCreated, "register a partner app"), "client")
	app := partnerApp{id: got["id"].(string), secret: got["secret"].(string)}
	t.Cleanup(func() {
		admin.api.h.cleanupExec(t, admin.api.h.super, `DELETE FROM oauth_client WHERE id = $1`, app.id)
	})
	return app
}

// token posts to the token endpoint as the app, with HTTP Basic, form-encoded first.
func (p partnerApp) token(t *testing.T, a *apiServer, form url.Values) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, a.URL+httpapi.APIPrefix+"/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(p.id), url.QueryEscape(p.secret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	a.h.settle(t)
	return resp.StatusCode, out
}

func (p partnerApp) exchange(t *testing.T, a *apiServer, subject string, extra ...string) (int, map[string]any) {
	t.Helper()
	form := url.Values{"grant_type": {oauth.GrantTokenExchange}, "subject_token": {subject}, "subject_token_type": {oauth.TokenTypeAccess}}
	for i := 0; i+1 < len(extra); i += 2 {
		form.Set(extra[i], extra[i+1])
	}
	return p.token(t, a, form)
}

// bearerGet reads an API path with an access token, as the partner would.
func bearerGet(t *testing.T, a *apiServer, bearer, path string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, a.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// A partner app trades a person's token from the shared realm for a grant here, with no consent page: the person is
// matched by the token, the grant reads no more than the app may and the person can, it refreshes and rotates, it
// shows among their connected apps and ends there (#273).
func TestAPartnerAppExchangesAPersonsTokenFromTheRealm(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, a, kc.issuer(), true)
	// An administrator, so what the token may not do is the grant's doing rather than her role's.
	h.letIn(t, aliceEmail, "admin")
	boss := a.as(t, h.owner(t, "partners").user)
	stator := registerPartner(t, boss, map[string]any{"name": "Stator", "clientUri": "https://stator.example.com", "keycloakClientId": statorClient})
	subject := kc.realmToken(t, statorClient, statorSecret, "alice", alicePassword)

	status, got := stator.exchange(t, a, subject)
	if status != http.StatusOK || !strings.HasPrefix(got["access_token"].(string), oauth.AccessTokenPrefix) ||
		!strings.HasPrefix(got["refresh_token"].(string), oauth.RefreshTokenPrefix) || got["scope"] != "read" ||
		got["issued_token_type"] != oauth.TokenTypeAccess || got["token_type"] != "Bearer" {
		t.Fatalf("the exchange answered %d %v", status, got)
	}
	access := got["access_token"].(string)

	t.Run("the token is alice on the whole API, reading only", func(t *testing.T) {
		key := projectKey(t, "PXR")
		alice := a.withToken(h.userID(t, aliceEmail), access)
		if me := obj(t, want(t, alice.get(t, "/api/v1/auth/me"), http.StatusOK, "who the token is"), "user"); me["email"] != aliceEmail {
			t.Errorf("the token is %v, want alice", me)
		}
		want(t, alice.get(t, "/api/v1/projects"), http.StatusOK, "list projects with it")
		if r := alice.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Not the token's to make"}); r.Status != http.StatusForbidden {
			t.Errorf("a reading partner's token made a project: %d %s", r.Status, r.Raw)
		}
		refuse(t, "a partner's token managing partner apps", alice.get(t, "/api/v1/oauth/clients"), http.StatusForbidden, "forbidden")
	})

	t.Run("it refreshes, rotating, and a rotated refresh token ends the grant", func(t *testing.T) {
		first := got["refresh_token"].(string)
		status, turned := stator.token(t, a, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first}})
		if status != http.StatusOK || turned["scope"] != "read" || turned["refresh_token"] == first {
			t.Fatalf("the refresh answered %d %v", status, turned)
		}
		if code := bearerGet(t, a, turned["access_token"].(string), "/api/v1/projects"); code != http.StatusOK {
			t.Errorf("the refreshed token reads %d", code)
		}
		if status, again := stator.token(t, a, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first}}); status != http.StatusBadRequest || again["error"] != oauth.ErrInvalidGrant {
			t.Errorf("a rotated refresh token answered %d %v", status, again)
		}
		if code := bearerGet(t, a, turned["access_token"].(string), "/api/v1/projects"); code != http.StatusUnauthorized {
			t.Errorf("after a replay the grant still reads: %d", code)
		}
	})

	t.Run("signing in again replaces the tokens of the one grant", func(t *testing.T) {
		_, one := stator.exchange(t, a, subject)
		_, two := stator.exchange(t, a, subject)
		if code := bearerGet(t, a, one["access_token"].(string), "/api/v1/projects"); code != http.StatusUnauthorized {
			t.Errorf("the earlier exchange's token still reads: %d", code)
		}
		if code := bearerGet(t, a, two["access_token"].(string), "/api/v1/projects"); code != http.StatusOK {
			t.Errorf("the latest exchange's token reads %d", code)
		}
		if n := h.countRows(t, `SELECT count(*) FROM oauth_grant WHERE client_id = $1`, stator.id); n != 1 {
			t.Errorf("alice holds %d grants of the app, want one", n)
		}
	})

	t.Run("it is among alice's connected apps, and ends there", func(t *testing.T) {
		alice := a.as(t, h.userID(t, aliceEmail))
		g, ok := grantsIn(t, want(t, alice.get(t, "/api/v1/oauth/grants"), http.StatusOK, "alice's apps"))["Stator"]
		if !ok || g["client"].(map[string]any)["kind"] != string(oauth.ClientPartner) || g["scopes"].([]any)[0] != "read" {
			t.Fatalf("alice's apps read %v", g)
		}
		_, live := stator.exchange(t, a, subject)
		want(t, alice.delete(t, "/api/v1/oauth/grants/"+g["id"].(string)), http.StatusNoContent, "disconnect Stator")
		if code := bearerGet(t, a, live["access_token"].(string), "/api/v1/projects"); code != http.StatusUnauthorized {
			t.Errorf("a disconnected app's token reads %d", code)
		}
		for _, each := range list(t, want(t, boss.get(t, "/api/v1/oauth/clients"), http.StatusOK, "list the apps"), "clients") {
			if c := each.(map[string]any); c["id"] == stator.id && c["grants"] != float64(0) {
				t.Errorf("after alice disconnected it the app holds %v grants", c["grants"])
			}
		}
	})

	t.Run("write, once the app may, and narrowed at once when it may no longer", func(t *testing.T) {
		want(t, boss.patch(t, "/api/v1/oauth/clients/"+stator.id, map[string]any{"scopes": []string{"read", "write"}}), http.StatusOK, "let Stator write")
		status, wide := stator.exchange(t, a, subject)
		if status != http.StatusOK || wide["scope"] != "read write" {
			t.Fatalf("an exchange of a writing app answered %d %v", status, wide)
		}
		if status, narrowed := stator.exchange(t, a, subject, "scope", "read"); status != http.StatusOK || narrowed["scope"] != "read" {
			t.Errorf("asking for read alone answered %d %v", status, narrowed)
		}
		_, wide = stator.exchange(t, a, subject)
		alice := a.withToken(h.userID(t, aliceEmail), wide["access_token"].(string))
		want(t, alice.post(t, "/api/v1/projects", map[string]any{"key": projectKey(t, "PXW"), "name": "Made through Stator"}), http.StatusCreated, "a writing token makes a project")
		want(t, boss.patch(t, "/api/v1/oauth/clients/"+stator.id, map[string]any{"scopes": []string{"read"}}), http.StatusOK, "take write away")
		if r := alice.post(t, "/api/v1/projects", map[string]any{"key": projectKey(t, "PXN"), "name": "Not any more"}); r.Status != http.StatusForbidden {
			t.Errorf("after write was taken away the token still writes: %d %s", r.Status, r.Raw)
		}
	})
}

// What a partner app may not exchange is refused in OAuth's words, and the token's person is never let in by a
// token the realm did not issue to the app for Exciter (#273).
func TestAnExchangeIsRefusedWhatTheRealmDidNotVouchFor(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, a, kc.issuer(), true)
	h.letIn(t, aliceEmail, "member")
	boss := a.as(t, h.owner(t, "refusals").user)
	stator := registerPartner(t, boss, map[string]any{"name": "Stator", "keycloakClientId": statorClient})
	other := registerPartner(t, boss, map[string]any{"name": "Armature", "keycloakClientId": armatureClient})
	unnamed := registerPartner(t, boss, map[string]any{"name": "Unnamed"})
	itself := registerPartner(t, boss, map[string]any{"name": "Exciter itself", "keycloakClientId": exciterClient})
	subject := kc.realmToken(t, statorClient, statorSecret, "alice", alicePassword)

	for _, c := range []struct {
		what   string
		status int
		code   string
		send   func() (int, map[string]any)
	}{
		{"a token issued to another client", http.StatusBadRequest, oauth.ErrInvalidGrant, func() (int, map[string]any) { return other.exchange(t, a, subject) }},
		{"a token whose audience lacks Exciter", http.StatusBadRequest, oauth.ErrInvalidGrant, func() (int, map[string]any) {
			return itself.exchange(t, a, kc.realmToken(t, exciterClient, exciterSecret, "alice", alicePassword))
		}},
		{"a token the realm never signed", http.StatusBadRequest, oauth.ErrInvalidGrant, func() (int, map[string]any) {
			return stator.exchange(t, a, "eyJhbGciOiJub25lIn0.eyJzdWIiOiJhbGljZSJ9.")
		}},
		{"an app without a Keycloak client", http.StatusBadRequest, oauth.ErrUnauthorizedClient, func() (int, map[string]any) { return unnamed.exchange(t, a, subject) }},
		{"a scope beyond the app's", http.StatusBadRequest, oauth.ErrInvalidScope, func() (int, map[string]any) {
			return stator.exchange(t, a, subject, "scope", "read write")
		}},
		{"another kind of subject token", http.StatusBadRequest, oauth.ErrInvalidRequest, func() (int, map[string]any) {
			return stator.exchange(t, a, subject, "subject_token_type", "urn:ietf:params:oauth:token-type:id_token")
		}},
		{"a wrong secret", http.StatusUnauthorized, oauth.ErrInvalidClient, func() (int, map[string]any) {
			return partnerApp{id: stator.id, secret: "exciter_ocs_wrong"}.exchange(t, a, subject)
		}},
	} {
		status, got := c.send()
		if status != c.status || got["error"] != c.code || got["error_description"] == "" {
			t.Errorf("%s answered %d %v, want %d %s", c.what, status, got, c.status, c.code)
		}
	}
	if n := h.countRows(t, `SELECT count(*) FROM oauth_grant WHERE client_id = ANY ($1)`, []string{stator.id, other.id, unnamed.id, itself.id}); n != 0 {
		t.Errorf("the refusals left %d grants", n)
	}
	t.Run("an assistant may not exchange", func(t *testing.T) {
		desk, _, _ := register(t, newOAuthServer(t, h), newAddress(), nil)
		form := url.Values{"grant_type": {oauth.GrantTokenExchange}, "subject_token": {subject}, "subject_token_type": {oauth.TokenTypeAccess}, "client_id": {desk.id}}
		resp, err := http.PostForm(a.URL+httpapi.APIPrefix+"/oauth/token", form)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode != http.StatusBadRequest || out["error"] != oauth.ErrUnauthorizedClient {
			t.Errorf("an assistant's exchange answered %d %v", resp.StatusCode, out)
		}
	})
}

// Only an administrator registers, changes and removes a partner app, straight through SQL as much as over the API,
// and its secret is shown once (#273).
func TestPartnerAppsAreTheAdministratorsToKeep(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	boss := h.owner(t, "keepers")
	admin := a.as(t, boss.user)
	memberID := h.addPerson(t, "member")
	member := a.as(t, memberID)
	app := registerPartner(t, admin, map[string]any{"name": "Stator", "keycloakClientId": statorClient})

	t.Run("the secret is shown once and stored as a digest", func(t *testing.T) {
		if !strings.HasPrefix(app.secret, "exciter_ocs_") {
			t.Errorf("the secret is %q", app.secret)
		}
		for _, each := range list(t, want(t, admin.get(t, "/api/v1/oauth/clients"), http.StatusOK, "list the apps"), "clients") {
			if c := each.(map[string]any); c["id"] == app.id && (c["secret"] != nil || c["keycloakClientId"] != statorClient) {
				t.Errorf("the list answers %v", c)
			}
		}
		rotated := obj(t, want(t, admin.post(t, "/api/v1/oauth/clients/"+app.id+"/rotate-secret", nil), http.StatusOK, "rotate the secret"), "client")
		if rotated["secret"] == app.secret || rotated["secret"] == nil {
			t.Errorf("the rotation answered %v", rotated)
		}
		if status, got := app.token(t, a, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"exciter_ort_x"}}); status != http.StatusUnauthorized || got["error"] != oauth.ErrInvalidClient {
			t.Errorf("the old secret still authenticates: %d %v", status, got)
		}
	})

	t.Run("the API refuses whoever does not administer", func(t *testing.T) {
		refuse(t, "a member listing", member.get(t, "/api/v1/oauth/clients"), http.StatusForbidden, "forbidden")
		refuse(t, "a member registering", member.post(t, "/api/v1/oauth/clients", map[string]any{"name": "Mine"}), http.StatusForbidden, "forbidden")
		refuse(t, "write alone", admin.post(t, "/api/v1/oauth/clients", map[string]any{"name": "Writer", "scopes": []string{"write"}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "an unknown app", admin.patch(t, "/api/v1/oauth/clients/nothing", map[string]any{"name": "x"}), http.StatusNotFound, "not_found")
	})

	t.Run("straight through SQL only an administrator touches a partner", func(t *testing.T) {
		plain := h.appConn(t, memberID)
		touchesNothing(t, plain, "a member renaming a partner", `UPDATE oauth_client SET name = 'Mine' WHERE id = $1`, app.id)
		touchesNothing(t, plain, "a member removing a partner", `DELETE FROM oauth_client WHERE id = $1`, app.id)
		if _, err := plain.Exec(context.Background(), `INSERT INTO oauth_client (id, kind, name, redirect_uris, auth_method, secret_hash, grant_types, scopes)
			VALUES ('sneaky', 'partner', 'Sneaky', '{}', 'client_secret_basic', sha256('x'), $1, '{read}')`, []string{oauth.GrantTokenExchange, "refresh_token"}); !isPolicyViolation(err) {
			t.Errorf("a member registered a partner: %v", err)
		}
		owner := h.appConn(t, boss.user)
		if _, err := owner.Exec(context.Background(), `INSERT INTO oauth_client (id, kind, name, redirect_uris, grant_types) VALUES ('assistant', 'registered', 'Assistant', '{https://x.test/cb}', '{authorization_code}')`); !isPolicyViolation(err) {
			t.Errorf("an administrator made an assistant's client as the app role: %v", err)
		}
		if _, err := owner.Exec(context.Background(), `SELECT secret_hash FROM oauth_client`); !isPermissionDenied(err) {
			t.Errorf("the app role read a client's secret digest: %v", err)
		}
	})

	t.Run("removing it ends its grants", func(t *testing.T) {
		want(t, admin.delete(t, "/api/v1/oauth/clients/"+app.id), http.StatusNoContent, "remove the app")
		refuse(t, "removing it twice", admin.delete(t, "/api/v1/oauth/clients/"+app.id), http.StatusNotFound, "not_found")
		if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE action LIKE 'oauth_app.%' AND data->>'clientId' = $1`, app.id); n != 3 {
			t.Errorf("the log holds %d records of the app, want its creation, rotation and removal", n)
		}
	})
}
