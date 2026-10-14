package httpapi

import (
	"reflect"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/comment"
	"github.com/cloudster1/exciter/backend/internal/notify"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/review"
	"github.com/cloudster1/exciter/backend/internal/trace"
	"github.com/cloudster1/exciter/backend/internal/watch"
)

// reviewSchemas names the types of review and control (#32), whose packages reuse short names such as Input and Can,
// and spells their enums.
func reviewSchemas(b *openapi.Builder) {
	names := map[reflect.Type]string{
		reflect.TypeFor[comment.Thread]():          "CommentThread",
		reflect.TypeFor[comment.ThreadCan]():       "CommentThreadCan",
		reflect.TypeFor[comment.Comment]():         "Comment",
		reflect.TypeFor[comment.Can]():             "CommentCan",
		reflect.TypeFor[comment.Input]():           "CommentInput",
		reflect.TypeFor[comment.Mentionable]():     "MentionablePerson",
		reflect.TypeFor[watch.Watching]():          "Watching",
		reflect.TypeFor[watch.Watch]():             "Watch",
		reflect.TypeFor[watch.Watcher]():           "Watcher",
		reflect.TypeFor[notify.Notification]():     "Notification",
		reflect.TypeFor[notify.ReadInput]():        "NotificationsRead",
		reflect.TypeFor[notify.Preferences]():      "NotificationPreferences",
		reflect.TypeFor[review.Rule]():             "ApprovalRule",
		reflect.TypeFor[review.RuleInput]():        "ApprovalRuleInput",
		reflect.TypeFor[review.Subject]():          "Approver",
		reflect.TypeFor[review.Review]():           "Review",
		reflect.TypeFor[review.DecisionEntry]():    "ReviewDecision",
		reflect.TypeFor[review.Can]():              "ReviewCan",
		reflect.TypeFor[review.RequestInput]():     "ReviewRequest",
		reflect.TypeFor[review.DecisionInput]():    "ReviewDecisionInput",
		reflect.TypeFor[review.Approval]():         "Approval",
		reflect.TypeFor[trace.MovedEnd]():          "SuspectLinkEnd",
		reflect.TypeFor[trace.SuspectLink]():       "SuspectLink",
		reflect.TypeFor[trace.ClearInput]():        "LinkClearInput",
		reflect.TypeFor[trace.Clearance]():         "LinkClearance",
		reflect.TypeFor[trace.CoverageRule]():      "CoverageRule",
		reflect.TypeFor[trace.CoverageRuleInput](): "CoverageRuleInput",
		reflect.TypeFor[trace.Totals]():            "CoverageTotals",
		reflect.TypeFor[trace.CoverageSummary]():   "CoverageSummary",
		reflect.TypeFor[trace.CoverageRow]():       "CoverageRow",
		reflect.TypeFor[trace.MatrixColumn]():      "MatrixColumn",
		reflect.TypeFor[trace.MatrixCell]():        "MatrixCell",
		reflect.TypeFor[trace.MatrixLink]():        "MatrixLink",
		reflect.TypeFor[trace.Matrix]():            "TraceMatrix",
		reflect.TypeFor[baseline.Baseline]():       "Baseline",
		reflect.TypeFor[baseline.Input]():          "NewBaseline",
		reflect.TypeFor[baseline.UpdateInput]():    "BaselineUpdate",
		reflect.TypeFor[baseline.Entry]():          "BaselineEntry",
		reflect.TypeFor[baseline.Side]():           "BaselineSide",
		reflect.TypeFor[baseline.ItemSide]():       "BaselineItem",
		reflect.TypeFor[baseline.ItemChange]():     "BaselineItemChange",
		reflect.TypeFor[baseline.LinkChange]():     "BaselineLinkChange",
		reflect.TypeFor[baseline.ExternalChange](): "BaselineExternalChange",
		reflect.TypeFor[baseline.Comparison]():     "BaselineComparison",
		reflect.TypeFor[armature.ClearInput]():     "ArmatureClearInput",
	}
	for t, name := range names {
		b.Names[t] = name
	}
	b.Enums[reflect.TypeOf(watch.Kind(""))] = enumStrings(watch.Kinds)
	b.Enums[reflect.TypeOf(notify.Kind(""))] = enumStrings(notify.Kinds)
	b.Enums[reflect.TypeOf(notify.Digest(""))] = enumStrings(notify.Digests)
	b.Enums[reflect.TypeOf(review.SubjectType(""))] = enumStrings(review.SubjectTypes)
	b.Enums[reflect.TypeOf(review.State(""))] = enumStrings(review.States)
	b.Enums[reflect.TypeOf(review.Decision(""))] = enumStrings(review.Decisions)
	b.Enums[reflect.TypeOf(trace.End(""))] = enumStrings(trace.Ends)
	b.Enums[reflect.TypeOf(trace.Level(""))] = enumStrings(trace.Levels)
	b.Enums[reflect.TypeOf(baseline.Change(""))] = enumStrings(baseline.Changes)
}
