package httpapi

import (
	"net/http"
	"strconv"

	"github.com/cloudster1/exciter/backend/internal/codetrace"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/testresult"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// The operations verification evidence adds (#223), kept beside the table they join; docs/api-contract-m9.md agrees
// them.
func init() { operations = append(operations, evidenceOperations...) }

// branchQuery names the branch whose evidence is read; the project's counting branch when absent.
var branchQuery = param{name: "branch", description: "The branch whose evidence counts; the project's counting branch when absent."}

// checkQuery narrows a trace check beyond the grid's filter.
var checkQuery = []param{branchQuery,
	{name: "fail", repeated: true, schema: &openapi.Schema{Type: "string", Enum: enumStrings(trace.CheckReasons)},
		description: "What fails the check; broken chains, coverage gaps and failing tests when absent."}}

var evidenceOperations = []operation{
	// Test results from CI (#200).
	{method: "POST", path: "/projects/{projectKey}/test-runs", evidence: true, handler: "handleUploadTestRun", tag: "evidence",
		summary: "Upload a CI run's JUnit files, naming the repository, the commit, the branch and the run; the worker reads them and the run answers what it found. Needs edit; at most " +
			strconv.Itoa(testresult.MaxRunFiles) + " files of " + strconv.Itoa(testresult.MaxRunMB) + " MB together.",
		multipart: true, files: true, request: testresult.RunForm{},
		responses: map[int]any{http.StatusCreated: env{"run": testresult.Run{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusRequestEntityTooLarge: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/test-runs/{runID}", evidence: true, handler: "handleGetTestRun", tag: "evidence",
		summary:   "One upload: where the worker has it, its counts and the keys that verify nothing.",
		responses: ok(env{"run": testresult.Run{}})},
	{method: "GET", path: "/requirements/{reqKey}/test-results", handler: "handleListTestResults", tag: "evidence",
		tool: "get_test_results", toolHelp: "The latest CI result of each test that verifies a requirement, per repository, branch and job, with its commit and run, and whether a newer revision outdated it; branch keeps one branch.",
		summary:   "The latest result of each test that verifies the requirement, per repository, branch and job, each saying whether a newer revision outdated it.",
		query:     []param{branchQuery},
		responses: ok(env{"results": []testresult.Result{}, "countingBranch": ""})},
	{method: "GET", path: "/projects/{projectKey}/evidence-settings", handler: "handleGetEvidenceSettings", tag: "evidence",
		summary:   "The branch whose results and code tags count for the project's coverage, matrix and queries.",
		responses: ok(env{"settings": testresult.Settings{}})},
	{method: "PUT", path: "/projects/{projectKey}/evidence-settings", handler: "handleSaveEvidenceSettings", tag: "evidence",
		summary: "Name the branch whose evidence counts; the project's administrators'.",
		request: testresult.SettingsInput{}, responses: map[int]any{http.StatusOK: env{"settings": testresult.Settings{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/trace-check", evidence: true, handler: "handleTraceCheck", tag: "evidence",
		tool: "run_trace_check", toolHelp: "Pass or fail for a project's trace, narrowed like list_requirements, on one branch: its broken chains, coverage gaps and failing or outdated test results; fail names what fails it, broken, gaps and failing when absent.",
		summary:   "Pass or fail for a pipeline: the broken chains, coverage gaps and failing or outdated evidence of the requirements the filter keeps, on one branch.",
		query:     append(append([]param{}, gridQuery...), checkQuery...),
		responses: map[int]any{http.StatusOK: env{"check": trace.Check{}}, http.StatusBadRequest: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},

	// Code that implements and tests requirements (#201).
	{method: "POST", path: "/projects/{projectKey}/code-scans", evidence: true, handler: "handleUploadCodeScan", tag: "evidence",
		summary: "Upload the tags a scan found in a repository at a commit; it replaces the branch's scan before. Needs edit; at most " +
			strconv.Itoa(codetrace.MaxScanTags) + " tags.",
		request: codetrace.ScanInput{}, responses: map[int]any{http.StatusCreated: env{"scan": codetrace.Scan{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusRequestEntityTooLarge: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/code-scans/{scanID}", evidence: true, handler: "handleGetCodeScan", tag: "evidence",
		summary:   "One scan and the tags it carried that link nothing.",
		responses: ok(env{"scan": codetrace.Scan{}})},
	{method: "GET", path: "/requirements/{reqKey}/code-links", handler: "handleListCodeLinks", tag: "evidence",
		tool: "get_code_links", toolHelp: "The files and lines that implement or verify a requirement, per repository and branch, each with its commit, the revision its tag names and whether a newer revision outdated it; branch keeps one branch.",
		summary:   "The files and lines that implement or verify the requirement, per repository and branch, linking to them at their commit.",
		query:     []param{branchQuery},
		responses: ok(env{"links": []codetrace.Link{}, "countingBranch": ""})},
	{method: "GET", path: "/projects/{projectKey}/code-tags", handler: "handleListCodeTags", tag: "evidence",
		summary: "The project's code tags in one state, outdated or orphaned, on one branch.",
		query: append([]param{branchQuery, {name: "state", schema: &openapi.Schema{Type: "string", Enum: enumStrings(codetrace.States)},
			description: "outdated or orphaned; both when absent."}}, windowQuery(codetrace.DefaultLimit, codetrace.MaxLimit)...),
		responses: map[int]any{http.StatusOK: env{"tags": []codetrace.Tag{}, "total": 0, "limit": 0, "offset": 0}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/code-tag-words", evidence: true, handler: "handleGetCodeTagWords", tag: "evidence",
		summary:   "The words the project's tags use for implements and verifies, and the keys a tag may carry, for the scanner.",
		responses: ok(env{"words": codetrace.TagWords{}})},
	{method: "PUT", path: "/projects/{projectKey}/code-tag-words", handler: "handleSaveCodeTagWords", tag: "evidence",
		summary: "Set the words the project's tags use; the project's administrators'. Tags are read by the words of the day, so nothing is scanned again.",
		request: codetrace.Words{}, responses: map[int]any{http.StatusOK: env{"words": codetrace.TagWords{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
}
