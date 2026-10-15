package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const (
	apiPrefix  = "/api/v1"
	stubPrefix = "/_stub"
	// product and apiVersion are what Stator answers on GET /meta.
	product    = "stator"
	apiVersion = "1"
)

// route is one operation the stub serves. Path is Stator's, relative to
// /api/v1, which the contract test finds in Stator's document.
type route struct {
	Method, Path string
	// Public routes need no token.
	Public  bool
	handler func(*stub, *call)
}

// routes are the part of Stator's API Exciter calls; see
// docs/api-contract-m4.md. Each issue of M4 adds what it calls.
var routes = []route{
	{Method: "GET", Path: "/meta", Public: true, handler: (*stub).meta},
	{Method: "GET", Path: "/auth/me", handler: (*stub).me},
}

type stub struct {
	world *world
}

// call is one request as a person of a tenant.
type call struct {
	w      http.ResponseWriter
	r      *http.Request
	tenant *tenant
	person *person
}

func newStub() *stub { return &stub{world: newWorld()} }

// handler routes Stator's operations and the /_stub/ controls.
func (s *stub) handler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range routes {
		mux.HandleFunc(rt.Method+" "+apiPrefix+rt.Path, s.serve(rt))
	}
	mux.HandleFunc("GET "+stubPrefix+"/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("PUT "+stubPrefix+"/capabilities", s.setCapabilities)
	mux.HandleFunc("DELETE "+stubPrefix+"/{tenant}", func(w http.ResponseWriter, r *http.Request) {
		s.world.mu.Lock()
		delete(s.world.tenants, r.PathValue("tenant"))
		s.world.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		refuse(w, http.StatusNotFound, "not_found", "The stub does not serve this. Add it to cmd/stator-stub when Exciter starts calling it.")
	})
	return mux
}

func (s *stub) serve(rt route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &call{w: w, r: r}
		s.world.mu.Lock()
		defer s.world.mu.Unlock()
		if rt.Public {
			rt.handler(s, c)
			return
		}
		token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		t, p, ok := s.world.caller(strings.TrimSpace(token))
		if !found || !ok {
			refuse(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.")
			return
		}
		c.tenant, c.person = t, p
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

// meta answers what any Stator answers anybody, with the capabilities a test set.
func (s *stub) meta(c *call) {
	respond(c.w, http.StatusOK, map[string]any{
		"product":      product,
		"build":        map[string]string{"version": "stub"},
		"apiVersion":   apiVersion,
		"capabilities": s.world.capabilities,
	})
}

type userView struct {
	ID            uuid.UUID `json:"id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	Locale        string    `json:"locale"`
	ShowInReaders bool      `json:"showInReaders"`
}

func (s *stub) me(c *call) {
	p := c.person
	respond(c.w, http.StatusOK, map[string]any{
		"user":         userView{ID: p.ID, Email: p.Email, Name: p.Display, Locale: "en", ShowInReaders: true},
		"organization": map[string]any{"name": "Stub " + c.tenant.Slug, "role": "member", "guestSpace": nil},
	})
}

// setCapabilities sets what GET /meta lists, for every tenant at once since /meta names none; a test that changes
// it puts it back.
func (s *stub) setCapabilities(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		refuse(w, http.StatusUnprocessableEntity, "invalid", "Send {\"capabilities\": [...]}.")
		return
	}
	if body.Capabilities == nil {
		body.Capabilities = []string{}
	}
	s.world.mu.Lock()
	s.world.capabilities = body.Capabilities
	s.world.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
