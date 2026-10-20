package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/oauth"
)

// Exciter as the authorization server of its MCP endpoint (#188, docs/api-contract-m7.md). The protocol's own
// endpoints answer in the RFCs' shape; the consent page's are the API's like any other.

// The client endpoints never read the session: a client authenticates itself, so a cookie the browser adds
// proves nothing and the same-site check has nothing to guard.
var clientEndpoints = map[string]bool{
	APIPrefix + "/oauth/register": true, APIPrefix + "/oauth/token": true, APIPrefix + "/oauth/revoke": true,
}

// maxFormBytes bounds a token or revocation request, a handful of short fields.
const maxFormBytes = 16 << 10

var (
	errOAuthOff = &APIError{Status: http.StatusServiceUnavailable, Code: "oauth_unavailable",
		Message: "OAuth is not set up on this server. Connect with a personal access token instead."}
	errMCPOnly = &APIError{Status: http.StatusUnauthorized, Code: "invalid_token",
		Message: "This token is for MCP alone. Use a personal access token, or sign in."}
	errOAuthSwitchedOff = &APIError{Status: http.StatusUnauthorized, Code: "invalid_token",
		Message: "OAuth connections are switched off on this server. Ask its administrators, or connect with a personal access token."}
)

func (s *Server) oauthOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.OAuth == nil {
			respondError(w, r, errOAuthOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// mountOAuthDiscovery serves the metadata where the RFCs put it, outside the API's prefix.
func (s *Server) mountOAuthDiscovery(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.oauthOn)
		r.Get("/.well-known/oauth-protected-resource", s.handleProtectedResource)
		r.Get("/.well-known/oauth-protected-resource"+APIPrefix+"/mcp", s.handleProtectedResource)
		r.Get("/.well-known/oauth-authorization-server", s.handleAuthorizationServer)
	})
}

// mountOAuth serves the protocol to clients, and the consent page's calls to whoever signed in.
func (s *Server) mountOAuth(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.oauthOn)
		r.Post("/oauth/register", s.handleOAuthRegister)
		r.Get("/oauth/authorize", s.handleOAuthAuthorize)
		r.Post("/oauth/token", s.handleOAuthToken)
		r.Post("/oauth/revoke", s.handleOAuthRevoke)
		r.With(requireAuth, requireSession).Get("/oauth/requests/{requestID}", s.handleGetOAuthRequest)
		r.With(requireAuth, requireSession).Post("/oauth/requests/{requestID}", s.handleDecideOAuthRequest)
		// Connected apps (#189): a person's own, and everybody's for the administrators, ended only from a session.
		r.With(requireAuth).Get("/oauth/grants", s.handleListOAuthGrants)
		r.With(requireAuth, requireSession).Delete("/oauth/grants/{grantID}", s.handleRevokeOAuthGrant)
		r.With(requireAdmin).Get("/oauth/grants/everyone", s.handleListEveryonesOAuthGrants)
		r.With(requireAdmin, requireSession).Delete("/oauth/grants/everyone/{grantID}", s.handleRevokeAnybodysOAuthGrant)
		// Partner apps (#273): registered by an administrator, also with a token, so a deployment's setup job wires
		// the tools together; never by a partner app's own token.
		r.Group(func(r chi.Router) {
			r.Use(requireAdmin, notPartnerApp)
			r.Get("/oauth/clients", s.handleListPartnerApps)
			r.Post("/oauth/clients", s.handleCreatePartnerApp)
			r.Patch("/oauth/clients/{clientID}", s.handleUpdatePartnerApp)
			r.Post("/oauth/clients/{clientID}/rotate-secret", s.handleRotatePartnerSecret)
			r.Delete("/oauth/clients/{clientID}", s.handleDeletePartnerApp)
		})
	})
	r.With(requireAdmin, requireSession, s.installationOn).Put("/installation/oauth-connections", s.handleSwitchOAuthConnections)
}

// whileOn answers 404 for what is not there while OAuth connections are switched off.
func (s *Server) whileOn(w http.ResponseWriter, r *http.Request) bool {
	on, err := s.OAuth.Enabled(r.Context())
	if err != nil {
		respondError(w, r, err)
		return false
	}
	if !on {
		respondError(w, r, ErrNotFound("OAuth connections are switched off on this server. Connect with a personal access token."))
		return false
	}
	return true
}

func (s *Server) handleProtectedResource(w http.ResponseWriter, r *http.Request) {
	if s.whileOn(w, r) {
		respondJSON(w, r, http.StatusOK, s.OAuth.ProtectedResource())
	}
}

func (s *Server) handleAuthorizationServer(w http.ResponseWriter, r *http.Request) {
	if s.whileOn(w, r) {
		respondJSON(w, r, http.StatusOK, s.OAuth.Metadata())
	}
}

// answerOAuth answers a protocol endpoint: what it made, or its refusal in the RFCs' shape. Nothing of it is kept
// by a cache, as it carries secrets.
func answerOAuth(w http.ResponseWriter, r *http.Request, status int, out any, err error, basic bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	var refused *oauth.Refusal
	switch {
	case errors.As(err, &refused):
		if refused.Status == http.StatusUnauthorized && basic {
			w.Header().Set("WWW-Authenticate", `Basic realm="exciter"`)
		}
		respondJSON(w, r, refused.Status, refused.Body)
	case err != nil:
		respondError(w, r, err)
	default:
		respondJSON(w, r, status, out)
	}
}

func (s *Server) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	var in oauth.ClientMetadataInput
	// A registration may carry metadata Exciter does not use, which RFC 7591 says to ignore, so unknown fields pass.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxFormBytes)).Decode(&in); err != nil {
		answerOAuth(w, r, 0, nil, &oauth.Refusal{Status: http.StatusBadRequest, Body: oauth.Error{Error: oauth.ErrInvalidClientMetadata,
			ErrorDescription: "Send the client's metadata as one JSON object."}}, false)
		return
	}
	made, err := s.OAuth.Register(r.Context(), in, s.clientIP(r))
	answerOAuth(w, r, http.StatusCreated, made, err, false)
}

func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	to, err := s.OAuth.Authorize(r.Context(), r.URL.Query())
	if err != nil {
		respondError(w, r, err)
		return
	}
	http.Redirect(w, r, to, http.StatusFound)
}

// readForm reads a client's form and who it says it is, preferring HTTP Basic, whose parts are form-encoded (RFC
// 6749, 2.3.1).
func readForm(w http.ResponseWriter, r *http.Request) (url.Values, oauth.ClientAuth, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		return nil, oauth.ClientAuth{}, &oauth.Refusal{Status: http.StatusBadRequest, Body: oauth.Error{Error: oauth.ErrInvalidRequest,
			ErrorDescription: "Send the request as a form, application/x-www-form-urlencoded."}}
	}
	f := r.PostForm
	if id, secret, ok := r.BasicAuth(); ok {
		id, idErr := url.QueryUnescape(id)
		secret, secretErr := url.QueryUnescape(secret)
		if idErr != nil || secretErr != nil {
			return nil, oauth.ClientAuth{}, &oauth.Refusal{Status: http.StatusUnauthorized, Body: oauth.Error{Error: oauth.ErrInvalidClient,
				ErrorDescription: "The client's id and secret in HTTP Basic are not form-encoded."}}
		}
		return f, oauth.ClientAuth{ID: id, Secret: secret, Basic: true}, nil
	}
	return f, oauth.ClientAuth{ID: f.Get("client_id"), Secret: f.Get("client_secret")}, nil
}

func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	f, who, err := readForm(w, r)
	if err != nil {
		answerOAuth(w, r, 0, nil, err, false)
		return
	}
	out, err := s.OAuth.Token(r.Context(), oauth.TokenRequest{
		GrantType: f.Get("grant_type"), Code: f.Get("code"), RedirectURI: f.Get("redirect_uri"), CodeVerifier: f.Get("code_verifier"),
		RefreshToken: f.Get("refresh_token"), Resource: f.Get("resource"), Scope: f.Get("scope"),
		SubjectToken: f.Get("subject_token"), SubjectTokenType: f.Get("subject_token_type"), RequestedTokenType: f.Get("requested_token_type"),
	}, who)
	answerOAuth(w, r, http.StatusOK, out, err, who.Basic)
}

func (s *Server) handleOAuthRevoke(w http.ResponseWriter, r *http.Request) {
	f, who, err := readForm(w, r)
	if err == nil {
		err = s.OAuth.Revoke(r.Context(), oauth.RevokeRequest{Token: f.Get("token"), TokenTypeHint: f.Get("token_type_hint")}, who)
	}
	if err != nil {
		answerOAuth(w, r, 0, nil, err, who.Basic)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}

// oauthError maps the consent's refusals; nil when err is none of theirs.
func oauthError(err error) *APIError {
	switch {
	case errors.Is(err, oauth.ErrNoSuchGrant):
		return ErrNotFound("That connection was not found. It may have been ended already; reload the list.")
	case errors.Is(err, oauth.ErrNoRequest):
		return ErrNotFound("The request to connect has expired or was answered already. Start connecting from the client again.")
	case errors.Is(err, oauth.ErrWiderThanAsked):
		return ErrValidation(map[string]string{"readOnly": "The client asked only to read, so it can only be granted reading."})
	case errors.Is(err, auth.ErrTokenProjects):
		return ErrValidation(map[string]string{"projects": "A connection names at most 50 projects. Leave them all out for one that reaches every project you do."})
	case errors.Is(err, auth.ErrNoSuchProject):
		return ErrValidation(map[string]string{"projects": "A connection can only name projects you can already see. Check the keys."})
	}
	return nil
}

// consentError speaks of a connection where the token's sentences, for the same refusals, speak of a token.
func consentError(err error) error {
	if apiErr := oauthError(err); apiErr != nil {
		return apiErr
	}
	return err
}

func (s *Server) handleGetOAuthRequest(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "requestID", "request")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	req, err := s.OAuth.Request(r.Context(), id)
	if err != nil {
		respondError(w, r, consentError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"request": req})
}

func (s *Server) handleDecideOAuthRequest(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "requestID", "request")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var in oauth.ConsentInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	to, lsn, err := s.OAuth.Decide(r.Context(), userFrom(r), id, in, s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, consentError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"redirectTo": to})
}

func (s *Server) handleSwitchOAuthConnections(w http.ResponseWriter, r *http.Request) {
	var req oauthConnectionsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	inst, lsn, err := s.Installation.SwitchOAuth(r.Context(), userFrom(r), req.Enabled)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, installationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"installation": inst})
}

// principalOf reads who a credential acts for: an access token only at MCP and for the calls it carries, an
// evidence one at the routes evidenceToken lets it reach, or a partner app's wherever a personal token goes.
func (s *Server) principalOf(r *http.Request, secret string) (*auth.Principal, error) {
	if !oauth.IsAccessToken(secret) {
		return s.Auth.Authenticate(r.Context(), secret)
	}
	if s.OAuth == nil {
		return nil, auth.ErrInvalidToken
	}
	p, err := s.OAuth.Authenticate(r.Context(), secret)
	if errors.Is(err, oauth.ErrSwitchedOff) {
		return nil, errOAuthSwitchedOff
	}
	if err == nil && r.URL.Path != mcpPath && !inProcessCall(r.Context()) && !p.Evidence() && !p.PartnerApp {
		return nil, errMCPOnly
	}
	return p, err
}

// requireMCPAuth is requireAuth for MCP, whose 401 tells a client where to learn how to be let in (RFC 9728).
func (s *Server) requireMCPAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if PrincipalFrom(r.Context()) != nil {
			next.ServeHTTP(w, r)
			return
		}
		if s.OAuth != nil {
			if on, err := s.OAuth.Enabled(r.Context()); err == nil && on {
				challenge := `Bearer resource_metadata="` + s.OAuth.ResourceMetadata() + `", scope="` + oauth.ScopeMCP + `"`
				if credentialFrom(r, s.CookieName) != "" {
					challenge += `, error="invalid_token"`
				}
				w.Header().Set("WWW-Authenticate", challenge)
			}
		}
		respondError(w, r, ErrUnauthorized(""))
	})
}

type ctxKeyInProcess struct{}

// withinProcess marks a call MCP makes in this process, which an access token is good for.
func withinProcess(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyInProcess{}, true)
}

func inProcessCall(ctx context.Context) bool {
	on, _ := ctx.Value(ctxKeyInProcess{}).(bool)
	return on
}

// isClientEndpoint says whether a path is one a client calls with its own credentials, never the session's.
func isClientEndpoint(path string) bool { return clientEndpoints[strings.TrimRight(path, "/")] }

func (s *Server) handleListOAuthGrants(w http.ResponseWriter, r *http.Request) {
	grants, err := s.OAuth.Grants(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"grants": grants})
}

func (s *Server) handleListEveryonesOAuthGrants(w http.ResponseWriter, r *http.Request) {
	grants, err := s.OAuth.EveryonesGrants(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"grants": grants})
}

func (s *Server) handleRevokeOAuthGrant(w http.ResponseWriter, r *http.Request) {
	s.endGrant(w, r, s.OAuth.EndGrant)
}

func (s *Server) handleRevokeAnybodysOAuthGrant(w http.ResponseWriter, r *http.Request) {
	s.endGrant(w, r, s.OAuth.EndAnybodysGrant)
}

func (s *Server) endGrant(w http.ResponseWriter, r *http.Request, end func(context.Context, uuid.UUID, uuid.UUID, string) (db.LSN, error)) {
	id, apiErr := pathUUID(r, "grantID", "connection")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := end(r.Context(), userFrom(r), id, s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var errPartnerManagesPartners = ErrForbidden("A partner app cannot manage partner apps. Do it in Exciter, signed in as an administrator.")

// notPartnerApp keeps a partner app's token from registering partners, which would hand it secrets of its own.
func notPartnerApp(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := PrincipalFrom(r.Context()); p != nil && p.PartnerApp {
			respondError(w, r, errPartnerManagesPartners)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// partnerError maps a partner app's refusals.
func partnerError(err error) error {
	var field *oauth.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, oauth.ErrNoSuchPartner):
		return ErrNotFound("That partner app was not found. It may have been removed already; reload the list.")
	}
	return err
}

func (s *Server) handleListPartnerApps(w http.ResponseWriter, r *http.Request) {
	apps, err := s.OAuth.PartnerApps(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"clients": apps})
}

func (s *Server) handleCreatePartnerApp(w http.ResponseWriter, r *http.Request) {
	var in oauth.PartnerAppInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	app, lsn, err := s.OAuth.CreatePartnerApp(r.Context(), in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, partnerError(err))
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"client": app})
}

func (s *Server) handleUpdatePartnerApp(w http.ResponseWriter, r *http.Request) {
	var in oauth.PartnerAppInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	app, lsn, err := s.OAuth.UpdatePartnerApp(r.Context(), chi.URLParam(r, "clientID"), in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, partnerError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"client": app})
}

func (s *Server) handleRotatePartnerSecret(w http.ResponseWriter, r *http.Request) {
	app, lsn, err := s.OAuth.RotatePartnerSecret(r.Context(), chi.URLParam(r, "clientID"))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, partnerError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"client": app})
}

func (s *Server) handleDeletePartnerApp(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.OAuth.DeletePartnerApp(r.Context(), chi.URLParam(r, "clientID"))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, partnerError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
