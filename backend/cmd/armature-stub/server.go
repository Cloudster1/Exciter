package main

import (
	"encoding/json"
	"net/http"
	"slices"
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
	{Method: "GET", Path: "/meta", Public: true, handler: (*stub).meta},
	{Method: "GET", Path: "/auth/me", handler: (*stub).me},
	{Method: "GET", Path: "/access/me", handler: (*stub).access},
	{Method: "GET", Path: "/projects", handler: (*stub).projects},
	{Method: "GET", Path: "/issue-types", handler: (*stub).issueTypes},
	{Method: "GET", Path: "/issues", handler: (*stub).search},
	{Method: "POST", Path: "/issues", handler: (*stub).createIssue},
	{Method: "GET", Path: "/issues/{issueKey}", handler: (*stub).getIssue},
	{Method: "GET", Path: "/issues/{issueKey}/remote-links", handler: (*stub).listLinks},
	{Method: "POST", Path: "/issues/{issueKey}/remote-links", handler: (*stub).putLink},
	{Method: "DELETE", Path: "/issues/{issueKey}/remote-links/{remoteLinkID}", handler: (*stub).deleteLink},
	{Method: "GET", Path: "/themes/active", handler: (*stub).activeTheme},
	{Method: "GET", Path: "/themes/{themeID}/export", handler: (*stub).exportTheme},
}

type stub struct {
	world   *world
	openapi []byte
	client  *http.Client
	// backchannel maps an origin people open to the one the stub reaches it at.
	backchannel map[string]string
	// bins receive Exciter's own webhooks for the suites (#48).
	bins *bins
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

func newStub(openapi []byte) *stub {
	return &stub{world: newWorld(), openapi: openapi, client: &http.Client{Timeout: webhookTimeout}, bins: newBins()}
}

// finish posts what a call owes, once the world is unlocked.
func (s *stub) finish(c *call) {
	done := s.deliver(c.r.Context(), c.events)
	if c.answer != nil {
		c.answer(done)
	}
}

// handler routes Armature's operations and the /_stub/ controls.
func (s *stub) handler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range routes {
		mux.HandleFunc(rt.Method+" "+apiPrefix+rt.Path, s.serve(rt))
	}
	s.bins.routes(mux)
	mux.HandleFunc("GET "+stubPrefix+"/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("PUT "+stubPrefix+"/capabilities", s.setCapabilities)
	mux.HandleFunc("GET "+stubPrefix+"/{tenant}/remote-links", s.control((*stub).allLinks))
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/remote-links/outage", s.control((*stub).setOutage))
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/people/{person}/read-only-projects", s.control((*stub).setReads))
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/refused-summary", s.control((*stub).setRefused))
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/webhook", s.control((*stub).setWebhook))
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/people/{person}/theme", s.control((*stub).setTheme))
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/people/{person}/theme-delay", s.control((*stub).setThemeDelay))
	mux.HandleFunc("GET "+stubPrefix+"/{tenant}/deliveries", s.control((*stub).listDeliveries))
	mux.HandleFunc("POST "+stubPrefix+"/{tenant}/webhooks", s.control((*stub).sendEvent))
	mux.HandleFunc("PATCH "+stubPrefix+"/{tenant}/issues/{issueKey}", s.control((*stub).changeIssue))
	mux.HandleFunc("POST "+stubPrefix+"/{tenant}/issues/{issueKey}/move", s.control((*stub).moveIssue))
	mux.HandleFunc("DELETE "+stubPrefix+"/{tenant}/issues/{issueKey}", s.control((*stub).deleteIssue))
	mux.HandleFunc("DELETE "+stubPrefix+"/{tenant}/issues/{issueKey}/remote-links", s.control((*stub).takeOffLink))
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
		if rt.Public {
			rt.handler(s, c)
			return
		}
		if rt.Path == "/themes/active" {
			if delay := s.themeDelay(r); delay > 0 {
				select {
				case <-time.After(delay):
				case <-r.Context().Done():
					return
				}
			}
		}
		token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		s.world.mu.Lock()
		t, p, ok := s.world.caller(strings.TrimSpace(token))
		if !found || !ok {
			s.world.mu.Unlock()
			refuse(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.")
			return
		}
		c.tenant, c.person = t, p
		rt.handler(s, c)
		s.world.mu.Unlock()
		s.finish(c)
	}
}

func (s *stub) control(fn func(*stub, *call)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.world.mu.Lock()
		c := &call{w: w, r: r, tenant: s.world.tenant(r.PathValue("tenant"))}
		fn(s, c)
		s.world.mu.Unlock()
		s.finish(c)
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

// meta answers what any Armature answers anybody, with the capabilities a test left; like Armature, the commit
// and build time only for somebody signed in, which the stub never is here.
func (s *stub) meta(c *call) {
	s.world.mu.Lock()
	offered := slices.Clone(s.world.capabilities)
	s.world.mu.Unlock()
	respond(c.w, http.StatusOK, map[string]any{
		"product":      "armature",
		"build":        map[string]string{"version": "stub"},
		"apiVersion":   "1",
		"capabilities": offered,
	})
}

// setCapabilities sets what GET /meta lists; null puts back Armature's own list.
func (s *stub) setCapabilities(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		refuse(w, http.StatusUnprocessableEntity, "invalid", "Send {\"capabilities\": [...]}, or null for Armature's own.")
		return
	}
	if body.Capabilities == nil {
		body.Capabilities = slices.Clone(armatureCapabilities)
	}
	s.world.mu.Lock()
	s.world.capabilities = body.Capabilities
	s.world.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
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
		"token":  tokenView{ID: idOf(t.Slug, "token", p.Name), Name: "Exciter", Scopes: scopes, CreatedAt: seeded, Projects: []string{}},
	})
}
