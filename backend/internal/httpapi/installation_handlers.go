package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/installation"
)

// errInstallationOff answers for a server built without the installation service, which only a test does.
var errInstallationOff = &APIError{Status: http.StatusServiceUnavailable, Code: "installation_unavailable",
	Message: "The installation's settings are not set up on this server. Ask its operator to configure it."}

type renameInstallationRequest struct {
	Name string `json:"name"`
}

func (s *Server) installationOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Installation == nil {
			respondError(w, r, errInstallationOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleGetInstallation(w http.ResponseWriter, r *http.Request) {
	inst, err := s.Installation.Get(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"installation": inst})
}

func (s *Server) handleRenameInstallation(w http.ResponseWriter, r *http.Request) {
	var req renameInstallationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	inst, lsn, err := s.Installation.Rename(r.Context(), userFrom(r), req.Name)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, installationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"installation": inst})
}

func installationError(err error) error {
	switch {
	case errors.Is(err, installation.ErrNameRequired):
		return ErrValidation(map[string]string{"name": "Give the installation a name."})
	case errors.Is(err, installation.ErrNameTooLong):
		return ErrValidation(map[string]string{"name": fmt.Sprintf("Keep the name to %d characters.", installation.MaxNameLength)})
	case errors.Is(err, installation.ErrNotFound):
		return ErrForbidden("Only an administrator can rename the installation. Ask one of them.")
	}
	return err
}
