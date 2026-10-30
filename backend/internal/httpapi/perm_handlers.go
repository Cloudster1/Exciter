package httpapi

import (
	"errors"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/perm"
)

// Permissions (#12, #471): who may do what, and why.

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
	respondJSON(w, r, http.StatusOK, map[string]any{"can": perm.Mine(PrincipalFrom(r.Context()))})
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
	case errors.Is(err, perm.ErrNotAdministrator):
		return ErrForbidden("Only an administrator sees and changes who may do what across Exciter. Ask one.")
	case errors.Is(err, perm.ErrArchived):
		return &APIError{Status: http.StatusConflict, Code: "project_archived",
			Message: "This project is archived. Unarchive it under its settings to change anything in it."}
	case errors.Is(err, perm.ErrNoAdministrator):
		return &APIError{Status: http.StatusConflict, Code: "no_administrator",
			Message: "This is the last grant that lets somebody run the project. Grant project administrator to another person or group first."}
	case errors.Is(err, perm.ErrLastAdministrator):
		return &APIError{Status: http.StatusConflict, Code: "last_administrator",
			Message: "Exciter keeps at least one active administrator. Make somebody else an administrator first, then take it from this one."}
	case errors.Is(err, perm.ErrNoSuchRole):
		return ErrNotFound("There is no such role. Reload the list to see the roles there are.")
	case errors.Is(err, perm.ErrBuiltinRole):
		return ErrConflict("A built-in role cannot be deleted. Take its grants away instead.")
	case errors.Is(err, perm.ErrNoSuchGroup):
		return ErrNotFound("There is no such group. Reload the list; somebody may have deleted it already.")
	case errors.Is(err, perm.ErrBuiltinGroup):
		return ErrConflict("Everyone holds every member, so it is not renamed, filled or deleted by hand.")
	case errors.Is(err, perm.ErrProviderGroup):
		return ErrConflict("The identity provider fills this group at every sign-in, so its members are not changed here. Change them in the provider.")
	case errors.Is(err, perm.ErrNoSuchAssignment):
		return ErrNotFound("There is no such grant. Reload the list; somebody may have taken it away already.")
	}
	return nil
}
