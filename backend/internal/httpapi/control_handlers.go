package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/signing"
)

// Signed baselines and locked requirements (#205). See docs/api-contract-m8.md.

// signer puts on a request the session and language a signature is given in (#46, #205).
func signer(r *http.Request) *http.Request {
	var session *uuid.UUID
	if p := PrincipalFrom(r.Context()); p != nil {
		session = p.SessionID
	}
	return r.WithContext(signing.With(r.Context(), session, language(r)))
}

// lockedError answers what the database refused because a requirement is locked, in the sentence it gave.
func lockedError(err error) *APIError {
	if sentence, ok := requirement.LockedSentence(err); ok {
		return &APIError{Status: http.StatusConflict, Code: "requirement_locked", Message: sentence}
	}
	return nil
}

// lockError maps the hand lock's refusals; nil when err is none of theirs.
func lockError(err error) *APIError {
	switch {
	case errors.Is(err, requirement.ErrNotLockKeeper):
		return ErrForbidden("Only the project's administrators lock and unlock its requirements by hand. Ask one of them.")
	case errors.Is(err, requirement.ErrSectionNotLocked):
		return &APIError{Status: http.StatusConflict, Code: "section_not_locked",
			Message: "A section or a requirement in the trash is not locked by hand. Lock the requirements in it instead."}
	case errors.Is(err, requirement.ErrNotLockedByHand):
		return &APIError{Status: http.StatusConflict, Code: "not_locked_by_hand",
			Message: "Nobody locked this requirement by hand. A lock by its status ends with a transition out of the status."}
	}
	return nil
}

// signError maps what refuses a baseline's signature, before the baselines' other refusals.
func signError(err error) error {
	var field *baseline.FieldError
	switch {
	case errors.As(err, &field):
		return err
	case errors.Is(err, signing.ErrNeedsSession):
		return &APIError{Status: http.StatusForbidden, Code: "session_only",
			Message: "Signing a baseline is an electronic signature, which a token cannot give. Sign in, and sign there."}
	case errors.Is(err, signing.ErrTooManyAttempts):
		return &APIError{Status: http.StatusTooManyRequests, Code: "too_many_attempts",
			Message: "Too many wrong passwords were given to sign. Wait a quarter of an hour, then sign again."}
	case errors.Is(err, signing.ErrSpent):
		return &APIError{Status: http.StatusConflict, Code: "signature_spent",
			Message: "That sign-in has been used for a signature already, or it has expired. Sign in again at the identity provider, then sign."}
	case errors.Is(err, baseline.ErrNoSigningRule):
		return &APIError{Status: http.StatusConflict, Code: "no_signing_rule",
			Message: "Nobody signs this project's baselines yet. Ask a project administrator to name who signs them."}
	case errors.Is(err, baseline.ErrNotSigner):
		return &APIError{Status: http.StatusForbidden, Code: "not_a_signer",
			Message: "You are not one of the people who sign this baseline. Ask a project administrator if you should be."}
	case errors.Is(err, baseline.ErrAlreadySigned):
		return &APIError{Status: http.StatusConflict, Code: "already_signed", Message: "You signed this baseline already."}
	case errors.Is(err, baseline.ErrChanged):
		return &APIError{Status: http.StatusConflict, Code: "baseline_changed",
			Message: "The baseline changed since you opened it. Read it again, then sign."}
	}
	return err
}

func (s *Server) handleGetBaselineSigning(w http.ResponseWriter, r *http.Request) {
	rule, err := s.Baselines.SigningRule(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rule": rule})
}

func (s *Server) handleSaveBaselineSigning(w http.ResponseWriter, r *http.Request) {
	var req baseline.SigningRuleInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	rule, lsn, err := s.Baselines.SaveSigningRule(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rule": rule})
}

func (s *Server) handleDeleteBaselineSigning(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Baselines.DeleteSigningRule(r.Context(), userFrom(r), projectKeyParam(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleListBaselineSignatures(w http.ResponseWriter, r *http.Request) {
	id, ok := baselineID(w, r)
	if !ok {
		return
	}
	all, err := s.Baselines.Signatures(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"signatures": all})
}

func (s *Server) handleSignBaseline(w http.ResponseWriter, r *http.Request) {
	id, ok := baselineID(w, r)
	if !ok {
		return
	}
	var req baseline.SignInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	b, lsn, err := s.Baselines.Sign(r.Context(), userFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, signError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"baseline": b})
}

func (s *Server) handleLockRequirement(w http.ResponseWriter, r *http.Request) {
	var req requirement.LockInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Requirements.Lock(r.Context(), userFrom(r), reqKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requirement": got})
}

func (s *Server) handleUnlockRequirement(w http.ResponseWriter, r *http.Request) {
	got, lsn, err := s.Requirements.Unlock(r.Context(), userFrom(r), reqKeyParam(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requirement": got})
}
