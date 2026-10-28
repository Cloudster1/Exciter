package exchangestub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const (
	testClient   = "exciter-app"
	testSecret   = "s3cret"
	testKeycloak = "exciter"
	testAudience = "stator"
)

// fakeVerify reads "kc|email|verified|azp|aud" as a verified token, so the tests need no identity provider.
func fakeVerify(_ context.Context, raw string) (*Claims, error) {
	parts := strings.Split(raw, "|")
	if len(parts) != 5 || parts[0] != "kc" {
		return nil, errors.New("it is not a current token signed by the identity provider")
	}
	verified := parts[2] == "true"
	return &Claims{Email: parts[1], EmailVerified: &verified, AuthorizedBy: parts[3], Audience: []string{parts[4]}}, nil
}

func newTestServer(t *testing.T, verify Verifier, app App) *Server {
	t.Helper()
	s := NewServer("stator", "stator_oat_", "stator_ort_", testAudience, verify)
	s.SetApp("acme", app)
	return s
}

func registered() App {
	return App{ClientID: testClient, ClientSecret: testSecret, KeycloakClientID: testKeycloak}
}

// post sends the token endpoint a form as the app, by HTTP Basic, and reads the answer.
func post(t *testing.T, s *Server, values url.Values) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/oauth/token", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClient, testSecret)
	rec := httptest.NewRecorder()
	s.ServeToken(rec, req)
	out := map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the answer is no JSON: %s", rec.Body.String())
	}
	return rec.Code, out
}

func exchangeOf(subject string) url.Values {
	return url.Values{"grant_type": {GrantExchange}, "subject_token": {subject}, "subject_token_type": {TokenTypeAccess}}
}

func TestTheExchangeRefusesInOAuthsWords(t *testing.T) {
	alice := "kc|alice@suite.test|true|" + testKeycloak + "|" + testAudience
	for _, tc := range []struct {
		name   string
		app    App
		verify Verifier
		form   url.Values
		status int
		code   string
	}{
		{"a grant the stub does not offer", registered(), fakeVerify, url.Values{"grant_type": {"client_credentials"}}, http.StatusBadRequest, "unsupported_grant_type"},
		{"no subject token", registered(), fakeVerify, url.Values{"grant_type": {GrantExchange}, "subject_token_type": {TokenTypeAccess}}, http.StatusBadRequest, "invalid_request"},
		{"a subject of another type", registered(), fakeVerify, url.Values{"grant_type": {GrantExchange}, "subject_token": {alice}, "subject_token_type": {"urn:ietf:params:oauth:token-type:id_token"}}, http.StatusBadRequest, "invalid_request"},
		{"an app without a Keycloak client", App{ClientID: testClient, ClientSecret: testSecret}, fakeVerify, exchangeOf(alice), http.StatusBadRequest, "unauthorized_client"},
		{"a stub without single sign-on", registered(), nil, exchangeOf(alice), http.StatusBadRequest, "unauthorized_client"},
		{"a token the identity provider did not sign", registered(), fakeVerify, exchangeOf("forged"), http.StatusBadRequest, "invalid_grant"},
		{"a token issued to another client", registered(), fakeVerify, exchangeOf("kc|alice@suite.test|true|armature|" + testAudience), http.StatusBadRequest, "invalid_grant"},
		{"a token for another audience", registered(), fakeVerify, exchangeOf("kc|alice@suite.test|true|" + testKeycloak + "|armature"), http.StatusBadRequest, "invalid_grant"},
		{"an unverified address", registered(), fakeVerify, exchangeOf("kc|alice@suite.test|false|" + testKeycloak + "|" + testAudience), http.StatusBadRequest, "invalid_grant"},
		{"no address", registered(), fakeVerify, exchangeOf("kc||true|" + testKeycloak + "|" + testAudience), http.StatusBadRequest, "invalid_grant"},
		{"an app nobody registered", App{ClientID: "someone-else", ClientSecret: testSecret}, fakeVerify, exchangeOf(alice), http.StatusUnauthorized, "invalid_client"},
		{"a wrong secret", App{ClientID: testClient, ClientSecret: "other", KeycloakClientID: testKeycloak}, fakeVerify, exchangeOf(alice), http.StatusUnauthorized, "invalid_client"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t, tc.verify, tc.app)
			status, body := post(t, s, tc.form)
			if status != tc.status || body["error"] != tc.code || body["error_description"] == "" {
				t.Errorf("answered %d %v, want %d %s with a description", status, body, tc.status, tc.code)
			}
			if grants := s.Grants("acme"); len(grants) != 0 {
				t.Errorf("a refusal left %d grants", len(grants))
			}
		})
	}
}

func TestAnExchangedGrantIsRefreshedAndEndsWhenARefreshIsReplayed(t *testing.T) {
	s := newTestServer(t, fakeVerify, registered())
	status, got := post(t, s, exchangeOf("kc|Alice@suite.test|true|"+testKeycloak+"|"+testAudience))
	if status != http.StatusOK {
		t.Fatalf("the exchange answered %d %v", status, got)
	}
	access, refresh := got["access_token"].(string), got["refresh_token"].(string)
	if tenant, person, ok := s.Caller(access); !ok || tenant != "acme" || person != "alice" || s.ScopeOf(access) != "read" {
		t.Errorf("the exchanged token acts as %s %s %v with %q", tenant, person, ok, s.ScopeOf(access))
	}
	status, renewed := post(t, s, url.Values{"grant_type": {GrantRefresh}, "refresh_token": {refresh}})
	if status != http.StatusOK || renewed["refresh_token"] == refresh || renewed["access_token"] == access {
		t.Fatalf("the refresh answered %d %v", status, renewed)
	}
	if _, _, ok := s.Caller(access); ok {
		t.Error("the access token before the refresh still acts")
	}
	if g := s.Grants("acme"); len(g) != 1 || g[0].Refreshes != 1 {
		t.Errorf("after one refresh the grants are %+v", g)
	}
	status, ended := post(t, s, url.Values{"grant_type": {GrantRefresh}, "refresh_token": {refresh}})
	if status != http.StatusBadRequest || ended["error"] != "invalid_grant" {
		t.Errorf("a replayed refresh token answered %d %v", status, ended)
	}
	if _, _, ok := s.Caller(renewed["access_token"].(string)); ok {
		t.Error("a replayed refresh token left the grant standing")
	}
	if g := s.Grants("acme"); len(g) != 0 {
		t.Errorf("the ended grant is still held: %+v", g)
	}
}

// jwtWith is a token of three parts whose payload is the claims given, unsigned, for the verifier to read its issuer.
func jwtWith(claims string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc([]byte(claims)) + "." + enc([]byte("signature"))
}

func TestKeycloakVerifiesOnlyTokensOfTheIssuersItTrusts(t *testing.T) {
	verify := Keycloak([]string{"https://keycloak.example/realms/suite"}, nil)
	for name, raw := range map[string]string{
		"no JWT":                      "stator_pat_acme_alice",
		"a payload that is no base64": "a.%%%.c",
		"no issuer":                   jwtWith(`{"sub":"alice"}`),
		"an untrusted issuer":         jwtWith(`{"iss":"https://evil.example/realms/suite"}`),
	} {
		if claims, err := verify(context.Background(), raw); err == nil || claims != nil {
			t.Errorf("%s was verified: %v", name, claims)
		}
	}
	if _, err := verify(context.Background(), jwtWith(`{"iss":"https://evil.example/realms/suite"}`)); err == nil || !strings.Contains(err.Error(), "not one the stub trusts") {
		t.Errorf("an untrusted issuer is refused with %v", err)
	}
}
