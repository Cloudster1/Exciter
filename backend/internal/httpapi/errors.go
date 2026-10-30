// Package httpapi is the HTTP surface: routing, middleware and handlers.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/rql"
	"github.com/cloudster1/exciter/backend/internal/theme"
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
	// Position is the 1-based character a query went wrong at, with bad_query, as Armature and Stator report it.
	Position *int `json:"position,omitempty"`
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

// ErrNotFound is also what a caller gets for a resource behind a wall:
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

// errTimedOut answers a request that ran past the request deadline, which a busy server, not a fault, can cause.
var errTimedOut = &APIError{Status: http.StatusServiceUnavailable, Code: "timed_out",
	Message: "The server took too long to answer. Try again in a moment, or ask for less at once if it keeps happening."}

// errNotImplemented answers an operation the API describes but does not serve yet.
var errNotImplemented = &APIError{Status: http.StatusNotImplemented, Code: "not_implemented",
	Message: "This part of the API is not built yet. Update Exciter to a release that has it, or leave it out for now."}

// toAPIError maps a domain error onto the wire shape. One place for it is what
// stops handlers leaking internals into responses by accident.
func toAPIError(err error) *APIError {
	var apiErr *APIError
	var invalid *oidc.ValidationError
	var query *rql.Error
	switch {
	case errors.As(err, &apiErr):
		return apiErr
	case errors.As(err, &query):
		return &APIError{Status: http.StatusBadRequest, Code: "bad_query", Message: query.Msg, Position: &query.Pos}
	case errors.As(err, &invalid):
		return ErrValidation(map[string]string{invalid.Field: invalid.Message})
	case errors.Is(err, auth.ErrInvalidToken):
		return ErrUnauthorized("Your session has expired. Sign in again.")
	case errors.Is(err, auth.ErrInvalidCredentials):
		return &APIError{Status: http.StatusUnauthorized, Code: "invalid_credentials",
			Message: "That email and password do not match an account. Check both and try again, or sign in with single sign-on."}
	case errors.Is(err, auth.ErrSwitchedOff):
		return &APIError{Status: http.StatusForbidden, Code: "account_switched_off", Message: "Your account has been switched off. Ask an administrator."}
	case errors.Is(err, auth.ErrBadLocale):
		return ErrValidation(map[string]string{"locale": "Choose English, German, or the language of your browser."})
	case errors.Is(err, auth.ErrBadTimezone):
		return ErrValidation(map[string]string{"timezone": "Choose a time zone from the list, or the one of your browser."})
	case errors.Is(err, auth.ErrNoSuchMember):
		return ErrNotFound("That person is not a member here. Reload the list to see who is.")
	case errors.Is(err, auth.ErrLastAdministrator):
		return &APIError{Status: http.StatusConflict, Code: "last_administrator",
			Message: "The last active administrator cannot be switched off or made a guest. Make somebody else an administrator first."}
	case errors.Is(err, auth.ErrSwitchSelfOff):
		return ErrConflict("You cannot switch yourself off. Ask another administrator to do it.")
	case errors.Is(err, auth.ErrChangeOwnRole):
		return ErrConflict("You cannot change your own role. Ask another administrator to do it.")
	case errors.Is(err, auth.ErrBadRole):
		return ErrValidation(map[string]string{"role": "Make the person a member or a guest. To let them administer, grant them the administrator role under Access."})
	case errors.Is(err, auth.ErrBadNewRole):
		return ErrValidation(map[string]string{"role": "Add the person as an admin, a member or a guest."})
	case errors.Is(err, auth.ErrBadMemberEmail):
		return ErrValidation(map[string]string{"email": sentence(err.Error())})
	case errors.Is(err, auth.ErrBadMemberName):
		return ErrValidation(map[string]string{"name": sentence(err.Error())})
	case errors.Is(err, auth.ErrPasswordTooShort):
		return ErrValidation(map[string]string{"password": sentence(err.Error()) + " Leave it empty to have one made."})
	case errors.Is(err, auth.ErrAlreadyMember):
		return ErrConflict("That person is already a member. Reload the list to find them.")
	case errors.Is(err, auth.ErrNoSuchRequest):
		return ErrNotFound("That person is not waiting to be let in. Reload the list; somebody may have answered already.")
	case errors.Is(err, auth.ErrBadJoinRole):
		return ErrValidation(map[string]string{"role": "Let the person in as member or admin."})
	case errors.Is(err, oidc.ErrNotConfigured):
		return &APIError{Status: http.StatusNotFound, Code: "sso_not_configured",
			Message: "Exciter does not sign in through an identity provider yet. Sign in with a password, or ask an administrator to set one up."}
	default:
		if apiErr := lockedError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := lockError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := fieldRuleError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := sessionError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := projectError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := permError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := requirementError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := attributeError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := workflowError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := labelError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := searchError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := webhookError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := traceError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := coverageError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := chainError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := evidenceError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := codeError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := qualityError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := glossaryError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := dashboardError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := templateError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := testError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := riskError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := duplicatesError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := attachmentError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := tokenError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := oauthError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := importError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := reqifError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := documentError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := commentError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := notifyError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := reviewError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := baselineError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := armatureLinkError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := armatureError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := statorLinkError(err); apiErr != nil {
			return apiErr
		}
		if apiErr := statorError(err); apiErr != nil {
			return apiErr
		}
		return themeError(err)
	}
}

// themeError maps the themes' refusals, and the file storage's they meet; anything else is ours.
func themeError(err error) *APIError {
	switch {
	case errors.Is(err, objectstore.ErrUnavailable):
		return &APIError{Status: http.StatusServiceUnavailable, Code: "storage_unavailable", cause: err,
			Message: "Files cannot be stored on this server yet. Ask an administrator to set up file storage."}
	case errors.Is(err, theme.ErrNotAThemeFile), errors.Is(err, theme.ErrDefaultNotShared):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: sentence(err.Error())}
	case errors.Is(err, theme.ErrNotFound):
		return ErrNotFound("That theme was not found. It may have been deleted or taken private; reload the list.")
	case errors.Is(err, theme.ErrAssetNotFound):
		return ErrNotFound("That file is not in the theme. Reload the theme to see the files it has.")
	case errors.Is(err, theme.ErrNotYours):
		return ErrForbidden("Only the owner of a theme, or an administrator once it is shared, can change it. Make a copy of your own instead.")
	case errors.Is(err, theme.ErrNotAdmin):
		return ErrForbidden("Only an administrator names the default theme for everyone. Ask one.")
	case errors.Is(err, theme.ErrDuplicateName):
		return ErrConflict("You already have a theme with that name. Choose another name.")
	case errors.Is(err, theme.ErrAssetInUse), errors.Is(err, theme.ErrTooManyAssets):
		return ErrConflict(sentence(err.Error()))
	case errors.Is(err, theme.ErrBadAssetType), errors.Is(err, theme.ErrUnsafeSVG):
		return ErrBadRequest(sentence(err.Error()))
	case errors.Is(err, theme.ErrAssetTooLarge):
		return &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: sentence(theme.ErrAssetTooLarge.Error())}
	default:
		return ErrInternal(err)
	}
}

// respondError writes err in the envelope and logs the cause when there is one
// worth seeing.
func respondError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := toAPIError(err)
	timedOut := apiErr.Status == http.StatusInternalServerError && errors.Is(r.Context().Err(), context.DeadlineExceeded)
	if timedOut {
		apiErr = errTimedOut
		w.Header().Set("Retry-After", strconv.Itoa(int(config.TimedOutRetryAfter.Seconds())))
	}
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
