package httpapi

import (
	"bytes"
	"net/http"
	"time"

	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
	"github.com/cloudster1/exciter/backend/internal/xlsx"
)

// The matrix, the chain report and a baseline's matrix as Excel workbooks (#154), each holding what its CSV holds.

const xlsxType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// sendWorkbook writes a sheet whole before answering, so a refusal while writing is still the error envelope.
func sendWorkbook(w http.ResponseWriter, r *http.Request, name string, sheet xlsx.Sheet, truncated bool) {
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, sheet, time.Now()); err != nil {
		respondError(w, r, err)
		return
	}
	download(w, xlsxType, name)
	if truncated {
		w.Header().Set("X-Export-Truncated", "true")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) handleTraceMatrixXLSX(w http.ResponseWriter, r *http.Request) {
	m, err := s.matrix(r, csvio.MaxExportRows, 0, 0, 0)
	if err != nil {
		respondError(w, r, err)
		return
	}
	s.titlePages(r, m)
	key := requirement.NormalizedProjectKey(projectKeyParam(r))
	sendWorkbook(w, r, key+"-matrix.xlsx", csvio.MatrixSheet(key+" matrix", m), len(m.Rows) < m.Total)
}

func (s *Server) handleTraceChainsXLSX(w http.ResponseWriter, r *http.Request) {
	c, err := s.chains(r, trace.ChainQuery{Limit: csvio.MaxExportRows, MaxLines: csvio.MaxExportRows, Truncate: true})
	if err != nil {
		respondError(w, r, err)
		return
	}
	s.titleChainPages(r, c)
	key := requirement.NormalizedProjectKey(projectKeyParam(r))
	sendWorkbook(w, r, key+"-chains.xlsx", csvio.ChainsSheet(key+" chains", c), c.Truncated || len(c.Roots) < c.Total)
}

func (s *Server) handleBaselineMatrixXLSX(w http.ResponseWriter, r *http.Request) {
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
	sendWorkbook(w, r, "baseline-matrix.xlsx", csvio.BaselineMatrixSheet("Baseline matrix", m), len(m.Rows) < m.Total)
}
