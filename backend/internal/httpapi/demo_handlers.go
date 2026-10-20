package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/cloudster1/exciter/backend/internal/demo"
)

var errDemoOff = &APIError{Status: http.StatusServiceUnavailable, Code: "suite_demo_unavailable",
	Message: "The suite demo is not set up on this server. Ask its operator to configure it."}

// mountSuiteDemo serves the suite demo to administrators.
func (s *Server) mountSuiteDemo(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(requireAdmin, s.demoOn)
		r.Get("/installation/suite-demo", s.handleGetSuiteDemo)
		r.Post("/installation/suite-demo", s.handleAddSuiteDemo)
		r.Post("/installation/suite-demo/links", s.handleLinkSuiteDemo)
	})
}

func (s *Server) demoOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Demo == nil {
			respondError(w, r, errDemoOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleGetSuiteDemo(w http.ResponseWriter, r *http.Request) {
	d, err := s.Demo.Status(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"demo": d})
}

func (s *Server) handleAddSuiteDemo(w http.ResponseWriter, r *http.Request) {
	d, lsn, err := s.Demo.Start(r.Context(), userFrom(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, demoError(err))
		return
	}
	respondJSON(w, r, http.StatusAccepted, map[string]any{"demo": d})
}

func (s *Server) handleLinkSuiteDemo(w http.ResponseWriter, r *http.Request) {
	d, lsn, err := s.Demo.Relink(r.Context(), userFrom(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, demoError(err))
		return
	}
	respondJSON(w, r, http.StatusAccepted, map[string]any{"demo": d})
}

func demoError(err error) error {
	var taken *demo.KeyTakenError
	switch {
	case errors.As(err, &taken):
		return ErrValidation(map[string]string{"key": taken.Error()})
	case errors.Is(err, demo.ErrExists):
		return ErrConflict("The suite demo was added already. Link it again to add the links it is missing.")
	case errors.Is(err, demo.ErrBusy):
		return ErrConflict("The suite demo is being built or linked. Wait until it is done, then try again.")
	case errors.Is(err, demo.ErrNone):
		return ErrNotFound("Nobody added the suite demo yet. Add it first.")
	}
	return err
}
