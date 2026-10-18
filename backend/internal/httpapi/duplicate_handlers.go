package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/duplicates"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Duplicates (#208). See docs/api-contract-m10.md.

var errDuplicatesOff = &APIError{Status: http.StatusServiceUnavailable, Code: "duplicates_unavailable",
	Message: "Finding duplicates is not set up on this server. Ask an administrator to start Exciter with its database."}

func (s *Server) duplicatesOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Duplicates == nil {
			respondError(w, r, errDuplicatesOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// duplicatesError maps the duplicates' refusals; nil when err is none of theirs.
func duplicatesError(err error) *APIError {
	var field *duplicates.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, duplicates.ErrStale):
		return &APIError{Status: http.StatusConflict, Code: "stale_revision",
			Message: "One of the two changed since you read them. Read the pair again, then decide."}
	case errors.Is(err, duplicates.ErrForbidden):
		return ErrForbidden("Only the project's editors decide about duplicates, and its administrators set the threshold. Ask one of them.")
	}
	return nil
}

func (s *Server) handleSimilarToDraft(w http.ResponseWriter, r *http.Request) {
	var req duplicates.DraftInput
	if err := decodeJSONWithin(w, r, &req, requirement.MaxBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	found, threshold, err := s.Duplicates.Draft(r.Context(), userFrom(r), projectKeyParam(r), req)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"similar": found, "threshold": threshold})
}

func (s *Server) handleListSimilar(w http.ResponseWriter, r *http.Request) {
	found, threshold, err := s.Duplicates.Of(r.Context(), userFrom(r), reqKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"similar": found, "threshold": threshold})
}

func (s *Server) handleListDuplicates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := duplicates.DefaultLimit
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > duplicates.MaxLimit {
			respondError(w, r, ErrValidation(map[string]string{"limit": "Ask for 1 to " + strconv.Itoa(duplicates.MaxLimit) + " pairs."}))
			return
		}
		limit = n
	}
	in := duplicates.PairsQuery{Section: q.Get("section"), Query: q.Get("query"), Cursor: q.Get("cursor"), Limit: limit}
	for _, raw := range q["type"] {
		id, err := uuid.Parse(raw)
		if err != nil {
			respondError(w, r, ErrValidation(map[string]string{"type": "Name types by their ids."}))
			return
		}
		in.Types = append(in.Types, id)
	}
	pairs, next, threshold, err := s.Duplicates.Pairs(r.Context(), userFrom(r), projectKeyParam(r), in)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"pairs": pairs, "next": next, "threshold": threshold})
}

func (s *Server) handleDismissDuplicate(w http.ResponseWriter, r *http.Request) {
	var req duplicates.DismissInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	decision, lsn, err := s.Duplicates.Dismiss(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"decision": decision})
}

func (s *Server) handleMergeDuplicate(w http.ResponseWriter, r *http.Request) {
	var req duplicates.MergeInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	merge, lsn, err := s.Duplicates.Merge(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"merge": merge})
}

func (s *Server) handleLinkDuplicate(w http.ResponseWriter, r *http.Request) {
	var req duplicates.LinkInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	decision, lsn, err := s.Duplicates.Link(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"decision": decision})
}

func (s *Server) handleGetDuplicateSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.Duplicates.Settings(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"settings": settings})
}

func (s *Server) handleSaveDuplicateSettings(w http.ResponseWriter, r *http.Request) {
	var req duplicates.SettingsInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	settings, lsn, err := s.Duplicates.SaveSettings(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"settings": settings})
}
