package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/trace"
)

// Trace links (#18): typed links between requirements, and the organization's and the projects' link types.

var errTraceOff = &APIError{Status: http.StatusServiceUnavailable, Code: "links_unavailable",
	Message: "Trace links are not set up on this server. Ask its operator to configure it."}

// traceOn refuses every link route of a server built without the services.
func (s *Server) traceOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Trace == nil || s.Requirements == nil {
			respondError(w, r, errTraceOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// traceError maps the links' refusals; nil when err is none of theirs.
func traceError(err error) *APIError {
	var (
		field *trace.FieldError
		inUse *trace.InUseError
	)
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.As(err, &inUse):
		return &APIError{Status: http.StatusConflict, Code: "in_use", Message: fmt.Sprintf(
			"This link type has %d links, removed ones included. Retire it instead, so they keep their type.", inUse.Count)}
	case errors.Is(err, trace.ErrNotFound):
		return ErrNotFound("That link type was not found. Reload the list; somebody may have deleted it.")
	case errors.Is(err, trace.ErrForbidden):
		return ErrForbidden("Only an administrator changes the link types for all projects, and a project's administrators the project's own. Ask one of them.")
	case errors.Is(err, trace.ErrLinkNotFound):
		return ErrNotFound("That link was not found. Reload the links; somebody may have removed it already.")
	case errors.Is(err, trace.ErrSection):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed",
			Message: "Sections take no links. Link the requirements in it instead."}
	}
	return nil
}

func (s *Server) handleListOrgLinkTypes(w http.ResponseWriter, r *http.Request) {
	types, err := s.Trace.OrgTypes(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"linkTypes": types})
}

func (s *Server) handleCreateOrgLinkType(w http.ResponseWriter, r *http.Request) {
	var req trace.LinkTypeInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Trace.CreateOrgType(r.Context(), userFrom(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"linkType": made})
}

func (s *Server) handleUpdateLinkType(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "linkTypeID", "link type")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req trace.LinkTypeInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Trace.UpdateType(r.Context(), userFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"linkType": got})
}

func (s *Server) handleDeleteLinkType(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "linkTypeID", "link type")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Trace.DeleteType(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListProjectLinkTypes(w http.ResponseWriter, r *http.Request) {
	types, err := s.Trace.ProjectTypes(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"linkTypes": types})
}

func (s *Server) handleCreateProjectLinkType(w http.ResponseWriter, r *http.Request) {
	var req trace.LinkTypeInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Trace.CreateProjectType(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"linkType": made})
}

func (s *Server) handleSetProjectLinkType(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "linkTypeID", "link type")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req trace.ProjectLinkTypeInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Trace.SetProjectType(r.Context(), userFrom(r), projectKeyParam(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"linkType": got})
}

func (s *Server) handleListLinks(w http.ResponseWriter, r *http.Request) {
	links, err := s.Trace.Links(r.Context(), userFrom(r), reqKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"links": links})
}

func (s *Server) handleCreateLink(w http.ResponseWriter, r *http.Request) {
	var req trace.LinkInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Trace.Link(r.Context(), userFrom(r), reqKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"link": made})
}

func (s *Server) handleDeleteLink(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "linkID", "link")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Trace.Unlink(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
