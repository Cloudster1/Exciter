package httpapi

import (
	"bytes"
	"errors"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// Coverage and the traceability matrix (#38). See docs/api-contract-m3.md.

// coverageError maps coverage's refusals; nil when err is none of theirs.
func coverageError(err error) *APIError {
	var wide *trace.TooWideError
	switch {
	case errors.As(err, &wide):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: wide.Error()}
	case errors.Is(err, trace.ErrCoverageRuleNotFound):
		return ErrNotFound("That coverage rule was not found. Reload the project's coverage to see its rules.")
	case errors.Is(err, trace.ErrNotCoverageKeeper):
		return ErrForbidden("Only the project's administrators set what its coverage asks for. Ask one of them.")
	}
	return nil
}

func (s *Server) handleListCoverageRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Trace.CoverageRules(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rules": rules})
}

func (s *Server) handleSaveCoverageRule(w http.ResponseWriter, r *http.Request) {
	var req trace.CoverageRuleInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	rule, lsn, err := s.Trace.SaveCoverageRule(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rule": rule})
}

func (s *Server) handleDeleteCoverageRule(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "ruleID", "coverage rule")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Trace.DeleteCoverageRule(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleProjectCoverage(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, trace.CoverageDefaultLimit, trace.CoverageMaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	filter, _, err := search.ParseGrid(r.URL.Query())
	if err != nil {
		respondError(w, r, err)
		return
	}
	only, err := trace.ParseLevelFilter(r.URL.Query())
	if err != nil {
		respondError(w, r, err)
		return
	}
	summary, rows, total, err := s.Trace.Coverage(r.Context(), userFrom(r), projectKeyParam(r), filter, only, limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"summary": summary, "rows": rows, "total": total, "limit": limit, "offset": offset})
}

// matrix reads the matrix a request asks for, a window of it or, for a file, up to an export's rows.
func (s *Server) matrix(r *http.Request, limit, offset int) (*trace.Matrix, error) {
	filter, _, err := search.ParseGrid(r.URL.Query())
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	return s.Trace.Matrix(r.Context(), userFrom(r), projectKeyParam(r), filter, trace.MatrixColumns(q.Get("columns")), q.Get("columnProject"), limit, offset)
}

func (s *Server) handleTraceMatrix(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, trace.MatrixDefaultLimit, trace.MatrixMaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	m, err := s.matrix(r, limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"matrix": m})
}

// handleTraceMatrixCSV writes the file whole before answering, so a refusal is still the error envelope.
func (s *Server) handleTraceMatrixCSV(w http.ResponseWriter, r *http.Request) {
	m, err := s.matrix(r, csvio.MaxExportRows, 0)
	if err != nil {
		respondError(w, r, err)
		return
	}
	var buf bytes.Buffer
	buf.Write(utf8Mark)
	if err := csvio.WriteMatrix(&buf, m); err != nil {
		respondError(w, r, err)
		return
	}
	download(w, "text/csv; charset=utf-8", requirement.NormalizedProjectKey(projectKeyParam(r))+"-matrix.csv")
	if len(m.Rows) < m.Total {
		w.Header().Set("X-Export-Truncated", "true")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
