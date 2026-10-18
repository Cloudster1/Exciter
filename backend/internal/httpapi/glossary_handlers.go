package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/cloudster1/exciter/backend/internal/glossary"
)

// The glossary (#210). See docs/api-contract-m10.md.

// searchTerms is how many terms a search answers beside its requirements.
const searchTerms = 5

var errGlossaryOff = &APIError{Status: http.StatusServiceUnavailable, Code: "glossary_unavailable",
	Message: "The glossary is not set up on this server. Ask an administrator to start Exciter with its database."}

func (s *Server) glossaryOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Glossary == nil {
			respondError(w, r, errGlossaryOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// glossaryError maps the glossary's refusals; nil when err is none of theirs.
func glossaryError(err error) *APIError {
	var (
		field  *glossary.FieldError
		exists *glossary.ExistsError
	)
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.As(err, &exists):
		return &APIError{Status: http.StatusConflict, Code: "term_exists",
			Message: exists.Word + " already names a term of this glossary. Change that term, or choose another name."}
	case errors.Is(err, glossary.ErrStale):
		return &APIError{Status: http.StatusConflict, Code: "stale_revision",
			Message: "The term changed since you read it. Read it again, then make your change."}
	case errors.Is(err, glossary.ErrNotUsed):
		return ErrNotFound("That requirement does not use that term. Read its terms again.")
	case errors.Is(err, glossary.ErrNotFound):
		return ErrNotFound("That term was not found. Check its link, or ask the project's administrators to let you in.")
	case errors.Is(err, glossary.ErrForbidden):
		return ErrForbidden("Only the project's editors define and change terms, and its administrators delete them and choose its glossaries. Ask one of them.")
	}
	return nil
}

func (s *Server) handleListTerms(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, glossary.DefaultLimit, glossary.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	q := r.URL.Query()
	var statuses []glossary.Status
	for _, st := range q["status"] {
		statuses = append(statuses, glossary.Status(st))
	}
	terms, total, err := s.Glossary.List(r.Context(), userFrom(r), projectKeyParam(r), q.Get("q"), statuses, q.Get("own") == "true", limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"terms": terms, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleCreateTerm(w http.ResponseWriter, r *http.Request) {
	var req glossary.TermInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	term, lsn, err := s.Glossary.Create(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"term": term})
}

func (s *Server) handleGetTerm(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "termID", "term")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	term, err := s.Glossary.Get(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"term": term})
}

func (s *Server) handleUpdateTerm(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "termID", "term")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req glossary.TermPatch
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	term, lsn, err := s.Glossary.Update(r.Context(), userFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"term": term})
}

func (s *Server) handleDeleteTerm(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "termID", "term")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Glossary.Delete(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleListTermRevisions(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "termID", "term")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	revisions, err := s.Glossary.Revisions(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"revisions": revisions})
}

func (s *Server) handleGetTermRevision(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "termID", "term")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	number, err := strconv.Atoi(chi.URLParam(r, "revisionNumber"))
	if err != nil || number < 1 {
		respondError(w, r, ErrNotFound("That revision was not found. Pick one from the term's revisions."))
		return
	}
	revision, err := s.Glossary.Revision(r.Context(), userFrom(r), id, number)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"revision": revision})
}

func (s *Server) handleListTermUses(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "termID", "term")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	limit, offset, apiErr := offsetWindow(r, glossary.DefaultLimit, glossary.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	uses, total, err := s.Glossary.Uses(r.Context(), userFrom(r), id, limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"uses": uses, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleListRequirementTerms(w http.ResponseWriter, r *http.Request) {
	terms, err := s.Glossary.RequirementTerms(r.Context(), userFrom(r), reqKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"terms": terms})
}

func (s *Server) handleClearTermUse(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "termID", "term")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req glossary.ClearInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	term, lsn, err := s.Glossary.Clear(r.Context(), userFrom(r), reqKeyParam(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"term": term})
}

func (s *Server) handleGetGlossaries(w http.ResponseWriter, r *http.Request) {
	found, err := s.Glossary.Glossaries(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"glossaries": found})
}

func (s *Server) handleSaveGlossaries(w http.ResponseWriter, r *http.Request) {
	var req glossary.GlossariesInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	found, lsn, err := s.Glossary.SaveGlossaries(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"glossaries": found})
}
