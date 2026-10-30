package httpapi

import (
	"errors"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/stator"
)

// Connecting Stator (#40): the installation's instance for its administrators, and each member's own token, as
// Armature's. See docs/api-contract-m4.md.

var errStatorOff = &APIError{Status: http.StatusServiceUnavailable, Code: "stator_unavailable",
	Message: "This server is not set up to reach Stator. Ask the operator to check its configuration."}

// statorOn refuses every Stator route of a server built without the service.
func (s *Server) statorOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Stator == nil {
			respondError(w, r, errStatorOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// statorError maps Stator's answers and refusals; nil when err is none of theirs.
func statorError(err error) *APIError {
	var (
		field   *stator.FieldError
		refused *stator.RefusedError
	)
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.As(err, &refused):
		// A 401 is never relayed as one, which would read as "sign in to Exciter".
		status := refused.Status
		if status == http.StatusUnauthorized {
			status = http.StatusConflict
		}
		return &APIError{Status: status, Code: refused.Code, Message: sentence(refused.Message), Fields: refused.Fields, cause: err}
	case errors.Is(err, stator.ErrNotConfigured):
		return &APIError{Status: http.StatusConflict, Code: "stator_not_configured",
			Message: "Exciter is not connected to Stator yet. Ask an administrator to connect it under Admin, Stator."}
	case errors.Is(err, stator.ErrNotConnected):
		return &APIError{Status: http.StatusConflict, Code: "stator_not_connected",
			Message: "You have not connected your Stator account. Paste a Stator token under Your account, Stator account, then try again."}
	case errors.Is(err, stator.ErrRejected):
		return &APIError{Status: http.StatusConflict, Code: "stator_rejected",
			Message: "Stator no longer accepts your token. Make a new one under Your account, Access tokens in Stator and paste it under Your account, Stator account."}
	case errors.Is(err, stator.ErrUnreachable):
		return &APIError{Status: http.StatusBadGateway, Code: "stator_unreachable",
			Message: "Stator did not answer. Try again in a moment, and tell an administrator if it keeps happening.", cause: err}
	}
	return nil
}

func (s *Server) handleGetStatorConnection(w http.ResponseWriter, r *http.Request) {
	connection, err := s.Stator.Connection(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"connection": connection})
}

func (s *Server) handleSaveStatorConnection(w http.ResponseWriter, r *http.Request) {
	var req stator.ConnectionInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	connection, lsn, err := s.Stator.SaveConnection(r.Context(), req, s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"connection": connection})
}

func (s *Server) handleRemoveStatorConnection(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Stator.RemoveConnection(r.Context(), s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleGetStatorAccount(w http.ResponseWriter, r *http.Request) {
	account, err := s.Stator.Account(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleStoreStatorToken(w http.ResponseWriter, r *http.Request) {
	var req stator.TokenInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	account, lsn, err := s.Stator.Connect(r.Context(), req.Token)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleCheckStatorAccount(w http.ResponseWriter, r *http.Request) {
	account, lsn, err := s.Stator.Check(r.Context())
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleRemoveStatorToken(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Stator.Disconnect(r.Context())
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
