package httpapi

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/stator"
	"github.com/cloudster1/exciter/backend/internal/webhook"
)

// Requirements traced to Stator pages (#41): pages read as the caller, a requirement's links, and Stator's webhooks,
// vouched for by the installation's secret alone.

// statorLinkError maps the links' refusals; nil when err is none of theirs.
func statorLinkError(err error) *APIError {
	switch {
	case errors.Is(err, stator.ErrLinkNotFound):
		return ErrNotFound("That link was not found. Reload the requirement; somebody may have removed it already.")
	case errors.Is(err, stator.ErrSection):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed",
			Message: "Sections take no Stator pages. Link the requirements in it instead."}
	case errors.Is(err, stator.ErrLinkExists):
		return &APIError{Status: http.StatusConflict, Code: "stator_link_exists",
			Message: "This requirement is linked to that page with that relation already. Pick another relation, or another page."}
	}
	return nil
}

func (s *Server) handleLookupStatorPages(w http.ResponseWriter, r *http.Request) {
	var ids []uuid.UUID
	for _, raw := range r.URL.Query()["id"] {
		id, err := uuid.Parse(raw)
		if err != nil {
			respondError(w, r, ErrValidation(map[string]string{"id": raw + " is not a page id. Name pages by the ids Stator gives them."}))
			return
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) > stator.MaxLookupPages {
		respondError(w, r, ErrValidation(map[string]string{"id": "Ask for at most " + strconv.Itoa(stator.MaxLookupPages) + " pages at once."}))
		return
	}
	status, pages, err := s.Stator.Lookup(r.Context(), ids)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if pages == nil {
		pages = []stator.LookupResult{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "pages": pages})
}

func (s *Server) handleSearchStatorPages(w http.ResponseWriter, r *http.Request) {
	limit := stator.SearchDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			respondError(w, r, ErrValidation(map[string]string{"limit": "Ask for 1 to " + strconv.Itoa(stator.SearchMaxLimit) + " pages."}))
			return
		}
		limit = n
	}
	status, pages, err := s.Stator.Search(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("space"), limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if pages == nil {
		pages = []stator.PageHit{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "pages": pages})
}

func (s *Server) handleListStatorLinks(w http.ResponseWriter, r *http.Request) {
	links, err := s.Stator.Links(r.Context(), reqKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"links": links})
}

func (s *Server) handleCreateStatorLink(w http.ResponseWriter, r *http.Request) {
	var req stator.LinkInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	link, lsn, err := s.Stator.Link(r.Context(), reqKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"link": link})
}

func (s *Server) handleDeleteStatorLink(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "linkID", "link")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Stator.Unlink(r.Context(), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleClearStatorLink(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "linkID", "link")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req stator.ClearInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	link, lsn, err := s.Stator.Clear(r.Context(), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"link": link})
}

func (s *Server) handleStatorWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, stator.WebhookMaxBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		respondError(w, r, &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
			Message: "A webhook delivery may be at most " + strconv.Itoa(stator.WebhookMaxBytes>>20) + " MiB. Subscribe the webhook to the topics Exciter lists only."})
		return
	}
	if err != nil {
		respondError(w, r, ErrBadRequest("The delivery could not be read. Stator sends it again later."))
		return
	}
	err = s.Stator.Webhook(r.Context(), body, r.Header.Get(stator.SignatureHeader), r.Header.Get(stator.VersionHeader))
	switch {
	case errors.Is(err, stator.ErrBadSignature):
		respondError(w, r, &APIError{Status: http.StatusUnauthorized, Code: "bad_signature",
			Message: "The signature does not match. Paste the webhook's secret from Stator under Admin, Stator, then redeliver."})
	case errors.Is(err, stator.ErrUnknownVersion):
		respondError(w, r, &APIError{Status: http.StatusUnprocessableEntity, Code: "unknown_version",
			Message: "Exciter reads webhook envelopes of version " + webhook.VersionList() + ". Update Exciter to read this one."})
	case err != nil:
		respondError(w, r, err)
	default:
		respondNoContent(w)
	}
}
