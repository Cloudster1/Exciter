package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/testorg"
)

const unitTestToken = "a token for the unit tests"

// fakeTestOrgs answers without a database, so a test sees what reached it.
type fakeTestOrgs struct{ created, deleted int }

func (f *fakeTestOrgs) Create(_ context.Context, label string) (*testorg.Org, error) {
	f.created++
	return &testorg.Org{ID: uuid.New(), Slug: label + "-0123abcd"}, nil
}

func (f *fakeTestOrgs) Delete(context.Context, string) error {
	f.deleted++
	return nil
}

// Sessions neither open the test endpoints nor refuse them: a deactivated
// account or a session on a foreign origin still comes down to the test token alone.
func TestTestEndpointsIgnoreSessions(t *testing.T) {
	s := newServer(t)
	sessionID := uuid.New()
	s.Auth = fakeAuth{"session": {UserID: uuid.New(), SessionID: &sessionID, Role: auth.RoleOwner}}
	orgs := &fakeTestOrgs{}
	s.TestOrgs = orgs
	s.TestToken = unitTestToken
	h := s.Routes(nil)

	withCredential := func(req *http.Request, credential string) *http.Request {
		if credential != "" {
			req.AddCookie(&http.Cookie{Name: s.CookieName, Value: credential})
			req.Header.Set("Origin", "http://elsewhere.test")
		}
		return req
	}
	credentials := []string{"", "blocked", "session", "no such session"}
	for _, credential := range credentials {
		for _, req := range testRequests() {
			resp, body := serve(t, h, withCredential(req, credential))
			if resp.StatusCode != http.StatusUnauthorized || errorOf(t, body)["code"] != "unauthorized" {
				t.Errorf("%s %s with %q and no test token = %d %v, want 401", req.Method, req.URL.Path, credential, resp.StatusCode, body)
			}
		}
		for i, req := range testRequests() {
			req.Header.Set(TestTokenHeader, unitTestToken)
			req.Header.Set("Content-Type", "application/json")
			want := []int{http.StatusCreated, http.StatusNoContent}[i]
			if resp, body := serve(t, h, withCredential(req, credential)); resp.StatusCode != want {
				t.Errorf("%s %s with %q and the test token = %d %v, want %d", req.Method, req.URL.Path, credential, resp.StatusCode, body, want)
			}
		}
	}
	if orgs.created != len(credentials) || orgs.deleted != len(credentials) {
		t.Fatalf("reached the service %d and %d times, want %d each", orgs.created, orgs.deleted, len(credentials))
	}
}

func testRequests() []*http.Request {
	return []*http.Request{
		httptest.NewRequest(http.MethodPost, APIPrefix+"/test/orgs", strings.NewReader(`{"label":"x"}`)),
		httptest.NewRequest(http.MethodDelete, APIPrefix+"/test/orgs/x-1234", nil),
	}
}

// Off, the test endpoints are paths like any other that does not exist, even
// to a caller who knows the token.
func TestTestEndpointsAreNotFoundWhenOff(t *testing.T) {
	s := newServer(t)
	s.TestToken = unitTestToken
	h := s.Routes(nil)
	for _, req := range testRequests() {
		req.Header.Set(TestTokenHeader, unitTestToken)
		resp, body := serve(t, h, req)
		if resp.StatusCode != http.StatusNotFound || errorOf(t, body)["code"] != "not_found" {
			t.Errorf("%s %s with the endpoints off = %d %v, want 404", req.Method, req.URL.Path, resp.StatusCode, body)
		}
	}
}

func TestTestEndpointsRefuseAWrongOrMissingToken(t *testing.T) {
	s := newServer(t)
	s.TestOrgs = &fakeTestOrgs{}
	s.TestToken = unitTestToken
	h := s.Routes(nil)
	for _, token := range []string{"", "wrong", unitTestToken + "x"} {
		for _, req := range testRequests() {
			if token != "" {
				req.Header.Set(TestTokenHeader, token)
			}
			resp, body := serve(t, h, req)
			if resp.StatusCode != http.StatusUnauthorized || errorOf(t, body)["code"] != "unauthorized" {
				t.Errorf("%s %s with token %q = %d %v, want 401", req.Method, req.URL.Path, token, resp.StatusCode, body)
			}
		}
	}

	s.TestToken = ""
	h = s.Routes(nil)
	req := testRequests()[0]
	req.Header.Set(TestTokenHeader, "")
	if resp, _ := serve(t, h, req); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("with no token configured an empty one got in: %d", resp.StatusCode)
	}
}

// On, the router serves exactly the public table and the test one, and the
// published document still names only the public table.
func TestTestRoutesAreServedWhenOnAndNeverDocumented(t *testing.T) {
	s := newServer(t)
	s.TestOrgs = &fakeTestOrgs{}
	served := routedBy(t, s)
	want := map[string]bool{}
	for _, op := range append(append([]operation{}, operations...), testOperations...) {
		want[op.method+" "+op.path] = true
	}
	for key := range served {
		if !want[key] {
			t.Errorf("%s is served but in neither table", key)
		}
	}
	for key := range want {
		if !served[key] {
			t.Errorf("%s is in a table but not served", key)
		}
	}
	for path := range Spec().Paths {
		if strings.HasPrefix(path, "/test/") {
			t.Errorf("the OpenAPI document names the test path %s", path)
		}
	}
}
