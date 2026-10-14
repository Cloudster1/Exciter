//go:build integration

package test

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/oidc"
)

// providerGroups lists the provider groups a person is in within one organization.
func (h *harness) providerGroups(t *testing.T, org installationRow, email string) []string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT g.external_ref FROM group_member m
		JOIN groups g ON g.id = m.group_id
		JOIN app_user u ON u.id = m.user_id
		WHERE m.org_id = $1 AND u.email = $2 AND g.source = 'oidc'
		ORDER BY 1`, org.ID, email)
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
	org := h.singleSignOn(t)
	configureProvider(t, a, org, kc.issuer(), true)
	h.letIn(t, org, aliceEmail, "member")
	ctx := context.Background()

	t.Run("the client secret is sealed at rest", func(t *testing.T) {
		var stored []byte
		if err := h.super.QueryRow(ctx, `SELECT client_secret FROM oidc_provider WHERE org_id = $1`, org.ID).Scan(&stored); err != nil {
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
		if got := h.providerGroups(t, org, aliceEmail); !slices.Equal(got, []string{"engineering", "exciter-administrators"}) {
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
		if got := h.providerGroups(t, org, aliceEmail); !slices.Equal(got, []string{"exciter-administrators"}) {
			t.Fatalf("groups after leaving engineering = %q", got)
		}
		if n := h.countRows(t, `SELECT count(*) FROM app_user WHERE email = $1`, aliceEmail); n != 1 {
			t.Fatalf("alice has %d accounts after signing in twice", n)
		}

		kc.setMembership(t, "alice", "engineering", true)
		newBrowser(t, a).signIn(t, "alice", alicePassword, "")
		if got := h.providerGroups(t, org, aliceEmail); !slices.Equal(got, []string{"engineering", "exciter-administrators"}) {
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
	org := h.singleSignOn(t)
	configureProvider(t, a, org, kc.issuer(), false)
	group, username := "crew-"+h.suffix(), "crew-member-"+h.suffix()
	kc.newGroup(t, group)
	email := kc.newPerson(t, h, username, bobPassword, "/"+group)
	h.letIn(t, org, email, "member")

	newBrowser(t, a).signIn(t, username, bobPassword, "")
	if got := h.providerGroups(t, org, email); len(got) != 0 {
		t.Fatalf("groups = %q, want none while no group is made", got)
	}

	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO groups (org_id, name, source, external_ref) VALUES ($1, $2, 'oidc', $3)`, org.ID, "Crew "+group, group); err != nil {
		t.Fatal(err)
	}
	newBrowser(t, a).signIn(t, username, bobPassword, "")
	if got := h.providerGroups(t, org, email); !slices.Equal(got, []string{group}) {
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
		org := h.singleSignOn(t)
		configureProvider(t, a, org, kc.issuer(), false)
		admin := h.addPerson(t, org.ID, "admin")
		if _, _, err := a.sso.Save(withActor(admin), oidc.Provider{Issuer: kc.issuer(), ClientID: exciterClient, Enabled: false}); err != nil {
			t.Fatal(err)
		}
		if resp := start(); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != notConfigured {
			t.Errorf("start with the provider off = %s to %q", resp.Status, resp.Header.Get("Location"))
		}
	})

	t.Run("a provider that cannot be reached is named as the reason", func(t *testing.T) {
		org := h.singleSignOn(t)
		configureProvider(t, a, org, "http://nowhere.invalid/realms/none", false)
		if resp := start(); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "http://"+appHost+"/login?sso=unreachable" {
			t.Errorf("start with an unreachable provider = %s to %q", resp.Status, resp.Header.Get("Location"))
		}
	})

	t.Run("an expired sign-in is refused at the callback", func(t *testing.T) {
		org := h.singleSignOn(t)
		configureProvider(t, a, org, kc.issuer(), false)
		if resp := start(); resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), kc.issuer()) {
			t.Fatalf("start = %s to %q", resp.Status, resp.Header.Get("Location"))
		}
		var state string
		if err := h.super.QueryRow(context.Background(), `
			UPDATE oidc_login SET expires_at = now() - interval '1 second'
			WHERE org_id = $1 RETURNING state`, org.ID).Scan(&state); err != nil {
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
		org := h.singleSignOn(t)
		configureProvider(t, a, org, kc.issuer(), false)
		h.letIn(t, org, aliceEmail, "member")
		if landing := newBrowser(t, a).signIn(t, "alice", alicePassword, "//evil.test/login"); landing != "http://"+appHost+"/" {
			t.Fatalf("landed on %q, want the application's own start", landing)
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
