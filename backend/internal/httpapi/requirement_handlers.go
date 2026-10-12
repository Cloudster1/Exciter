package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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

// gridFilter reads the grid's narrowing from the query; an id that is none is refused on its parameter.
func gridFilter(r *http.Request) (search.Filter, *APIError) {
	q := r.URL.Query()
	f := search.Filter{Q: q.Get("q"), Categories: q["category"], Labels: q["label"], Priorities: q["priority"]}
	for _, k := range q["kind"] {
		f.Kinds = append(f.Kinds, requirement.Kind(k))
	}
	ids := map[string]*[]uuid.UUID{"owner": &f.Owners, "type": &f.Types, "status": &f.Statuses}
	for name, into := range ids {
		for _, raw := range q[name] {
			id, err := uuid.Parse(raw)
			if err != nil {
				return f, ErrValidation(map[string]string{name: "That is not an id. Pick it from the list."})
			}
			*into = append(*into, id)
		}
	}
	// The grid reads attr's conditions from #20 on; until then that one was asked for is enough to refuse it.
	if len(q["attr"]) > 0 {
		f.Attributes = []search.AttributeCondition{{}}
	}
	return f, nil
}

func (s *Server) handleListGrid(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, search.DefaultLimit, search.MaxGridLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	filter, apiErr := gridFilter(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	rows, total, err := s.Search.Grid(r.Context(), userFrom(r), projectKeyParam(r), filter, limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rows": rows, "total": total, "limit": limit, "offset": offset})
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
	case errors.Is(err, requirement.ErrTrashed):
		return &APIError{Status: http.StatusConflict, Code: "requirement_trashed",
			Message: "This requirement is in the trash. Restore it from the project's trash to change it."}
	}
	return nil
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
