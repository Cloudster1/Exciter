package httpapi

import (
	"net/http"
	"strconv"

	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/testexec"
)

// The operations test management adds (#202), kept beside the table they join; docs/api-contract-m12.md agrees
// them.
func init() { operations = append(operations, testOperations...) }

// iterationQuery names the iteration of a case with parameters.
var iterationQuery = param{name: "iteration", schema: intParam, description: "The iteration, from 1; 1 when absent."}

// executionStateQuery narrows a project's executions to a state.
var executionStateQuery = param{name: "state", schema: &openapi.Schema{Type: "string", Enum: enumStrings(testexec.States)},
	description: "Only executions in this state; every state when absent."}

var testOperations = []operation{
	// Test plans (#251).
	{method: "GET", path: "/projects/{projectKey}/test-plans", handler: "handleListTestPlans", tag: "tests", pending: true,
		summary:   "The project's test plans, by title.",
		responses: ok(env{"plans": []testexec.Plan{}})},
	{method: "POST", path: "/projects/{projectKey}/test-plans", handler: "handleCreateTestPlan", tag: "tests", pending: true,
		summary: "Make a test plan of test cases picked in order, or found by a query when an execution starts; at most " +
			strconv.Itoa(testexec.MaxPlanCases) + " cases.",
		request: testexec.PlanInput{}, responses: with(http.StatusCreated, env{"plan": testexec.Plan{}})},
	{method: "GET", path: "/test-plans/{planID}", handler: "handleGetTestPlan", tag: "tests", pending: true,
		summary:   "A test plan and its cases: those picked, or what its query finds now.",
		responses: ok(env{"plan": testexec.Plan{}})},
	{method: "PATCH", path: "/test-plans/{planID}", handler: "handleUpdateTestPlan", tag: "tests", pending: true,
		summary: "Rename a test plan, describe it, or change its query.",
		request: testexec.PlanPatch{}, responses: with(http.StatusOK, env{"plan": testexec.Plan{}})},
	{method: "PUT", path: "/test-plans/{planID}/cases", handler: "handleSetTestPlanCases", tag: "tests", pending: true,
		summary: "Set the cases a plan picks, in order; each a test case of its project.",
		request: testexec.CasesInput{}, responses: with(http.StatusOK, env{"plan": testexec.Plan{}})},
	{method: "DELETE", path: "/test-plans/{planID}", handler: "handleDeleteTestPlan", tag: "tests", pending: true,
		summary:   "Delete a test plan; the executions it started keep its title.",
		responses: none()},

	// Test executions (#251).
	{method: "GET", path: "/projects/{projectKey}/test-executions", handler: "handleListTestExecutions", tag: "tests", pending: true,
		summary:   "The project's test executions, open ones first, with their progress.",
		query:     []param{executionStateQuery},
		responses: ok(env{"executions": []testexec.Execution{}})},
	{method: "POST", path: "/projects/{projectKey}/test-executions", handler: "handleStartTestExecution", tag: "tests", pending: true,
		summary: "Start an execution of a plan against an optional baseline, a version and a configuration, with testers; its cases are pinned at once, at the revisions the baseline holds or their current ones. The caller manages it.",
		request: testexec.ExecutionInput{}, responses: with(http.StatusCreated, env{"execution": testexec.Execution{}})},
	{method: "GET", path: "/test-executions/mine", handler: "handleListMyTestExecutions", tag: "tests", pending: true,
		summary:   "The executions the caller tests or manages, across projects, open ones first.",
		responses: ok(env{"executions": []testexec.Execution{}})},
	{method: "GET", path: "/test-executions/{executionID}", handler: "handleGetTestExecution", tag: "tests", pending: true,
		summary:   "A test execution, its testers, its cases with their outcomes, and its progress.",
		responses: ok(env{"execution": testexec.Execution{}})},
	{method: "PATCH", path: "/test-executions/{executionID}", handler: "handleUpdateTestExecution", tag: "tests", pending: true,
		summary: "Change an open execution's title, version, configuration or due date; its manager's.",
		request: testexec.ExecutionPatch{}, responses: with(http.StatusOK, env{"execution": testexec.Execution{}})},
	{method: "POST", path: "/test-executions/{executionID}/close", handler: "handleCloseTestExecution", tag: "tests", pending: true,
		summary: "Close an execution, or abort it so it counts for nothing; it never changes again. Its manager's.",
		request: testexec.CloseInput{}, responses: with(http.StatusOK, env{"execution": testexec.Execution{}})},
	{method: "GET", path: "/test-executions/{executionID}/cases/{reqKey}", handler: "handleGetTestExecutionCase", tag: "tests", pending: true,
		summary:   "One case iteration of an execution: its preconditions and steps at the pinned revision, and what was recorded, with files and issues.",
		query:     []param{iterationQuery},
		responses: ok(env{"case": testexec.Case{}})},
	{method: "PUT", path: "/test-executions/{executionID}/cases/{reqKey}/result", handler: "handleRecordTestResult", tag: "tests", pending: true,
		summary: "Record a case iteration: each step's outcome and actual result, and the case's own, which follows from its steps when absent. For the manager and testers who may comment, while it is open.",
		request: testexec.RecordInput{}, responses: with(http.StatusOK, env{"case": testexec.Case{}})},
	{method: "POST", path: "/test-executions/{executionID}/cases/{reqKey}/files", handler: "handleUploadTestExecutionFile", tag: "tests", pending: true,
		summary:   "Record a file on a step or a case iteration, sent as a multipart part named file; at most " + strconv.Itoa(testexec.MaxFileMB) + " MB.",
		query:     []param{iterationQuery, {name: "step", schema: intParam, description: "The step, from 1; the case itself when absent."}},
		multipart: true, responses: map[int]any{http.StatusCreated: env{"file": testexec.File{}}, http.StatusRequestEntityTooLarge: errorEnvelope{}}},
	{method: "GET", path: "/test-execution-files/{fileID}", handler: "handleDownloadTestExecutionFile", tag: "tests", pending: true,
		summary: "The bytes of a file recorded in an execution.",
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusNotFound: errorEnvelope{}}},
	{method: "DELETE", path: "/test-execution-files/{fileID}", handler: "handleDeleteTestExecutionFile", tag: "tests", pending: true,
		summary:   "Delete a file its uploader recorded, while the execution is open.",
		responses: none()},
	{method: "POST", path: "/test-executions/{executionID}/cases/{reqKey}/issues", handler: "handleFileTestFailureIssue", tag: "tests", pending: true,
		summary: "File a failed case iteration as an issue in Armature with the caller's token, linked with relates to the case and the requirements it verifies.",
		request: testexec.FailureIssueInput{}, responses: map[int]any{http.StatusCreated: env{"issue": testexec.Issue{}}, http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/test-executions/{executionID}/report", handler: "handleGetTestExecutionReport", tag: "tests", pending: true,
		summary:   "An execution's report: its target, testers, counts by outcome, and each case with its steps, outcomes, notes, files and issues.",
		responses: ok(env{"report": testexec.Report{}})},
	{method: "GET", path: "/test-executions/{executionID}/report.docx", handler: "handleTestExecutionReportDocx", tag: "tests", pending: true,
		summary: "An execution's report as a Word file, through a template of the project when one is named.",
		query:   []param{templateQuery},
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusNotFound: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/test-executions/{executionID}/report.pdf", handler: "handleTestExecutionReportPDF", tag: "tests", pending: true,
		summary: "An execution's report as a PDF, printed by the render service as the reader.",
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusNotFound: errorEnvelope{}, http.StatusServiceUnavailable: errorEnvelope{}}},
	{method: "GET", path: "/requirements/{reqKey}/test-executions", handler: "handleListTestCaseResults", tag: "tests", pending: true,
		summary:   "A test case's results in every execution, newest first.",
		responses: ok(env{"results": []testexec.CaseResult{}})},
}
