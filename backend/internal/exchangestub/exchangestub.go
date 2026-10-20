// Package exchangestub is the token exchange (RFC 8693) as armature-stub and stator-stub serve it, after Armature's
// docs/oauth.md (#274): a partner app trades a person's access token from the shared Keycloak realm for that
// person's tokens, and refreshes them, rotating the refresh token. Only the stubs import it.
package exchangestub

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
)

// The grants and token type, as Armature names them.
const (
	GrantExchange   = "urn:ietf:params:oauth:grant-type:token-exchange"
	GrantRefresh    = "refresh_token"
	TokenTypeAccess = "urn:ietf:params:oauth:token-type:access_token"
	// DefaultAccessTTL is Armature's hour.
	DefaultAccessTTL = time.Hour
	maxFormBytes     = 16 << 10
)

// Claims are what the stub reads of a verified access token.
type Claims struct {
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified"`
	Name          string `json:"name"`
	AuthorizedBy  string `json:"azp"`
	Audience      []string
}

// Verifier checks a raw access token of the identity provider and reads its claims.
type Verifier func(ctx context.Context, raw string) (*Claims, error)

// Keycloak verifies against the published keys of any issuer in issuers, reached through backchannel, which maps
// the issuer's public origin to the one the stub reaches it at, as Exciter's EXCITER_OIDC_BACKCHANNEL does.
func Keycloak(issuers []string, backchannel map[string]string) Verifier {
	client := &http.Client{Timeout: 5 * time.Second, Transport: rewriting(backchannel)}
	var (
		mu    sync.Mutex
		known = map[string]*coreoidc.Provider{}
	)
	return func(ctx context.Context, raw string) (*Claims, error) {
		issuer, err := unverifiedIssuer(raw)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(issuers, issuer) {
			return nil, fmt.Errorf("the token's issuer %s is not one the stub trusts", issuer)
		}
		ctx = coreoidc.ClientContext(ctx, client)
		mu.Lock()
		provider := known[issuer]
		mu.Unlock()
		if provider == nil {
			if provider, err = coreoidc.NewProvider(ctx, issuer); err != nil {
				return nil, fmt.Errorf("ask %s for its keys: %w", issuer, err)
			}
			mu.Lock()
			known[issuer] = provider
			mu.Unlock()
		}
		verified, err := provider.Verifier(&coreoidc.Config{SkipClientIDCheck: true}).Verify(ctx, raw)
		if err != nil {
			return nil, errors.New("it is not a current token signed by the identity provider")
		}
		var claims Claims
		if err := verified.Claims(&claims); err != nil {
			return nil, err
		}
		claims.Audience = verified.Audience
		return &claims, nil
	}
}

// unverifiedIssuer reads iss before the signature is checked, to pick whose keys check it.
func unverifiedIssuer(raw string) (string, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "", errors.New("it is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("it is not a JWT")
	}
	var body struct {
		Issuer string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || body.Issuer == "" {
		return "", errors.New("it names no issuer")
	}
	return body.Issuer, nil
}

type rewrites map[string]string

func rewriting(pairs map[string]string) http.RoundTripper { return rewrites(pairs) }

func (r rewrites) RoundTrip(req *http.Request) (*http.Response, error) {
	if target, ok := r[req.URL.Scheme+"://"+req.URL.Host]; ok {
		if reach, err := url.Parse(target); err == nil && reach.Host != "" {
			clone := req.Clone(req.Context())
			clone.URL.Scheme, clone.URL.Host, clone.Host = reach.Scheme, reach.Host, req.URL.Host
			return http.DefaultTransport.RoundTrip(clone)
		}
	}
	return http.DefaultTransport.RoundTrip(req)
}

// App is a partner app a tenant of the stub registered, as an administrator does in Armature.
type App struct {
	ClientID         string `json:"clientId"`
	ClientSecret     string `json:"clientSecret"`
	KeycloakClientID string `json:"keycloakClientId"`
	// Scope is what the app may be given, space separated.
	Scope string `json:"scope"`
	// AccessTTLSeconds shortens the hour, so a test sees a refresh without waiting one.
	AccessTTLSeconds int `json:"accessTtlSeconds"`
}

// Grant is one person's tokens for one app.
type Grant struct {
	Tenant, Person string
	Access         string
	Refresh        string
	Scope          string
	ExpiresAt      time.Time
	// Rotated are refresh tokens this grant used up; one presented again ends the grant.
	Rotated []string
	// Refreshes counts how often the grant was refreshed, for the tests.
	Refreshes int
}

// Server keeps the apps and grants of one stub. Prefixes name the stub's tokens, Audience its own Keycloak client.
type Server struct {
	AccessPrefix, RefreshPrefix, Audience, Product string
	Verify                                         Verifier

	mu     sync.Mutex
	apps   map[string]App // by tenant
	grants map[string]*Grant
}

// NewServer is a stub's exchange; a nil verify refuses every exchange, as an installation without single sign-on does.
func NewServer(product, accessPrefix, refreshPrefix, audience string, verify Verifier) *Server {
	return &Server{Product: product, AccessPrefix: accessPrefix, RefreshPrefix: refreshPrefix, Audience: audience, Verify: verify,
		apps: map[string]App{}, grants: map[string]*Grant{}}
}

// SetApp registers the tenant's app, or with an empty client id removes it and every grant of it.
func (s *Server) SetApp(tenant string, app App) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, g := range s.grants {
		if g.Tenant == tenant {
			delete(s.grants, key)
		}
	}
	if app.ClientID == "" {
		delete(s.apps, tenant)
		return
	}
	if app.Scope == "" {
		app.Scope = "read"
	}
	s.apps[tenant] = app
}

// Forget removes a tenant's app and grants, as the stub's tenant reset does.
func (s *Server) Forget(tenant string) { s.SetApp(tenant, App{}) }

// Caller is the tenant and person a live access token acts for.
func (s *Server) Caller(token string) (tenant, person string, ok bool) {
	if !strings.HasPrefix(token, s.AccessPrefix) {
		return "", "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.grants[token]
	if g == nil || g.Access != token || !time.Now().Before(g.ExpiresAt) {
		return "", "", false
	}
	return g.Tenant, g.Person, true
}

// ScopeOf is the scope of a live access token.
func (s *Server) ScopeOf(token string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g := s.grants[token]; g != nil {
		return g.Scope
	}
	return ""
}

// Grants are a tenant's grants, for the stub's control routes.
func (s *Server) Grants(tenant string) []Grant {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Grant{}
	seen := map[*Grant]bool{}
	for _, g := range s.grants {
		if g.Tenant == tenant && !seen[g] {
			seen[g] = true
			out = append(out, *g)
		}
	}
	return out
}

// Answer is the token endpoint's success, and Refusal its error, in Armature's shapes.
type Answer struct {
	AccessToken     string `json:"access_token"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int    `json:"expires_in"`
	RefreshToken    string `json:"refresh_token"`
	Scope           string `json:"scope"`
	IssuedTokenType string `json:"issued_token_type,omitempty"`
}

type Refusal struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

// ServeToken answers POST /api/v1/oauth/token for the exchange and the refresh.
func (s *Server) ServeToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		refuse(w, http.StatusBadRequest, "invalid_request", "Send the request as a form.")
		return
	}
	id, secret, basic := r.BasicAuth()
	if basic {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant, app, found := "", App{}, false
	for t, a := range s.apps {
		if a.ClientID == id {
			tenant, app, found = t, a, true
		}
	}
	if !found || secret == "" || secret != app.ClientSecret {
		if basic {
			w.Header().Set("WWW-Authenticate", `Basic realm="`+s.Product+`"`)
		}
		refuse(w, http.StatusUnauthorized, "invalid_client", strings.ToUpper(s.Product[:1])+s.Product[1:]+" does not know this app, its secret is wrong, or nobody has allowed it yet.")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case GrantExchange:
		s.exchange(w, r, tenant, app)
	case GrantRefresh:
		s.refresh(w, r, tenant, app)
	default:
		refuse(w, http.StatusBadRequest, "unsupported_grant_type", "The stub offers "+GrantExchange+" and refresh_token.")
	}
}

func (s *Server) exchange(w http.ResponseWriter, r *http.Request, tenant string, app App) {
	if r.PostForm.Get("subject_token") == "" || r.PostForm.Get("subject_token_type") != TokenTypeAccess {
		refuse(w, http.StatusBadRequest, "invalid_request", "Send subject_token, an access token, with subject_token_type "+TokenTypeAccess+".")
		return
	}
	if app.KeycloakClientID == "" || s.Verify == nil {
		refuse(w, http.StatusBadRequest, "unauthorized_client", "This app may not exchange identity provider tokens. An administrator names its Keycloak client under Settings, Connected apps.")
		return
	}
	claims, err := s.Verify(r.Context(), r.PostForm.Get("subject_token"))
	switch {
	case err != nil:
		refuse(w, http.StatusBadRequest, "invalid_grant", "The identity provider's token was refused: "+err.Error()+".")
		return
	case claims.AuthorizedBy != app.KeycloakClientID:
		refuse(w, http.StatusBadRequest, "invalid_grant", fmt.Sprintf("The identity provider's token was refused: it was issued to %q, not to the app's Keycloak client.", claims.AuthorizedBy))
		return
	case !slices.Contains(claims.Audience, s.Audience):
		refuse(w, http.StatusBadRequest, "invalid_grant", "The identity provider's token was refused: its audience does not hold "+s.Audience+"; add an audience mapper to the app's Keycloak client.")
		return
	case claims.Email == "" || (claims.EmailVerified != nil && !*claims.EmailVerified):
		refuse(w, http.StatusBadRequest, "invalid_grant", "The identity provider's token carries no verified address.")
		return
	}
	person, _, _ := strings.Cut(strings.ToLower(claims.Email), "@")
	s.issue(w, &Grant{Tenant: tenant, Person: person, Scope: app.Scope}, app)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request, tenant string, app App) {
	presented := r.PostForm.Get("refresh_token")
	g := s.grants[presented]
	ended := Refusal{Error: "invalid_grant", Description: "This connection has ended. Connect again."}
	switch {
	case g == nil || g.Tenant != tenant:
		respond(w, http.StatusBadRequest, ended)
		return
	case g.Refresh != presented:
		// A rotated refresh token presented again ends the grant, as Armature does (RFC 9700 section 4.14.2).
		s.end(g)
		respond(w, http.StatusBadRequest, ended)
		return
	}
	g.Rotated = append(g.Rotated, presented)
	g.Refreshes++
	s.issue(w, g, app)
}

func (s *Server) end(g *Grant) {
	for key, each := range s.grants {
		if each == g {
			delete(s.grants, key)
		}
	}
}

// issue gives g a new access and refresh token, ending its previous access token; one person holds one grant.
func (s *Server) issue(w http.ResponseWriter, g *Grant, app App) {
	for key, each := range s.grants {
		if each != g && each.Tenant == g.Tenant && each.Person == g.Person {
			delete(s.grants, key)
		}
	}
	delete(s.grants, g.Access)
	ttl := DefaultAccessTTL
	if app.AccessTTLSeconds > 0 {
		ttl = time.Duration(app.AccessTTLSeconds) * time.Second
	}
	g.Access = s.AccessPrefix + g.Tenant + "_" + g.Person + "_" + random()
	g.Refresh = s.RefreshPrefix + g.Tenant + "_" + g.Person + "_" + random()
	g.ExpiresAt = time.Now().Add(ttl)
	s.grants[g.Access] = g
	s.grants[g.Refresh] = g
	for _, old := range g.Rotated {
		s.grants[old] = g
	}
	respond(w, http.StatusOK, Answer{AccessToken: g.Access, TokenType: "Bearer", ExpiresIn: int(ttl / time.Second),
		RefreshToken: g.Refresh, Scope: g.Scope})
}

func random() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func refuse(w http.ResponseWriter, status int, code, description string) {
	respond(w, status, Refusal{Error: code, Description: description})
}

func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// List reads a comma separated setting, blanks dropped.
func List(raw string) []string {
	out := []string{}
	for _, each := range strings.Split(raw, ",") {
		if each = strings.TrimSpace(each); each != "" {
			out = append(out, each)
		}
	}
	return out
}
