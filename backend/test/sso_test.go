//go:build integration

package test

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/oidc"
)

// providerGroups lists the provider groups a person is in within one organization.
func (h *harness) providerGroups(t *testing.T, email string) []string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT g.external_ref FROM group_member m
		JOIN groups g ON g.id = m.group_id
		JOIN app_user u ON u.id = m.user_id
		WHERE u.email = $1 AND g.source = 'oidc'
		ORDER BY 1`, email)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			t.Fatal(err)
		}
		out = append(out, ref)
	}
	return out
}

// Alice signs in through Keycloak's own login form, with nothing stood in for,
// and her groups follow her until she signs out.
func TestSignInThroughKeycloak(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, a, kc.issuer(), true)
	h.letIn(t, aliceEmail, "member")
	ctx := context.Background()

	t.Run("the client secret is sealed at rest", func(t *testing.T) {
		var stored []byte
		if err := h.super.QueryRow(ctx, `SELECT client_secret FROM oidc_provider`).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if len(stored) == 0 || bytes.Contains(stored, []byte(exciterSecret)) {
			t.Fatalf("the stored secret is %q", stored)
		}
	})

	b := newBrowser(t, a)
	landing := b.signIn(t, "alice", alicePassword, "/settings/members")
	if landing != "http://"+appHost+"/settings/members" {
		t.Fatalf("landed on %q, want the page asked for", landing)
	}

	t.Run("the account is made from the token and remembered by subject", func(t *testing.T) {
		status, me := b.me(t)
		if status != http.StatusOK {
			t.Fatalf("/auth/me = %d %v", status, me)
		}
		user, _ := me["user"].(map[string]any)
		if user["email"] != aliceEmail || user["name"] != "Alice Admin" {
			t.Errorf("user = %v", user)
		}
		if me["role"] != string(auth.RoleMember) {
			t.Errorf("role = %v, want a member", me["role"])
		}
		var subject string
		userID, _ := user["id"].(string)
		if err := h.super.QueryRow(ctx, `SELECT subject FROM user_identity WHERE issuer = $1 AND user_id = $2`, kc.issuer(), userID).Scan(&subject); err != nil || subject == "" {
			t.Fatalf("no identity for alice under %s: %v", kc.issuer(), err)
		}
	})

	t.Run("her groups are created and joined", func(t *testing.T) {
		if got := h.providerGroups(t, aliceEmail); !slices.Equal(got, aliceGroups) {
			t.Fatalf("groups = %q", got)
		}
	})

	t.Run("only the digest of the session token is stored", func(t *testing.T) {
		secret := b.sessionCookie(t)
		if secret == "" {
			t.Fatal("the browser holds no session cookie")
		}
		var n int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM user_session WHERE token_hash = $1 AND proof = 'oidc'`, auth.HashToken(secret)).Scan(&n); err != nil || n != 1 {
			t.Fatalf("provider sessions under the digest = %d (%v)", n, err)
		}
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM user_session WHERE position(convert_to($1, 'UTF8') in token_hash) > 0`, secret).Scan(&n); err != nil || n != 0 {
			t.Fatalf("the raw token is stored: %d (%v)", n, err)
		}
	})

	t.Run("a replayed callback opens nothing", func(t *testing.T) {
		if len(b.callbacks) == 0 {
			t.Fatal("the browser never passed the callback")
		}
		replay := newBrowser(t, a)
		resp, _ := replay.get(t, b.callbacks[len(b.callbacks)-1])
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "http://"+appHost+"/login?sso=expired" {
			t.Fatalf("a replay = %s to %q", resp.Status, resp.Header.Get("Location"))
		}
		if replay.sessionCookie(t) != "" {
			t.Fatal("a replayed callback set a session cookie")
		}
	})

	t.Run("a group removed at the provider is left on the next sign-in", func(t *testing.T) {
		kc.setMembership(t, "alice", "engineering", false)
		t.Cleanup(func() { kc.setMembership(t, "alice", "engineering", true) })

		newBrowser(t, a).signIn(t, "alice", alicePassword, "")
		if got := h.providerGroups(t, aliceEmail); !slices.Equal(got, slices.DeleteFunc(slices.Clone(aliceGroups), func(g string) bool { return g == "engineering" })) {
			t.Fatalf("groups after leaving engineering = %q", got)
		}
		if n := h.countRows(t, `SELECT count(*) FROM app_user WHERE email = $1`, aliceEmail); n != 1 {
			t.Fatalf("alice has %d accounts after signing in twice", n)
		}

		kc.setMembership(t, "alice", "engineering", true)
		newBrowser(t, a).signIn(t, "alice", alicePassword, "")
		if got := h.providerGroups(t, aliceEmail); !slices.Equal(got, aliceGroups) {
			t.Fatalf("groups after rejoining engineering = %q", got)
		}
	})

	t.Run("signing out ends the session", func(t *testing.T) {
		secret := b.sessionCookie(t)
		resp, _ := b.send(t, http.MethodPost, a.URL+httpapi.APIPrefix+"/auth/logout")
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("logout = %s", resp.Status)
		}
		if status, _ := b.me(t); status != http.StatusUnauthorized {
			t.Fatalf("/auth/me after logout = %d", status)
		}
		if n := h.countRows(t, `SELECT count(*) FROM user_session WHERE token_hash = $1`, auth.HashToken(secret)); n != 0 {
			t.Fatalf("the session outlived the logout: %d", n)
		}
	})
}

// Somebody is in a group of the realm's, and a provider that does not create groups only joins the ones an
// administrator already made. Group and person are the test's own, since a sign-in elsewhere may have made the
// realm's groups in the installation.
func TestOnlyExistingGroupsAreJoinedWhenCreationIsOff(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, a, kc.issuer(), false)
	group, username := "crew-"+h.suffix(), "crew-member-"+h.suffix()
	kc.newGroup(t, group)
	email := kc.newPerson(t, h, username, bobPassword, "/"+group)
	h.letIn(t, email, "member")

	newBrowser(t, a).signIn(t, username, bobPassword, "")
	if got := h.providerGroups(t, email); len(got) != 0 {
		t.Fatalf("groups = %q, want none while no group is made", got)
	}

	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO groups (name, source, external_ref) VALUES ($1, 'oidc', $2)`, "Crew "+group, group); err != nil {
		t.Fatal(err)
	}
	newBrowser(t, a).signIn(t, username, bobPassword, "")
	if got := h.providerGroups(t, email); !slices.Equal(got, []string{group}) {
		t.Fatalf("groups = %q, want the one made", got)
	}
}

func TestSignInRefusals(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	start := func() *http.Response {
		b := newBrowser(t, a)
		b.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, _ := b.get(t, a.URL+httpapi.APIPrefix+"/auth/oidc/start")
		return resp
	}

	notConfigured := "http://" + appHost + "/login?sso=not_configured"
	t.Run("an installation without a provider sends the browser back saying so", func(t *testing.T) {
		h.singleSignOn(t)
		if resp := start(); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != notConfigured {
			t.Errorf("start without a provider = %s to %q, want the sign-in page saying so", resp.Status, resp.Header.Get("Location"))
		}
	})

	t.Run("a provider that is turned off is not used", func(t *testing.T) {
		h.singleSignOn(t)
		configureProvider(t, a, kc.issuer(), false)
		admin := h.addPerson(t, "admin")
		if _, _, err := a.sso.Save(withActor(admin), oidc.Provider{Issuer: kc.issuer(), ClientID: exciterClient, Enabled: false}); err != nil {
			t.Fatal(err)
		}
		if resp := start(); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != notConfigured {
			t.Errorf("start with the provider off = %s to %q", resp.Status, resp.Header.Get("Location"))
		}
	})

	t.Run("a provider that cannot be reached is named as the reason", func(t *testing.T) {
		h.singleSignOn(t)
		configureProvider(t, a, "http://nowhere.invalid/realms/none", false)
		if resp := start(); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "http://"+appHost+"/login?sso=unreachable" {
			t.Errorf("start with an unreachable provider = %s to %q", resp.Status, resp.Header.Get("Location"))
		}
	})

	t.Run("an expired sign-in is refused at the callback", func(t *testing.T) {
		h.singleSignOn(t)
		configureProvider(t, a, kc.issuer(), false)
		if resp := start(); resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), kc.issuer()) {
			t.Fatalf("start = %s to %q", resp.Status, resp.Header.Get("Location"))
		}
		var state string
		if err := h.super.QueryRow(context.Background(), `
			UPDATE oidc_login SET expires_at = now() - interval '1 second' RETURNING state`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		resp, _ := newBrowser(t, a).get(t, a.URL+httpapi.APIPrefix+"/auth/oidc/callback?state="+state+"&code=whatever")
		if resp.Header.Get("Location") != "http://"+appHost+"/login?sso=expired" {
			t.Fatalf("an expired callback went to %q", resp.Header.Get("Location"))
		}
	})

	t.Run("a refusal by the provider is shown, not swallowed", func(t *testing.T) {
		resp, _ := newBrowser(t, a).get(t, a.URL+httpapi.APIPrefix+"/auth/oidc/callback?error=access_denied&state=x")
		if resp.Header.Get("Location") != "http://"+appHost+"/login?sso=denied" {
			t.Fatalf("a refused sign-in went to %q", resp.Header.Get("Location"))
		}
	})

	t.Run("a redirect out of the application is dropped", func(t *testing.T) {
		h.singleSignOn(t)
		configureProvider(t, a, kc.issuer(), false)
		h.letIn(t, aliceEmail, "member")
		if landing := newBrowser(t, a).signIn(t, "alice", alicePassword, "//evil.test/login"); landing != "http://"+appHost+"/" {
			t.Fatalf("landed on %q, want the application's own start", landing)
		}
	})
}

// A callback finishes only in the browser that started its sign-in: somebody who signs in at the provider as
// themselves and carries the callback to another browser signs nobody in there, and does not spend the sign-in.
func TestASignInFinishesOnlyInTheBrowserThatStartedIt(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, a, kc.issuer(), false)
	h.letIn(t, aliceEmail, "member")
	ctx := context.Background()
	bindingCookie := h.cfg.Auth.SessionCookie + "_oidc"
	callbackPath := httpapi.APIPrefix + "/auth/oidc/callback"
	leftFor := func(t *testing.T, state string) int {
		t.Helper()
		return h.countRows(t, `SELECT count(*) FROM oidc_login WHERE state = $1`, state)
	}
	callbackState := func(t *testing.T, callback string) string {
		t.Helper()
		u, err := url.Parse(callback)
		if err != nil || u.Query().Get("state") == "" {
			t.Fatalf("no state in the callback %q (%v)", callback, err)
		}
		return u.Query().Get("state")
	}

	t.Run("the start hands the browser a secret only the callback is sent, and keeps its digest", func(t *testing.T) {
		b := newBrowser(t, a)
		b.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, _ := b.get(t, a.URL+httpapi.APIPrefix+"/auth/oidc/start")
		var bound *http.Cookie
		for _, c := range resp.Cookies() {
			if c.Name == bindingCookie {
				bound = c
			}
		}
		if bound == nil || bound.Value == "" {
			t.Fatalf("the start set no %s cookie: %q", bindingCookie, resp.Header.Values("Set-Cookie"))
		}
		if !bound.HttpOnly || bound.SameSite != http.SameSiteLaxMode || bound.Path != callbackPath ||
			bound.MaxAge != int(oidc.HowLongALoginMayTake.Seconds()) {
			t.Errorf("the cookie is %+v, want HttpOnly, Lax, only for %s and as long as a sign-in may take", bound, callbackPath)
		}
		target, _ := url.Parse(resp.Header.Get("Location"))
		state := target.Query().Get("state")
		if n := h.countRows(t, `SELECT count(*) FROM oidc_login WHERE state = $1 AND browser_hash = $2`, state, auth.HashToken(bound.Value)); n != 1 {
			t.Fatalf("logins under the cookie's digest = %d", n)
		}
		if n := h.countRows(t, `SELECT count(*) FROM oidc_login WHERE position(convert_to($1, 'UTF8') in browser_hash) > 0`, bound.Value); n != 0 {
			t.Fatalf("the raw secret is stored: %d", n)
		}
	})

	t.Run("the database refuses a sign-in bound to no browser", func(t *testing.T) {
		_, err := h.super.Exec(ctx, `
			INSERT INTO oidc_login (state, nonce, code_verifier, expires_at) VALUES (gen_random_uuid()::text, 'n', 'v', now() + interval '1 hour')`)
		if err == nil {
			t.Fatal("a sign-in without a browser's digest was stored")
		}
	})

	// The attacker goes the whole way at the provider as alice, then keeps the callback instead of following it.
	attacker := newBrowser(t, a)
	attacker.holdCallback = true
	callback := attacker.signIn(t, "alice", alicePassword, "/settings/members")
	if !strings.HasSuffix(strings.SplitN(callback, "?", 2)[0], callbackPath) {
		t.Fatalf("the provider sent the browser to %q, want the callback", callback)
	}
	state := callbackState(t, callback)
	refused := "http://" + appHost + "/login?sso=other_browser"

	t.Run("a browser holding no secret is refused and signed in as nobody", func(t *testing.T) {
		victim := newBrowser(t, a)
		resp, _ := victim.get(t, callback)
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != refused {
			t.Fatalf("the callback in another browser = %s to %q, want %q", resp.Status, resp.Header.Get("Location"), refused)
		}
		if victim.sessionCookie(t) != "" {
			t.Fatal("the callback in another browser set a session cookie")
		}
		if n := leftFor(t, state); n != 1 {
			t.Fatalf("the refused callback spent the sign-in: %d left", n)
		}
	})

	t.Run("a browser holding the secret of a sign-in of its own is refused too", func(t *testing.T) {
		victim := newBrowser(t, a)
		victim.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, _ := victim.get(t, a.URL+httpapi.APIPrefix+"/auth/oidc/start")
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("the victim's own start = %s", resp.Status)
		}
		resp, _ = victim.get(t, callback)
		if resp.Header.Get("Location") != refused {
			t.Fatalf("the callback in a browser with its own sign-in went to %q, want %q", resp.Header.Get("Location"), refused)
		}
		if victim.sessionCookie(t) != "" {
			t.Fatal("the callback in another browser set a session cookie")
		}
		if n := leftFor(t, state); n != 1 {
			t.Fatalf("the refused callback spent the sign-in: %d left", n)
		}
	})

	t.Run("the browser that started it signs in, and keeps no secret after", func(t *testing.T) {
		attacker.holdCallback = false
		resp, _ := attacker.get(t, callback)
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "http://"+appHost+"/settings/members" {
			t.Fatalf("the callback in its own browser = %s to %q", resp.Status, resp.Header.Get("Location"))
		}
		h.settle(t)
		if status, me := attacker.me(t); status != http.StatusOK {
			t.Fatalf("/auth/me after the callback = %d %v", status, me)
		}
		u, _ := url.Parse(a.URL + callbackPath)
		for _, c := range attacker.Jar.Cookies(u) {
			if c.Name == bindingCookie {
				t.Fatalf("the browser still holds %s after its callback", bindingCookie)
			}
		}
		if n := leftFor(t, state); n != 0 {
			t.Fatalf("the sign-in outlived its callback: %d left", n)
		}
	})
}

// The API reaches a provider at an address of the network's while asking for
// it by its public name, and Keycloak then describes itself under that name.
func TestTheBackchannelReachesKeycloakUnderItsPublicName(t *testing.T) {
	kc := newKeycloak(t)
	const public = "http://idp.public.test"
	client := oidc.Backchannel(map[string]string{public: kc.base})
	provider, err := coreoidc.NewProvider(coreoidc.ClientContext(context.Background(), client), public+"/realms/"+kc.realm)
	if err != nil {
		t.Fatalf("discovery through the rewrite: %v", err)
	}
	if got := provider.Endpoint().TokenURL; !strings.HasPrefix(got, public+"/") {
		t.Fatalf("the token endpoint is %q, want it under the public name", got)
	}
}
