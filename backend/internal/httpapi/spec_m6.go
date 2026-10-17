package httpapi

import (
	"reflect"

	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/review"
)

// documentSchemas names the types of the document view (#167), whose bulk results share short names across packages,
// and spells their enums.
func documentSchemas(b *openapi.Builder) {
	names := map[reflect.Type]string{
		reflect.TypeFor[requirement.BulkRefusal]():            "BulkRefusal",
		reflect.TypeFor[requirement.RequirementTransitions](): "RequirementTransitions",
		reflect.TypeFor[requirement.BulkTransitionInput]():    "BulkTransitionInput",
		reflect.TypeFor[requirement.BulkTransitionResult]():   "BulkTransitionResult",
		reflect.TypeFor[review.BulkRequestItem]():             "BulkReviewRequestItem",
		reflect.TypeFor[review.BulkRequestInput]():            "BulkReviewRequest",
		reflect.TypeFor[review.BulkRequestResult]():           "BulkReviewRequestResult",
		reflect.TypeFor[requirement.SpecReview]():             "SpecReview",
	}
	for t, name := range names {
		b.Names[t] = name
	}
	b.Enums[reflect.TypeOf(requirement.BulkOutcome(""))] = enumStrings(requirement.BulkOutcomes)
	b.Enums[reflect.TypeOf(requirement.ChangeBase(""))] = enumStrings(requirement.ChangeBases)
	b.Enums[reflect.TypeOf(requirement.ChangeKind(""))] = enumStrings(requirement.ChangeKinds)
}
