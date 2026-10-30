package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
)

// Every theme route needs somebody signed in, and somebody outside the installation is not; either is told so
// in the envelope.
func TestThemeRoutesRefuseAnonymousCallers(t *testing.T) {
	s := newServer(t)
	s.Auth = fakeAuth{"switched-off": {UserID: uuid.New()}, "member": {UserID: uuid.New(), Role: auth.RoleMember}}
	h := s.Routes(nil)
	someID := uuid.NewString()
	for _, op := range operations {
		if op.tag != "themes" {
			continue
		}
		path := strings.NewReplacer("{themeID}", someID, "{assetID}", someID).Replace(op.path)
		resp, body := serve(t, h, httptest.NewRequest(op.method, APIPrefix+path, nil))
		if resp.StatusCode != http.StatusUnauthorized || errorOf(t, body)["code"] != "unauthorized" {
			t.Errorf("anonymous %s %s = %d %v, want 401", op.method, op.path, resp.StatusCode, body)
		}
		req := httptest.NewRequest(op.method, APIPrefix+path, nil)
		req.Header.Set("Authorization", "Bearer switched-off")
		resp, body = serve(t, h, req)
		if resp.StatusCode != http.StatusForbidden || errorOf(t, body)["code"] != "account_switched_off" {
			t.Errorf("%s %s outside the installation = %d %v, want 403", op.method, op.path, resp.StatusCode, body)
		}
	}

	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/themes/examples", nil)
	req.Header.Set("Authorization", "Bearer member")
	resp, body := serve(t, h, req)
	examples, _ := body["examples"].([]any)
	if resp.StatusCode != http.StatusOK || len(examples) != 2 {
		t.Fatalf("the examples for somebody signed in = %d %v", resp.StatusCode, body)
	}
}

// A server built without themes says so rather than failing on a nil service,
// and only an administrator gets as far as naming the organization's default.
func TestThemeRoutesWithoutTheServiceAndTheDefaultForMembers(t *testing.T) {
	s := newServer(t)
	s.Auth = fakeAuth{
		"member": {UserID: uuid.New(), Role: auth.RoleMember},
		"admin":  {UserID: uuid.New(), Role: auth.RoleMember, Granted: []string{auth.PermissionCreateProjects, auth.PermissionAdministerInstallation}},
	}
	h := s.Routes(nil)
	asked := func(credential, method, path string) (*http.Response, map[string]any) {
		req := httptest.NewRequest(method, APIPrefix+path, strings.NewReader(`{"themeId":null}`))
		req.Header.Set("Authorization", "Bearer "+credential)
		req.Header.Set("Content-Type", "application/json")
		return serve(t, h, req)
	}
	if resp, body := asked("member", http.MethodGet, "/themes"); resp.StatusCode != http.StatusServiceUnavailable || errorOf(t, body)["code"] != "themes_unavailable" {
		t.Errorf("themes without the service = %d %v, want 503", resp.StatusCode, body)
	}
	if resp, body := asked("member", http.MethodPut, "/themes/default"); resp.StatusCode != http.StatusForbidden || errorOf(t, body)["code"] != "forbidden" {
		t.Errorf("a member naming the default = %d %v, want 403", resp.StatusCode, body)
	}
	if resp, body := asked("admin", http.MethodPut, "/themes/default"); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("an administrator naming the default without the service = %d %v, want 503", resp.StatusCode, body)
	}
}
