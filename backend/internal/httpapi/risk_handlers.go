package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/docx"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/risk"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// riskError maps the refusals of risk schemes; nil when err is none of theirs.
func riskError(err error) *APIError {
	var (
		field *risk.FieldError
		held  *risk.ScaleHeldError
	)
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.As(err, &held) && held.Dimension == risk.Detection && held.Value == 0:
		return &APIError{Status: http.StatusConflict, Code: "risk_scale_held",
			Message: "Risks are rated on detection. Keep detection on, or clear it from their ratings first."}
	case errors.As(err, &held) && held.Value > 0:
		return &APIError{Status: http.StatusConflict, Code: "risk_scale_held",
			Message: fmt.Sprintf("A risk is rated %s %d, which stays while it is. Keep the value, or change the ratings first.", held.Dimension, held.Value)}
	case errors.As(err, &held):
		return &APIError{Status: http.StatusConflict, Code: "risk_scale_held",
			Message: "A risk is rated on a value the scheme would take away. Keep the values in use, or change the ratings first."}
	case errors.Is(err, risk.ErrForbidden):
		return ErrForbidden("Only the project's administrators set its risk scheme. Ask one of them.")
	case errors.Is(err, risk.ErrNoScheme):
		return &APIError{Status: http.StatusConflict, Code: "risk_scheme_missing",
			Message: "This project has no risk scheme yet, so no report can rate its risks. Ask an administrator to set one under Settings, Risks."}
	case errors.Is(err, risk.ErrNotRisk):
		return ErrNotFound("That requirement is no risk, so nothing mitigates it. Give it a type marked as risks first.")
	case errors.Is(err, risk.ErrNotFound):
		return ErrNotFound("That risk was not found. It may have been deleted, or be of a project you may not view.")
	}
	return nil
}

func (s *Server) handleGetRiskScheme(w http.ResponseWriter, r *http.Request) {
	scheme, err := s.Risks.Scheme(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"scheme": scheme})
}

func (s *Server) handleSaveRiskScheme(w http.ResponseWriter, r *http.Request) {
	var in risk.SchemeInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	scheme, lsn, err := s.Risks.SaveScheme(r.Context(), userFrom(r), projectKeyParam(r), in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"scheme": scheme})
}

// parseRiskRows reads the FMEA table's narrowing from the query string: the grid's filter, a side and a cell.
func parseRiskRows(r *http.Request) (risk.RowsQuery, error) {
	filter, sort, err := search.ParseGrid(r.URL.Query())
	if err != nil {
		return risk.RowsQuery{}, err
	}
	q := risk.RowsQuery{Filter: filter, Sort: sort, Side: risk.SideEffective}
	values := r.URL.Query()
	if side := values.Get("side"); side != "" {
		if !slices.Contains(risk.Sides, risk.Side(side)) {
			return q, &risk.FieldError{Field: "side", Message: "Read the risks before mitigation, or as they stand: before or effective."}
		}
		q.Side = risk.Side(side)
	}
	for _, each := range []struct {
		name string
		into **int
	}{{"severity", &q.Severity}, {"probability", &q.Probability}} {
		if raw := values.Get(each.name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > risk.MaxScaleValue {
				return q, &risk.FieldError{Field: each.name, Message: fmt.Sprintf("Give a value from 1 to %d.", risk.MaxScaleValue)}
			}
			*each.into = &n
		}
	}
	return q, nil
}

func (s *Server) handleGetRiskMitigations(w http.ResponseWriter, r *http.Request) {
	out, err := s.Risks.Mitigations(r.Context(), userFrom(r), reqKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleListRisks(w http.ResponseWriter, r *http.Request) {
	q, err := parseRiskRows(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	limit, offset, apiErr := offsetWindow(r, risk.DefaultRows, risk.MaxRows)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	q.Limit, q.Offset = limit, offset
	rows, err := s.Risks.Rows(r.Context(), userFrom(r), projectKeyParam(r), q)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, rows)
}

func (s *Server) handleRisksXLSX(w http.ResponseWriter, r *http.Request) {
	q, err := parseRiskRows(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	out, err := s.Risks.Export(r.Context(), userFrom(r), projectKeyParam(r), q, csvio.MaxExportRows)
	if err != nil {
		respondError(w, r, err)
		return
	}
	key := requirement.NormalizedProjectKey(projectKeyParam(r))
	if !s.noteExport(w, r, audit.Entry{Action: audit.ActionSpecificationExported, TargetType: "project", TargetID: &out.ProjectID, Actor: userFrom(r),
		Data: map[string]any{"projectKey": key, "format": "xlsx", "of": "risks", "risks": len(out.Rows)}}) {
		return
	}
	sendWorkbook(w, r, key+"-risks.xlsx", csvio.RisksSheet(key+" risks", out.Rows, out.Defs, out.People), false)
}

func (s *Server) handleRiskMatrix(w http.ResponseWriter, r *http.Request) {
	q, err := parseRiskRows(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	m, err := s.Risks.Matrix(r.Context(), userFrom(r), projectKeyParam(r), q.Filter, q.Sort, q.Side)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"matrix": m})
}

// riskReport reads the risk report the query string narrows, as the grid narrows the requirements.
func (s *Server) riskReport(r *http.Request) (*risk.Report, error) {
	filter, sort, err := search.ParseGrid(r.URL.Query())
	if err != nil {
		return nil, err
	}
	return s.Risks.Report(r.Context(), userFrom(r), projectKeyParam(r), filter, sort)
}

// noteRiskReport records a risk report about to leave, as an exported document is.
func (s *Server) noteRiskReport(w http.ResponseWriter, r *http.Request, rep *risk.Report, format string) bool {
	return s.noteExport(w, r, audit.Entry{Action: audit.ActionSpecificationExported, TargetType: "project", Actor: userFrom(r),
		Data: map[string]any{"project": rep.ProjectKey, "kind": "risks", "format": format, "risks": len(rep.Risks)}})
}

func (s *Server) handleGetRiskReport(w http.ResponseWriter, r *http.Request) {
	rep, err := s.riskReport(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"report": rep})
}

func (s *Server) handleRiskReportDocx(w http.ResponseWriter, r *http.Request) {
	rep, err := s.riskReport(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	at := time.Now().UTC()
	f, err := s.framingOf(r, rep.ProjectKey)
	if err != nil {
		respondError(w, r, err)
		return
	}
	var frame *docx.Template
	fill := docx.Fill{"project": rep.ProjectKey, "title": rep.ProjectName, "date": at.Format(time.DateOnly), "exportedBy": s.nameOf(r)}
	if f != nil {
		frame = f.frame
		fill["project"] = rep.ProjectName + " (" + rep.ProjectKey + ")"
	}
	body, err := docx.RiskReportBytesWith(r.Context(), rep, s.nameOf(r), at, language(r), frame, fill)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if !s.noteRiskReport(w, r, rep, "docx") {
		return
	}
	sendFile(w, docx.ContentType, docx.RiskReportFileName(rep, at, "docx"), body)
}

func (s *Server) handleRiskReportPDF(w http.ResponseWriter, r *http.Request) {
	rep, err := s.riskReport(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	path := "/print/risks/" + url.PathEscape(rep.ProjectKey)
	if q := r.URL.Query().Get("query"); q != "" {
		path += "?" + url.Values{"query": {q}}.Encode()
	}
	pdf, ok := s.printed(w, r, path)
	if !ok || !s.noteRiskReport(w, r, rep, "pdf") {
		return
	}
	sendFile(w, "application/pdf", docx.RiskReportFileName(rep, time.Now().UTC(), "pdf"), pdf)
}
