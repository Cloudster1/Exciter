package httpapi

import (
	"net/http"
	"strconv"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/review"
)

// The operations review sessions and control add (#215), kept beside the table they join; docs/api-contract-m8.md
// agrees them.
func init() { operations = append(operations, controlOperations...) }

// sessionStateQuery keeps a project's sessions in one state.
var sessionStateQuery = param{name: "state", description: "open or closed; both, open first, when absent."}

var controlOperations = []operation{
	// Signed baselines (#205).
	{method: "GET", path: "/projects/{projectKey}/baseline-signing", handler: "handleGetBaselineSigning", tag: "baselines", pending: true,
		summary:   "Who signs the project's baselines and how many signatures release one; null while nobody does.",
		responses: ok(env{"rule": (*baseline.SigningRule)(nil)})},
	{method: "PUT", path: "/projects/{projectKey}/baseline-signing", handler: "handleSaveBaselineSigning", tag: "baselines", pending: true,
		summary: "Name who signs the project's baselines and how many must; the project's administrators'. A baseline being signed keeps the rule its first signature froze.",
		request: baseline.SigningRuleInput{}, responses: map[int]any{http.StatusOK: env{"rule": baseline.SigningRule{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/projects/{projectKey}/baseline-signing", handler: "handleDeleteBaselineSigning", tag: "baselines", pending: true,
		summary:   "Stop signing new baselines; the project's administrators'. Signatures given stay.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusForbidden: errorEnvelope{}}},
	{method: "GET", path: "/baselines/{baselineID}/signatures", handler: "handleListBaselineSignatures", tag: "baselines", pending: true,
		summary:   "Every signature of a baseline, in the order they were given.",
		responses: ok(env{"signatures": []baseline.Signature{}})},
	{method: "POST", path: "/baselines/{baselineID}/signatures", handler: "handleSignBaseline", tag: "baselines", pending: true,
		summary: "Sign a baseline with the meaning shown, proving who one is again with the password or a fresh sign-in; a signed baseline keeps its name and is never deleted.",
		request: baseline.SignInput{}, responses: map[int]any{http.StatusOK: env{"baseline": baseline.Baseline{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}, http.StatusTooManyRequests: errorEnvelope{}}},

	// Locked requirements (#205).
	{method: "POST", path: "/requirements/{reqKey}/lock", handler: "handleLockRequirement", tag: "requirements", pending: true,
		summary: "Lock a requirement by hand, with a reason, or change the reason; the project's administrators'. It takes no new revision, move, link or trash until unlocked.",
		request: requirement.LockInput{}, responses: map[int]any{http.StatusOK: env{"requirement": requirement.Requirement{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/requirements/{reqKey}/lock", handler: "handleUnlockRequirement", tag: "requirements", pending: true,
		summary: "Clear a requirement's hand lock; the project's administrators'. A lock by its status stays until a transition takes it out.",
		responses: map[int]any{http.StatusOK: env{"requirement": requirement.Requirement{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}}},

	// Field rules (#206).
	{method: "GET", path: "/projects/{projectKey}/field-rules", handler: "handleListFieldRules", tag: "types", pending: true,
		summary:   "When the fixed fields are required and who changes them; an attribute's rules are on its definition.",
		responses: ok(env{"rules": []attribute.FieldRule{}})},
	{method: "PUT", path: "/projects/{projectKey}/field-rules/{field}", handler: "handleSaveFieldRule", tag: "types", pending: true,
		summary: "Set a fixed field's rule whole: the status it is required from and who changes it in which statuses. For the project's administrators.",
		request: attribute.FieldRuleInput{}, responses: map[int]any{http.StatusOK: env{"rule": attribute.FieldRule{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},

	// Review sessions (#204).
	{method: "GET", path: "/projects/{projectKey}/review-sessions", handler: "handleListReviewSessions", tag: "reviews", pending: true,
		summary:   "A project's review sessions, open first, then newest first.",
		query:     append([]param{sessionStateQuery}, windowQuery(review.DefaultLimit, review.MaxLimit)...),
		responses: ok(env{"sessions": []review.SessionSummary{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "POST", path: "/projects/{projectKey}/review-sessions", handler: "handleOpenReviewSession", tag: "reviews", pending: true,
		summary: "Open a review of a section, a baseline's requirements or a query's, with its participants, deadline and quorum; each requirement is pinned and asked for review at once, or none is. At most " +
			strconv.Itoa(review.MaxSessionItems) + " requirements.",
		request: review.SessionInput{}, responses: map[int]any{http.StatusCreated: env{"session": review.Session{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/review-sessions/{sessionID}", handler: "handleGetReviewSession", tag: "reviews", pending: true,
		summary:   "One review session with its progress per requirement and per participant.",
		responses: ok(env{"session": review.Session{}})},
	{method: "PATCH", path: "/review-sessions/{sessionID}", handler: "handleUpdateReviewSession", tag: "reviews", pending: true,
		summary: "Change a session's title, description or deadline; its moderator's.",
		request: review.SessionUpdate{}, responses: map[int]any{http.StatusOK: env{"session": review.Session{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "POST", path: "/review-sessions/{sessionID}/rounds", handler: "handleStartReviewRound", tag: "reviews", pending: true,
		summary: "Start the next round: what was revised since its pin is pinned and asked for review anew, the rest keep their decisions; its moderator's.",
		request: review.RoundInput{}, responses: map[int]any{http.StatusOK: env{"session": review.Session{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}}},
	{method: "POST", path: "/review-sessions/{sessionID}/close", handler: "handleCloseReviewSession", tag: "reviews", pending: true,
		summary: "Close a session, withdrawing what is still pending, and take a baseline of what it approved when one is named; its moderator's.",
		request: review.CloseInput{}, responses: map[int]any{http.StatusOK: env{"session": review.Session{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/review-sessions/{sessionID}/report", handler: "handleGetReviewSessionReport", tag: "reviews", pending: true,
		summary:   "A session's report: participants, every requirement's decisions per round, open comment threads, rounds and the baseline it took.",
		responses: ok(env{"report": review.Report{}})},
	{method: "GET", path: "/review-sessions/{sessionID}/report.docx", handler: "handleReviewSessionReportDocx", tag: "reviews", pending: true,
		summary: "A session's report as a Word file.",
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusNotFound: errorEnvelope{}}},
	{method: "GET", path: "/review-sessions/{sessionID}/report.pdf", handler: "handleReviewSessionReportPDF", tag: "reviews", pending: true,
		summary: "A session's report as a PDF, printed by the render service as the reader.",
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusNotFound: errorEnvelope{}, http.StatusServiceUnavailable: errorEnvelope{}}},
	{method: "GET", path: "/reviews/sessions", handler: "handleListMyReviewSessions", tag: "reviews", pending: true,
		summary:   "The open sessions the caller takes part in, across every project, nearest deadline first, each with what is still the caller's to do.",
		query:     windowQuery(review.DefaultLimit, review.MaxLimit),
		responses: ok(env{"sessions": []review.SessionSummary{}, "total": 0, "limit": 0, "offset": 0})},
}
