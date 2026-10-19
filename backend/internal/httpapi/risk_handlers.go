package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/risk"
)

// riskError maps the refusals of risk schemes; nil when err is none of theirs.
func riskError(err error) *APIError {
	var (
		field *risk.FieldError
		held  *risk.ScaleHeldError
	)
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.As(err, &held) && held.Dimension == risk.Detection && held.Value == 0:
		return &APIError{Status: http.StatusConflict, Code: "risk_scale_held",
			Message: "Risks are rated on detection. Keep detection on, or clear it from their ratings first."}
	case errors.As(err, &held) && held.Value > 0:
		return &APIError{Status: http.StatusConflict, Code: "risk_scale_held",
			Message: fmt.Sprintf("A risk is rated %s %d, which stays while it is. Keep the value, or change the ratings first.", held.Dimension, held.Value)}
	case errors.As(err, &held):
		return &APIError{Status: http.StatusConflict, Code: "risk_scale_held",
			Message: "A risk is rated on a value the scheme would take away. Keep the values in use, or change the ratings first."}
	case errors.Is(err, risk.ErrForbidden):
		return ErrForbidden("Only the project's administrators set its risk scheme. Ask one of them.")
	case errors.Is(err, risk.ErrNotFound):
		return ErrNotFound("That risk was not found. It may have been deleted, or be of a project you may not view.")
	}
	return nil
}

func (s *Server) handleGetRiskScheme(w http.ResponseWriter, r *http.Request) {
	scheme, err := s.Risks.Scheme(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"scheme": scheme})
}

func (s *Server) handleSaveRiskScheme(w http.ResponseWriter, r *http.Request) {
	var in risk.SchemeInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	scheme, lsn, err := s.Risks.SaveScheme(r.Context(), userFrom(r), projectKeyParam(r), in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"scheme": scheme})
}
