package main

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/exchangestub"
)

const (
	apiPrefix  = "/api/v1"
	stubPrefix = "/_stub"
	// product and apiVersion are what Stator answers on GET /meta.
	product    = "stator"
	apiVersion = "1"
	// maxBody bounds what the stub reads of a request.
	maxBody = 1 << 20
	// The stub's own client in the shared realm, which an exchanged token's audience must hold, and its tokens,
	// named as Stator names its others until its #461 says.
	keycloakClient     = "stator"
	accessTokenPrefix  = "stator_oat_"
	refreshTokenPrefix = "stator_ort_"
	defaultAppScope    = "read write"
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
	{Method: "GET", Path: "/pages/{pageID}", handler: (*stub).getPage},
	{Method: "GET", Path: "/search/quick", handler: (*stub).quickSearch},
	{Method: "GET", Path: "/pages/{pageID}/remote-links", handler: (*stub).listLinks},
	{Method: "POST", Path: "/pages/{pageID}/remote-links", handler: (*stub).putLink},
	{Method: "DELETE", Path: "/pages/{pageID}/remote-links/{linkID}", handler: (*stub).deleteLink},
}

type stub struct {
	world  *world
	client *http.Client
	// backchannel maps an origin people open to the one the stub reaches it at.
	backchannel map[string]string
	// exchange serves the token exchange for a partner app a test registered (#274).
	exchange *exchangestub.Server
}

// call is one request as a person of a tenant.
type call struct {
	w      http.ResponseWriter
	r      *http.Request
	tenant *tenant
	person *person
	// events are the webhooks the call owes, posted before it answers; answer, when set, answers after them.
	events []event
	answer func([]delivery)
}

func newStub(verify exchangestub.Verifier) *stub {
	return &stub{world: newWorld(), client: &http.Client{Timeout: webhookTimeout},
		exchange: exchangestub.NewServer(product, accessTokenPrefix, refreshTokenPrefix, keycloakClient, verify)}
}

// setApp registers the tenant's partner app, as an administrator does in Stator; an empty client id removes it.
func (s *stub) setApp(c *call) {
	var app exchangestub.App
	if !decode(c, &app) {
		return
	}
	if app.Scope == "" {
		app.Scope = defaultAppScope
	}
	s.exchange.SetApp(c.tenant.Slug, app)
	c.w.WriteHeader(http.StatusNoContent)
}

// grants lists the tenant's grants from the exchange, with how often each was refreshed.
func (s *stub) grants(c *call) {
	out := []map[string]any{}
	for _, g := range s.exchange.Grants(c.tenant.Slug) {
		out = append(out, map[string]any{"person": g.Person, "scope": g.Scope, "refreshes": g.Refreshes, "expiresAt": g.ExpiresAt})
	}
	respond(c.w, http.StatusOK, map[string]any{"grants": out})
}

// finish posts what a call owes, once the world is unlocked.
func (s *stub) finish(c *call) {
	done := s.deliver(c.r.Context(), c.events)
	if c.answer != nil {
		c.answer(done)
	}
}

// handler routes Stator's operations and the /_stub/ controls.
func (s *stub) handler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range routes {
		mux.HandleFunc(rt.Method+" "+apiPrefix+rt.Path, s.serve(rt))
	}
	mux.HandleFunc("GET "+stubPrefix+"/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	// Stator's token exchange is being built in its #461, so its document has no such route yet and the route is kept
	// out of routes, which the contract test holds to the document; it answers as Armature's does (docs/oauth.md).
	mux.HandleFunc("POST "+apiPrefix+"/oauth/token", s.exchange.ServeToken)
	mux.HandleFunc("PUT "+stubPrefix+"/capabilities", s.setCapabilities)
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/oauth-app", s.control((*stub).setApp))
	mux.HandleFunc("GET "+stubPrefix+"/{tenant}/oauth-grants", s.control((*stub).grants))
	mux.HandleFunc("GET "+stubPrefix+"/{tenant}/pages", s.control((*stub).listPages))
	mux.HandleFunc("POST "+stubPrefix+"/{tenant}/pages/{pageID}/publish", s.control((*stub).publishPage))
	mux.HandleFunc("POST "+stubPrefix+"/{tenant}/pages/{pageID}/move", s.control((*stub).movePage))
	mux.HandleFunc("DELETE "+stubPrefix+"/{tenant}/pages/{pageID}", s.control((*stub).trashPage))
	mux.HandleFunc("DELETE "+stubPrefix+"/{tenant}/pages/{pageID}/remote-links", s.control((*stub).takeOffLink))
	mux.HandleFunc("GET "+stubPrefix+"/{tenant}/remote-links", s.control((*stub).allLinks))
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/remote-links/outage", s.control((*stub).setOutage))
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/webhook", s.control((*stub).setWebhook))
	mux.HandleFunc("GET "+stubPrefix+"/{tenant}/deliveries", s.control((*stub).listDeliveries))
	mux.HandleFunc("POST "+stubPrefix+"/{tenant}/webhooks", s.control((*stub).sendEvent))
	mux.HandleFunc("DELETE "+stubPrefix+"/{tenant}", func(w http.ResponseWriter, r *http.Request) {
		s.world.mu.Lock()
		delete(s.world.tenants, r.PathValue("tenant"))
		s.world.mu.Unlock()
		s.exchange.Forget(r.PathValue("tenant"))
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
		if rt.Public {
			rt.handler(s, c)
			s.world.mu.Unlock()
			return
		}
		token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		token = strings.TrimSpace(token)
		t, p, ok := s.world.caller(token)
		if slug, name, exchanged := s.exchange.Caller(token); exchanged {
			t = s.world.tenant(slug)
			p, ok = t.person(name), true
		}
		if !found || !ok {
			s.world.mu.Unlock()
			refuse(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.")
			return
		}
		c.tenant, c.person = t, p
		rt.handler(s, c)
		s.world.mu.Unlock()
		// A removal is announced after its answer, as Stator's outbox sends it once the change is committed.
		s.finish(c)
	}
}

// control serves a /_stub/ control of one tenant, posting the webhooks it owes once the world is unlocked.
func (s *stub) control(fn func(*stub, *call)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.world.mu.Lock()
		c := &call{w: w, r: r, tenant: s.world.tenant(r.PathValue("tenant"))}
		fn(s, c)
		s.world.mu.Unlock()
		s.finish(c)
	}
}

func decode(c *call, into any) bool {
	dec := json.NewDecoder(io.LimitReader(c.r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		refuse(c.w, http.StatusBadRequest, "bad_request", "The request body is not valid JSON: "+err.Error()+".")
		return false
	}
	return true
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

// setCapabilities sets what GET /meta lists, for every tenant at once since /meta names none, null for Stator's own;
// a test that changes it puts it back.
func (s *stub) setCapabilities(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		refuse(w, http.StatusUnprocessableEntity, "invalid", "Send {\"capabilities\": [...]}, or null for Stator's own.")
		return
	}
	if body.Capabilities == nil {
		body.Capabilities = slices.Clone(statorCapabilities)
	}
	s.world.mu.Lock()
	s.world.capabilities = body.Capabilities
	s.world.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
