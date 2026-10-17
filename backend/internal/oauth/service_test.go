package oauth

import (
	"testing"
	"time"
)

func TestTheMetadataNamesEverythingFromTheAppsAddress(t *testing.T) {
	s := NewService(nil, "https://exciter.example/", "/api/v1", nil)
	if s.Resource() != "https://exciter.example/api/v1/mcp" || s.Issuer() != "https://exciter.example" ||
		s.ResourceMetadata() != "https://exciter.example/.well-known/oauth-protected-resource/api/v1/mcp" || s.ConsentPage() != "https://exciter.example/oauth/consent" {
		t.Errorf("the service is named %s, %s, %s", s.Issuer(), s.Resource(), s.ResourceMetadata())
	}
	m := s.Metadata()
	if m.TokenEndpoint != "https://exciter.example/api/v1/oauth/token" || m.AuthorizationEndpoint != "https://exciter.example/api/v1/oauth/authorize" ||
		len(m.CodeChallengeMethodsSupported) != 1 || m.CodeChallengeMethodsSupported[0] != ChallengeS256 || !m.ClientIDMetadataDocumentSupported ||
		!m.AuthorizationResponseIssParameterSupported {
		t.Errorf("the server reads %+v", m)
	}
	if r := s.ProtectedResource(); r.Resource != s.Resource() || len(r.AuthorizationServers) != 1 || r.AuthorizationServers[0] != s.Issuer() {
		t.Errorf("the resource reads %+v", r)
	}
	for _, asked := range []string{"https://exciter.example/api/v1/mcp", "https://exciter.example/api/v1/mcp/"} {
		if !s.isResource(asked) {
			t.Errorf("%s is not the resource", asked)
		}
	}
	if s.isResource("https://exciter.example/api/v1") || s.isResource("") {
		t.Error("another address is taken for the resource")
	}
}

func TestADocumentIsKeptAsItsCacheHeadersAllowAtMostADay(t *testing.T) {
	for header, want := range map[string]time.Duration{
		"":                           0,
		"max-age=300":                5 * time.Minute,
		"public, max-age=604800":     MaxMetadataAge,
		"no-store, max-age=300":      0,
		"no-cache":                   0,
		"s-maxage=60":                0,
		"private, MAX-AGE=60, other": time.Minute,
	} {
		if got := keepFor(header); got != want {
			t.Errorf("%q keeps it %v, want %v", header, got, want)
		}
	}
}
