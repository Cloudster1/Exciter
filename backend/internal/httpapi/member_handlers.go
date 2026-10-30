package httpapi

import (
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// The installation's people, for its administrators.

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
	// Role is what the person is added as: member, guest, or admin, a member who holds the administrator role.
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
	member, lsn, err := s.Accounts.CreateMember(r.Context(), auth.NewMember{Email: req.Email, Name: req.Name, Role: req.Role, Password: req.Password}, userFrom(r), s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"member": member})
}

type updateMemberRequest struct {
	// Role is the member's new standing: member or guest. Absent leaves it.
	Role *auth.OrgRole `json:"role,omitempty"`
	// Active switches the member's account on or off. Absent leaves it.
	Active *bool `json:"active,omitempty"`
}

func (s *Server) handleUpdateMember(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	userID, apiErr := pathUUID(r, "userID", "person's")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req updateMemberRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.Role == nil && req.Active == nil {
		respondError(w, r, ErrValidation(map[string]string{"role": "Make the person a member or a guest, or switch their account off or on."}))
		return
	}
	var member *auth.Member
	if req.Role != nil {
		var lsn db.LSN
		var err error
		member, lsn, err = s.Accounts.SetRole(r.Context(), userID, *req.Role, userFrom(r), s.clientIP(r))
		noteWrite(r.Context(), lsn)
		if err != nil {
			respondError(w, r, err)
			return
		}
	}
	if req.Active != nil {
		var lsn db.LSN
		var err error
		member, lsn, err = s.Accounts.SetActive(r.Context(), userID, *req.Active, userFrom(r), s.clientIP(r))
		noteWrite(r.Context(), lsn)
		if err != nil {
			respondError(w, r, err)
			return
		}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"member": member})
}

func (s *Server) handleSwitchOffMember(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	userID, apiErr := pathUUID(r, "userID", "person's")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	_, lsn, err := s.Accounts.SetActive(r.Context(), userID, false, userFrom(r), s.clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
