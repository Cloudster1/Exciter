package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/oidc"
)

// Signing in through an identity provider: the start and the callback come before anybody is signed in, the
// configuration behind them and its group roles are the administrators'.

// handleOIDCStart sends the browser to the installation's provider.
func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil || s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	// The browser arrived by navigation, so a refusal sends it back to the
	// sign-in page with a reason it can show, like the callback does.
	org, err := s.Accounts.InstallationOrg(r.Context())
	if err != nil {
		// An installation nobody set up has no provider either.
		s.landAfterSignIn(w, r, "", signInFailure(oidc.ErrNotConfigured))
		return
	}
	target, err := s.OIDC.Start(r.Context(), org.ID, safeRedirect(r.URL.Query().Get("next")))
	if err != nil {
		loggerFrom(r.Context()).Warn("a sign-in through an identity provider could not start", "error", err, "org_id", org.ID)
		s.landAfterSignIn(w, r, "", signInFailure(err))
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// handleOIDCCallback finishes a sign-in with a redirect rather than JSON: the
// caller is the browser following the provider, not the client's fetch.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil || s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	query := r.URL.Query()
	if refused := query.Get("error"); refused != "" {
		loggerFrom(r.Context()).Info("the identity provider refused a sign-in", "error", refused, "description", query.Get("error_description"))
		s.landAfterSignIn(w, r, "", "denied")
		return
	}

	orgID, identity, redirect, err := s.OIDC.Exchange(r.Context(), query.Get("state"), query.Get("code"))
	if err != nil {
		loggerFrom(r.Context()).Warn("a sign-in through an identity provider failed", "error", err)
		s.landAfterSignIn(w, r, "", signInFailure(err))
		return
	}
	session, err := s.OIDC.SignIn(r.Context(), orgID, identity, s.Accounts.SessionTTL(), r.UserAgent(), s.clientIP(r))
	if err != nil {
		loggerFrom(r.Context()).Warn("a verified identity could not be signed in", "error", err, "subject", identity.Subject)
		s.landAfterSignIn(w, r, "", signInFailure(err))
		return
	}
	loggerFrom(r.Context()).Info("signed in through an identity provider", "user_id", session.UserID, "org_id", orgID, "role", session.Role,
		"groups_joined", session.Joined, "groups_left", session.Left)
	s.noteSession(r.Context(), session.SessionID, session.LSN)
	s.setSessionCookie(w, session.Secret, session.ExpiresAt)
	s.landAfterSignIn(w, r, redirect, "")
}

// signInFailure names a refusal for the sign-in page to explain, without
// handing back anything about the tenant that was asked for.
func signInFailure(err error) string {
	switch {
	case errors.Is(err, oidc.ErrNotAMember):
		return "not_a_member"
	case errors.Is(err, oidc.ErrNoEmail):
		return "no_email"
	case errors.Is(err, oidc.ErrEmailUnverified):
		return "unverified_email"
	case errors.Is(err, oidc.ErrUnknownLogin):
		return "expired"
	case errors.Is(err, oidc.ErrNotConfigured):
		return "not_configured"
	case errors.Is(err, oidc.ErrUnreachable):
		return "unreachable"
	case errors.Is(err, auth.ErrSwitchedOff):
		return "inactive"
	default:
		return "failed"
	}
}

// landAfterSignIn sends the browser into the app, where it was heading or to
// the sign-in page with a reason it can show.
func (s *Server) landAfterSignIn(w http.ResponseWriter, r *http.Request, redirect, failure string) {
	target := "/"
	if redirect != "" {
		target = redirect
	}
	if failure != "" {
		target = "/login?sso=" + url.QueryEscape(failure)
	}
	http.Redirect(w, r, s.AppBaseURL+target, http.StatusFound)
}

// safeRedirect keeps a redirect inside this application. An open redirect on a
// sign-in endpoint is how a convincing phishing link gets made.
func safeRedirect(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n") {
		return ""
	}
	return next
}

// providerView is the settings page's whole answer: the provider, and the
// callback address to register with it, which only the server knows.
type providerView struct {
	Provider    *oidc.Provider `json:"provider"`
	CallbackURL string         `json:"callbackUrl"`
}

// handleGetOIDCProvider returns the organization's provider. The client secret
// is never included, only whether one is stored.
func (s *Server) handleGetOIDCProvider(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		respondError(w, r, errSignInOff)
		return
	}
	found, err := s.OIDC.Provider(r.Context())
	if err != nil && !errors.Is(err, oidc.ErrNotConfigured) {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, providerView{Provider: found, CallbackURL: s.OIDCCallbackURL})
}

type saveOIDCProviderRequest struct {
	Issuer   string `json:"issuer"`
	ClientID string `json:"clientId"`
	// ClientSecret left out keeps the stored one.
	ClientSecret string `json:"clientSecret,omitempty"`
	GroupsClaim  string `json:"groupsClaim,omitempty"`
	Scopes       string `json:"scopes,omitempty"`
	CreateGroups bool   `json:"createGroups"`
	Enabled      bool   `json:"enabled"`
}

func (s *Server) handleSaveOIDCProvider(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		respondError(w, r, errSignInOff)
		return
	}
	var req saveOIDCProviderRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	saved, lsn, err := s.OIDC.Save(audit.WithIP(r.Context(), s.clientIP(r)), oidc.Provider{
		Issuer:       req.Issuer,
		ClientID:     req.ClientID,
		ClientSecret: req.ClientSecret,
		GroupsClaim:  req.GroupsClaim,
		Scopes:       req.Scopes,
		CreateGroups: req.CreateGroups,
		Enabled:      req.Enabled,
	})
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, providerView{Provider: saved, CallbackURL: s.OIDCCallbackURL})
}

type setGroupRoleRequest struct {
	// Group is a value of the provider's groups claim.
	Group string `json:"group"`
	// Role is what the group grants: member, or admin.
	Role auth.OrgRole `json:"role"`
}

func (s *Server) handleListGroupRoles(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		respondError(w, r, errSignInOff)
		return
	}
	mapping, err := s.OIDC.GroupRoles(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"groupRoles": mapping})
}

func (s *Server) handleSetGroupRole(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		respondError(w, r, errSignInOff)
		return
	}
	var req setGroupRoleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	saved, lsn, err := s.OIDC.SetGroupRole(r.Context(), req.Group, req.Role, userFrom(r), s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"groupRole": saved})
}

func (s *Server) handleRemoveGroupRole(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		respondError(w, r, errSignInOff)
		return
	}
	id, apiErr := pathUUID(r, "groupRoleID", "group role's")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.OIDC.RemoveGroupRole(r.Context(), id, userFrom(r), s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
