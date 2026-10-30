package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
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
	target, err := s.OIDC.Start(r.Context(), safeRedirect(r.URL.Query().Get("next")))
	if err != nil {
		loggerFrom(r.Context()).Warn("a sign-in through an identity provider could not start", "error", err)
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

	identity, login, err := s.OIDC.Exchange(r.Context(), query.Get("state"), query.Get("code"))
	if err != nil {
		loggerFrom(r.Context()).Warn("a sign-in through an identity provider failed", "error", err)
		s.landAfterSignIn(w, r, "", signInFailure(err))
		return
	}
	if login.Purpose == oidc.PurposeReauth {
		s.finishReauth(w, r, identity, login)
		return
	}
	redirect := login.Redirect
	session, err := s.OIDC.SignIn(r.Context(), identity, s.Accounts.SessionTTL(), r.UserAgent(), s.clientIP(r))
	if err != nil {
		loggerFrom(r.Context()).Warn("a verified identity could not be signed in", "error", err, "subject", identity.Subject)
		s.landAfterSignIn(w, r, "", signInFailure(err))
		return
	}
	loggerFrom(r.Context()).Info("signed in through an identity provider", "user_id", session.UserID, "role", session.Role,
		"groups_joined", session.Joined, "groups_left", session.Left)
	s.noteSession(r.Context(), session.SessionID, max(session.LSN, s.connectPartners(r.Context(), session.UserID, identity.AccessToken)))
	s.setSessionCookie(w, session.Secret, session.ExpiresAt)
	s.landAfterSignIn(w, r, redirect, "")
}

// connectPartners exchanges the sign-in's access token at Armature and Stator at once (#274), before the browser
// lands, so the first page already shows the person connected; each exchange is bounded and never fails the sign-in.
// It answers where the later of the two last wrote, which the session's reads then wait for on a replica.
func (s *Server) connectPartners(ctx context.Context, user uuid.UUID, subject string) db.LSN {
	if subject == "" {
		return 0
	}
	ctx = context.WithoutCancel(ctx)
	var (
		wg            sync.WaitGroup
		atArm, atStat db.LSN
	)
	if s.Armature != nil {
		wg.Go(func() { atArm = s.Armature.ExchangeAtSignIn(ctx, user, subject) })
	}
	if s.Stator != nil {
		wg.Go(func() { atStat = s.Stator.ExchangeAtSignIn(ctx, user, subject) })
	}
	wg.Wait()
	return max(atArm, atStat)
}

// signInFailure names a refusal for the sign-in page to explain, without
// handing back anything about the provider or the account.
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

// handleGetOIDCProvider returns the installation's provider. The client secret
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
	// AdmitByGroup lets somebody new in whose group is mapped to a role, at
	// sign-in and at a partner's token exchange. Left out, it is off.
	AdmitByGroup bool `json:"admitByGroup"`
	Enabled      bool `json:"enabled"`
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
		AdmitByGroup: req.AdmitByGroup,
		Enabled:      req.Enabled,
	})
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, providerView{Provider: saved, CallbackURL: s.OIDCCallbackURL})
}

// handleReauthStart sends a signed-in person to the provider to prove again who they are, for a signature (#46);
// the browser comes back to next with the proof to sign with, or with why there is none.
func (s *Server) handleReauthStart(w http.ResponseWriter, r *http.Request) {
	next := safeRedirect(r.URL.Query().Get("next"))
	p := PrincipalFrom(r.Context())
	if s.OIDC == nil || p == nil || p.SessionID == nil {
		landWithSignature(w, r, s.AppBaseURL, next, "", "session")
		return
	}
	target, err := s.OIDC.StartReauth(r.Context(), p.UserID, *p.SessionID, p.Email, next)
	if err != nil {
		loggerFrom(r.Context()).Warn("a re-authentication could not start", "error", err)
		landWithSignature(w, r, s.AppBaseURL, next, "", "unavailable")
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// finishReauth answers a re-authentication: in the session that asked for it, by the person signed in there, freshly.
func (s *Server) finishReauth(w http.ResponseWriter, r *http.Request, identity *oidc.Identity, login *oidc.Login) {
	p := PrincipalFrom(r.Context())
	if p == nil || p.SessionID == nil || login.SessionID == nil || *p.SessionID != *login.SessionID {
		landWithSignature(w, r, s.AppBaseURL, login.Redirect, "", "session")
		return
	}
	id, err := s.OIDC.Reauthenticated(r.Context(), identity, login)
	switch {
	case errors.Is(err, oidc.ErrNotTheSigner):
		landWithSignature(w, r, s.AppBaseURL, login.Redirect, "", "someone_else")
	case errors.Is(err, oidc.ErrNotFresh):
		landWithSignature(w, r, s.AppBaseURL, login.Redirect, "", "not_fresh")
	case err != nil:
		loggerFrom(r.Context()).Warn("a re-authentication could not be kept", "error", err)
		landWithSignature(w, r, s.AppBaseURL, login.Redirect, "", "unavailable")
	default:
		landWithSignature(w, r, s.AppBaseURL, login.Redirect, id.String(), "")
	}
}

// landWithSignature sends the browser back where it signs, with the proof it gained or why it gained none.
func landWithSignature(w http.ResponseWriter, r *http.Request, base, redirect, proof, failure string) {
	target, err := url.Parse(redirect)
	if err != nil || redirect == "" {
		target = &url.URL{Path: "/"}
	}
	query := target.Query()
	if proof != "" {
		query.Set("signature", proof)
	} else {
		query.Set("signature_error", failure)
	}
	target.RawQuery = query.Encode()
	http.Redirect(w, r, base+target.String(), http.StatusFound)
}
