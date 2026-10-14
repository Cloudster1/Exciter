package httpapi

import (
	"errors"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/perm"
)

// Permissions (#12): who may do what in a project, and why.

var errPermsOff = &APIError{Status: http.StatusServiceUnavailable, Code: "permissions_unavailable",
	Message: "Permissions are not set up on this server. Ask its operator to configure it."}

// permsOn refuses every permission route of a server built without the service.
func (s *Server) permsOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Perms == nil {
			respondError(w, r, errPermsOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleMyAccess(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]any{"can": perm.Mine(PrincipalFrom(r.Context()).Role)})
}

func (s *Server) handleListPeople(w http.ResponseWriter, r *http.Request) {
	limit, apiErr := queryLimit(r, perm.DefaultPickerLimit, perm.MaxPickerLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	people, err := s.Perms.People(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"people": people})
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	limit, apiErr := queryLimit(r, perm.DefaultPickerLimit, perm.MaxPickerLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	groups, err := s.Perms.Groups(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"groups": groups})
}

func (s *Server) handleListProjectGrants(w http.ResponseWriter, r *http.Request) {
	p, err := s.Projects.Get(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	grants, err := s.Perms.Grants(r.Context(), userFrom(r), p.ID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"grants": grants})
}

func (s *Server) handleSetProjectGrants(w http.ResponseWriter, r *http.Request) {
	var req perm.GrantsInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p, err := s.Projects.Get(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	grants, lsn, err := s.Perms.ReplaceGrants(r.Context(), userFrom(r), p.ID, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"grants": grants})
}

func (s *Server) handleInspectProjectAccess(w http.ResponseWriter, r *http.Request) {
	person, apiErr := pathUUID(r, "userID", "person")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	p, err := s.Projects.Get(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	report, err := s.Perms.Access(r.Context(), userFrom(r), p.ID, person)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"access": report})
}

// permError maps the permissions' refusals; nil when err is none of theirs.
func permError(err error) *APIError {
	var field *perm.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, perm.ErrForbidden):
		return ErrForbidden("Only the project's administrators see and change who may do what in it. Ask one of them.")
	case errors.Is(err, perm.ErrArchived):
		return &APIError{Status: http.StatusConflict, Code: "project_archived",
			Message: "This project is archived. Unarchive it under its settings to change anything in it."}
	case errors.Is(err, perm.ErrNoAdministrator):
		return &APIError{Status: http.StatusConflict, Code: "no_administrator",
			Message: "Give at least one person or group the admin role, so somebody besides the administrators of Exciter can manage the project."}
	}
	return nil
}
