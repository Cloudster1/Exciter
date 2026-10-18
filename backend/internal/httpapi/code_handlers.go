package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/cloudster1/exciter/backend/internal/codetrace"
)

// Code that implements and tests requirements (#201). See docs/api-contract-m9.md.

var errCodeOff = &APIError{Status: http.StatusServiceUnavailable, Code: "code_unavailable",
	Message: "Code tags are not set up on this server. Ask an administrator to start Exciter with its database."}

func (s *Server) codeOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Code == nil {
			respondError(w, r, errCodeOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// codeError maps the code tags' refusals; nil when err is none of theirs.
func codeError(err error) *APIError {
	var field *codetrace.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, codetrace.ErrTooManyTags):
		return &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
			Message: "A scan holds at most " + strconv.Itoa(codetrace.MaxScanTags) + " tags. Scan the repository in parts, one per repository."}
	case errors.Is(err, codetrace.ErrNotFound):
		return ErrNotFound("That scan was not found. Its uploader and the project's administrators read it.")
	case errors.Is(err, codetrace.ErrForbidden):
		return ErrForbidden("Only the project's editors upload code scans, and its administrators set the tag words. Ask one of them.")
	}
	return nil
}

func (s *Server) handleUploadCodeScan(w http.ResponseWriter, r *http.Request) {
	// Read whole first, so a scan past the limit is refused as too large rather than as bad JSON.
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, codetrace.MaxScanBytes))
	if err != nil {
		respondError(w, r, uploadError(err, codetrace.MaxScanBytes))
		return
	}
	var req codetrace.ScanInput
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		respondError(w, r, ErrBadRequest("The scan is not valid JSON: "+err.Error()+"."))
		return
	}
	scan, lsn, err := s.Code.Upload(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"scan": scan})
}

func (s *Server) handleGetCodeScan(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "scanID", "code scan")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	scan, err := s.Code.Get(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"scan": scan})
}

func (s *Server) handleListCodeLinks(w http.ResponseWriter, r *http.Request) {
	links, counting, err := s.Code.Links(r.Context(), userFrom(r), reqKeyParam(r), r.URL.Query().Get("branch"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"links": links, "countingBranch": counting})
}

func (s *Server) handleListCodeTags(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, codetrace.DefaultLimit, codetrace.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	tags, total, err := s.Code.Tags(r.Context(), userFrom(r), projectKeyParam(r), r.URL.Query().Get("branch"),
		codetrace.State(r.URL.Query().Get("state")), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"tags": tags, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleGetCodeTagWords(w http.ResponseWriter, r *http.Request) {
	words, err := s.Code.Words(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"words": words})
}

func (s *Server) handleSaveCodeTagWords(w http.ResponseWriter, r *http.Request) {
	var req codetrace.Words
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	words, lsn, err := s.Code.SaveWords(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"words": words})
}
