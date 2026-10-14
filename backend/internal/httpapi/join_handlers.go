package httpapi

import (
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/auth"
)

// People the identity provider vouched for who found no membership here, and
// their administrators letting them in or turning them away, as in Armature.

type admitRequest struct {
	// Role is the standing the person gets: member, or admin.
	Role auth.OrgRole `json:"role"`
}

func (s *Server) handleListJoinRequests(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	requests, err := s.Accounts.JoinRequests(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requests": requests})
}

func (s *Server) handleAdmitJoinRequest(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	userID, apiErr := pathUUID(r, "userID", "person's")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req admitRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p := PrincipalFrom(r.Context())
	membership, lsn, err := s.Accounts.AdmitJoinRequest(r.Context(), userID, req.Role, p.UserID, s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"membership": membership})
}

func (s *Server) handleDeclineJoinRequest(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	userID, apiErr := pathUUID(r, "userID", "person's")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	p := PrincipalFrom(r.Context())
	lsn, err := s.Accounts.DeclineJoinRequest(r.Context(), userID, p.UserID, s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
