package httpapi

import (
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/cloudster1/exciter/backend/internal/armature"
)

// Requirements traced to Armature issues (#27): issues read as the caller, and a requirement's links.

// armatureLinkError maps the links' refusals; nil when err is none of theirs.
func armatureLinkError(err error) *APIError {
	switch {
	case errors.Is(err, armature.ErrLinkNotFound):
		return ErrNotFound("That link was not found. Reload the requirement; somebody may have removed it already.")
	case errors.Is(err, armature.ErrSection):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed",
			Message: "Sections take no Armature issues. Link the requirements in it instead."}
	case errors.Is(err, armature.ErrLinkExists):
		return &APIError{Status: http.StatusConflict, Code: "armature_link_exists",
			Message: "This requirement is linked to that issue with that relation already. Pick another relation, or another issue."}
	case errors.Is(err, armature.ErrReadOnlyToken):
		return &APIError{Status: http.StatusConflict, Code: "armature_rejected",
			Message: "Your Armature token may only read, so it cannot link issues. Paste a token without the read scope under Your account, Armature account."}
	}
	return nil
}

func (s *Server) handleLookupArmatureIssues(w http.ResponseWriter, r *http.Request) {
	var keys []string
	for _, raw := range r.URL.Query()["key"] {
		key, ok := armature.NormalizeKey(raw)
		if !ok {
			respondError(w, r, ErrValidation(map[string]string{"key": raw + " is not an issue key. Write the project key, a hyphen and the number, such as CP-12."}))
			return
		}
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	if len(keys) > armature.MaxLookupKeys {
		respondError(w, r, ErrValidation(map[string]string{"key": "Ask for at most " + strconv.Itoa(armature.MaxLookupKeys) + " issues at once."}))
		return
	}
	status, issues, err := s.Armature.Lookup(r.Context(), keys)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if issues == nil {
		issues = []armature.LookupResult{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "issues": issues})
}

func (s *Server) handleGetArmatureIssue(w http.ResponseWriter, r *http.Request) {
	key, ok := armature.NormalizeKey(chi.URLParam(r, "issueKey"))
	if !ok {
		// No issue answers to a key of the wrong shape, as none the caller may see.
		respondJSON(w, r, http.StatusOK, map[string]any{"status": armature.StatusOK, "issue": nil})
		return
	}
	status, issue, err := s.Armature.Issue(r.Context(), key)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "issue": issue})
}

func (s *Server) handleSearchArmatureIssues(w http.ResponseWriter, r *http.Request) {
	limit := armature.SearchDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			respondError(w, r, ErrValidation(map[string]string{"limit": "Ask for 1 to " + strconv.Itoa(armature.SearchMaxLimit) + " issues."}))
			return
		}
		limit = n
	}
	status, issues, err := s.Armature.Search(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if issues == nil {
		issues = []armature.Issue{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "issues": issues})
}

func (s *Server) handleListArmatureLinks(w http.ResponseWriter, r *http.Request) {
	links, err := s.Armature.Links(r.Context(), reqKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"links": links})
}

func (s *Server) handleCreateArmatureLink(w http.ResponseWriter, r *http.Request) {
	var req armature.LinkInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	link, lsn, err := s.Armature.Link(r.Context(), reqKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"link": link})
}

func (s *Server) handleDeleteArmatureLink(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "linkID", "link")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Armature.Unlink(r.Context(), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
