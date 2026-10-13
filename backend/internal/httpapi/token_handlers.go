package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/cloudster1/exciter/backend/internal/auth"
)

// Personal access tokens (#22): a person's own, and the organization's for its administrators, as in Stator.

var (
	errReadOnlyToken = &APIError{Status: http.StatusForbidden, Code: "read_only_token",
		Message: "This token can only read. Use a token without the read scope, or sign in, to make changes."}
	errSessionOnly = &APIError{Status: http.StatusForbidden, Code: "session_only",
		Message: "A token cannot do this. Sign in to Exciter and do it there."}
	errProjectsToken = &APIError{Status: http.StatusForbidden, Code: "projects_token",
		Message: "This token is limited to some projects, and this concerns the whole organization. Use a token without that limit, or sign in."}
)

// tokenError maps the tokens' refusals; nil when err is none of theirs.
func tokenError(err error) *APIError {
	switch {
	case errors.Is(err, auth.ErrTokenName):
		return ErrValidation(map[string]string{"name": "Give the token a name of at most 100 characters."})
	case errors.Is(err, auth.ErrTokenScope):
		return ErrValidation(map[string]string{"scopes": "That is not a token scope. Use read, or leave the scopes empty."})
	case errors.Is(err, auth.ErrTokenExpiry):
		return ErrValidation(map[string]string{"expiresAt": "The expiry has to be in the future. Pick a later day, or leave it empty."})
	case errors.Is(err, auth.ErrTokenProjects):
		return ErrValidation(map[string]string{"projects": "A token names at most 50 projects. Leave them all out for one that reaches every project you do."})
	case errors.Is(err, auth.ErrNoSuchProject):
		return ErrValidation(map[string]string{"projects": "A token can only name projects you can already see. Check the keys."})
	case errors.Is(err, auth.ErrNoSuchToken):
		return ErrNotFound("That token was not found. It may have been revoked already; reload the list.")
	}
	return nil
}

// accountsOn answers for a server built without the identity services, which only a test does.
func (s *Server) accountsOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Accounts == nil {
			respondError(w, r, errSignInOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// readOnlyToken refuses a write from a token made to read, before any handler, so the promise holds for every
// route including ones added later.
func readOnlyToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// MCP carries reads and writes alike; the rule is applied to the calls it carries, which run through this
		// chain again.
		if !safeMethod(r.Method) && r.URL.Path != mcpPath && PrincipalFrom(r.Context()).ReadOnly() {
			respondError(w, r, errReadOnlyToken)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireSession refuses a token what has to be done by somebody at a keyboard, such as making another token.
func requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := PrincipalFrom(r.Context()); p != nil && p.Token != nil {
			respondError(w, r, errSessionOnly)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// orgWideRoutes are the operations of the table that concern the whole organization, by method and pattern.
var orgWideRoutes = sync.OnceValue(func() map[string]bool {
	out := map[string]bool{}
	for _, op := range operations {
		if op.orgWide {
			out[op.method+" "+op.path] = true
		}
	}
	return out
})

// wholeOrg refuses a token limited to projects every operation the table marks orgWide, found by matching the
// request against the API's own router, so a route added later is held to its row.
func wholeOrg(api chi.Routes) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if PrincipalFrom(r.Context()).InProjectsOnly() {
				match := chi.NewRouteContext()
				if api.Match(match, r.Method, strings.TrimPrefix(r.URL.Path, APIPrefix)) && orgWideRoutes()[r.Method+" "+match.RoutePattern()] {
					respondError(w, r, errProjectsToken)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	var req auth.NewAPIToken
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p := PrincipalFrom(r.Context())
	token, lsn, err := s.Accounts.CreateAPIToken(r.Context(), p.Org.ID, p.UserID, req, s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"token": token})
}

func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.Accounts.ListAPITokens(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"tokens": tokens})
}

func (s *Server) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "tokenID", "token")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	p := PrincipalFrom(r.Context())
	lsn, err := s.Accounts.RevokeAPIToken(r.Context(), p.Org.ID, p.UserID, id, s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleListEveryonesAPITokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.Accounts.ListEveryonesAPITokens(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"tokens": tokens})
}

func (s *Server) handleRevokeAnybodysAPIToken(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "tokenID", "token")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	p := PrincipalFrom(r.Context())
	lsn, err := s.Accounts.RevokeOrgAPIToken(r.Context(), p.Org.ID, p.UserID, id, s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
