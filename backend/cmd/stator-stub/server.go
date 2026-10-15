package main

import (
	"encoding/json"
	"io"
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
	// maxBody bounds what the stub reads of a request.
	maxBody = 1 << 20
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
}

type stub struct {
	world  *world
	client *http.Client
	// backchannel maps an origin people open to the one the stub reaches it at.
	backchannel map[string]string
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

func newStub() *stub { return &stub{world: newWorld(), client: &http.Client{Timeout: webhookTimeout}} }

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
	mux.HandleFunc("PUT "+stubPrefix+"/capabilities", s.setCapabilities)
	mux.HandleFunc("GET "+stubPrefix+"/{tenant}/pages", s.control((*stub).listPages))
	mux.HandleFunc("POST "+stubPrefix+"/{tenant}/pages/{pageID}/publish", s.control((*stub).publishPage))
	mux.HandleFunc("POST "+stubPrefix+"/{tenant}/pages/{pageID}/move", s.control((*stub).movePage))
	mux.HandleFunc("DELETE "+stubPrefix+"/{tenant}/pages/{pageID}", s.control((*stub).trashPage))
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/webhook", s.control((*stub).setWebhook))
	mux.HandleFunc("GET "+stubPrefix+"/{tenant}/deliveries", s.control((*stub).listDeliveries))
	mux.HandleFunc("POST "+stubPrefix+"/{tenant}/webhooks", s.control((*stub).sendEvent))
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
