package httpapi

import (
	"errors"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/webhook"
)

// Webhooks (#48): where the installation's requirement events are posted, kept by its administrators as in Stator.
// Adapted from Stator's handlers.

// errWebhooksOff answers for a server built without webhooks, which only a
// test does.
var errWebhooksOff = &APIError{Status: http.StatusServiceUnavailable, Code: "webhooks_unavailable",
	Message: "Webhooks are not set up on this server. Ask its operator to configure them."}

// webhookError maps the webhooks' refusals.
func webhookError(err error) *APIError {
	var field *webhook.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, webhook.ErrNotFound):
		return ErrNotFound("That webhook or delivery is not here. It may have been deleted; reload the list.")
	case errors.Is(err, webhook.ErrNoKey):
		return &APIError{Status: http.StatusServiceUnavailable, Code: "secret_key_missing",
			Message: "Webhooks keep their secrets sealed with the server's key, and this server has none. Ask its operator to set EXCITER_SECRET_KEY."}
	}
	return nil
}

func (s *Server) webhooksOn(w http.ResponseWriter, r *http.Request) bool {
	if s.Webhooks == nil {
		respondError(w, r, errWebhooksOff)
		return false
	}
	return true
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	hooks, err := s.Webhooks.List(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"webhooks": hooks})
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	var req webhook.WebhookInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Webhooks.Create(r.Context(), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"webhook": made})
}

func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "webhookID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req webhook.WebhookInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Webhooks.Update(r.Context(), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"webhook": updated})
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "webhookID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Webhooks.Delete(r.Context(), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "webhookID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	rotated, lsn, err := s.Webhooks.RotateSecret(r.Context(), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"webhook": rotated})
}

func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "webhookID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	sent, lsn, err := s.Webhooks.Test(r.Context(), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"delivery": sent})
}

func (s *Server) handleListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "webhookID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	limit, apiErr := queryLimit(r, webhook.DefaultDeliveries, webhook.MaxDeliveries)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	deliveries, err := s.Webhooks.Deliveries(r.Context(), id, limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"deliveries": deliveries})
}

func (s *Server) handleRedeliverWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "webhookID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	deliveryID, apiErr := pathUUID(r, "deliveryID", "delivery")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	sent, lsn, err := s.Webhooks.Redeliver(r.Context(), id, deliveryID)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"delivery": sent})
}
