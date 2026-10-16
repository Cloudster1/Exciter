package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// Requirements (#13): written as revisions, kept in a trash, listed until the grid (#20) narrows them further.

var errRequirementsOff = &APIError{Status: http.StatusServiceUnavailable, Code: "requirements_unavailable",
	Message: "Requirements are not set up on this server. Ask its operator to configure it."}

// requirementsOn refuses every requirement route of a server built without the services.
func (s *Server) requirementsOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Requirements == nil || s.Search == nil || s.Projects == nil {
			respondError(w, r, errRequirementsOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func reqKeyParam(r *http.Request) string { return chi.URLParam(r, "reqKey") }

func (s *Server) handleCreateRequirement(w http.ResponseWriter, r *http.Request) {
	var req requirement.CreateInput
	if err := decodeJSONWithin(w, r, &req, requirement.MaxBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Requirements.Create(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"requirement": made})
}

func (s *Server) handleGetRequirement(w http.ResponseWriter, r *http.Request) {
	got, err := s.Requirements.Get(r.Context(), userFrom(r), reqKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requirement": got})
}

func (s *Server) handleGetRequirementByID(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "requirementID", "requirement")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	got, err := s.Requirements.GetByID(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requirement": got})
}

func (s *Server) handleUpdateRequirement(w http.ResponseWriter, r *http.Request) {
	var req requirement.UpdateInput
	if err := decodeJSONWithin(w, r, &req, requirement.MaxBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Requirements.Update(r.Context(), userFrom(r), reqKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requirement": got})
}

func (s *Server) handleTrashRequirement(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Requirements.Trash(r.Context(), userFrom(r), reqKeyParam(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleListRequirementTrash(w http.ResponseWriter, r *http.Request) {
	items, err := s.Requirements.TrashItems(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleRestoreRequirement(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "requirementID", "requirement")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	got, lsn, err := s.Requirements.Restore(r.Context(), userFrom(r), projectKeyParam(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requirement": got})
}

func (s *Server) handlePurgeRequirement(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "requirementID", "requirement")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Requirements.Purge(r.Context(), userFrom(r), projectKeyParam(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleEmptyRequirementTrash(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Requirements.EmptyTrash(r.Context(), userFrom(r), projectKeyParam(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

// requirementError maps the requirements' refusals; nil when err is none of theirs.
func requirementError(err error) *APIError {
	var (
		field    *requirement.FieldError
		query    *search.FieldError
		conflict *requirement.ConflictError
	)
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.As(err, &query):
		return ErrValidation(map[string]string{query.Field: query.Message})
	case errors.As(err, &conflict):
		return &APIError{Status: http.StatusConflict, Code: "revision_conflict",
			Message: "Somebody saved a newer revision of " + conflict.Key + " while you were editing. Compare it with your changes, then save again over it."}
	case errors.Is(err, requirement.ErrNotFound):
		return ErrNotFound("That requirement was not found. Check its key, or ask the project's administrators to let you in.")
	case errors.Is(err, requirement.ErrForbidden):
		return ErrForbidden("You may read this project but not change its requirements. Ask one of its administrators to make you an editor.")
	case errors.Is(err, requirement.ErrNotAdministrator):
		return ErrForbidden("Only the project's administrators delete from the trash for good. Restore the requirement instead, or ask one of them.")
	case errors.Is(err, requirement.ErrNoRevision):
		return ErrNotFound("That revision does not exist. Pick one from the requirement's history.")
	case errors.Is(err, requirement.ErrNoChange):
		return &APIError{Status: http.StatusConflict, Code: "no_change", Message: "That revision says what the requirement says now. Pick another to restore."}
	case errors.Is(err, requirement.ErrTrashed):
		return &APIError{Status: http.StatusConflict, Code: "requirement_trashed",
			Message: "This requirement is in the trash. Restore it from the project's trash to change it."}
	}
	return reuseError(err)
}

func (s *Server) handleGetTree(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.Requirements.Tree(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"nodes": nodes})
}

func (s *Server) handleGetSpecification(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, requirement.DefaultLimit, requirement.MaxSpecLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	entries, total, err := s.Requirements.Spec(r.Context(), userFrom(r), projectKeyParam(r), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"entries": entries, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleMoveRequirement(w http.ResponseWriter, r *http.Request) {
	var req requirement.MoveInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Requirements.Move(r.Context(), userFrom(r), reqKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requirement": got})
}

func (s *Server) handleCopyRequirement(w http.ResponseWriter, r *http.Request) {
	var req requirement.CopyInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, copied, lsn, err := s.Requirements.Copy(r.Context(), userFrom(r), reqKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"requirement": made, "copied": copied})
}

// revisionParam reads a revision number out of the route.
func revisionParam(r *http.Request) (int, *APIError) {
	n, err := strconv.Atoi(chi.URLParam(r, "revisionNumber"))
	if err != nil || n < 1 {
		return 0, ErrBadRequest("That is not a revision number. Check the address.")
	}
	return n, nil
}

// optionalInt reads a number from the query; absent is nil.
func optionalInt(r *http.Request, name string) (*int, *APIError) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return nil, ErrValidation(map[string]string{name: "Name a revision by its number, 0 for the empty requirement."})
	}
	return &n, nil
}

func (s *Server) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, requirement.DefaultLimit, requirement.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	revisions, total, err := s.Requirements.Revisions(r.Context(), userFrom(r), reqKeyParam(r), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"revisions": revisions, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleGetRevision(w http.ResponseWriter, r *http.Request) {
	n, apiErr := revisionParam(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	got, err := s.Requirements.Revision(r.Context(), userFrom(r), reqKeyParam(r), n)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"revision": got})
}

func (s *Server) handleDiffRevisions(w http.ResponseWriter, r *http.Request) {
	from, apiErr := optionalInt(r, "from")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	to, apiErr := optionalInt(r, "to")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	got, err := s.Requirements.Compare(r.Context(), userFrom(r), reqKeyParam(r), from, to)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"comparison": got})
}

func (s *Server) handleRestoreRevision(w http.ResponseWriter, r *http.Request) {
	n, apiErr := revisionParam(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req requirement.RestoreInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, revision, lsn, err := s.Requirements.RestoreRevision(r.Context(), userFrom(r), reqKeyParam(r), n, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requirement": got, "revision": revision})
}

func (s *Server) handleListActivity(w http.ResponseWriter, r *http.Request) {
	limit, after, apiErr := keysetWindow(r, requirement.DefaultLimit, requirement.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	entries, next, err := s.Requirements.Activity(r.Context(), userFrom(r), reqKeyParam(r), after, limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"entries": entries, "next": next})
}

func (s *Server) handleReuseRequirement(w http.ResponseWriter, r *http.Request) {
	var req requirement.ReuseInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, copied, lsn, err := s.Requirements.Reuse(r.Context(), userFrom(r), reqKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"requirement": made, "copied": copied})
}

func (s *Server) handleTakeSource(w http.ResponseWriter, r *http.Request) {
	var req requirement.TakeInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Requirements.TakeSource(r.Context(), userFrom(r), reqKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requirement": got})
}

func (s *Server) handleMakeVariant(w http.ResponseWriter, r *http.Request) {
	got, lsn, err := s.Requirements.MakeVariant(r.Context(), userFrom(r), reqKeyParam(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requirement": got})
}

// reuseError maps the refusals of reuse (#49); nil when err is none of them.
func reuseError(err error) *APIError {
	var moved *requirement.SourceMovedError
	switch {
	case errors.As(err, &moved):
		return &APIError{Status: http.StatusConflict, Code: "source_moved_on",
			Message: fmt.Sprintf("%s has revision %d now. Look at what changed in it, then take that one.", moved.Key, moved.Revision)}
	case errors.Is(err, requirement.ErrNotLibrary):
		return &APIError{Status: http.StatusConflict, Code: "not_a_library",
			Message: "Only a library's requirements are reused. Ask an administrator of this project to mark it a library, or copy the requirement instead."}
	case errors.Is(err, requirement.ErrNotReused):
		return &APIError{Status: http.StatusConflict, Code: "not_reused", Message: "This requirement follows no source. Edit it as it is."}
	case errors.Is(err, requirement.ErrFollowsSource):
		return &APIError{Status: http.StatusConflict, Code: "follows_source",
			Message: "This requirement takes its title, statement and rationale from its library source. Change them there, or make it a variant to change them here."}
	case errors.Is(err, requirement.ErrSourceHidden):
		return ErrForbidden("You may not read this requirement's source, so you cannot take its words. Ask the library's administrators to let you read it.")
	}
	return nil
}
