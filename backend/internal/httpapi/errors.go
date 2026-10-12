// Package httpapi is the HTTP surface: routing, middleware and handlers.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// APIError is the single error shape every endpoint returns, so that clients
// have exactly one thing to parse.
type APIError struct {
	// Status is the HTTP status code; it is not serialised.
	Status int `json:"-"`
	// Code is a stable machine readable identifier such as "not_found".
	Code string `json:"code"`
	// Message is a sentence a person can read and act on.
	Message string `json:"message"`
	// Fields carries per-field validation messages keyed by field name.
	Fields map[string]string `json:"fields,omitempty"`
	// RequestID lets a user quote something we can find in the logs.
	RequestID string `json:"requestId,omitempty"`

	// cause is logged but never sent to the client.
	cause error
}

func (e *APIError) Error() string {
	if e.cause != nil {
		return e.Code + ": " + e.Message + ": " + e.cause.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *APIError) Unwrap() error { return e.cause }

// errorEnvelope is how every failure is written.
type errorEnvelope struct {
	Error APIError `json:"error"`
}

func ErrBadRequest(message string) *APIError {
	return &APIError{Status: http.StatusBadRequest, Code: "bad_request", Message: message}
}

func ErrValidation(fields map[string]string) *APIError {
	return &APIError{
		Status:  http.StatusUnprocessableEntity,
		Code:    "validation_failed",
		Message: "Some fields need attention.",
		Fields:  fields,
	}
}

func ErrUnauthorized(message string) *APIError {
	if message == "" {
		message = "Sign in to continue."
	}
	return &APIError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: message}
}

func ErrForbidden(message string) *APIError {
	if message == "" {
		message = "You do not have permission to do that."
	}
	return &APIError{Status: http.StatusForbidden, Code: "forbidden", Message: message}
}

// ErrNotFound is also what a caller gets for a resource in another tenant:
// existence itself is privileged information.
func ErrNotFound(what string) *APIError {
	if what == "" {
		what = "That was not found."
	}
	return &APIError{Status: http.StatusNotFound, Code: "not_found", Message: what}
}

func ErrConflict(message string) *APIError {
	return &APIError{Status: http.StatusConflict, Code: "conflict", Message: message}
}

func ErrInternal(cause error) *APIError {
	return &APIError{
		Status:  http.StatusInternalServerError,
		Code:    "internal_error",
		Message: "Something went wrong on our side. Try again, and quote the request id if it keeps happening.",
		cause:   cause,
	}
}

// errNotImplemented answers an operation the API describes but does not serve yet.
var errNotImplemented = &APIError{Status: http.StatusNotImplemented, Code: "not_implemented",
	Message: "This part of the API is not built yet. Update Exciter to a release that has it, or leave it out for now."}

// toAPIError maps a domain error onto the wire shape. One place for it is what
// stops handlers leaking internals into responses by accident.
func toAPIError(err error) *APIError {
	var apiErr *APIError
	switch {
	case errors.As(err, &apiErr):
		return apiErr
	case errors.Is(err, tenant.ErrNoTenant):
		return &APIError{Status: http.StatusBadRequest, Code: "no_organization", Message: "Select an organization first."}
	default:
		return ErrInternal(err)
	}
}

// respondError writes err in the envelope and logs the cause when there is one
// worth seeing.
func respondError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := toAPIError(err)
	body := *apiErr
	body.RequestID = RequestIDFrom(r.Context())

	log := loggerFrom(r.Context())
	if body.Status >= http.StatusInternalServerError {
		log.Error("request failed", "code", body.Code, "error", err)
	} else {
		log.Debug("request rejected", "code", body.Code, "status", body.Status, "message", body.Message)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(body.Status)
	if encErr := json.NewEncoder(w).Encode(errorEnvelope{Error: body}); encErr != nil {
		log.Error("failed to write error response", "error", encErr)
	}
}

// respondJSON writes a successful response.
func respondJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent, so there is nothing to do but note it.
		loggerFrom(r.Context()).Error("failed to write response body", "error", err)
	}
}
