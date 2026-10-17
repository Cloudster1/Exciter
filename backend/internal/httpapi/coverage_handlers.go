package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/stator"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// Coverage and the traceability matrix (#38, #147). See docs/api-contract-m3.md and docs/api-contract-m5.md.

// coverageError maps coverage's refusals; nil when err is none of theirs.
func coverageError(err error) *APIError {
	var (
		wide   *trace.TooWideError
		column *trace.ColumnQueryError
	)
	switch {
	case errors.As(err, &wide):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: wide.Error()}
	case errors.As(err, &column):
		// fields names the column filter's box, so the page shows the refusal there and not under the rows' query.
		return &APIError{Status: http.StatusBadRequest, Code: "bad_query", Message: column.Msg, Position: &column.Pos,
			Fields: map[string]string{"columnQuery": column.Msg}}
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

// matrix reads the matrix a request asks for: a window of its rows and columns, or for a file every column and up to
// an export's rows, when columnLimit is 0.
func (s *Server) matrix(r *http.Request, limit, offset, columnLimit, columnOffset int) (*trace.Matrix, error) {
	filter, sort, err := search.ParseGrid(r.URL.Query())
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	relations := q["relation"]
	known, names := enumStrings(armature.Relations), "implements, verifies or relates"
	if trace.MatrixColumns(q.Get("columns")) == trace.ColumnsPages {
		known, names = enumStrings(stator.Relations), "specifies, describes or relates"
	}
	for _, rel := range relations {
		if !slices.Contains(known, rel) {
			return nil, &trace.FieldError{Field: "relation", Message: fmt.Sprintf("There is no relation %q here. Pick %s.", rel, names)}
		}
	}
	var columnFilter *search.Filter
	if strings.TrimSpace(q.Get("columnQuery")) != "" || len(q["columnType"]) > 0 {
		f, _, err := search.ParseGrid(url.Values{"query": {q.Get("columnQuery")}, "type": q["columnType"]})
		var field *search.FieldError
		if errors.As(err, &field) {
			return nil, &trace.FieldError{Field: "columnType", Message: field.Message}
		} else if err != nil {
			return nil, err
		}
		columnFilter = &f
	}
	return s.Trace.Matrix(r.Context(), userFrom(r), projectKeyParam(r), trace.MatrixQuery{
		Filter: filter, Sort: sort, Columns: trace.MatrixColumns(q.Get("columns")), ColumnProject: q.Get("columnProject"),
		ColumnFilter: columnFilter, LinkTypes: q["linkType"], Direction: trace.MatrixDirection(q.Get("direction")), Relations: relations,
		ColumnRepository: strings.TrimSpace(q.Get("columnRepository")),
		Gaps:             trace.Gaps(q.Get("gaps")), Limit: limit, Offset: offset, ColumnLimit: columnLimit, ColumnOffset: columnOffset,
	})
}

func (s *Server) handleTraceMatrix(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, trace.MatrixDefaultLimit, trace.MatrixMaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	columnLimit, columnOffset, apiErr := namedWindow(r, "columnLimit", "columnOffset", trace.MatrixColumnDefaultLimit, trace.MatrixMaxColumns)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	m, err := s.matrix(r, limit, offset, columnLimit, columnOffset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"matrix": m})
}

// handleTraceMatrixCSV writes the file whole before answering, so a refusal is still the error envelope.
func (s *Server) handleTraceMatrixCSV(w http.ResponseWriter, r *http.Request) {
	m, err := s.matrix(r, csvio.MaxExportRows, 0, 0, 0)
	if err != nil {
		respondError(w, r, err)
		return
	}
	s.titlePages(r, m)
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

// titlePages names a matrix's page columns as the reader sees them in Stator, for a file.
func (s *Server) titlePages(r *http.Request, m *trace.Matrix) {
	var ids []uuid.UUID
	for _, c := range m.Columns {
		if c.Page != nil {
			ids = append(ids, c.Page.ID)
		}
	}
	titles := s.pageTitles(r, ids)
	for _, c := range m.Columns {
		if c.Page != nil {
			c.Page.Title = titles[c.Page.ID]
		}
	}
}

// pageTitles looks pages up in Stator as the reader, for a file nobody reads beside the web client's lookup; a page
// Stator does not show them has no title, and the file names it by space and id.
func (s *Server) pageTitles(r *http.Request, ids []uuid.UUID) map[uuid.UUID]string {
	titles := map[uuid.UUID]string{}
	if s.Stator == nil || len(ids) == 0 {
		return titles
	}
	var distinct []uuid.UUID
	for _, id := range ids {
		if !slices.Contains(distinct, id) {
			distinct = append(distinct, id)
		}
	}
	for batch := range slices.Chunk(distinct, stator.MaxLookupPages) {
		status, found, err := s.Stator.Lookup(r.Context(), batch)
		if err != nil || status != stator.StatusOK {
			return titles
		}
		for _, each := range found {
			if each.Page != nil {
				titles[each.ID] = each.Page.Title
			}
		}
	}
	return titles
}
