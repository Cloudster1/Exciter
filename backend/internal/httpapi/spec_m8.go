package httpapi

import (
	"reflect"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/review"
)

// controlSchemas names the types of review sessions and control (#216), whose short names would meet others', and
// spells their enums.
func controlSchemas(b *openapi.Builder) {
	names := map[reflect.Type]string{
		reflect.TypeFor[baseline.Signer]():           "BaselineSigner",
		reflect.TypeFor[baseline.SigningRule]():      "BaselineSigningRule",
		reflect.TypeFor[baseline.SigningRuleInput](): "BaselineSigningRuleInput",
		reflect.TypeFor[baseline.Signing]():          "BaselineSigning",
		reflect.TypeFor[baseline.Signature]():        "BaselineSignature",
		reflect.TypeFor[baseline.SignInput]():        "BaselineSignRequest",
		reflect.TypeFor[requirement.Lock]():          "RequirementLock",
		reflect.TypeFor[requirement.LockInput]():     "RequirementLockRequest",
		reflect.TypeFor[attribute.Permit]():          "FieldPermit",
		reflect.TypeFor[attribute.Dependency]():      "AttributeDependency",
		reflect.TypeFor[attribute.FieldRule]():       "FieldRule",
		reflect.TypeFor[attribute.FieldRuleInput]():  "FieldRuleInput",
		reflect.TypeFor[review.Scope]():              "ReviewSessionScope",
		reflect.TypeFor[review.Since]():              "ReviewSessionSince",
		reflect.TypeFor[review.Participant]():        "ReviewSessionParticipant",
		reflect.TypeFor[review.SessionInput]():       "ReviewSessionInput",
		reflect.TypeFor[review.SessionUpdate]():      "ReviewSessionUpdate",
		reflect.TypeFor[review.RoundInput]():         "ReviewRoundRequest",
		reflect.TypeFor[review.CloseBaseline]():      "ReviewSessionBaseline",
		reflect.TypeFor[review.CloseInput]():         "ReviewSessionClose",
		reflect.TypeFor[review.SessionRef]():         "ReviewSessionRef",
		reflect.TypeFor[review.SessionItem]():        "ReviewSessionItem",
		reflect.TypeFor[review.Excluded]():           "ReviewSessionExcluded",
		reflect.TypeFor[review.Progress]():           "ReviewSessionProgress",
		reflect.TypeFor[review.SessionCounts]():      "ReviewSessionCounts",
		reflect.TypeFor[review.SessionCan]():         "ReviewSessionCan",
		reflect.TypeFor[review.Session]():            "ReviewSession",
		reflect.TypeFor[review.SessionSummary]():     "ReviewSessionSummary",
		reflect.TypeFor[review.RoundEntry]():         "ReviewRound",
		reflect.TypeFor[review.ReportRound]():        "ReviewReportRound",
		reflect.TypeFor[review.ReportItem]():         "ReviewReportItem",
		reflect.TypeFor[review.Report]():             "ReviewSessionReport",
	}
	for t, name := range names {
		b.Names[t] = name
	}
	b.Enums[reflect.TypeOf(baseline.SignerType(""))] = enumStrings(baseline.SignerTypes)
	b.Enums[reflect.TypeOf(requirement.LockBy(""))] = enumStrings(requirement.LockBys)
	b.Enums[reflect.TypeOf(review.SessionState(""))] = enumStrings(review.SessionStates)
	b.Enums[reflect.TypeOf(review.ScopeKind(""))] = enumStrings(review.ScopeKinds)
	b.Enums[reflect.TypeOf(review.ParticipantRole(""))] = enumStrings(review.ParticipantRoles)
	b.FieldOverrides["ReviewSessionSince.base"] = &openapi.Schema{Type: "string", Enum: []string{"approved", "baseline"}}
	b.FieldOverrides["FieldRule.field"] = &openapi.Schema{Type: "string", Enum: attribute.FixedFields}
}
