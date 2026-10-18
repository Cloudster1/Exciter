package httpapi

import (
	"net/http"
	"strconv"

	"github.com/cloudster1/exciter/backend/internal/attribute/formula"
	"github.com/cloudster1/exciter/backend/internal/dashboard"
	"github.com/cloudster1/exciter/backend/internal/exporttemplate"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// The operations analysis and reporting adds (#241), kept beside the table they join; docs/api-contract-m11.md agrees
// them.
func init() { operations = append(operations, analysisOperations...) }

// graphQuery asks the trace graph which way, how far, and through what.
var graphQuery = []param{
	{name: "direction", schema: &openapi.Schema{Type: "string", Enum: enumStrings(trace.GraphDirections)}, description: "Up, down or both; both when absent."},
	{name: "depth", schema: intParam, description: "Levels to follow, 1 to " + strconv.Itoa(trace.GraphMaxDepth) + "; " + strconv.Itoa(trace.GraphDefaultDepth) + " when absent."},
	{name: "type", repeated: true, description: "Keys of the link types to follow; every type when absent."},
	{name: "relation", repeated: true, description: "Armature, Stator, test and code relations to reach; every one when absent."},
	{name: "branch", description: "The evidence branch of the requirement's project; its counting branch when absent."},
}

// templateQuery names the Word template an export goes through.
var templateQuery = param{name: "template", schema: uuidParam, description: "A template the project may use; none when absent."}

var analysisOperations = []operation{
	// The trace graph (#211).
	{method: "GET", path: "/requirements/{reqKey}/graph", handler: "handleTraceGraph", tag: "coverage",
		tool: "get_trace_graph", toolHelp: "Everything a requirement traces to, up to what it derives from and down to what derives from it, implements or verifies it, to a depth: what a change to it would affect, with suspect and broken links and coverage gaps marked.",
		summary: "Everything a requirement traces to, up, down or both, to a depth, through chosen link types and relations, as nodes and edges, with suspect and broken links and coverage gaps marked. A requirement the reader may not see is counted, never named; at most " +
			strconv.Itoa(trace.MaxGraphNodes) + " nodes.",
		query:     graphQuery,
		responses: map[int]any{http.StatusOK: env{"graph": trace.Graph{}}, http.StatusBadRequest: errorEnvelope{}, http.StatusNotFound: errorEnvelope{}}},
	{method: "GET", path: "/requirements/{reqKey}/graph.csv", handler: "handleTraceGraphCSV", tag: "coverage",
		summary: "The impact of a change to a requirement as a CSV file: everything the graph reaches, by level.",
		query:   graphQuery,
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusBadRequest: errorEnvelope{}, http.StatusNotFound: errorEnvelope{}}},

	// Computed attributes (#213).
	{method: "POST", path: "/projects/{projectKey}/formula-check", handler: "handleFormulaCheck", tag: "types",
		summary: "Check a computed attribute's formula as it is written: what is wrong and where, what kind of value it gives, what it reads, and its value for a requirement. The project's administrators'.",
		request: formula.CheckInput{}, responses: with(http.StatusOK, env{"check": formula.Check{}})},

	// Dashboards (#212).
	{method: "GET", path: "/projects/{projectKey}/dashboards", handler: "handleListDashboards", tag: "dashboards",
		tool: "list_dashboards", toolHelp: "A project's dashboards you may read, yours and those shared with the project, with whether each is the project's home; get_dashboard_widget reads a widget.",
		summary:   "The project's dashboards the caller may read: their own, and those shared with the project.",
		responses: ok(env{"dashboards": []dashboard.Summary{}})},
	{method: "POST", path: "/projects/{projectKey}/dashboards", handler: "handleCreateDashboard", tag: "dashboards",
		summary: "Make a dashboard, empty or from a template, for oneself or shared with the project; sharing needs edit. At most " + strconv.Itoa(dashboard.MaxDashboards) + " per person and project.",
		request: dashboard.Input{}, responses: with(http.StatusCreated, env{"dashboard": dashboard.Dashboard{}})},
	{method: "GET", path: "/dashboards/{dashboardID}", handler: "handleGetDashboard", tag: "dashboards",
		summary:   "A dashboard and its widgets in order, for its owner or, once shared, anybody who may read the project.",
		responses: ok(env{"dashboard": dashboard.Dashboard{}})},
	{method: "PATCH", path: "/dashboards/{dashboardID}", handler: "handleUpdateDashboard", tag: "dashboards",
		summary: "Rename a dashboard or share it; its owner's, or once shared the project's administrators'.",
		request: dashboard.Patch{}, responses: with(http.StatusOK, env{"dashboard": dashboard.Dashboard{}})},
	{method: "DELETE", path: "/dashboards/{dashboardID}", handler: "handleDeleteDashboard", tag: "dashboards",
		summary:   "Delete a dashboard; its owner's, or once shared the project's administrators'.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusForbidden: errorEnvelope{}, http.StatusConflict: errorEnvelope{}}},
	{method: "POST", path: "/dashboards/{dashboardID}/widgets", handler: "handleAddWidget", tag: "dashboards",
		summary: "Add a widget at the end of a dashboard; whoever may change the dashboard, at most " + strconv.Itoa(dashboard.MaxWidgets) + " widgets.",
		request: dashboard.WidgetInput{}, responses: with(http.StatusCreated, env{"widget": dashboard.Widget{}})},
	{method: "PUT", path: "/dashboards/{dashboardID}/order", handler: "handleOrderWidgets", tag: "dashboards",
		summary: "Lay a dashboard's widgets out in a new order, naming every one.",
		request: dashboard.OrderInput{}, responses: with(http.StatusOK, env{"dashboard": dashboard.Dashboard{}})},
	{method: "GET", path: "/dashboards/{dashboardID}/dashboard.pdf", handler: "handleDashboardPDF", tag: "dashboards",
		summary: "A dashboard as a PDF, printed by the render service as the reader; 503 render_unavailable when the server has none.",
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusNotFound: errorEnvelope{}, http.StatusServiceUnavailable: errorEnvelope{}}},
	{method: "PATCH", path: "/widgets/{widgetID}", handler: "handleUpdateWidget", tag: "dashboards",
		summary: "Change a widget's title, width or what it reads.",
		request: dashboard.WidgetPatch{}, responses: with(http.StatusOK, env{"widget": dashboard.Widget{}})},
	{method: "DELETE", path: "/widgets/{widgetID}", handler: "handleDeleteWidget", tag: "dashboards",
		summary:   "Take a widget off its dashboard.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusForbidden: errorEnvelope{}, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/widgets/{widgetID}/data", handler: "handleWidgetData", tag: "dashboards",
		tool: "get_dashboard_widget", toolHelp: "What a dashboard's widget shows now, read as you: counts per status, type, owner or attribute, coverage, review, suspects, changes or quality, each with the query that finds its requirements.",
		summary:   "What a widget shows, read now as the caller: only requirements the caller may see count, and each bucket carries the query that opens it in the grid.",
		responses: map[int]any{http.StatusOK: env{"data": dashboard.Data{}}, http.StatusNotFound: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "POST", path: "/projects/{projectKey}/widget-preview", handler: "handlePreviewWidget", tag: "dashboards",
		summary:   "What a widget would show as it is being set up, before it is saved, read as the caller.",
		request:   dashboard.PreviewInput{},
		responses: map[int]any{http.StatusOK: env{"data": dashboard.Data{}}, http.StatusNotFound: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/dashboard-templates", handler: "handleListDashboardTemplates", tag: "dashboards",
		summary:   "The templates a first dashboard is made from, and the widgets each starts with.",
		responses: ok(env{"templates": []dashboard.Template{}, "kinds": []dashboard.KindInfo{}})},

	// Word templates and the static HTML site (#214).
	{method: "GET", path: "/projects/{projectKey}/export-templates", handler: "handleListExportTemplates", tag: "documents", pending: true,
		summary:   "The Word templates the project may export through: its own, and those other projects share.",
		responses: ok(env{"templates": []exporttemplate.Template{}})},
	{method: "POST", path: "/projects/{projectKey}/export-templates", handler: "handleUploadExportTemplate", tag: "documents", pending: true,
		summary: "Upload a Word template, sent as a multipart part named file, whose styles, cover, headers and footers exports take. A template that cannot be used is refused with what is wrong. For the project's administrators; at most " +
			strconv.Itoa(exporttemplate.MaxFileMB) + " MB.",
		multipart: true, request: exporttemplate.Form{},
		responses: map[int]any{http.StatusCreated: env{"template": exporttemplate.Template{}}, http.StatusForbidden: errorEnvelope{}, http.StatusConflict: errorEnvelope{},
			http.StatusRequestEntityTooLarge: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/export-templates/{templateID}", handler: "handleGetExportTemplate", tag: "documents", pending: true,
		summary:   "One Word template, the fields it shows and what it includes.",
		responses: ok(env{"template": exporttemplate.Template{}})},
	{method: "PATCH", path: "/export-templates/{templateID}", handler: "handleUpdateExportTemplate", tag: "documents", pending: true,
		summary: "Rename a template, choose its fields and what it includes, or share it with every project; its project's administrators'.",
		request: exporttemplate.Patch{}, responses: with(http.StatusOK, env{"template": exporttemplate.Template{}})},
	{method: "DELETE", path: "/export-templates/{templateID}", handler: "handleDeleteExportTemplate", tag: "documents", pending: true,
		summary:   "Delete a template; its project's administrators'. Exports made with it stay as they are.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusForbidden: errorEnvelope{}, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/export-templates/{templateID}/file", handler: "handleDownloadExportTemplate", tag: "documents", pending: true,
		summary: "The template's Word file as it was uploaded.",
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusNotFound: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/document.html.zip", handler: "handleProjectSite", tag: "documents", pending: true,
		summary: "The specification as a static HTML site in a zip that opens offline: linked pages with its contents, trace links between them and a search. Printed by the render service as the reader; 503 render_unavailable when the server has none.",
		query:   []param{documentSection, templateQuery},
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusNotFound: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}, http.StatusServiceUnavailable: errorEnvelope{}}},
	{method: "GET", path: "/baselines/{baselineID}/document.html.zip", handler: "handleBaselineSite", tag: "documents", pending: true,
		summary: "A baseline's document as a static HTML site in a zip.",
		query:   []param{templateQuery},
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusNotFound: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}, http.StatusServiceUnavailable: errorEnvelope{}}},
}
