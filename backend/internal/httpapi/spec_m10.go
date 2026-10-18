package httpapi

import (
	"reflect"

	"github.com/cloudster1/exciter/backend/internal/duplicates"
	"github.com/cloudster1/exciter/backend/internal/glossary"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/quality"
	"github.com/cloudster1/exciter/backend/internal/reqifio"
)

// authoringSchemas names the types of authoring quality (#233), whose short names would meet others', and spells
// their enums.
func authoringSchemas(b *openapi.Builder) {
	names := map[reflect.Type]string{
		reflect.TypeFor[quality.Span]():             "QualitySpan",
		reflect.TypeFor[quality.Person]():           "QualityPerson",
		reflect.TypeFor[quality.Acceptance]():       "QualityAcceptance",
		reflect.TypeFor[quality.Finding]():          "QualityFinding",
		reflect.TypeFor[quality.Sentence]():         "QualitySentence",
		reflect.TypeFor[quality.TermMatch]():        "QualityTermMatch",
		reflect.TypeFor[quality.CheckInput]():       "QualityCheckInput",
		reflect.TypeFor[quality.Check]():            "QualityCheck",
		reflect.TypeFor[quality.Quality]():          "Quality",
		reflect.TypeFor[quality.Findings]():         "QualityFindings",
		reflect.TypeFor[quality.AcceptInput]():      "QualityAcceptInput",
		reflect.TypeFor[quality.RuleInfo]():         "QualityRule",
		reflect.TypeFor[quality.RuleSetting]():      "QualityRuleSetting",
		reflect.TypeFor[quality.Words]():            "QualityWords",
		reflect.TypeFor[quality.SettingsInput]():    "QualitySettingsInput",
		reflect.TypeFor[quality.Settings]():         "QualitySettings",
		reflect.TypeFor[quality.Band]():             "QualityBand",
		reflect.TypeFor[quality.RuleCount]():        "QualityRuleCount",
		reflect.TypeFor[quality.Scored]():           "QualityScored",
		reflect.TypeFor[quality.Summary]():          "QualitySummary",
		reflect.TypeFor[glossary.TermRef]():         "TermRef",
		reflect.TypeFor[glossary.Term]():            "Term",
		reflect.TypeFor[glossary.TermInput]():       "TermInput",
		reflect.TypeFor[glossary.TermPatch]():       "TermPatch",
		reflect.TypeFor[glossary.TermRevision]():    "TermRevision",
		reflect.TypeFor[glossary.Use]():             "TermUse",
		reflect.TypeFor[glossary.RequirementTerm](): "RequirementTerm",
		reflect.TypeFor[glossary.ClearInput]():      "TermClearInput",
		reflect.TypeFor[glossary.Library]():         "GlossaryLibrary",
		reflect.TypeFor[glossary.GlossariesInput](): "GlossariesInput",
		reflect.TypeFor[glossary.Glossaries]():      "Glossaries",
		reflect.TypeFor[glossary.Hit]():             "TermHit",
		reflect.TypeFor[duplicates.Similar]():       "SimilarRequirement",
		reflect.TypeFor[duplicates.DraftInput]():    "SimilarDraftInput",
		reflect.TypeFor[duplicates.Pair]():          "DuplicatePair",
		reflect.TypeFor[duplicates.Side]():          "DuplicateSide",
		reflect.TypeFor[duplicates.DismissInput]():  "DuplicateDismissInput",
		reflect.TypeFor[duplicates.MergeInput]():    "DuplicateMergeInput",
		reflect.TypeFor[duplicates.LinkInput]():     "DuplicateLinkInput",
		reflect.TypeFor[duplicates.Decision]():      "DuplicateDecision",
		reflect.TypeFor[duplicates.MovedLink]():     "DuplicateMovedLink",
		reflect.TypeFor[duplicates.Merge]():         "DuplicateMerge",
		reflect.TypeFor[duplicates.Settings]():      "DuplicateSettings",
		reflect.TypeFor[duplicates.SettingsInput](): "DuplicateSettingsInput",
		reflect.TypeFor[reqifio.WordForm]():         "WordImportForm",
		reflect.TypeFor[reqifio.PreviewNode]():      "ImportPreviewNode",
	}
	for t, name := range names {
		b.Names[t] = name
	}
	b.Enums[reflect.TypeOf(quality.Language(""))] = enumStrings(quality.Languages)
	b.Enums[reflect.TypeOf(quality.Severity(""))] = enumStrings(quality.Severities)
	b.Enums[reflect.TypeOf(quality.RuleID(""))] = enumStrings(quality.RuleIDs)
	b.Enums[reflect.TypeOf(quality.Field(""))] = enumStrings(quality.Fields)
	b.Enums[reflect.TypeOf(quality.Pattern(""))] = enumStrings(quality.Patterns)
	b.Enums[reflect.TypeOf(glossary.Status(""))] = enumStrings(glossary.Statuses)
	b.Enums[reflect.TypeOf(duplicates.Kind(""))] = enumStrings(duplicates.Kinds)
	b.Enums[reflect.TypeOf(duplicates.SkipReason(""))] = enumStrings(duplicates.SkipReasons)
	b.Enums[reflect.TypeOf(reqifio.WordMatch(""))] = enumStrings(reqifio.WordMatches)
	b.Enums[reflect.TypeOf(reqifio.NodeKind(""))] = enumStrings(reqifio.NodeKinds)
}
