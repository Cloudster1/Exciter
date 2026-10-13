package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	apiPrefix  = "/api/v1"
	stubPrefix = "/_stub"
	// uploadBytes is what /auth/me says Armature takes; nothing is uploaded.
	uploadBytes = 25 << 20
	// scopeRead is Armature's only scope: a token without it may do whatever its owner may.
	scopeRead = "read"
)

// tokenCreated is when every stub token was made, so answers are the same on every run.
var tokenCreated = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

// route is one operation the stub serves. Path is Armature's, relative to
// /api/v1, which the contract test finds in Armature's document.
type route struct {
	Method, Path string
	// Public routes need no token.
	Public  bool
	handler func(*stub, *call)
}

// routes are the part of Armature's API Exciter calls; see
// docs/api-contract-m2.md. Each issue of M2 adds what it calls.
var routes = []route{
	{Method: "GET", Path: "/openapi.json", Public: true, handler: (*stub).openAPI},
	{Method: "GET", Path: "/auth/me", handler: (*stub).me},
}

type stub struct {
	world   *world
	openapi []byte
}

// call is one request as a person of a tenant.
type call struct {
	w      http.ResponseWriter
	r      *http.Request
	tenant *tenant
	person *person
}

func newStub(openapi []byte) *stub {
	return &stub{world: newWorld(), openapi: openapi}
}

// handler routes Armature's operations and the /_stub/ controls.
func (s *stub) handler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range routes {
		mux.HandleFunc(rt.Method+" "+apiPrefix+rt.Path, s.serve(rt))
	}
	mux.HandleFunc("GET "+stubPrefix+"/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("DELETE "+stubPrefix+"/{tenant}", func(w http.ResponseWriter, r *http.Request) {
		s.world.mu.Lock()
		delete(s.world.tenants, r.PathValue("tenant"))
		s.world.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		refuse(w, http.StatusNotFound, "not_found", "The stub does not serve this. Add it to cmd/armature-stub when Exciter starts calling it.")
	})
	return mux
}

func (s *stub) serve(rt route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &call{w: w, r: r}
		if !rt.Public {
			token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			s.world.mu.Lock()
			defer s.world.mu.Unlock()
			t, p, ok := s.world.caller(strings.TrimSpace(token))
			if !found || !ok {
				refuse(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.")
				return
			}
			c.tenant, c.person = t, p
		}
		rt.handler(s, c)
	}
}

func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func refuse(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]apiError{"error": {Code: code, Message: message}})
}

func (s *stub) openAPI(c *call) {
	c.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = c.w.Write(s.openapi)
}

type userView struct {
	ID       uuid.UUID `json:"id"`
	Email    string    `json:"email"`
	Name     string    `json:"name"`
	Timezone string    `json:"timezone"`
	Locale   string    `json:"locale"`
	IsActive bool      `json:"isActive"`
}

type orgView struct {
	ID   uuid.UUID `json:"id"`
	Slug string    `json:"slug"`
	Name string    `json:"name"`
}

type tokenView struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Scopes    []string  `json:"scopes"`
	CreatedAt time.Time `json:"createdAt"`
	Projects  []string  `json:"projects"`
}

func orgName(t *tenant) string { return "Stub " + t.Slug }

func roleOf(p *person) string {
	if p.Name == adminName {
		return "admin"
	}
	return "member"
}

func (s *stub) me(c *call) {
	p, t := c.person, c.tenant
	scopes := []string{}
	if p.ReadOnly {
		scopes = []string{scopeRead}
	}
	respond(c.w, http.StatusOK, map[string]any{
		"principal": map[string]any{
			"user": userView{ID: p.ID, Email: p.Email, Name: p.Display, Timezone: "UTC", Locale: "en", IsActive: true},
			"org":  orgView{ID: t.OrgID, Slug: t.Slug, Name: orgName(t)},
			"role": roleOf(p),
		},
		"limits": map[string]any{"uploadBytes": uploadBytes},
		"token":  tokenView{ID: idOf(t.Slug, "token", p.Name), Name: "Exciter", Scopes: scopes, CreatedAt: tokenCreated, Projects: []string{}},
	})
}
