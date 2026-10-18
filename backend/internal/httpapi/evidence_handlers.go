package httpapi

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/testresult"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// Test results from CI and the trace check (#200). See docs/api-contract-m9.md.

var errEvidenceOff = &APIError{Status: http.StatusServiceUnavailable, Code: "evidence_unavailable",
	Message: "Test results are not set up on this server. Ask an administrator to start Exciter with its database."}

// maxFormField bounds one form field of an upload beside its files.
const maxFormField = 4 << 10

func (s *Server) evidenceOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.TestResults == nil || s.Trace == nil {
			respondError(w, r, errEvidenceOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// evidenceError maps the test results' refusals; nil when err is none of theirs.
func evidenceError(err error) *APIError {
	var field *testresult.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, testresult.ErrNotFound):
		return ErrNotFound("That test run was not found. Its uploader and the project's administrators read it.")
	case errors.Is(err, testresult.ErrForbidden):
		return ErrForbidden("Only the project's editors upload test results, and its administrators name the counting branch. Ask one of them.")
	}
	return nil
}

// uploadedRun reads an upload's form fields and every part named file, the files together within the limit the
// body reader holds.
func uploadedRun(r *http.Request) (testresult.RunForm, []testresult.File, error) {
	var (
		form  testresult.RunForm
		files []testresult.File
	)
	reader, err := r.MultipartReader()
	if err != nil {
		return form, nil, ErrBadRequest("Send the run as multipart form data: its fields, and its JUnit files in parts named file.")
	}
	fields := map[string]*string{"repository": &form.Repository, "commit": &form.Commit, "branch": &form.Branch, "job": &form.Job,
		"runUrl": &form.RunURL}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return form, files, nil
		}
		if err != nil {
			if tooBig(err) {
				return form, nil, err
			}
			return form, nil, ErrBadRequest("The upload could not be read. Try sending it again.")
		}
		name := part.FormName()
		if name == "file" {
			if len(files) == testresult.MaxRunFiles {
				return form, nil, &testresult.FieldError{Field: "file", Message: "Upload at most " + strconv.Itoa(testresult.MaxRunFiles) + " files in one run."}
			}
			data, err := io.ReadAll(part)
			if err != nil {
				return form, nil, uploadError(err, testresult.MaxRunBytes)
			}
			files = append(files, testresult.File{Name: part.FileName(), Data: data})
			continue
		}
		into, known := fields[name]
		if !known {
			continue
		}
		value, err := io.ReadAll(io.LimitReader(part, maxFormField+1))
		if err != nil {
			return form, nil, uploadError(err, testresult.MaxRunBytes)
		}
		if len(value) > maxFormField {
			return form, nil, &testresult.FieldError{Field: name, Message: "This field is too long. Keep it to the agreed length."}
		}
		*into = string(value)
	}
}

func (s *Server) handleUploadTestRun(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, testresult.MaxRunBytes+uploadSlack)
	form, files, err := uploadedRun(r)
	if err != nil {
		respondError(w, r, uploadError(err, testresult.MaxRunBytes))
		return
	}
	run, lsn, err := s.TestResults.Upload(r.Context(), userFrom(r), projectKeyParam(r), form, files)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"run": run})
}

func (s *Server) handleGetTestRun(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "runID", "test run")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	run, err := s.TestResults.Get(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"run": run})
}

func (s *Server) handleListTestResults(w http.ResponseWriter, r *http.Request) {
	results, counting, err := s.TestResults.Results(r.Context(), userFrom(r), reqKeyParam(r), r.URL.Query().Get("branch"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"results": results, "countingBranch": counting})
}

func (s *Server) handleGetEvidenceSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.TestResults.Settings(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"settings": settings})
}

func (s *Server) handleSaveEvidenceSettings(w http.ResponseWriter, r *http.Request) {
	var req testresult.SettingsInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	settings, lsn, err := s.TestResults.SaveSettings(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"settings": settings})
}

func (s *Server) handleTraceCheck(w http.ResponseWriter, r *http.Request) {
	filter, _, err := search.ParseGrid(r.URL.Query())
	if err != nil {
		respondError(w, r, err)
		return
	}
	q := trace.CheckQuery{Filter: filter, Branch: strings.TrimSpace(r.URL.Query().Get("branch"))}
	for _, raw := range r.URL.Query()["fail"] {
		for _, f := range strings.Split(raw, ",") {
			reason := trace.CheckReason(strings.TrimSpace(f))
			if !slices.Contains(trace.CheckReasons, reason) {
				respondError(w, r, ErrBadRequest("Fail on broken, gaps, failing, outdated or orphaned."))
				return
			}
			if !slices.Contains(q.Fail, reason) {
				q.Fail = append(q.Fail, reason)
			}
		}
	}
	check, err := s.Trace.Check(r.Context(), userFrom(r), projectKeyParam(r), q)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"check": check})
}
