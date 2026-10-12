package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/cloudster1/exciter/backend/internal/project"
)

// Projects (#11): the homes of requirements, with keys that may change and keep resolving.

// errProjectsOff answers for a server built without projects, which only a test does.
var errProjectsOff = &APIError{Status: http.StatusServiceUnavailable, Code: "projects_unavailable",
	Message: "Projects are not set up on this server. Ask its operator to configure it."}

// projectsOn refuses every project route of a server built without the service.
func (s *Server) projectsOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Projects == nil {
			respondError(w, r, errProjectsOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func projectKeyParam(r *http.Request) string { return chi.URLParam(r, "projectKey") }

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	archived, err := queryBool(r, "archived")
	if err != nil {
		respondError(w, r, err)
		return
	}
	projects, err := s.Projects.List(r.Context(), userFrom(r), archived)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"projects": projects})
}

func (s *Server) handleCheckProjectKey(w http.ResponseWriter, r *http.Request) {
	check, err := s.Projects.CheckKey(r.Context(), r.URL.Query().Get("key"), r.URL.Query().Get("name"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"check": check})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req project.CreateInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Projects.Create(r.Context(), userFrom(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"project": made})
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.Projects.Get(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"project": p})
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	var req project.UpdateInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p, lsn, err := s.Projects.Update(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"project": p})
}

func (s *Server) handleChangeProjectKey(w http.ResponseWriter, r *http.Request) {
	var req project.KeyInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p, lsn, err := s.Projects.ChangeKey(r.Context(), userFrom(r), projectKeyParam(r), req.Key)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"project": p})
}

func (s *Server) handleArchiveProject(w http.ResponseWriter, r *http.Request) {
	s.setArchived(w, r, true)
}
func (s *Server) handleUnarchiveProject(w http.ResponseWriter, r *http.Request) {
	s.setArchived(w, r, false)
}

func (s *Server) setArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	p, lsn, err := s.Projects.SetArchived(r.Context(), userFrom(r), projectKeyParam(r), archived)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"project": p})
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Projects.Delete(r.Context(), userFrom(r), projectKeyParam(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

// projectError maps the projects' refusals; nil when err is none of theirs.
func projectError(err error) *APIError {
	var field *project.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, project.ErrNotFound):
		return ErrNotFound("That project was not found. Check its key, or ask one of its administrators to let you in.")
	case errors.Is(err, project.ErrNotAdmin):
		return ErrForbidden("Only an owner or an administrator of the organization makes projects. Ask one of them.")
	case errors.Is(err, project.ErrForbidden):
		return ErrForbidden("Only the project's administrators change it. Ask one of them.")
	case errors.Is(err, project.ErrArchived):
		return &APIError{Status: http.StatusConflict, Code: "project_archived",
			Message: "This project is archived. Unarchive it under its settings to change anything in it."}
	case errors.Is(err, project.ErrNotArchived):
		return &APIError{Status: http.StatusConflict, Code: "project_not_archived",
			Message: "Archive the project first. Deleting it removes every requirement and its history for good."}
	}
	return nil
}
