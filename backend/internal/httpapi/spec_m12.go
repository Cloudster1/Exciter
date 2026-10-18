package httpapi

import (
	"reflect"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/testexec"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// testSchemas names the types of test management (#253), whose short names would meet others', and spells their
// enums.
func testSchemas(b *openapi.Builder) {
	names := map[reflect.Type]string{
		reflect.TypeFor[requirement.Step]():           "TestStep",
		reflect.TypeFor[requirement.Parameter]():      "TestParameter",
		reflect.TypeFor[requirement.TestCase]():       "TestCaseContent",
		reflect.TypeFor[requirement.StepDiff]():       "TestStepDiff",
		reflect.TypeFor[requirement.ParameterDiff]():  "TestParameterDiff",
		reflect.TypeFor[testexec.CaseRef]():           "TestCaseRef",
		reflect.TypeFor[testexec.PlanCan]():           "TestPlanCan",
		reflect.TypeFor[testexec.Plan]():              "TestPlan",
		reflect.TypeFor[testexec.PlanInput]():         "TestPlanInput",
		reflect.TypeFor[testexec.PlanPatch]():         "TestPlanPatch",
		reflect.TypeFor[testexec.CasesInput]():        "TestPlanCasesInput",
		reflect.TypeFor[testexec.BaselineRef]():       "TestExecutionBaseline",
		reflect.TypeFor[testexec.Excluded]():          "TestExecutionExcluded",
		reflect.TypeFor[testexec.Progress]():          "TestExecutionProgress",
		reflect.TypeFor[testexec.ExecutionCan]():      "TestExecutionCan",
		reflect.TypeFor[testexec.Execution]():         "TestExecution",
		reflect.TypeFor[testexec.ExecutionInput]():    "TestExecutionInput",
		reflect.TypeFor[testexec.ExecutionPatch]():    "TestExecutionPatch",
		reflect.TypeFor[testexec.CloseInput]():        "TestExecutionCloseInput",
		reflect.TypeFor[testexec.CaseRun]():           "TestCaseRun",
		reflect.TypeFor[testexec.CIResult]():          "TestCaseCIResult",
		reflect.TypeFor[testexec.StepResult]():        "TestStepResult",
		reflect.TypeFor[testexec.File]():              "TestExecutionFile",
		reflect.TypeFor[testexec.Issue]():             "TestExecutionIssue",
		reflect.TypeFor[testexec.Case]():              "TestExecutionCase",
		reflect.TypeFor[testexec.StepInput]():         "TestStepInput",
		reflect.TypeFor[testexec.RecordInput]():       "TestRecordInput",
		reflect.TypeFor[testexec.FailureIssueInput](): "TestFailureIssueInput",
		reflect.TypeFor[testexec.CaseResult]():        "TestCaseResult",
		reflect.TypeFor[testexec.Report]():            "TestExecutionReport",
		reflect.TypeFor[testexec.ReportCase]():        "TestExecutionReportCase",
		reflect.TypeFor[trace.LastRun]():              "TestCaseLastRun",
	}
	for t, name := range names {
		b.Names[t] = name
	}
	b.Enums[reflect.TypeOf(attribute.TypeRole(""))] = enumStrings(attribute.TypeRoles)
	b.Enums[reflect.TypeOf(testexec.Outcome(""))] = enumStrings(testexec.Outcomes)
	b.Enums[reflect.TypeOf(testexec.State(""))] = enumStrings(testexec.States)
	b.Enums[reflect.TypeOf(testexec.Scope(""))] = enumStrings(testexec.Scopes)
	b.Enums[reflect.TypeOf(testexec.Source(""))] = enumStrings(testexec.Sources)
	b.Enums[reflect.TypeOf(trace.RunOutcome(""))] = enumStrings(trace.RunOutcomes)
}
