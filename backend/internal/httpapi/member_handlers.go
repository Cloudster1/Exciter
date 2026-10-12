package httpapi

import (
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/auth"
)

// The organization's people, for its administrators.

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	members, err := s.Accounts.Members(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"members": members})
}

type createMemberRequest struct {
	// Email is the address the person signs in with.
	Email string `json:"email"`
	// Name is what the person is called; blank takes the part of the address before the at sign.
	Name string `json:"name"`
	// Role is the standing the person gets: admin, member or guest.
	Role auth.OrgRole `json:"role"`
	// Password is the person's first password, at least 12 characters. Blank has one made, which the answer shows once.
	Password string `json:"password"`
}

func (s *Server) handleCreateMember(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	var req createMemberRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	member, lsn, err := s.Accounts.CreateMember(r.Context(), auth.NewMember{Email: req.Email, Name: req.Name, Role: req.Role, Password: req.Password}, userFrom(r), clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"member": member})
}

type setRoleRequest struct {
	// Role is the member's new standing: admin, member or guest.
	Role auth.OrgRole `json:"role"`
}

func (s *Server) handleSetMemberRole(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	userID, apiErr := pathUUID(r, "userID", "person's")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req setRoleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	member, lsn, err := s.Accounts.SetRole(r.Context(), userID, req.Role, userFrom(r), clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"member": member})
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	userID, apiErr := pathUUID(r, "userID", "person's")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Accounts.RemoveMember(r.Context(), userID, userFrom(r), clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
