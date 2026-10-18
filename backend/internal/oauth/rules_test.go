package oauth

import (
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestAVerifierMatchesOnlyTheChallengeMadeFromIt(t *testing.T) {
	verifier := oauth2.GenerateVerifier()
	challenge := oauth2.S256ChallengeFromVerifier(verifier)
	if !VerifyPKCE(challenge, verifier) {
		t.Fatal("the verifier does not match its own challenge")
	}
	for name, c := range map[string]struct{ challenge, verifier string }{
		"another verifier":   {challenge, oauth2.GenerateVerifier()},
		"the plain method":   {verifier, verifier},
		"a short verifier":   {oauth2.S256ChallengeFromVerifier("short"), "short"},
		"a padded challenge": {challenge + "=", verifier},
		"nothing":            {"", ""},
	} {
		if VerifyPKCE(c.challenge, c.verifier) {
			t.Errorf("%s is accepted", name)
		}
	}
}

func TestARedirectIsHttpsOrOnTheLoopback(t *testing.T) {
	for _, ok := range []string{"https://app.example.com/callback", "http://127.0.0.1:33418/cb", "http://localhost/cb", "http://[::1]:8080/cb", "https://x.example/cb?a=1"} {
		if !validRedirect(ok) {
			t.Errorf("%s is refused", ok)
		}
	}
	for _, bad := range []string{"http://app.example.com/cb", "https://x.example/cb#frag", "javascript:alert(1)", "/relative", "https://user@x.example/cb",
		"custom-scheme:/cb", "http://127.0.0.2.example.com/cb", ""} {
		if validRedirect(bad) {
			t.Errorf("%s is accepted", bad)
		}
	}
}

func TestARedirectMatchesExactlyButForALoopbackPort(t *testing.T) {
	registered := []string{"https://app.example.com/callback", "http://127.0.0.1:3000/cb"}
	for asked, want := range map[string]bool{
		"https://app.example.com/callback":       true,
		"https://app.example.com/callback/":      false,
		"https://app.example.com/callback?x=1":   false,
		"https://app.example.com:8443/callback":  false,
		"http://127.0.0.1:51234/cb":              true,
		"http://127.0.0.1/cb":                    true,
		"http://127.0.0.1:51234/other":           false,
		"http://localhost:51234/cb":              false,
		"https://evil.example.com/callback":      false,
		"http://127.0.0.1:51234/cb?next=https:/": false,
	} {
		if got := RedirectMatches(registered, asked); got != want {
			t.Errorf("%s matches %v, want %v", asked, got, want)
		}
	}
}

func TestARegistrationIsHeldToTheRulesAndGivenItsDefaults(t *testing.T) {
	in := ClientMetadataInput{RedirectURIs: []string{"http://127.0.0.1:4000/cb"}}
	if r := checkMetadata(&in); r != nil {
		t.Fatalf("a plain registration is refused: %v", r)
	}
	if in.TokenEndpointAuthMethod != AuthNone || len(in.GrantTypes) != 2 || in.ClientName != "127.0.0.1:4000" {
		t.Errorf("the defaults are %+v", in)
	}
	for name, c := range map[string]struct {
		in   ClientMetadataInput
		code string
	}{
		"no redirect":          {ClientMetadataInput{}, ErrInvalidRedirectURI},
		"too many":             {ClientMetadataInput{RedirectURIs: strings.Split(strings.Repeat("https://a.example/cb,", MaxRedirectURIs+1), ",")[:MaxRedirectURIs+1]}, ErrInvalidRedirectURI},
		"plain http":           {ClientMetadataInput{RedirectURIs: []string{"http://a.example/cb"}}, ErrInvalidRedirectURI},
		"an unknown method":    {ClientMetadataInput{RedirectURIs: []string{"https://a.example/cb"}, TokenEndpointAuthMethod: "private_key_jwt"}, ErrInvalidClientMetadata},
		"an unknown grant":     {ClientMetadataInput{RedirectURIs: []string{"https://a.example/cb"}, GrantTypes: []string{"password"}}, ErrInvalidClientMetadata},
		"refreshing alone":     {ClientMetadataInput{RedirectURIs: []string{"https://a.example/cb"}, GrantTypes: []string{GrantRefreshToken}}, ErrInvalidClientMetadata},
		"the implicit flow":    {ClientMetadataInput{RedirectURIs: []string{"https://a.example/cb"}, ResponseTypes: []string{"token"}}, ErrInvalidClientMetadata},
		"a long name":          {ClientMetadataInput{RedirectURIs: []string{"https://a.example/cb"}, ClientName: strings.Repeat("n", MaxClientNameLength+1)}, ErrInvalidClientMetadata},
		"a script for address": {ClientMetadataInput{RedirectURIs: []string{"https://a.example/cb"}, ClientURI: "javascript:alert(1)"}, ErrInvalidClientMetadata},
	} {
		if r := checkMetadata(&c.in); r == nil || r.Body.Error != c.code || !strings.HasSuffix(r.Body.ErrorDescription, ".") {
			t.Errorf("%s answers %+v, want %s with a sentence", name, r, c.code)
		}
	}
}

func TestAScopeIsReadAsEverythingReadingOrEvidenceAlone(t *testing.T) {
	for asked, want := range map[string][3]bool{
		"": {false, false, true}, "mcp": {false, false, true}, "mcp:read": {true, false, true}, "mcp mcp:read": {false, false, true},
		"evidence": {false, true, true}, "evidence mcp": {false, false, false}, "mcp:read evidence": {false, false, false},
		"admin": {false, false, false}, "mcp offline_access": {false, false, false},
	} {
		if sc, ok := scopeAsked(asked); sc.readOnly != want[0] || sc.evidence != want[1] || ok != want[2] {
			t.Errorf("%q reads %+v %v", asked, sc, ok)
		}
	}
	for sc, name := range map[scope]string{{}: ScopeMCP, {readOnly: true}: ScopeMCPRead, {evidence: true}: ScopeEvidence} {
		if sc.name() != name {
			t.Errorf("%+v is named %s, want %s", sc, sc.name(), name)
		}
	}
	if !IsMetadataClientID("https://client.example/oauth/metadata.json") || IsMetadataClientID("https://client.example") || IsMetadataClientID("0193") ||
		IsMetadataClientID("http://client.example/meta") {
		t.Error("a metadata document's address is told apart wrongly")
	}
}
