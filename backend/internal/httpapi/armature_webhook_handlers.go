package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/cloudster1/exciter/backend/internal/armature"
)

// Armature's webhooks (#29): vouched for by the installation's secret alone, so nobody signs in.

func (s *Server) handleArmatureWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, armature.WebhookMaxBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		respondError(w, r, &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
			Message: "A webhook delivery may be at most " + strconv.Itoa(armature.WebhookMaxBytes>>20) + " MiB. Subscribe the endpoint to the topics Exciter lists only."})
		return
	}
	if err != nil {
		respondError(w, r, ErrBadRequest("The delivery could not be read. Armature sends it again later."))
		return
	}
	err = s.Armature.Webhook(r.Context(), body, armature.WebhookHeaders{
		Signature:            r.Header.Get(armature.SignatureHeader),
		Timestamp:            r.Header.Get(armature.TimestampHeader),
		TimestampedSignature: r.Header.Get(armature.TimestampedSignatureHeader),
	})
	switch {
	case errors.Is(err, armature.ErrBadSignature):
		respondError(w, r, &APIError{Status: http.StatusUnauthorized, Code: "bad_signature",
			Message: "The signature does not match. Paste the endpoint's secret from Armature under Settings, Armature, then redeliver."})
	case errors.Is(err, armature.ErrUnknownVersion):
		respondError(w, r, &APIError{Status: http.StatusUnprocessableEntity, Code: "unknown_version",
			Message: "Exciter reads webhook envelopes of version " + strconv.Itoa(armature.WebhookVersion) + ". Register the endpoint in Armature for that version."})
	case err != nil:
		respondError(w, r, err)
	default:
		respondNoContent(w)
	}
}
