package oidc

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestClaimsBecomeAnIdentity(t *testing.T) {
	claims := map[string]any{
		"email":          " Alice@Example.TEST ",
		"email_verified": true,
		"name":           "Alice Liddell",
		"groups":         []any{"engineering", " exciter-admins ", "engineering", "", 42},
	}
	got, err := identityFromClaims("https://id.test/realms/r", "sub-1", claims, "groups")
	if err != nil {
		t.Fatal(err)
	}
	want := &Identity{
		Issuer: "https://id.test/realms/r", Subject: "sub-1",
		Email: "alice@example.test", Name: "Alice Liddell",
		Groups: []string{"engineering", "exciter-admins"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("identity = %+v, want %+v", got, want)
	}
}

func TestTheGroupsClaimIsConfigurable(t *testing.T) {
	claims := map[string]any{
		"email":                        "bob@example.test",
		"roles":                        "marketing",
		"realm_access":                 map[string]any{"roles": []any{"reader", "writer"}},
		"https://example.test/groups":  []any{"by-url"},
		"https://example.test/nothing": nil,
	}
	for claim, want := range map[string][]string{
		"roles":                       {"marketing"},
		"realm_access.roles":          {"reader", "writer"},
		"https://example.test/groups": {"by-url"},
		"groups":                      {},
		"realm_access.missing.deeper": {},
	} {
		got, err := identityFromClaims("i", "s", claims, claim)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.Groups, want) {
			t.Errorf("claim %q gave %q, want %q", claim, got.Groups, want)
		}
	}
}

func TestANameIsAlwaysFound(t *testing.T) {
	for _, tc := range []struct {
		claims map[string]any
		want   string
	}{
		{map[string]any{"given_name": "Bob", "family_name": "Builder"}, "Bob Builder"},
		{map[string]any{"preferred_username": "bobby"}, "bobby"},
		{map[string]any{}, "bob"},
	} {
		tc.claims["email"] = "bob@example.test"
		got, err := identityFromClaims("i", "s", tc.claims, "groups")
		if err != nil || got.Name != tc.want {
			t.Errorf("claims %v named %q (%v), want %q", tc.claims, got.Name, err, tc.want)
		}
	}
}

func TestAnIdentityNeedsAVerifiedEmail(t *testing.T) {
	if _, err := identityFromClaims("i", "s", map[string]any{"name": "Nobody"}, "groups"); !errors.Is(err, ErrNoEmail) {
		t.Errorf("no email = %v, want ErrNoEmail", err)
	}
	for _, verified := range []any{false, "false", "FALSE"} {
		claims := map[string]any{"email": "x@example.test", "email_verified": verified}
		if _, err := identityFromClaims("i", "s", claims, "groups"); !errors.Is(err, ErrEmailUnverified) {
			t.Errorf("email_verified %v = %v, want ErrEmailUnverified", verified, err)
		}
	}
}

func TestGroupDiffMakesMembershipExact(t *testing.T) {
	for _, tc := range []struct {
		name                string
		current, claimed    []string
		wantJoin, wantLeave []string
	}{
		{"first sign-in joins everything", nil, []string{"a", "b"}, []string{"a", "b"}, nil},
		{"nothing changed", []string{"a", "b"}, []string{"b", "a"}, nil, nil},
		{"a group removed at the provider is left", []string{"a", "b"}, []string{"a"}, nil, []string{"b"}},
		{"moved between groups", []string{"a"}, []string{"c"}, []string{"c"}, []string{"a"}},
		{"an empty claim leaves every provider group", []string{"a", "b"}, nil, nil, []string{"a", "b"}},
		{"a repeated claim joins once", nil, []string{"a", "a"}, []string{"a"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			join, leave := diffGroups(tc.current, tc.claimed)
			slices.Sort(leave)
			if !slices.Equal(join, tc.wantJoin) || !slices.Equal(leave, tc.wantLeave) {
				t.Fatalf("join %q leave %q, want join %q leave %q", join, leave, tc.wantJoin, tc.wantLeave)
			}
		})
	}
}

func TestScopesAlwaysIncludeOpenID(t *testing.T) {
	got := Provider{Scopes: "profile email profile"}.ScopeList()
	if !slices.Equal(got, []string{"openid", "profile", "email"}) {
		t.Fatalf("scopes = %q", got)
	}
}

func TestProviderSettingsAreNormalizedAndChecked(t *testing.T) {
	got, err := normalize(Provider{Issuer: " https://id.test/realms/r/ ", ClientID: " exciter ", Scopes: "  openid   email "})
	if err != nil {
		t.Fatal(err)
	}
	if got.Issuer != "https://id.test/realms/r" || got.ClientID != "exciter" || got.GroupsClaim != DefaultGroupsClaim || got.Scopes != "openid email" {
		t.Fatalf("normalized = %+v", got)
	}
	for _, bad := range []Provider{
		{Issuer: "", ClientID: "c"},
		{Issuer: "id.test", ClientID: "c"},
		{Issuer: "ftp://id.test", ClientID: "c"},
		{Issuer: "https://id.test", ClientID: " "},
	} {
		var v *ValidationError
		if _, err := normalize(bad); !errors.As(err, &v) || !strings.HasSuffix(v.Message, ".") {
			t.Errorf("%+v = %v, want a validation error in a sentence", bad, err)
		}
	}
}

// The browser leaves with the state, the nonce and a challenge; the verifier
// itself never appears in the URL, and the challenge is its S256 digest.
func TestTheAuthorizationURLCarriesStateNonceAndPKCE(t *testing.T) {
	state, _ := randomToken()
	nonce, _ := randomToken()
	verifier := oauth2.GenerateVerifier()
	config := &oauth2.Config{
		ClientID:    "exciter",
		Endpoint:    oauth2.Endpoint{AuthURL: "https://id.test/auth", TokenURL: "https://id.test/token"},
		RedirectURL: "https://requirements.test/api/v1/auth/oidc/callback",
		Scopes:      []string{"openid", "email"},
	}
	raw := authCodeURL(config, state, nonce, verifier)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	sum := sha256.Sum256([]byte(verifier))
	for key, want := range map[string]string{
		"state":                 state,
		"nonce":                 nonce,
		"code_challenge":        base64.RawURLEncoding.EncodeToString(sum[:]),
		"code_challenge_method": "S256",
		"response_type":         "code",
		"client_id":             "exciter",
		"redirect_uri":          "https://requirements.test/api/v1/auth/oidc/callback",
		"scope":                 "openid email",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if strings.Contains(raw, verifier) {
		t.Error("the verifier left with the browser")
	}
}

// A re-authentication tells the provider to ask for the credentials again, whatever session it holds, and still
// carries everything a sign-in does; a sign-in tells it nothing of the kind.
func TestAReauthenticationAsksThePersonAgain(t *testing.T) {
	config := &oauth2.Config{ClientID: "exciter", Endpoint: oauth2.Endpoint{AuthURL: "https://id.test/auth", TokenURL: "https://id.test/token"}}
	again, err := url.Parse(reauthURL(config, "state", "nonce", oauth2.GenerateVerifier(), "ada@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	q := again.Query()
	for key, want := range map[string]string{"prompt": "login", "max_age": "0", "login_hint": "ada@example.com", "state": "state", "nonce": "nonce", "code_challenge_method": "S256"} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	plain, _ := url.Parse(authCodeURL(config, "state", "nonce", oauth2.GenerateVerifier()))
	if plain.Query().Has("prompt") || plain.Query().Has("max_age") {
		t.Error("a plain sign-in asks the person again")
	}
}

// The provider's word for when the person signed in is kept, so a re-authentication can be held to it.
func TestTheAuthTimeIsRead(t *testing.T) {
	identity, err := identityFromClaims("https://id.test", "sub", map[string]any{"email": "ada@example.com", "auth_time": float64(1_790_000_000)}, "groups")
	if err != nil {
		t.Fatal(err)
	}
	if identity.AuthTime.Unix() != 1_790_000_000 {
		t.Errorf("the auth time read as %v", identity.AuthTime)
	}
}

func TestRandomTokensAreLongAndFresh(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		token, err := randomToken()
		if err != nil {
			t.Fatal(err)
		}
		if len(token) != 43 || seen[token] {
			t.Fatalf("token %q is short or repeated", token)
		}
		seen[token] = true
	}
}

func TestBackchannelReachesTheProviderUnderItsPublicName(t *testing.T) {
	var sawHost, sawPath string
	reachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHost, sawPath = r.Host, r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer reachable.Close()

	client := Backchannel(map[string]string{"http://public.example:8180": reachable.URL})
	resp, err := client.Get("http://public.example:8180/realms/demo/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if sawHost != "public.example:8180" {
		t.Errorf("the provider was asked as %q, want the public name", sawHost)
	}
	if sawPath != "/realms/demo/.well-known/openid-configuration" {
		t.Errorf("path = %q", sawPath)
	}

	// Anywhere not named goes where it says.
	resp, err = client.Get(reachable.URL + "/direct")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if sawPath != "/direct" {
		t.Errorf("an unnamed address was rewritten: %q", sawPath)
	}
}
