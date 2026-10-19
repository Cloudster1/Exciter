package httpapi

import (
	"net/http"
	"strconv"

	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/risk"
)

// The operations risk management adds (#203), kept beside the table they join; docs/api-contract-m13.md agrees
// them.
func init() { operations = append(operations, riskOperations...) }

// riskSideQuery says which rating the FMEA table and the matrix read.
var riskSideQuery = param{name: "side", schema: &openapi.Schema{Type: "string", Enum: enumStrings(risk.Sides)},
	description: "before, or effective: after once every mitigation is in effect; effective when absent."}

// riskCellQuery narrows the FMEA table to one cell of the risk matrix.
var riskCellQuery = []param{
	{name: "severity", schema: intParam, description: "Only risks of this severity on the side read."},
	{name: "probability", schema: intParam, description: "Only risks of this probability on the side read."},
}

// riskRowsQuery narrows and pages the FMEA table, as the grid narrows a project's requirements.
var riskRowsQuery = append(append(append([]param{}, gridQuery...), riskSideQuery), append(riskCellQuery, windowQuery(risk.DefaultRows, risk.MaxRows)...)...)

var riskOperations = []operation{
	// The risk scheme (#259).
	{method: "GET", path: "/projects/{projectKey}/risk-scheme", handler: "handleGetRiskScheme", tag: "risks",
		tool: "get_risk_scheme", toolHelp: "A project's risk scheme: its scales and their named values, its levels, and the matrix or the priority-number thresholds that give a rating its level; null until set.",
		summary:   "A project's risk scheme: its scales, levels, and the matrix or thresholds that give a level; null until set.",
		responses: ok(env{"scheme": (*risk.Scheme)(nil)})},
	{method: "PUT", path: "/projects/{projectKey}/risk-scheme", handler: "handleSaveRiskScheme", tag: "risks",
		summary: "Set a project's risk scheme whole: scales of at most " + strconv.Itoa(risk.MaxScaleValue) + " values, at most " +
			strconv.Itoa(risk.MaxLevels) + " levels, and a full matrix or rising thresholds. A value a rating uses stays. The project's administrators'.",
		request: risk.SchemeInput{}, responses: map[int]any{http.StatusOK: env{"scheme": risk.Scheme{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},

	// Mitigations, the FMEA table and the risk matrix (#260).
	{method: "GET", path: "/requirements/{reqKey}/mitigations", handler: "handleGetRiskMitigations", tag: "risks",
		tool: "get_risk_mitigations", toolHelp: "The requirements mitigating a risk, each with whether it is implemented, verified, suspect and in effect, and the state they put the risk in.",
		summary:   "The requirements mitigating a risk, each with its coverage and whether it is in effect, and the risk's state.",
		responses: ok(risk.Mitigations{})},
	{method: "GET", path: "/projects/{projectKey}/risks", handler: "handleListRisks", tag: "risks",
		tool: "list_risks", toolHelp: "A project's FMEA table: a page of its risks, narrowed like the grid, each with its ratings, levels, mitigations and state; severity and probability narrow it to a cell of the risk matrix.",
		summary:   "The FMEA table: a page of the project's risks, narrowed like the grid, with their ratings, levels, mitigations and state.",
		query:     riskRowsQuery,
		responses: map[int]any{http.StatusOK: risk.Rows{}, http.StatusBadRequest: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/risks.xlsx", handler: "handleRisksXLSX", tag: "risks",
		summary: "The FMEA table as an Excel workbook, every risk the filter keeps, a row a mitigation, each level in its colour.",
		query:   append(append([]param{}, gridQuery...), riskSideQuery),
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusBadRequest: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/risk-matrix", handler: "handleRiskMatrix", tag: "risks",
		tool: "get_risk_matrix", toolHelp: "A project's risk matrix: how many risks fall in each cell of severity and probability, before mitigation or in effect, with each cell's level.",
		summary:   "The risk matrix: how many of the risks the filter keeps fall in each cell, before mitigation or in effect.",
		query:     append(append([]param{}, gridQuery...), riskSideQuery),
		responses: map[int]any{http.StatusOK: env{"matrix": risk.Matrix{}}, http.StatusBadRequest: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},

	// The risk report (#261).
	{method: "GET", path: "/projects/{projectKey}/risk-report", handler: "handleGetRiskReport", tag: "risks", pending: true,
		summary:   "The risk report: the scheme, the risks at each level before and in effect, and each risk with its ratings and mitigations.",
		query:     gridQuery,
		responses: map[int]any{http.StatusOK: env{"report": risk.Report{}}, http.StatusBadRequest: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/risk-report.docx", handler: "handleRiskReportDocx", tag: "risks", pending: true,
		summary: "The risk report as a Word file, through a template of the project when one is named.",
		query:   append(append([]param{}, gridQuery...), templateQuery),
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusNotFound: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/risk-report.pdf", handler: "handleRiskReportPDF", tag: "risks", pending: true,
		summary: "The risk report as a PDF, printed by the render service as the reader.",
		query:   gridQuery,
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusNotFound: errorEnvelope{}, http.StatusServiceUnavailable: errorEnvelope{}}},
}
