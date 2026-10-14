package httpapi

import (
	"errors"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/armature"
)

// Connecting Armature (#26): the installation's instance for its administrators, and each member's own token,
// as Stator's. See docs/api-contract-m2.md.

var errArmatureOff = &APIError{Status: http.StatusServiceUnavailable, Code: "armature_unavailable",
	Message: "This server is not set up to reach Armature. Ask the operator to check its configuration."}

// armatureOn refuses every Armature route of a server built without the service.
func (s *Server) armatureOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Armature == nil {
			respondError(w, r, errArmatureOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// armatureError maps Armature's answers and refusals; nil when err is none of theirs.
func armatureError(err error) *APIError {
	var (
		field   *armature.FieldError
		refused *armature.RefusedError
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
	case errors.Is(err, armature.ErrNotConfigured):
		return &APIError{Status: http.StatusConflict, Code: "armature_not_configured",
			Message: "Exciter is not connected to Armature yet. Ask an administrator to connect it under Settings, Armature."}
	case errors.Is(err, armature.ErrNotConnected):
		return &APIError{Status: http.StatusConflict, Code: "armature_not_connected",
			Message: "You have not connected your Armature account. Paste an Armature token under Your account, Armature account, then try again."}
	case errors.Is(err, armature.ErrRejected):
		return &APIError{Status: http.StatusConflict, Code: "armature_rejected",
			Message: "Armature no longer accepts your token. Make a new one under Tokens in Armature and paste it under Your account, Armature account."}
	case errors.Is(err, armature.ErrUnreachable):
		return &APIError{Status: http.StatusBadGateway, Code: "armature_unreachable",
			Message: "Armature did not answer. Try again in a moment, and tell an administrator if it keeps happening.", cause: err}
	}
	return nil
}

func (s *Server) handleGetArmatureConnection(w http.ResponseWriter, r *http.Request) {
	connection, err := s.Armature.Connection(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"connection": connection})
}

func (s *Server) handleSaveArmatureConnection(w http.ResponseWriter, r *http.Request) {
	var req armature.ConnectionInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	connection, lsn, err := s.Armature.SaveConnection(r.Context(), req, s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"connection": connection})
}

func (s *Server) handleRemoveArmatureConnection(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Armature.RemoveConnection(r.Context(), s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleGetArmatureAccount(w http.ResponseWriter, r *http.Request) {
	account, err := s.Armature.Account(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleStoreArmatureToken(w http.ResponseWriter, r *http.Request) {
	var req armature.TokenInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	account, lsn, err := s.Armature.Connect(r.Context(), req.Token)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleCheckArmatureAccount(w http.ResponseWriter, r *http.Request) {
	account, lsn, err := s.Armature.Check(r.Context())
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleRemoveArmatureToken(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Armature.Disconnect(r.Context())
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
