package httpapi

import (
	"reflect"

	"github.com/cloudster1/exciter/backend/internal/codetrace"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/testresult"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// evidenceSchemas names the types of verification evidence (#224), whose short names would meet others', and spells
// their enums.
func evidenceSchemas(b *openapi.Builder) {
	names := map[reflect.Type]string{
		reflect.TypeFor[testresult.RunForm]():       "TestRunForm",
		reflect.TypeFor[testresult.Run]():           "TestRun",
		reflect.TypeFor[testresult.Counts]():        "TestRunCounts",
		reflect.TypeFor[testresult.Reported]():      "TestRunReported",
		reflect.TypeFor[testresult.Result]():        "TestResult",
		reflect.TypeFor[testresult.CommitLink]():    "TestResultCommit",
		reflect.TypeFor[testresult.Settings]():      "EvidenceSettings",
		reflect.TypeFor[testresult.SettingsInput](): "EvidenceSettingsInput",
		reflect.TypeFor[codetrace.FoundTag]():       "CodeScanTag",
		reflect.TypeFor[codetrace.ScanInput]():      "CodeScanInput",
		reflect.TypeFor[codetrace.Reported]():       "CodeScanReported",
		reflect.TypeFor[codetrace.Scan]():           "CodeScan",
		reflect.TypeFor[codetrace.Link]():           "CodeLink",
		reflect.TypeFor[codetrace.Tag]():            "CodeTag",
		reflect.TypeFor[codetrace.Words]():          "CodeTagWordsInput",
		reflect.TypeFor[codetrace.TagWords]():       "CodeTagWords",
		reflect.TypeFor[trace.Check]():              "TraceCheck",
		reflect.TypeFor[trace.CheckBroken]():        "TraceCheckBroken",
		reflect.TypeFor[trace.CheckGap]():           "TraceCheckGap",
		reflect.TypeFor[trace.CheckEvidence]():      "TraceCheckEvidence",
		reflect.TypeFor[trace.CheckOrphan]():        "TraceCheckOrphan",
		reflect.TypeFor[trace.CheckTotals]():        "TraceCheckTotals",
		reflect.TypeFor[trace.CheckEvidenceSeen]():  "TraceCheckEvidenceSeen",
	}
	for t, name := range names {
		b.Names[t] = name
	}
	b.Enums[reflect.TypeOf(testresult.Outcome(""))] = enumStrings(testresult.Outcomes)
	b.Enums[reflect.TypeOf(testresult.RunState(""))] = enumStrings(testresult.RunStates)
	b.Enums[reflect.TypeOf(testresult.Problem(""))] = enumStrings(testresult.Problems)
	b.Enums[reflect.TypeOf(codetrace.Relation(""))] = enumStrings(codetrace.Relations)
	b.Enums[reflect.TypeOf(codetrace.State(""))] = enumStrings(codetrace.States)
	b.Enums[reflect.TypeOf(codetrace.Problem(""))] = enumStrings(codetrace.Problems)
	b.Enums[reflect.TypeOf(trace.Verdict(""))] = enumStrings(trace.Verdicts)
	b.Enums[reflect.TypeOf(trace.CheckReason(""))] = enumStrings(trace.CheckReasons)
	b.Enums[reflect.TypeOf(trace.EvidenceKind(""))] = enumStrings(trace.EvidenceKinds)
	b.Enums[reflect.TypeOf(trace.Source(""))] = enumStrings(trace.VerifiedSources)
	b.FieldOverrides["TraceCheckEvidence.outcome"] = &openapi.Schema{Type: "string", Enum: enumStrings(testresult.Outcomes)}
	b.FieldOverrides["TraceCheckOrphan.problem"] = &openapi.Schema{Type: "string", Enum: enumStrings(codetrace.Problems)}
}
