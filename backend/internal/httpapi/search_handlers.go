package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/cloudster1/exciter/backend/internal/search"
)

// searchError maps the views' refusals; the search's own are field errors, which requirementError maps.
func searchError(err error) *APIError {
	switch {
	case errors.Is(err, search.ErrViewNotFound):
		return ErrNotFound("That view was not found. It may have been deleted or no longer be shared; reload the list.")
	case errors.Is(err, search.ErrViewForbidden):
		return ErrForbidden("Only a view's owner changes it, and the project's administrators once it is shared. Save a copy of your own instead.")
	case errors.Is(err, search.ErrUnshareForbidden):
		return ErrForbidden("Only a view's owner stops sharing it. Delete it instead, or ask its owner to take it back.")
	case errors.Is(err, search.ErrShareForbidden):
		return ErrForbidden("Only the project's editors share views. Keep this one to yourself, or ask an administrator to make you an editor.")
	}
	return nil
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, search.DefaultLimit, search.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	q, err := search.ParseQuery(r.URL.Query())
	if err != nil {
		respondError(w, r, err)
		return
	}
	q.Limit, q.Offset = limit, offset
	hits, total, err := s.Search.Search(r.Context(), userFrom(r), q)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"hits": hits, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleQuickSearch(w http.ResponseWriter, r *http.Request) {
	limit, apiErr := queryLimit(r, search.DefaultQuickLimit, search.MaxQuickLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	q := r.URL.Query()
	found, err := s.Search.Quick(r.Context(), userFrom(r), q.Get("q"), q.Get("project"), limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requirements": found})
}

func (s *Server) handleListGrid(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, search.DefaultLimit, search.MaxGridLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	filter, sort, err := search.ParseGrid(r.URL.Query())
	if err != nil {
		respondError(w, r, err)
		return
	}
	rows, total, err := s.Search.Grid(r.Context(), userFrom(r), projectKeyParam(r), filter, sort, limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rows": rows, "total": total, "limit": limit, "offset": offset})
}

// handleSuggestQuery completes a query of the grid at the caret, which at counts in characters; the end of the text
// when absent.
func (s *Server) handleSuggestQuery(w http.ResponseWriter, r *http.Request) {
	limit, apiErr := queryLimit(r, search.DefaultSuggestLimit, search.MaxSuggestLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	q := r.URL.Query()
	text := q.Get("q")
	at := utf8.RuneCountInString(text)
	if raw := q.Get("at"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			respondError(w, r, ErrValidation(map[string]string{"at": "Say where the caret is, as a count of characters from 0."}))
			return
		}
		at = n
	}
	found, err := s.Search.Suggest(r.Context(), userFrom(r), projectKeyParam(r), text, at, limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, found)
}

func (s *Server) handleListViews(w http.ResponseWriter, r *http.Request) {
	views, err := s.Search.Views(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"views": views})
}

func (s *Server) handleCreateView(w http.ResponseWriter, r *http.Request) {
	var req search.ViewInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	view, lsn, err := s.Search.CreateView(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"view": view})
}

func (s *Server) handleUpdateView(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "viewID", "view")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req search.ViewInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	view, lsn, err := s.Search.UpdateView(r.Context(), userFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"view": view})
}

func (s *Server) handleDeleteView(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "viewID", "view")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Search.DeleteView(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
