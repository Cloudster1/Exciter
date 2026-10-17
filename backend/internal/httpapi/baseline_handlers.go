package httpapi

import (
	"bytes"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// Baselines (#37): named snapshots of a project, read as a specification and compared. See docs/api-contract-m3.md.

var errBaselinesOff = &APIError{Status: http.StatusServiceUnavailable, Code: "baselines_unavailable",
	Message: "This server is not set up for baselines. Ask the operator to check its configuration."}

func (s *Server) baselinesOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Baselines == nil {
			respondError(w, r, errBaselinesOff)
			return
		}
		// A baseline answers the meaning its signer would sign, in their language (#205).
		next.ServeHTTP(w, signer(r))
	})
}

// baselineError maps the baselines' refusals; nil when err is none of theirs.
func baselineError(err error) *APIError {
	var field *baseline.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, baseline.ErrNotFound):
		return ErrNotFound("That baseline was not found. Reload the project's baselines; somebody may have deleted it.")
	case errors.Is(err, baseline.ErrForbidden):
		return ErrForbidden("Only the project's administrators take, change and delete its baselines. Ask one of them.")
	case errors.Is(err, baseline.ErrSigned):
		return &APIError{Status: http.StatusConflict, Code: "baseline_signed",
			Message: "This baseline is signed and keeps its name. Take a new baseline to record a later state."}
	case errors.Is(err, baseline.ErrLocked):
		return &APIError{Status: http.StatusConflict, Code: "baseline_locked",
			Message: "This baseline is locked, so it keeps its name and is never deleted. Take a new baseline instead."}
	case errors.Is(err, baseline.ErrNameTaken):
		return &APIError{Status: http.StatusConflict, Code: "baseline_name_taken",
			Message: "Another baseline of this project has that name. Pick another name."}
	}
	return nil
}

func baselineID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, apiErr := pathUUID(r, "baselineID", "baseline")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return id, false
	}
	return id, true
}

func (s *Server) handleListBaselines(w http.ResponseWriter, r *http.Request) {
	all, err := s.Baselines.List(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"baselines": all})
}

func (s *Server) handleTakeBaseline(w http.ResponseWriter, r *http.Request) {
	var req baseline.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	b, lsn, err := s.Baselines.Take(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"baseline": b})
}

func (s *Server) handleGetBaseline(w http.ResponseWriter, r *http.Request) {
	id, ok := baselineID(w, r)
	if !ok {
		return
	}
	b, err := s.Baselines.Get(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"baseline": b})
}

func (s *Server) handleUpdateBaseline(w http.ResponseWriter, r *http.Request) {
	id, ok := baselineID(w, r)
	if !ok {
		return
	}
	var req baseline.UpdateInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	b, lsn, err := s.Baselines.Update(r.Context(), userFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"baseline": b})
}

func (s *Server) handleDeleteBaseline(w http.ResponseWriter, r *http.Request) {
	id, ok := baselineID(w, r)
	if !ok {
		return
	}
	lsn, err := s.Baselines.Delete(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleBaselineSpec(w http.ResponseWriter, r *http.Request) {
	id, ok := baselineID(w, r)
	if !ok {
		return
	}
	limit, offset, apiErr := offsetWindow(r, baseline.DefaultLimit, baseline.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	entries, total, err := s.Baselines.Spec(r.Context(), userFrom(r), id, limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"entries": entries, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleCompareBaseline(w http.ResponseWriter, r *http.Request) {
	id, ok := baselineID(w, r)
	if !ok {
		return
	}
	c, err := s.Baselines.Compare(r.Context(), userFrom(r), id, r.URL.Query().Get("with"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"comparison": c})
}

// baselineMatrix reads the matrix of a baseline a request asks for, a window of it or, for a file, every requirement.
func (s *Server) baselineMatrix(r *http.Request, id uuid.UUID, limit, offset int) (*baseline.Matrix, error) {
	q := r.URL.Query()
	return s.Baselines.Matrix(r.Context(), userFrom(r), id, baseline.MatrixQuery{
		Columns: trace.MatrixColumns(q.Get("columns")), LinkTypes: q["linkType"], Direction: trace.MatrixDirection(q.Get("direction")),
		With: q.Get("with"), Limit: limit, Offset: offset,
	})
}

func (s *Server) handleBaselineMatrix(w http.ResponseWriter, r *http.Request) {
	id, ok := baselineID(w, r)
	if !ok {
		return
	}
	limit, offset, apiErr := offsetWindow(r, trace.MatrixDefaultLimit, trace.MatrixMaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	m, err := s.baselineMatrix(r, id, limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"matrix": m})
}

// handleBaselineMatrixCSV writes the file whole before answering, so a refusal is still the error envelope.
func (s *Server) handleBaselineMatrixCSV(w http.ResponseWriter, r *http.Request) {
	id, ok := baselineID(w, r)
	if !ok {
		return
	}
	m, err := s.baselineMatrix(r, id, csvio.MaxExportRows, 0)
	if err != nil {
		respondError(w, r, err)
		return
	}
	s.titleBaselinePages(r, m)
	var buf bytes.Buffer
	buf.Write(utf8Mark)
	if err := csvio.WriteBaselineMatrix(&buf, m); err != nil {
		respondError(w, r, err)
		return
	}
	download(w, "text/csv; charset=utf-8", "baseline-matrix.csv")
	if len(m.Rows) < m.Total {
		w.Header().Set("X-Export-Truncated", "true")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// titleBaselinePages names a baseline matrix's page columns as the reader sees them in Stator, for a file.
func (s *Server) titleBaselinePages(r *http.Request, m *baseline.Matrix) {
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
