package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// maxBodyBytes caps JSON request bodies so a malformed or hostile client cannot
// make the server allocate without bound.
const maxBodyBytes = 1 << 20

// decodeJSON reads a JSON request body, rejecting unknown fields so that a typo
// in a client payload is an error rather than a silent no-op.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	return decodeJSONWithin(w, r, dst, maxBodyBytes)
}

// decodeJSONWithin is decodeJSON for a body allowed to be larger, such as a
// requirement carrying a whole statement.
func decodeJSONWithin(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			return ErrBadRequest("The request body is too large.")
		case errors.Is(err, io.EOF):
			return ErrBadRequest("The request needs a JSON body.")
		default:
			return ErrBadRequest("The request body is not valid JSON: " + err.Error() + ".")
		}
	}
	if dec.More() {
		return ErrBadRequest("Send a single JSON object as the request body.")
	}
	return nil
}

// pathUUID reads an id out of the route, naming what was wrong in the words the
// reader used rather than the route's own parameter name.
func pathUUID(r *http.Request, name, noun string) (uuid.UUID, *APIError) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, ErrBadRequest("That is not a valid " + noun + " id. Check the address.")
	}
	return id, nil
}

func respondNoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// asValidationError makes the service's plain validation errors a 422 rather
// than a 500; errors toAPIError already maps pass straight through.
func asValidationError(err error) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return err
	}
	// A database or a timeout failing is not the caller's input, and its words
	// are ours rather than something written for them to read.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) || errors.Is(err, pgx.ErrNoRows) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrInternal(err)
	}
	if toAPIError(err).Status != http.StatusInternalServerError {
		return err
	}
	return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: sentence(err.Error())}
}

// sentence makes a service error, written in lower case to wrap well, into
// something a person reads: capitalised and ending in a full stop.
func sentence(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	if last := r[len(r)-1]; last != '.' && last != '?' && last != '!' {
		r = append(r, '.')
	}
	return string(r)
}
