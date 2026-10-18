package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/cloudster1/exciter/backend/internal/quality"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// Writing rules (#207). See docs/api-contract-m10.md.

var errQualityOff = &APIError{Status: http.StatusServiceUnavailable, Code: "quality_unavailable",
	Message: "Writing rules are not set up on this server. Ask an administrator to start Exciter with its database."}

func (s *Server) qualityOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Quality == nil {
			respondError(w, r, errQualityOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// qualityError maps the writing rules' refusals; nil when err is none of theirs.
func qualityError(err error) *APIError {
	var field *quality.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, quality.ErrStale):
		return &APIError{Status: http.StatusConflict, Code: "stale_revision",
			Message: "The requirement changed since you read it. Read it again, then accept the finding."}
	case errors.Is(err, quality.ErrForbidden):
		return ErrForbidden("Only the project's editors check and accept findings, and its administrators set the writing rules. Ask one of them.")
	}
	return nil
}

func (s *Server) handleQualityCheck(w http.ResponseWriter, r *http.Request) {
	var req quality.CheckInput
	if err := decodeJSONWithin(w, r, &req, requirement.MaxBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	check, err := s.Quality.CheckDraft(r.Context(), userFrom(r), projectKeyParam(r), req)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"check": check})
}

func (s *Server) handleGetQuality(w http.ResponseWriter, r *http.Request) {
	var revision *int
	if raw := r.URL.Query().Get("revision"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			respondError(w, r, ErrValidation(map[string]string{"revision": "Give the revision as a whole number."}))
			return
		}
		revision = &n
	}
	found, err := s.Quality.Findings(r.Context(), userFrom(r), reqKeyParam(r), revision)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"quality": found})
}

func (s *Server) handleAcceptFinding(w http.ResponseWriter, r *http.Request) {
	var req quality.AcceptInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	found, lsn, err := s.Quality.Accept(r.Context(), userFrom(r), reqKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"quality": found})
}

func (s *Server) handleWithdrawAcceptance(w http.ResponseWriter, r *http.Request) {
	revision, err := strconv.Atoi(r.URL.Query().Get("revision"))
	if err != nil {
		respondError(w, r, ErrValidation(map[string]string{"revision": "Say which revision the acceptance is on, as a whole number."}))
		return
	}
	// A key holds slashes, which the router leaves encoded in the parameter.
	key, err := url.PathUnescape(chi.URLParam(r, "findingKey"))
	if err != nil {
		respondError(w, r, ErrValidation(map[string]string{"key": "Give the finding's key as the findings name it."}))
		return
	}
	found, lsn, err := s.Quality.Withdraw(r.Context(), userFrom(r), reqKeyParam(r), revision, key)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"quality": found})
}

func (s *Server) handleProjectQuality(w http.ResponseWriter, r *http.Request) {
	filter, _, err := search.ParseGrid(r.URL.Query())
	if err != nil {
		respondError(w, r, err)
		return
	}
	limit, offset, apiErr := offsetWindow(r, quality.DefaultLimit, quality.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	summary, err := s.Search.Quality(r.Context(), userFrom(r), projectKeyParam(r), filter, limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"quality": summary})
}

func (s *Server) handleGetQualitySettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.Quality.Settings(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"settings": settings})
}

func (s *Server) handleSaveQualitySettings(w http.ResponseWriter, r *http.Request) {
	var req quality.SettingsInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	settings, lsn, err := s.Quality.SaveSettings(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"settings": settings})
}

func (s *Server) handleListQualityRules(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]any{"rules": s.Quality.RulesCatalog()})
}
