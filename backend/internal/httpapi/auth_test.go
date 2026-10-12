package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// fakeAuth knows one credential per principal and refuses "blocked" outright.
type fakeAuth map[string]*auth.Principal

var errBlocked = &APIError{Status: http.StatusForbidden, Code: "forbidden", Message: "This account has been deactivated."}

func (f fakeAuth) Authenticate(_ context.Context, credential string) (*auth.Principal, error) {
	if credential == "blocked" {
		return nil, errBlocked
	}
	if p, ok := f[credential]; ok {
		return p, nil
	}
	return nil, auth.ErrInvalidToken
}

// authenticated runs the chain's authenticate step and guards in front of a
// handler that reports what the context carries.
func authenticated(s *Server, guards ...func(http.Handler) http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(requestID, s.authenticate)
	r.With(guards...).Get("/", func(w http.ResponseWriter, r *http.Request) {
		org, _ := tenant.FromContext(r.Context())
		user, _ := db.UserFrom(r.Context())
		respondJSON(w, r, http.StatusOK, map[string]any{"org": org.Slug, "signedIn": PrincipalFrom(r.Context()) != nil, "actsFor": user.String()})
	})
	return r
}

func bearer(credential string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	return req
}

func TestAuthenticationScopesTheTenantAndThePerson(t *testing.T) {
	s := newServer(t)
	orgID, person := uuid.New(), uuid.New()
	s.Auth = fakeAuth{
		"in-org": {UserID: person, Org: &tenant.Org{ID: orgID, Slug: "acme"}, Role: auth.RoleMember},
		"no-org": {UserID: uuid.New()},
		"admin":  {UserID: uuid.New(), Org: &tenant.Org{ID: orgID, Slug: "acme"}, Role: auth.RoleAdmin},
	}

	_, body := serve(t, authenticated(s), bearer("in-org"))
	if body["org"] != "acme" || body["signedIn"] != true || body["actsFor"] != person.String() {
		t.Errorf("a member's request = %v, want scoped to acme and acting for them", body)
	}

	cookie := httptest.NewRequest(http.MethodGet, "/", nil)
	cookie.AddCookie(&http.Cookie{Name: s.CookieName, Value: "expired"})
	resp, body := serve(t, authenticated(s), cookie)
	if body["signedIn"] != false {
		t.Errorf("a dead credential should leave the caller anonymous: %v", body)
	}
	if c := resp.Cookies(); len(c) != 1 || c[0].Name != s.CookieName || c[0].MaxAge >= 0 {
		t.Errorf("a dead session cookie should be cleared, got %v", c)
	}

	resp, body = serve(t, authenticated(s), bearer("blocked"))
	if resp.StatusCode != http.StatusForbidden || errorOf(t, body)["code"] != "forbidden" {
		t.Errorf("a refused caller = %d %v", resp.StatusCode, body)
	}

	resp, body = serve(t, authenticated(s, requireAuth), bearer(""))
	if resp.StatusCode != http.StatusUnauthorized || errorOf(t, body)["code"] != "unauthorized" {
		t.Errorf("anonymous past requireAuth = %d %v", resp.StatusCode, body)
	}

	resp, body = serve(t, authenticated(s, requireOrg), bearer("no-org"))
	if resp.StatusCode != http.StatusBadRequest || errorOf(t, body)["code"] != "no_organization" {
		t.Errorf("no organization past requireOrg = %d %v", resp.StatusCode, body)
	}

	resp, body = serve(t, authenticated(s, requireAdmin), bearer("in-org"))
	if resp.StatusCode != http.StatusForbidden || errorOf(t, body)["code"] != "forbidden" {
		t.Errorf("a member past requireAdmin = %d %v", resp.StatusCode, body)
	}
	if resp, _ = serve(t, authenticated(s, requireAdmin), bearer("admin")); resp.StatusCode != http.StatusOK {
		t.Errorf("an administrator past requireAdmin = %d", resp.StatusCode)
	}
}

func TestASessionKeysItsOwnWrites(t *testing.T) {
	s, f := freshServer(t)
	session := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", nil)
	req.AddCookie(&http.Cookie{Name: ClientCookie, Value: "00112233445566778899aabbccddeeff"})
	req = req.WithContext(context.WithValue(req.Context(), ctxPrincipal, &auth.Principal{SessionID: &session}))

	rec := httptest.NewRecorder()
	s.readYourWrites(writing(7)).ServeHTTP(rec, req)
	if f.lsns["s:"+session.String()] != 7 || len(f.lsns) != 1 {
		t.Fatalf("the write should be the session's alone: %v", f.lsns)
	}
	if clientCookie(rec.Result()) != nil {
		t.Error("a caller with a session was handed a client cookie")
	}
}
