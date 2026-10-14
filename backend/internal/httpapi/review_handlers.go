package httpapi

import (
	"errors"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/review"
)

// Review and approval of requirement revisions (#35). See docs/api-contract-m3.md.

var errReviewsOff = &APIError{Status: http.StatusServiceUnavailable, Code: "reviews_unavailable",
	Message: "This server is not set up for reviews. Ask the operator to check its configuration."}

func (s *Server) reviewsOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Reviews == nil {
			respondError(w, r, errReviewsOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// reviewError maps the reviews' refusals; nil when err is none of theirs.
func reviewError(err error) *APIError {
	var field *review.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, review.ErrNotFound):
		return ErrNotFound("That review was not found. Reload the requirement to see its reviews.")
	case errors.Is(err, review.ErrRuleNotFound):
		return ErrNotFound("That approval rule was not found. Reload the project's settings to see its rules.")
	case errors.Is(err, review.ErrNotAdministrator):
		return ErrForbidden("Only the project's administrators set who approves its requirements. Ask one of them.")
	case errors.Is(err, review.ErrNoRule):
		return &APIError{Status: http.StatusConflict, Code: "no_approval_rule",
			Message: "Nobody approves requirements of this type yet. Ask one of the project's administrators to set an approval rule."}
	case errors.Is(err, review.ErrPending):
		return &APIError{Status: http.StatusConflict, Code: "review_pending",
			Message: "A review of this requirement is already waiting. Let it finish, or withdraw it first."}
	case errors.Is(err, review.ErrRevisionMovedOn):
		return &APIError{Status: http.StatusConflict, Code: "revision_moved_on",
			Message: "The requirement has a newer revision than the one you saw. Reload it and read it again."}
	case errors.Is(err, review.ErrSection):
		return &APIError{Status: http.StatusConflict, Code: "section_not_reviewed",
			Message: "A section only groups requirements, so it has nothing to approve. Ask for a review of a requirement in it."}
	case errors.Is(err, review.ErrClosed):
		return &APIError{Status: http.StatusConflict, Code: "review_closed",
			Message: "This review is no longer waiting. Reload the requirement to see how it ended."}
	case errors.Is(err, review.ErrNotApprover):
		return &APIError{Status: http.StatusForbidden, Code: "not_an_approver",
			Message: "You are not one of this review's approvers. Ask one of the project's administrators if you should be."}
	case errors.Is(err, review.ErrOwnRevision):
		return &APIError{Status: http.StatusForbidden, Code: "own_revision",
			Message: "You wrote this revision, so somebody else approves it."}
	case errors.Is(err, review.ErrNotRequester):
		return ErrForbidden("Only whoever asked for this review, or the project's administrators, withdraw it.")
	case errors.Is(err, review.ErrSignedOut):
		return ErrUnauthorized("Sign in to take part in reviews.")
	}
	return nil
}

func (s *Server) handleListApprovalRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Reviews.Rules(r.Context(), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rules": rules})
}

func (s *Server) handleSaveApprovalRule(w http.ResponseWriter, r *http.Request) {
	var req review.RuleInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	rule, lsn, err := s.Reviews.SaveRule(r.Context(), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rule": rule})
}

func (s *Server) handleDeleteApprovalRule(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "ruleID", "approval rule")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Reviews.DeleteRule(r.Context(), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetApproval(w http.ResponseWriter, r *http.Request) {
	approval, err := s.Reviews.Approval(r.Context(), reqKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"approval": approval})
}

func (s *Server) handleListReviews(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, review.DefaultLimit, review.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	reviews, total, err := s.Reviews.List(r.Context(), reqKeyParam(r), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"reviews": reviews, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleRequestReview(w http.ResponseWriter, r *http.Request) {
	var req review.RequestInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Reviews.Request(r.Context(), reqKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"review": made})
}

func (s *Server) handleListWaitingReviews(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, review.DefaultLimit, review.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	reviews, total, err := s.Reviews.Waiting(r.Context(), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"reviews": reviews, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleGetReview(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "reviewID", "review")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, err := s.Reviews.Get(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"review": found})
}

func (s *Server) handleDecideReview(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "reviewID", "review")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req review.DecisionInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	decided, lsn, err := s.Reviews.Decide(r.Context(), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"review": decided})
}

func (s *Server) handleWithdrawReview(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "reviewID", "review")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	withdrawn, lsn, err := s.Reviews.Withdraw(r.Context(), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"review": withdrawn})
}
