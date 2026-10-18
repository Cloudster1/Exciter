package httpapi

import (
	"net/http"
	"strconv"

	"github.com/cloudster1/exciter/backend/internal/duplicates"
	"github.com/cloudster1/exciter/backend/internal/glossary"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/quality"
	"github.com/cloudster1/exciter/backend/internal/reqifio"
)

// The operations authoring quality adds (#232), kept beside the table they join; docs/api-contract-m10.md agrees them.
func init() { operations = append(operations, authoringOperations...) }

// revisionQuery names a revision of a requirement; its current one when absent.
var revisionQuery = param{name: "revision", schema: intParam, description: "A revision of the requirement; its current one when absent."}

// refusals are what a write of these operations may answer besides its success.
var refusals = map[int]any{http.StatusForbidden: errorEnvelope{}, http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}

// with is a success beside the usual refusals.
func with(status int, body any) map[int]any {
	out := map[int]any{status: body}
	for k, v := range refusals {
		out[k] = v
	}
	return out
}

var authoringOperations = []operation{
	// Writing rules (#207).
	{method: "POST", path: "/projects/{projectKey}/quality-check", handler: "handleQualityCheck", tag: "quality", pending: true,
		summary: "Check a draft's title and statement against the project's writing rules as it is written, before it is saved: each finding with where it is and how to fix it, the score, and the glossary terms it uses. Needs edit.",
		request: quality.CheckInput{}, responses: with(http.StatusOK, env{"check": quality.Check{}})},
	{method: "GET", path: "/requirements/{reqKey}/quality", handler: "handleGetQuality", tag: "quality", pending: true,
		summary:   "What the writing rules found in a revision of the requirement, which findings count and which an author accepted, and its score.",
		query:     []param{revisionQuery},
		responses: ok(env{"quality": quality.Findings{}})},
	{method: "POST", path: "/requirements/{reqKey}/quality/accepted", handler: "handleAcceptFinding", tag: "quality", pending: true,
		summary: "Accept a finding of the current revision with a reason, so it no longer counts; it stays with the revision and comes forward while the words do. Needs edit.",
		request: quality.AcceptInput{}, responses: with(http.StatusOK, env{"quality": quality.Findings{}})},
	{method: "DELETE", path: "/requirements/{reqKey}/quality/accepted/{findingKey}", handler: "handleWithdrawAcceptance", tag: "quality", pending: true,
		summary:   "Take back the acceptance of a finding of the current revision, so it counts again. Needs edit.",
		responses: with(http.StatusOK, env{"quality": quality.Findings{}})},
	{method: "GET", path: "/projects/{projectKey}/quality", handler: "handleProjectQuality", tag: "quality", pending: true,
		summary:   "The quality of a project's requirements: how many score in each band, the rules they break most, and the requirements lowest first, narrowed like the grid.",
		query:     append(append([]param{}, gridQuery...), windowQuery(quality.DefaultLimit, quality.MaxLimit)...),
		responses: map[int]any{http.StatusOK: env{"quality": quality.Summary{}}, http.StatusBadRequest: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/quality-settings", handler: "handleGetQualitySettings", tag: "quality", pending: true,
		summary:   "Which writing rules the project uses, how much each weighs and for which types, and the words it adds to their lists or excepts.",
		responses: ok(env{"settings": quality.Settings{}})},
	{method: "PUT", path: "/projects/{projectKey}/quality-settings", handler: "handleSaveQualitySettings", tag: "quality", pending: true,
		summary: "Set every rule and word list at once; the project's administrators'. Switching a rule takes effect at once; a changed word list checks the requirements again in the worker, at most " +
			strconv.Itoa(quality.MaxCustomWords) + " words per list.",
		request: quality.SettingsInput{}, responses: with(http.StatusOK, env{"settings": quality.Settings{}})},
	{method: "GET", path: "/quality-rules", handler: "handleListQualityRules", tag: "quality", pending: true,
		summary:   "Every writing rule there is, what it finds, and how it starts out in a project.",
		responses: ok(env{"rules": []quality.RuleInfo{}})},

	// The glossary (#210).
	{method: "GET", path: "/projects/{projectKey}/terms", handler: "handleListTerms", tag: "glossary", pending: true,
		summary: "The terms of the project's glossary, and of the library glossaries it uses that the caller may read, by name.",
		query: append([]param{{name: "q", description: "Words of a term's name, synonyms or definition."},
			{name: "status", repeated: true, schema: &openapi.Schema{Type: "string", Enum: enumStrings(glossary.Statuses)}, description: "Statuses; all when absent."},
			{name: "own", schema: &openapi.Schema{Type: "boolean"}, description: "The project's own terms alone, without its libraries'."}},
			windowQuery(glossary.DefaultLimit, glossary.MaxLimit)...),
		responses: ok(env{"terms": []glossary.Term{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "POST", path: "/projects/{projectKey}/terms", handler: "handleCreateTerm", tag: "glossary", pending: true,
		summary: "Define a term in the project's glossary. Needs edit; at most " + strconv.Itoa(glossary.MaxTerms) + " terms per project.",
		request: glossary.TermInput{}, responses: with(http.StatusCreated, env{"term": glossary.Term{}})},
	{method: "GET", path: "/terms/{termID}", handler: "handleGetTerm", tag: "glossary", pending: true,
		summary:   "One term, its definition, synonyms and the words not to use instead.",
		responses: ok(env{"term": glossary.Term{}})},
	{method: "PATCH", path: "/terms/{termID}", handler: "handleUpdateTerm", tag: "glossary", pending: true,
		summary: "Change a term as a new revision, against the revision it was read at; a changed name or definition makes the requirements that use it suspect. Needs edit on the term's project.",
		request: glossary.TermPatch{}, responses: with(http.StatusOK, env{"term": glossary.Term{}})},
	{method: "DELETE", path: "/terms/{termID}", handler: "handleDeleteTerm", tag: "glossary", pending: true,
		summary:   "Delete a term and its revisions; the project's administrators'. Words linked to it stay as plain words.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusForbidden: errorEnvelope{}, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/terms/{termID}/revisions", handler: "handleListTermRevisions", tag: "glossary", pending: true,
		summary:   "Every revision of a term, newest first.",
		responses: ok(env{"revisions": []glossary.TermRevision{}})},
	{method: "GET", path: "/terms/{termID}/revisions/{revisionNumber}", handler: "handleGetTermRevision", tag: "glossary", pending: true,
		summary:   "One revision of a term.",
		responses: ok(env{"revision": glossary.TermRevision{}})},
	{method: "GET", path: "/terms/{termID}/uses", handler: "handleListTermUses", tag: "glossary", pending: true,
		summary:   "The requirements the caller may read that use a term, by its name, a synonym or an explicit link, and whether its definition changed since their owners confirmed it.",
		query:     windowQuery(glossary.DefaultLimit, glossary.MaxLimit),
		responses: ok(env{"uses": []glossary.Use{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "GET", path: "/requirements/{reqKey}/terms", handler: "handleListRequirementTerms", tag: "glossary", pending: true,
		summary:   "The glossary terms a requirement uses, each saying whether its definition changed since the requirement's owner confirmed it.",
		responses: ok(env{"terms": []glossary.RequirementTerm{}})},
	{method: "POST", path: "/requirements/{reqKey}/terms/{termID}/clear", handler: "handleClearTermUse", tag: "glossary", pending: true,
		summary: "Confirm that the requirement still holds under the term's current definition, with a comment, as a suspect link is cleared. Needs edit.",
		request: glossary.ClearInput{}, responses: with(http.StatusOK, env{"term": glossary.RequirementTerm{}})},
	{method: "GET", path: "/projects/{projectKey}/glossaries", handler: "handleGetGlossaries", tag: "glossary", pending: true,
		summary:   "The library projects whose glossaries mark the project's requirements, first winning a word two share.",
		responses: ok(env{"glossaries": glossary.Glossaries{}})},
	{method: "PUT", path: "/projects/{projectKey}/glossaries", handler: "handleSaveGlossaries", tag: "glossary", pending: true,
		summary: "Name the library projects whose glossaries the project uses, in order; the project's administrators', and only libraries they may read, at most " +
			strconv.Itoa(glossary.MaxLibraries) + ".",
		request: glossary.GlossariesInput{}, responses: with(http.StatusOK, env{"glossaries": glossary.Glossaries{}})},

	// Duplicates (#208).
	{method: "POST", path: "/projects/{projectKey}/similar", handler: "handleSimilarToDraft", tag: "duplicates", pending: true,
		summary: "The requirements of the project, and of library projects the caller may read, most like a draft as it is written, above the project's threshold. Needs edit.",
		request: duplicates.DraftInput{}, responses: with(http.StatusOK, env{"similar": []duplicates.Similar{}, "threshold": 0.0})},
	{method: "GET", path: "/requirements/{reqKey}/similar", handler: "handleListSimilar", tag: "duplicates", pending: true,
		summary:   "The requirements most like this one, of its project and of library projects the caller may read, above the project's threshold, leaving out pairs decided while both sides stay as they were.",
		responses: ok(env{"similar": []duplicates.Similar{}, "threshold": 0.0})},
	{method: "GET", path: "/projects/{projectKey}/duplicates", handler: "handleListDuplicates", tag: "duplicates", pending: true,
		summary: "The project's likely duplicate pairs, most alike first, narrowed by type, section or query on either side; a pair somebody decided about stays away until either side is revised.",
		query: []param{{name: "type", repeated: true, schema: uuidParam, description: "Requirement types either side has."},
			{name: "section", description: "The key of a section either side is below."},
			{name: "query", description: "A query in the query language either side matches."},
			{name: "cursor", description: "Where the page before ended."},
			{name: "limit", schema: intParam, description: "1 to " + strconv.Itoa(duplicates.MaxLimit) + "; " + strconv.Itoa(duplicates.DefaultLimit) + " when absent."}},
		responses: map[int]any{http.StatusOK: env{"pairs": []duplicates.Pair{}, "next": "", "threshold": 0.0}, http.StatusBadRequest: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "POST", path: "/projects/{projectKey}/duplicates/dismiss", handler: "handleDismissDuplicate", tag: "duplicates", pending: true,
		summary: "Say a pair does not say the same thing, with a reason, at the revisions both sides were read at; it comes back once either is revised. Needs edit.",
		request: duplicates.DismissInput{}, responses: with(http.StatusCreated, env{"decision": duplicates.Decision{}})},
	{method: "POST", path: "/projects/{projectKey}/duplicates/merge", handler: "handleMergeDuplicate", tag: "duplicates", pending: true,
		summary: "Keep one side of a pair and put the other in the trash, its links moved to the side kept; restoring it brings its links back. Needs edit on both.",
		request: duplicates.MergeInput{}, responses: with(http.StatusOK, env{"merge": duplicates.Merge{}})},
	{method: "POST", path: "/projects/{projectKey}/duplicates/link", handler: "handleLinkDuplicate", tag: "duplicates", pending: true,
		summary: "Link the two sides of a pair with one of the project's link types, which settles the pair while the link lasts. Needs edit.",
		request: duplicates.LinkInput{}, responses: with(http.StatusCreated, env{"decision": duplicates.Decision{}})},
	{method: "GET", path: "/projects/{projectKey}/duplicate-settings", handler: "handleGetDuplicateSettings", tag: "duplicates", pending: true,
		summary:   "How alike two requirements must be for the project to list them.",
		responses: ok(env{"settings": duplicates.Settings{}})},
	{method: "PUT", path: "/projects/{projectKey}/duplicate-settings", handler: "handleSaveDuplicateSettings", tag: "duplicates", pending: true,
		summary: "Set how alike two requirements must be, from " + strconv.FormatFloat(duplicates.MinThreshold, 'f', 2, 64) + " to " +
			strconv.FormatFloat(duplicates.MaxThreshold, 'f', 2, 64) + "; the project's administrators'.",
		request: duplicates.SettingsInput{}, responses: with(http.StatusOK, env{"settings": duplicates.Settings{}})},

	// Word import (#209).
	{method: "POST", path: "/projects/{projectKey}/word-imports", handler: "handleUploadWord", tag: "reqif", pending: true,
		summary:   "Upload a Word document, sent as a multipart part named file, to import into the project or below a section, and see the tree it would make. Nothing is written yet; it continues as a ReqIF import does. For the project's administrators.",
		multipart: true, request: reqifio.WordForm{},
		responses: map[int]any{http.StatusCreated: env{"import": reqifio.Import{}}, http.StatusForbidden: errorEnvelope{}, http.StatusConflict: errorEnvelope{},
			http.StatusRequestEntityTooLarge: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
}
