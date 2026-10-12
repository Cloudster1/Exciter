package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// setSessionCookie writes the cookie out of scripts' reach, Lax so a provider's
// redirect back carries it, and Secure outside local development.
func (s *Server) setSessionCookie(w http.ResponseWriter, secret string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.CookieName,
		Value:    secret,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// noteSession remembers a write made before its session existed under that
// session's key, so the first reads on the new cookie see it.
func (s *Server) noteSession(ctx context.Context, sessionID uuid.UUID, lsn db.LSN) {
	if s.Fresh != nil && lsn != 0 {
		s.Fresh.Note(ctx, sessionFreshnessKey(sessionID), lsn)
	}
}

// errSignInOff answers for a server built without the identity services, which only a test does.
var errSignInOff = &APIError{Status: http.StatusServiceUnavailable, Code: "sign_in_unavailable",
	Message: "Signing in is not set up on this server. Ask its operator to configure it."}

// meResponse is who is signed in and where they may act, which is everything
// the account menu needs in one request.
type meResponse struct {
	User auth.User `json:"user"`
	// Organization is where the session acts now; null before one is chosen.
	Organization  *auth.CurrentOrg  `json:"organization"`
	Organizations []auth.Membership `json:"organizations"`
}

func (s *Server) me(r *http.Request, p *auth.Principal) (*meResponse, error) {
	organizations, err := s.Accounts.Organizations(r.Context(), p)
	if err != nil {
		return nil, err
	}
	out := &meResponse{
		User:          auth.User{ID: p.UserID, Email: p.Email, Name: p.Name, AvatarURL: p.AvatarURL, Locale: p.Locale},
		Organizations: organizations,
	}
	if p.InOrg() {
		out.Organization = &auth.CurrentOrg{ID: p.Org.ID, Slug: p.Org.Slug, Name: p.OrgName, Role: p.Role}
	}
	return out, nil
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleLogin signs a local account in with a password.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	fields := map[string]string{}
	if strings.TrimSpace(req.Email) == "" {
		fields["email"] = "Enter your email address."
	}
	if req.Password == "" {
		fields["password"] = "Enter your password."
	}
	if len(fields) > 0 {
		respondError(w, r, ErrValidation(fields))
		return
	}

	creds, err := s.Accounts.Login(r.Context(), req.Email, req.Password, r.UserAgent(), s.clientIP(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	s.noteSession(r.Context(), *creds.Principal.SessionID, creds.LSN)
	s.setSessionCookie(w, creds.SessionSecret, creds.ExpiresAt)
	me, err := s.me(r, creds.Principal)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, me)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if p := PrincipalFrom(r.Context()); p != nil && p.SessionID != nil {
		if err := s.Accounts.Logout(r.Context(), *p.SessionID); err != nil {
			respondError(w, r, err)
			return
		}
	}
	s.clearSessionCookie(w)
	respondNoContent(w)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	s.handleMeAs(w, r, PrincipalFrom(r.Context()))
}

func (s *Server) handleMeAs(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	me, err := s.me(r, p)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, me)
}

// updateMeRequest changes the caller's own settings; a field left out stays.
type updateMeRequest struct {
	// Locale is the interface language, "en" or "de"; empty follows the browser.
	Locale *auth.Locale `json:"locale,omitempty"`
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	var req updateMeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p := *PrincipalFrom(r.Context())
	if req.Locale != nil {
		lsn, err := s.Accounts.SetLocale(r.Context(), p.UserID, *req.Locale)
		noteWrite(r.Context(), lsn)
		if err != nil {
			respondError(w, r, err)
			return
		}
		p.Locale = *req.Locale
	}
	s.handleMeAs(w, r, &p)
}

type switchOrgRequest struct {
	Slug string `json:"slug"`
}

func (s *Server) handleSwitchOrg(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if p.SessionID == nil {
		respondError(w, r, ErrBadRequest("Only a browser session can switch organizations."))
		return
	}
	var req switchOrgRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if strings.TrimSpace(req.Slug) == "" {
		respondError(w, r, ErrValidation(map[string]string{"slug": "Name the organization to switch to."}))
		return
	}
	org, lsn, err := s.Accounts.SwitchOrg(r.Context(), *p.SessionID, p.UserID, req.Slug)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"organization": org})
}
