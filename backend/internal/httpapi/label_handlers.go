package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/cloudster1/exciter/backend/internal/label"
)

var errLabelsOff = &APIError{Status: http.StatusServiceUnavailable, Code: "labels_unavailable",
	Message: "Labels are not set up on this server. Ask its operator to configure it."}

// labelsOn refuses every label route of a server built without the services.
func (s *Server) labelsOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Labels == nil || s.Requirements == nil {
			respondError(w, r, errLabelsOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// labelName reads a label out of the route. No label holds a percent sign, so unescaping what the router left
// escaped cannot change one.
func labelName(r *http.Request) string {
	raw := chi.URLParam(r, "labelName")
	if name, err := url.PathUnescape(raw); err == nil {
		return name
	}
	return raw
}

// labelError maps the labels' refusals; nil when err is none of theirs.
func labelError(err error) *APIError {
	var field *label.FieldError
	if errors.As(err, &field) {
		return ErrValidation(map[string]string{field.Field: field.Message})
	}
	return nil
}

func (s *Server) handleListRequirementLabels(w http.ResponseWriter, r *http.Request) {
	names, err := s.Requirements.Labels(r.Context(), userFrom(r), reqKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"labels": names})
}

func (s *Server) handleAddRequirementLabel(w http.ResponseWriter, r *http.Request) {
	var req label.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	names, lsn, err := s.Requirements.AddLabel(r.Context(), userFrom(r), reqKeyParam(r), req.Name)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"labels": names})
}

func (s *Server) handleRemoveRequirementLabel(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Requirements.RemoveLabel(r.Context(), userFrom(r), reqKeyParam(r), labelName(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleListLabels(w http.ResponseWriter, r *http.Request) {
	limit, apiErr := queryLimit(r, label.DefaultSuggestions, label.MaxSuggestions)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	typed := r.URL.Query().Get("q")
	if utf8.RuneCountInString(typed) > label.MaxNameLength {
		respondError(w, r, ErrValidation(map[string]string{"q": "A label has at most " + strconv.Itoa(label.MaxNameLength) + " characters. Type fewer."}))
		return
	}
	found, err := s.Labels.Suggest(r.Context(), userFrom(r), typed, r.URL.Query().Get("project"), limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"labels": found})
}

func (s *Server) handleListLabelledRequirements(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, label.DefaultLimit, label.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, total, err := s.Labels.Requirements(r.Context(), userFrom(r), labelName(r), r.URL.Query().Get("project"), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requirements": found, "total": total, "limit": limit, "offset": offset})
}
