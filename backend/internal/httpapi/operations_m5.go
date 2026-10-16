package httpapi

import (
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// The operations traceability adds (#146), kept beside the table they join; docs/api-contract-m5.md agrees them.
func init() { operations = append(operations, traceabilityOperations...) }

var traceabilityOperations = []operation{
	{method: "GET", path: "/projects/{projectKey}/matrix.xlsx", handler: "handleTraceMatrixXLSX", tag: "coverage", pending: true,
		summary: "The traceability matrix as an Excel workbook, every row the filter keeps, its cells coloured.",
		query:   append(append([]param{}, gridQuery...), matrixQuery...),
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusBadRequest: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},

	// A project's trace model and the chains that follow it (#151), agreed in docs/api-contract-m5.md.
	{method: "GET", path: "/projects/{projectKey}/trace-model", handler: "handleGetTraceModel", tag: "coverage",
		summary:   "A project's trace levels, first to last, and the links that lead from each to the next; empty until set.",
		responses: ok(env{"model": trace.Model{}})},
	{method: "PUT", path: "/projects/{projectKey}/trace-model", handler: "handleSaveTraceModel", tag: "coverage",
		summary: "Replace a project's trace model; no levels clears it. The project's administrators'.",
		request: trace.ModelInput{}, responses: map[int]any{http.StatusOK: env{"model": trace.Model{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/trace-chains", handler: "handleTraceChains", tag: "coverage",
		summary:   "A page of the trace model's first level, narrowed like the grid, with every path down the levels and where it breaks.",
		query:     append(append([]param{}, gridQuery...), append(chainQuery, windowQuery(trace.ChainDefaultLimit, trace.ChainMaxLimit)...)...),
		responses: map[int]any{http.StatusOK: env{"chains": trace.Chains{}}, http.StatusBadRequest: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/trace-chains.csv", handler: "handleTraceChainsCSV", tag: "coverage",
		summary: "The chain report as a CSV file, every root the filter keeps.",
		query:   append(append([]param{}, gridQuery...), chainQuery...),
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusBadRequest: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/trace-chains.xlsx", handler: "handleTraceChainsXLSX", tag: "coverage", pending: true,
		summary: "The chain report as an Excel workbook, every root the filter keeps, its breaks coloured.",
		query:   append(append([]param{}, gridQuery...), chainQuery...),
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusBadRequest: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},

	// A baseline's traceability matrix (#153), agreed in docs/api-contract-m5.md.
	{method: "GET", path: "/baselines/{baselineID}/matrix", handler: "handleBaselineMatrix", tag: "baselines", pending: true,
		summary:   "A page of a baseline's requirements against what they linked to when it was taken, marked against another baseline or now when compared.",
		query:     append(append([]param{}, baselineMatrixQuery...), windowQuery(trace.MatrixDefaultLimit, trace.MatrixMaxLimit)...),
		responses: map[int]any{http.StatusOK: env{"matrix": baseline.Matrix{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/baselines/{baselineID}/matrix.csv", handler: "handleBaselineMatrixCSV", tag: "baselines", pending: true,
		summary: "A baseline's traceability matrix as a CSV file, every requirement it holds.",
		query:   baselineMatrixQuery,
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/baselines/{baselineID}/matrix.xlsx", handler: "handleBaselineMatrixXLSX", tag: "baselines", pending: true,
		summary: "A baseline's traceability matrix as an Excel workbook, every requirement it holds, its changes coloured when compared.",
		query:   baselineMatrixQuery,
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusUnprocessableEntity: errorEnvelope{}}},
}

// chainQuery says which lines of the chain report to keep and which levels to show.
var chainQuery = []param{
	{name: "onlyBroken", schema: &openapi.Schema{Type: "boolean"}, description: "true to keep only the lines that break before the last level shown."},
	{name: "level", repeated: true, schema: intParam, description: "Positions of the levels to show, from 0; every level when absent. The first level shown is the roots."},
}

// baselineMatrixQuery says what a baseline's matrix is laid out against, and what it is compared with.
var baselineMatrixQuery = []param{
	{name: "columns", schema: &openapi.Schema{Type: "string", Enum: enumStrings(trace.LevelKinds)}, description: "requirements, the default, issues or pages."},
	{name: "linkType", repeated: true, description: "Keys of the link types to count; every type when absent."},
	{name: "direction", schema: &openapi.Schema{Type: "string", Enum: []string{string(trace.Outgoing), string(trace.Incoming), "both"}}, description: "Which way a link must point from its row: outgoing, incoming, or both, the default."},
	{name: "with", description: "Another baseline's id, or now, to mark each link added, removed or changed; unmarked when absent."},
}
