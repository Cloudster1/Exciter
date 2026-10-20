package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/attachment"
	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/codetrace"
	"github.com/cloudster1/exciter/backend/internal/comment"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/dashboard"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/demo"
	"github.com/cloudster1/exciter/backend/internal/diagram"
	"github.com/cloudster1/exciter/backend/internal/duplicates"
	"github.com/cloudster1/exciter/backend/internal/exporttemplate"
	"github.com/cloudster1/exciter/backend/internal/glossary"
	"github.com/cloudster1/exciter/backend/internal/installation"
	"github.com/cloudster1/exciter/backend/internal/label"
	"github.com/cloudster1/exciter/backend/internal/notify"
	"github.com/cloudster1/exciter/backend/internal/oauth"
	"github.com/cloudster1/exciter/backend/internal/observability"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/perm"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/quality"
	"github.com/cloudster1/exciter/backend/internal/render"
	"github.com/cloudster1/exciter/backend/internal/reqifio"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/review"
	"github.com/cloudster1/exciter/backend/internal/risk"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/specdoc"
	"github.com/cloudster1/exciter/backend/internal/stator"
	"github.com/cloudster1/exciter/backend/internal/testexec"
	"github.com/cloudster1/exciter/backend/internal/testresult"
	"github.com/cloudster1/exciter/backend/internal/theme"
	"github.com/cloudster1/exciter/backend/internal/trace"
	"github.com/cloudster1/exciter/backend/internal/watch"
	"github.com/cloudster1/exciter/backend/internal/webhook"
	"github.com/cloudster1/exciter/backend/internal/workflow"
)

// readinessTimeout bounds the database round trip behind /readyz, so a probe
// gets an answer before its own deadline does.
const readinessTimeout = 3 * time.Second

// APIPrefix is where every endpoint but the probes lives.
const APIPrefix = "/api/v1"

// Database is what the handlers ask of the cluster; *db.Cluster is the real one.
type Database interface {
	Ping(ctx context.Context) error
	Stats() db.Stats
}

// Server holds everything the handlers need.
type Server struct {
	DB   Database
	Auth Authenticator
	// Accounts signs people in and out and keeps the installation's members;
	// nil answers every sign-in route that it is not set up.
	Accounts *auth.Service
	// OIDC runs sign-in through the installation's identity provider; nil answers its routes that it is not set
	// up. OIDCCallbackURL is the address to register with a provider.
	OIDC            *oidc.Service
	OIDCCallbackURL string
	// Audit reads the record for administrators and keeps exports; nil refuses
	// both. AuditRetention is how long the worker keeps an entry, zero forever.
	Audit          *audit.Service
	AuditRetention time.Duration
	// Installation keeps the installation's name; nil answers its routes that it is not set up.
	Installation *installation.Service
	// Demo adds the suite demo (#270); nil answers its routes that it is not set up.
	Demo *demo.Service
	// Themes keeps custom themes and their files; nil answers their routes that they are not set up.
	Themes *theme.Service
	// Projects keeps the projects requirements live in; nil answers their routes that they are not set up.
	Projects *project.Service
	// Perms keeps who may do what in a project, and names people and groups to pick; nil answers its routes
	// that it is not set up.
	Perms *perm.Service
	// Requirements keeps requirements, their revisions and trash, and Search lists them; nil answers their
	// routes that they are not set up.
	Requirements *requirement.Service
	Search       *search.Service
	// Attributes keeps a project's requirement types and attributes; nil answers their routes that they are not set up.
	Attributes *attribute.Service
	// Workflows keeps a project's statuses and workflows; nil answers their routes that they are not set up.
	Workflows *workflow.Service
	// Labels finds labels and what carries them; nil answers the label routes that they are not set up.
	Labels *label.Service
	// Trace keeps link types and the links between requirements; nil answers their routes that they are not set up.
	Trace *trace.Service
	// Armature connects the installation to Armature and holds each member's token; nil answers its routes that
	// it is not set up.
	Armature *armature.Service
	// Stator connects the installation to Stator and holds each member's token; nil answers its routes that it
	// is not set up.
	Stator *stator.Service
	// Webhooks keeps the installation's outbound webhooks (#48); nil answers their routes that they are not set up.
	Webhooks *webhook.Service
	// Comments keeps the discussion on requirements (#33); nil answers its routes that it is not set up.
	Comments *comment.Service
	// Watches and Notifications keep who follows what and what they were told (#34); nil answers their routes
	// that they are not set up.
	Watches       *watch.Service
	Notifications *notify.Service
	// Reviews keeps approval rules and the reviews of revisions (#35); nil answers their routes that they are not
	// set up.
	Reviews *review.Service
	// Baselines takes and compares snapshots of projects (#37); nil answers their routes that they are not set up.
	Baselines *baseline.Service
	// Imports moves requirements in and out of CSV files; nil answers their routes that they are not set up.
	Imports *csvio.Service
	// TestResults keeps the test results CI uploads (#200); nil answers their routes that they are not set up.
	TestResults *testresult.Service
	// Code keeps the tags code scans found (#201); nil answers their routes that they are not set up.
	Code *codetrace.Service
	// Quality checks requirements against writing rules (#207).
	Quality *quality.Service
	// Glossary keeps projects' terms and the requirements that use them (#210).
	Glossary *glossary.Service
	// Duplicates finds requirements that say the same thing (#208).
	Duplicates *duplicates.Service
	// Dashboards are widgets over a project's requirements, read as the reader (#212).
	Dashboards *dashboard.Service
	// ReqIF exchanges requirements with other tools (#44); nil answers its routes that it is not set up.
	ReqIF *reqifio.Service
	// OAuth is the authorization server of the MCP endpoint (#188); nil answers its routes that it is not set up.
	OAuth *oauth.Service
	// Documents reads specifications and baselines as documents to sign and send (#45).
	Documents *specdoc.Service
	// ExportTemplates are the Word templates exports go through (#214); nil answers their routes that they are not set up.
	ExportTemplates *exporttemplate.Service
	// Tests keeps test plans and executions (#251); nil answers their routes that they are not set up.
	Tests *testexec.Service
	// Risks keeps the projects' risk schemes (#259).
	Risks *risk.Service
	// Renderer prints a document's print view as PDF; nil or render.Unavailable says PDF export is not set up.
	Renderer render.Renderer
	// Diagrams draws diagrams from their source (#284); nil or diagram.Unavailable shows them as their source.
	Diagrams diagram.Drawer
	// handler is the router Routes built, which MCP sends each tool call through.
	handler http.Handler
	// Attachments keeps the files on requirements; nil answers their routes that file storage is not set up.
	Attachments *attachment.Service
	Log         *slog.Logger
	// Telemetry counts and traces requests; nil serves without either.
	Telemetry *observability.Telemetry
	// Fresh remembers each caller's last write between requests; nil leaves
	// reads unpinned, which is only right without replicas.
	Fresh Freshness

	// CookieName is the session cookie a sign-in sets and the same-site check guards.
	CookieName string
	Secure     bool
	// AppBaseURL is the web client's origin, which the same-site check trusts.
	AppBaseURL string
	// CheckOrigin also refuses a cookie-carried write from a foreign origin;
	// off in development, where the dev server proxies from an origin of its own.
	CheckOrigin bool
	// RequestTimeout bounds each handler; zero means the configured default.
	RequestTimeout time.Duration
	// TrustedProxies are the peers whose X-Real-IP names the client, such as the web container's nginx.
	TrustedProxies []netip.Prefix
}

// Routes builds the HTTP surface. The order matters: an id first so every later
// line can quote it, recovery inside the logger so a panic still logs a line.
func (s *Server) Routes(allowedOrigins []string) http.Handler {
	timeout := s.RequestTimeout
	if timeout <= 0 {
		timeout = config.DefaultRequestTimeout
	}

	r := chi.NewRouter()
	r.Use(requestID)
	if s.Telemetry != nil {
		r.Use(observe(s.Telemetry.Metrics))
	}
	r.Use(logging(s.Log))
	r.Use(recovery)
	r.Use(securityHeaders)
	r.Use(cors(allowedOrigins))
	r.Use(s.sameSite(allowedOrigins))
	r.Use(middleware.Timeout(timeout))
	r.Use(s.authenticate)
	r.Use(readOnlyToken)
	r.Use(s.readYourWrites)

	// Liveness and readiness are deliberately outside the API and authentication.
	r.Get("/healthz", s.handleLiveness)
	r.Get("/readyz", s.handleReadiness)
	s.mountOAuthDiscovery(r)
	mountPending(r, true, true)

	r.Route(APIPrefix, func(r chi.Router) {
		r.Use(wholeOrg(r), evidenceToken(r))
		r.Get("/openapi.json", s.handleOpenAPI)
		r.Get("/meta", s.handleMeta)
		r.Post("/auth/login", s.handleLogin)
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)
		r.Get("/auth/oidc/reauthenticate", s.handleReauthStart)
		r.With(s.armatureOn).Post("/armature/webhook", s.handleArmatureWebhook)
		r.With(s.statorOn).Post("/stator/webhook", s.handleStatorWebhook)
		s.mountOAuth(r)
		mountPending(r, true, false)

		r.Group(func(r chi.Router) {
			r.Use(requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Patch("/auth/me", s.handleUpdateMe)
		})

		r.Group(func(r chi.Router) {
			r.Use(requireAdmin)
			r.Get("/users", s.handleListMembers)
			r.Post("/users", s.handleCreateMember)
			r.Patch("/users/{userID}", s.handleUpdateMember)
			r.Delete("/users/{userID}", s.handleSwitchOffMember)
			r.Get("/users/requests", s.handleListJoinRequests)
			r.Post("/users/requests/{userID}/admit", s.handleAdmitJoinRequest)
			r.Delete("/users/requests/{userID}", s.handleDeclineJoinRequest)
			r.Get("/oidc-provider", s.handleGetOIDCProvider)
			r.Put("/oidc-provider", s.handleSaveOIDCProvider)
			r.Get("/oidc-provider/group-roles", s.handleListGroupRoles)
			r.Post("/oidc-provider/group-roles", s.handleSetGroupRole)
			r.Delete("/oidc-provider/group-roles/{groupRoleID}", s.handleRemoveGroupRole)
			r.Get("/audit", s.handleListAudit)
			r.Get("/audit/facets", s.handleAuditFacets)
			r.Get("/audit/export", s.handleExportAudit)
		})

		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.installationOn)
			r.Get("/installation", s.handleGetInstallation)
			r.With(requireAdmin).Put("/installation", s.handleRenameInstallation)
		})
		s.mountSuiteDemo(r)

		// Themes are a person's; the examples are anybody's who is signed in.
		r.With(requireAuth).Get("/themes/examples", s.handleThemeExamples)
		r.With(requireAuth).Post("/diagrams", s.handleDrawDiagram)
		r.Group(func(r chi.Router) {
			r.Use(requireAuth)
			r.With(requireAdmin, s.themesOn).Put("/themes/default", s.handleSetDefaultTheme)
			r.Group(func(r chi.Router) {
				r.Use(s.themesOn)
				r.Get("/themes", s.handleListThemes)
				r.Post("/themes", s.handleCreateTheme)
				r.Get("/themes/active", s.handleActiveTheme)
				r.Put("/themes/active", s.handleChooseTheme)
				r.Get("/themes/default", s.handleDefaultTheme)
				r.Post("/themes/import", s.handleImportTheme)
				r.Get("/themes/{themeID}/export", s.handleExportTheme)
				r.Get("/themes/{themeID}", s.handleGetTheme)
				r.Patch("/themes/{themeID}", s.handleUpdateTheme)
				r.Delete("/themes/{themeID}", s.handleDeleteTheme)
				r.Post("/themes/{themeID}/assets", s.handleUploadThemeAsset)
				r.Get("/themes/{themeID}/assets/{assetID}", s.handleThemeAsset)
				r.Delete("/themes/{themeID}/assets/{assetID}", s.handleDeleteThemeAsset)
			})
		})

		// Projects: every member reads those they may view; the installation's administrators make them.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.projectsOn)
			r.Get("/projects", s.handleListProjects)
			r.Get("/project-templates", s.handleListProjectTemplates)
			r.With(requireAdmin).Post("/projects", s.handleCreateProject)
			r.Get("/projects/key-check", s.handleCheckProjectKey)
			r.Get("/projects/{projectKey}", s.handleGetProject)
			r.Patch("/projects/{projectKey}", s.handleUpdateProject)
			r.Delete("/projects/{projectKey}", s.handleDeleteProject)
			r.Put("/projects/{projectKey}/key", s.handleChangeProjectKey)
			r.Put("/projects/{projectKey}/archive", s.handleArchiveProject)
			r.Delete("/projects/{projectKey}/archive", s.handleUnarchiveProject)
			r.With(s.permsOn).Get("/projects/{projectKey}/grants", s.handleListProjectGrants)
			r.With(s.permsOn).Put("/projects/{projectKey}/grants", s.handleSetProjectGrants)
			r.With(s.permsOn).Get("/projects/{projectKey}/access/{userID}", s.handleInspectProjectAccess)
		})

		// Requirements: read by whoever views their project, written by its editors, purged by its administrators.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.requirementsOn)
			r.Post("/projects/{projectKey}/requirements", s.handleCreateRequirement)
			r.Get("/projects/{projectKey}/requirements", s.handleListGrid)
			r.Get("/projects/{projectKey}/requirements/suggest", s.handleSuggestQuery)
			r.Get("/requirements/by-id/{requirementID}", s.handleGetRequirementByID)
			r.Get("/requirements/{reqKey}", s.handleGetRequirement)
			r.Patch("/requirements/{reqKey}", s.handleUpdateRequirement)
			r.Delete("/requirements/{reqKey}", s.handleTrashRequirement)
			r.Get("/projects/{projectKey}/trash", s.handleListRequirementTrash)
			r.Delete("/projects/{projectKey}/trash", s.handleEmptyRequirementTrash)
			r.Post("/projects/{projectKey}/trash/{requirementID}/restore", s.handleRestoreRequirement)
			r.Delete("/projects/{projectKey}/trash/{requirementID}", s.handlePurgeRequirement)
			r.Get("/projects/{projectKey}/tree", s.handleGetTree)
			r.Get("/projects/{projectKey}/spec", s.handleGetSpecification)
			r.Post("/requirements/{reqKey}/move", s.handleMoveRequirement)
			r.Post("/requirements/{reqKey}/lock", s.handleLockRequirement)
			r.Delete("/requirements/{reqKey}/lock", s.handleUnlockRequirement)
			r.Post("/requirements/{reqKey}/copy", s.handleCopyRequirement)
			r.Post("/requirements/{reqKey}/reuse", s.handleReuseRequirement)
			r.Post("/requirements/{reqKey}/reuse/take", s.handleTakeSource)
			r.Post("/requirements/{reqKey}/reuse/variant", s.handleMakeVariant)
			r.Get("/requirements/{reqKey}/revisions", s.handleListRevisions)
			r.Get("/requirements/{reqKey}/revisions/{revisionNumber}", s.handleGetRevision)
			r.Post("/requirements/{reqKey}/revisions/{revisionNumber}/restore", s.handleRestoreRevision)
			r.Get("/requirements/{reqKey}/diff", s.handleDiffRevisions)
			r.Get("/requirements/{reqKey}/activity", s.handleListActivity)
			r.Get("/search", s.handleSearch)
			r.Get("/search/quick", s.handleQuickSearch)
			r.Get("/projects/{projectKey}/views", s.handleListViews)
			r.Post("/projects/{projectKey}/views", s.handleCreateView)
			r.Patch("/views/{viewID}", s.handleUpdateView)
			r.Delete("/views/{viewID}", s.handleDeleteView)
		})

		// Types and attributes: read by whoever views their project, changed by its administrators.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.attributesOn)
			r.Get("/attribute-kinds", s.handleListAttributeKinds)
			r.Get("/projects/{projectKey}/types", s.handleListRequirementTypes)
			r.Post("/projects/{projectKey}/types", s.handleCreateRequirementType)
			r.Patch("/projects/{projectKey}/types/{typeID}", s.handleUpdateRequirementType)
			r.Delete("/projects/{projectKey}/types/{typeID}", s.handleDeleteRequirementType)
			r.Get("/projects/{projectKey}/attributes", s.handleListAttributes)
			r.Post("/projects/{projectKey}/attributes", s.handleCreateAttribute)
			r.Patch("/projects/{projectKey}/attributes/{attributeID}", s.handleUpdateAttribute)
			r.Delete("/projects/{projectKey}/attributes/{attributeID}", s.handleDeleteAttribute)
			r.Post("/projects/{projectKey}/formula-check", s.handleFormulaCheck)
			r.Get("/projects/{projectKey}/field-rules", s.handleListFieldRules)
			r.Put("/projects/{projectKey}/field-rules/{field}", s.handleSaveFieldRule)
		})

		// Statuses and workflows: read by whoever views their project, shaped by its administrators, taken by its editors.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.workflowsOn)
			r.Get("/workflow-rule-types", s.handleListRuleTypes)
			r.Get("/projects/{projectKey}/statuses", s.handleListStatuses)
			r.Post("/projects/{projectKey}/statuses", s.handleCreateStatus)
			r.Patch("/projects/{projectKey}/statuses/{statusID}", s.handleUpdateStatus)
			r.Delete("/projects/{projectKey}/statuses/{statusID}", s.handleDeleteStatus)
			r.Get("/projects/{projectKey}/workflows", s.handleListWorkflows)
			r.Post("/projects/{projectKey}/workflows", s.handleCreateWorkflow)
			r.Put("/projects/{projectKey}/workflow", s.handleAssignWorkflow)
			r.Get("/projects/{projectKey}/workflows/{workflowID}", s.handleGetWorkflow)
			r.Put("/projects/{projectKey}/workflows/{workflowID}", s.handleSaveWorkflow)
			r.Delete("/projects/{projectKey}/workflows/{workflowID}", s.handleDeleteWorkflow)
			r.Get("/requirements/{reqKey}/transitions", s.handleListTransitions)
			r.Post("/requirements/{reqKey}/transitions", s.handleTransitionRequirement)
			r.Get("/projects/{projectKey}/transitions", s.handleListBulkTransitions)
			r.Post("/projects/{projectKey}/transitions", s.handleBulkTransition)
		})

		// Labels: seen with their requirements, put on and taken off by who edits them.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.labelsOn)
			r.Get("/requirements/{reqKey}/labels", s.handleListRequirementLabels)
			r.Post("/requirements/{reqKey}/labels", s.handleAddRequirementLabel)
			r.Delete("/requirements/{reqKey}/labels/{labelName}", s.handleRemoveRequirementLabel)
			r.Get("/labels", s.handleListLabels)
			r.Get("/labels/{labelName}/requirements", s.handleListLabelledRequirements)
		})

		// Trace links: read by whoever views both ends, made by whoever edits one; link types shaped by the
		// installation's administrators, or a project's for its own.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.traceOn)
			r.Get("/link-types", s.handleListOrgLinkTypes)
			r.With(requireAdmin).Post("/link-types", s.handleCreateOrgLinkType)
			r.Patch("/link-types/{linkTypeID}", s.handleUpdateLinkType)
			r.Delete("/link-types/{linkTypeID}", s.handleDeleteLinkType)
			r.Get("/projects/{projectKey}/link-types", s.handleListProjectLinkTypes)
			r.Post("/projects/{projectKey}/link-types", s.handleCreateProjectLinkType)
			r.Put("/projects/{projectKey}/link-types/{linkTypeID}", s.handleSetProjectLinkType)
			r.Get("/requirements/{reqKey}/links", s.handleListLinks)
			r.Post("/requirements/{reqKey}/links", s.handleCreateLink)
			r.Delete("/links/{linkID}", s.handleDeleteLink)
			r.Get("/projects/{projectKey}/suspects", s.handleListSuspectLinks)
			r.Post("/links/{linkID}/clear", s.handleClearLink)
			r.Get("/links/{linkID}/clearances", s.handleListClearances)
			// Coverage and the matrix (#38): read by a project's readers, its rules set by its administrators.
			r.Get("/projects/{projectKey}/coverage-rules", s.handleListCoverageRules)
			r.Put("/projects/{projectKey}/coverage-rules", s.handleSaveCoverageRule)
			r.Delete("/coverage-rules/{ruleID}", s.handleDeleteCoverageRule)
			// A project's risk scheme (#259): read by its readers, set by its administrators.
			r.Get("/projects/{projectKey}/risk-scheme", s.handleGetRiskScheme)
			r.Put("/projects/{projectKey}/risk-scheme", s.handleSaveRiskScheme)
			// Mitigations, the FMEA table and the risk matrix (#260), read by the project's readers.
			r.Get("/requirements/{reqKey}/mitigations", s.handleGetRiskMitigations)
			r.Get("/projects/{projectKey}/risks", s.handleListRisks)
			r.Get("/projects/{projectKey}/risks.xlsx", s.handleRisksXLSX)
			r.Get("/projects/{projectKey}/risk-matrix", s.handleRiskMatrix)
			// The risk report (#261), as JSON for its print view, Word and PDF.
			r.Get("/projects/{projectKey}/risk-report", s.handleGetRiskReport)
			r.Get("/projects/{projectKey}/risk-report.docx", s.handleRiskReportDocx)
			r.Get("/projects/{projectKey}/risk-report.pdf", s.handleRiskReportPDF)
			r.Get("/projects/{projectKey}/coverage", s.handleProjectCoverage)
			r.Get("/projects/{projectKey}/matrix", s.handleTraceMatrix)
			r.Get("/projects/{projectKey}/matrix.csv", s.handleTraceMatrixCSV)
			r.Get("/projects/{projectKey}/matrix.xlsx", s.handleTraceMatrixXLSX)
			// The trace model and its chains (#151): read by a project's readers, set by its administrators.
			r.Get("/projects/{projectKey}/trace-model", s.handleGetTraceModel)
			r.Put("/projects/{projectKey}/trace-model", s.handleSaveTraceModel)
			r.Get("/projects/{projectKey}/trace-chains", s.handleTraceChains)
			r.Get("/projects/{projectKey}/trace-chains.csv", s.handleTraceChainsCSV)
			r.Get("/projects/{projectKey}/trace-chains.xlsx", s.handleTraceChainsXLSX)
			// A requirement's graph (#211): read by whoever views it, as far as they may see.
			r.Get("/requirements/{reqKey}/graph", s.handleTraceGraph)
			r.Get("/requirements/{reqKey}/graph.csv", s.handleTraceGraphCSV)
		})

		// Test results from CI and the trace check (#200): uploaded by a project's editors, read by its readers.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.evidenceOn)
			r.Post("/projects/{projectKey}/test-runs", s.handleUploadTestRun)
			r.Get("/test-runs/{runID}", s.handleGetTestRun)
			r.Get("/requirements/{reqKey}/test-results", s.handleListTestResults)
			r.Get("/projects/{projectKey}/evidence-settings", s.handleGetEvidenceSettings)
			r.Put("/projects/{projectKey}/evidence-settings", s.handleSaveEvidenceSettings)
			r.Get("/projects/{projectKey}/trace-check", s.handleTraceCheck)
		})

		// Code tags (#201): scans uploaded by a project's editors, read by its readers, words set by its administrators.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.codeOn)
			r.Post("/projects/{projectKey}/code-scans", s.handleUploadCodeScan)
			r.Get("/code-scans/{scanID}", s.handleGetCodeScan)
			r.Get("/requirements/{reqKey}/code-links", s.handleListCodeLinks)
			r.Get("/projects/{projectKey}/code-tags", s.handleListCodeTags)
			r.Get("/projects/{projectKey}/code-tag-words", s.handleGetCodeTagWords)
			r.Put("/projects/{projectKey}/code-tag-words", s.handleSaveCodeTagWords)
		})

		// Writing rules (#207): drafts checked and findings accepted by a project's editors, rules set by its administrators.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.qualityOn)
			r.Post("/projects/{projectKey}/quality-check", s.handleQualityCheck)
			r.Get("/requirements/{reqKey}/quality", s.handleGetQuality)
			r.Post("/requirements/{reqKey}/quality/accepted", s.handleAcceptFinding)
			r.Delete("/requirements/{reqKey}/quality/accepted/{findingKey}", s.handleWithdrawAcceptance)
			r.Get("/projects/{projectKey}/quality", s.handleProjectQuality)
			r.Get("/projects/{projectKey}/quality-settings", s.handleGetQualitySettings)
			r.Put("/projects/{projectKey}/quality-settings", s.handleSaveQualitySettings)
			r.Get("/quality-rules", s.handleListQualityRules)
		})

		// The glossary (#210): terms defined by a project's editors, deleted and their glossaries chosen by its administrators.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.glossaryOn)
			r.Get("/projects/{projectKey}/terms", s.handleListTerms)
			r.Post("/projects/{projectKey}/terms", s.handleCreateTerm)
			r.Get("/terms/{termID}", s.handleGetTerm)
			r.Patch("/terms/{termID}", s.handleUpdateTerm)
			r.Delete("/terms/{termID}", s.handleDeleteTerm)
			r.Get("/terms/{termID}/revisions", s.handleListTermRevisions)
			r.Get("/terms/{termID}/revisions/{revisionNumber}", s.handleGetTermRevision)
			r.Get("/terms/{termID}/uses", s.handleListTermUses)
			r.Get("/requirements/{reqKey}/terms", s.handleListRequirementTerms)
			r.Post("/requirements/{reqKey}/terms/{termID}/clear", s.handleClearTermUse)
			r.Get("/projects/{projectKey}/glossaries", s.handleGetGlossaries)
			r.Put("/projects/{projectKey}/glossaries", s.handleSaveGlossaries)
		})

		// Duplicates (#208): pairs read by a project's readers, decided by its editors, the threshold its administrators'.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.duplicatesOn)
			r.Post("/projects/{projectKey}/similar", s.handleSimilarToDraft)
			r.Get("/requirements/{reqKey}/similar", s.handleListSimilar)
			r.Get("/projects/{projectKey}/duplicates", s.handleListDuplicates)
			r.Post("/projects/{projectKey}/duplicates/dismiss", s.handleDismissDuplicate)
			r.Post("/projects/{projectKey}/duplicates/merge", s.handleMergeDuplicate)
			r.Post("/projects/{projectKey}/duplicates/link", s.handleLinkDuplicate)
			r.Get("/projects/{projectKey}/duplicate-settings", s.handleGetDuplicateSettings)
			r.Put("/projects/{projectKey}/duplicate-settings", s.handleSaveDuplicateSettings)
		})

		// Dashboards (#212): a person's own, or shared with whoever reads the project; widgets read as the reader.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.dashboardsOn)
			r.Get("/projects/{projectKey}/dashboards", s.handleListDashboards)
			r.Post("/projects/{projectKey}/dashboards", s.handleCreateDashboard)
			r.Get("/dashboards/{dashboardID}", s.handleGetDashboard)
			r.Patch("/dashboards/{dashboardID}", s.handleUpdateDashboard)
			r.Delete("/dashboards/{dashboardID}", s.handleDeleteDashboard)
			r.Post("/dashboards/{dashboardID}/widgets", s.handleAddWidget)
			r.Put("/dashboards/{dashboardID}/order", s.handleOrderWidgets)
			r.Get("/dashboards/{dashboardID}/dashboard.pdf", s.handleDashboardPDF)
			r.Patch("/widgets/{widgetID}", s.handleUpdateWidget)
			r.Delete("/widgets/{widgetID}", s.handleDeleteWidget)
			r.Get("/widgets/{widgetID}/data", s.handleWidgetData)
			r.Post("/projects/{projectKey}/widget-preview", s.handlePreviewWidget)
			r.Get("/dashboard-templates", s.handleListDashboardTemplates)
		})

		// Attachments: seen with their requirements, uploaded and deleted by who edits them.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.attachmentsOn)
			r.Get("/requirements/{reqKey}/attachments", s.handleListAttachments)
			r.Post("/requirements/{reqKey}/attachments", s.handleUploadAttachment)
			r.Get("/attachments/{attachmentID}", s.handleDownloadAttachment)
			r.Delete("/attachments/{attachmentID}", s.handleDeleteAttachment)
		})

		// Personal access tokens: a person's own, made only from a session, and everybody's for the
		// administrators. The assistants' endpoint runs each tool as the HTTP call it stands for.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.accountsOn)
			r.Get("/tokens", s.handleListAPITokens)
			r.With(requireSession).Post("/tokens", s.handleCreateAPIToken)
			r.Delete("/tokens/{tokenID}", s.handleRevokeAPIToken)
			r.With(requireAdmin).Get("/tokens/everyone", s.handleListEveryonesAPITokens)
			r.With(requireAdmin).Delete("/tokens/everyone/{tokenID}", s.handleRevokeAnybodysAPIToken)
		})
		r.With(s.requireMCPAuth).Post("/mcp", s.handleMCP)

		// Comments: read by whoever views the requirement, written by whoever may comment on it (#33).
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.commentsOn)
			r.Get("/requirements/{reqKey}/comments", s.handleListComments)
			r.Post("/requirements/{reqKey}/comments", s.handleCreateComment)
			r.Get("/requirements/{reqKey}/mentionable", s.handleListMentionable)
			r.Get("/comments/{commentID}", s.handleGetCommentThread)
			r.Post("/comments/{commentID}/replies", s.handleReplyToComment)
			r.Patch("/comments/{commentID}", s.handleEditComment)
			r.Delete("/comments/{commentID}", s.handleDeleteComment)
			r.Put("/comment-threads/{threadID}/resolution", s.handleResolveThread)
			r.Delete("/comment-threads/{threadID}/resolution", s.handleReopenThread)
		})

		// Watching and notifications: each person's own, about what they may view (#34).
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.notificationsOn)
			r.Put("/requirements/{reqKey}/watch", s.handleWatchRequirement)
			r.Delete("/requirements/{reqKey}/watch", s.handleUnwatchRequirement)
			r.Get("/requirements/{reqKey}/watchers", s.handleListWatchers)
			r.Put("/projects/{projectKey}/watch", s.handleWatchProject)
			r.Delete("/projects/{projectKey}/watch", s.handleUnwatchProject)
			r.Get("/watches", s.handleListWatches)
			r.Get("/notifications", s.handleListNotifications)
			r.Get("/notifications/unread-count", s.handleCountUnreadNotifications)
			r.Post("/notifications/read", s.handleMarkNotificationsRead)
			r.Get("/notification-preferences", s.handleGetNotificationPreferences)
			r.Put("/notification-preferences", s.handleSaveNotificationPreferences)
		})

		// Reviews: rules set by a project's administrators, reviews asked by its editors and decided by the
		// approvers each review froze (#35).
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.reviewsOn)
			r.Get("/projects/{projectKey}/approval-rules", s.handleListApprovalRules)
			r.Put("/projects/{projectKey}/approval-rules", s.handleSaveApprovalRule)
			r.Delete("/approval-rules/{ruleID}", s.handleDeleteApprovalRule)
			r.Get("/requirements/{reqKey}/approval", s.handleGetApproval)
			r.Get("/requirements/{reqKey}/reviews", s.handleListReviews)
			r.Post("/requirements/{reqKey}/reviews", s.handleRequestReview)
			r.Post("/projects/{projectKey}/reviews", s.handleBulkRequestReview)
			r.Get("/reviews/waiting", s.handleListWaitingReviews)
			r.Get("/reviews/sessions", s.handleListMyReviewSessions)
			r.Get("/projects/{projectKey}/review-sessions", s.handleListReviewSessions)
			r.Post("/projects/{projectKey}/review-sessions", s.handleOpenReviewSession)
			r.Get("/review-sessions/{sessionID}", s.handleGetReviewSession)
			r.Patch("/review-sessions/{sessionID}", s.handleUpdateReviewSession)
			r.Post("/review-sessions/{sessionID}/rounds", s.handleStartReviewRound)
			r.Post("/review-sessions/{sessionID}/close", s.handleCloseReviewSession)
			r.Get("/review-sessions/{sessionID}/report", s.handleGetReviewSessionReport)
			r.Get("/review-sessions/{sessionID}/report.docx", s.handleReviewSessionReportDocx)
			r.Get("/review-sessions/{sessionID}/report.pdf", s.handleReviewSessionReportPDF)
			r.Get("/reviews/{reviewID}", s.handleGetReview)
			r.Post("/reviews/{reviewID}/decisions", s.handleDecideReview)
			r.Post("/reviews/{reviewID}/withdraw", s.handleWithdrawReview)
		})

		// Baselines: read by a project's readers, taken, renamed, locked and deleted by its administrators (#37).
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.baselinesOn)
			r.Get("/projects/{projectKey}/baselines", s.handleListBaselines)
			r.Post("/projects/{projectKey}/baselines", s.handleTakeBaseline)
			r.Get("/baselines/{baselineID}", s.handleGetBaseline)
			r.Patch("/baselines/{baselineID}", s.handleUpdateBaseline)
			r.Delete("/baselines/{baselineID}", s.handleDeleteBaseline)
			r.Get("/baselines/{baselineID}/spec", s.handleBaselineSpec)
			r.Get("/baselines/{baselineID}/compare", s.handleCompareBaseline)
			r.Get("/projects/{projectKey}/baseline-signing", s.handleGetBaselineSigning)
			r.Put("/projects/{projectKey}/baseline-signing", s.handleSaveBaselineSigning)
			r.Delete("/projects/{projectKey}/baseline-signing", s.handleDeleteBaselineSigning)
			r.Get("/baselines/{baselineID}/signatures", s.handleListBaselineSignatures)
			r.Post("/baselines/{baselineID}/signatures", s.handleSignBaseline)
			r.Get("/baselines/{baselineID}/matrix", s.handleBaselineMatrix)
			r.Get("/baselines/{baselineID}/matrix.csv", s.handleBaselineMatrixCSV)
			r.Get("/baselines/{baselineID}/matrix.xlsx", s.handleBaselineMatrixXLSX)
		})

		// Outbound webhooks (#48): the installation's administrators', a token held to some projects refused by wholeOrg.
		r.Group(func(r chi.Router) {
			r.Use(requireAdmin)
			r.Get("/webhooks", s.handleListWebhooks)
			r.Post("/webhooks", s.handleCreateWebhook)
			r.Patch("/webhooks/{webhookID}", s.handleUpdateWebhook)
			r.Delete("/webhooks/{webhookID}", s.handleDeleteWebhook)
			r.Post("/webhooks/{webhookID}/rotate-secret", s.handleRotateWebhookSecret)
			r.Post("/webhooks/{webhookID}/test", s.handleTestWebhook)
			r.Get("/webhooks/{webhookID}/deliveries", s.handleListWebhookDeliveries)
			r.Post("/webhooks/{webhookID}/deliveries/{deliveryID}/redeliver", s.handleRedeliverWebhook)
		})

		// Armature: the installation's connection for its administrators, each member's own token for them, and the
		// issues requirements are traced to, read as the caller.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.armatureOn)
			r.With(requireAdmin).Get("/armature/connection", s.handleGetArmatureConnection)
			r.With(requireAdmin).Put("/armature/connection", s.handleSaveArmatureConnection)
			r.With(requireAdmin).Delete("/armature/connection", s.handleRemoveArmatureConnection)
			r.Get("/armature/account", s.handleGetArmatureAccount)
			r.With(requireSession).Put("/armature/account/token", s.handleStoreArmatureToken)
			r.Post("/armature/account/check", s.handleCheckArmatureAccount)
			r.With(requireSession).Delete("/armature/account/token", s.handleRemoveArmatureToken)
			r.Get("/armature/issues", s.handleLookupArmatureIssues)
			r.Get("/armature/issues/{issueKey}", s.handleGetArmatureIssue)
			r.Get("/armature/search", s.handleSearchArmatureIssues)
			r.Get("/requirements/{reqKey}/armature-links", s.handleListArmatureLinks)
			r.Post("/requirements/{reqKey}/armature-links", s.handleCreateArmatureLink)
			r.Delete("/armature-links/{linkID}", s.handleDeleteArmatureLink)
			r.Post("/armature-links/{linkID}/clear", s.handleClearArmatureLink)
			r.Get("/armature/projects", s.handleListArmatureProjects)
			r.Get("/armature/issue-types", s.handleListArmatureIssueTypes)
			r.Post("/projects/{projectKey}/armature-issues", s.handleCreateArmatureIssues)
			r.With(s.themesOn).Get("/armature/theme", s.handleGetArmatureTheme)
			r.With(s.themesOn).Put("/armature/theme", s.handleFollowArmatureTheme)
			r.With(s.themesOn).Delete("/armature/theme", s.handleUnfollowArmatureTheme)
		})

		// Stator: the installation's connection for its administrators, each member's own token for them (#40), and
		// the pages requirements are traced to, read as the caller (#41).
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.statorOn)
			r.With(requireAdmin).Get("/stator/connection", s.handleGetStatorConnection)
			r.With(requireAdmin).Put("/stator/connection", s.handleSaveStatorConnection)
			r.With(requireAdmin).Delete("/stator/connection", s.handleRemoveStatorConnection)
			r.Get("/stator/account", s.handleGetStatorAccount)
			r.With(requireSession).Put("/stator/account/token", s.handleStoreStatorToken)
			r.Post("/stator/account/check", s.handleCheckStatorAccount)
			r.With(requireSession).Delete("/stator/account/token", s.handleRemoveStatorToken)
			r.Get("/stator/pages", s.handleLookupStatorPages)
			r.Get("/stator/search", s.handleSearchStatorPages)
			r.Get("/requirements/{reqKey}/stator-links", s.handleListStatorLinks)
			r.Post("/requirements/{reqKey}/stator-links", s.handleCreateStatorLink)
			r.Delete("/stator-links/{linkID}", s.handleDeleteStatorLink)
			r.Post("/stator-links/{linkID}/clear", s.handleClearStatorLink)
		})

		// CSV import and export: a file into a project its administrators run, mapped and started by who uploaded it,
		// and a project's grid out as a file for whoever views it.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.importsOn)
			r.Post("/projects/{projectKey}/import", s.handleUploadImport)
			r.Get("/imports/{importID}", s.handleGetImport)
			r.Put("/imports/{importID}/mapping", s.handleMapImport)
			r.Post("/imports/{importID}/start", s.handleStartImport)
			r.Delete("/imports/{importID}", s.handleCancelImport)
			r.Get("/projects/{projectKey}/export.csv", s.handleExportCSV)
		})

		// ReqIF import and export: a file from another tool into a project, run the way a CSV import is, and a
		// project or a section out as ReqIF for whoever views it.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.reqifOn)
			r.Post("/projects/{projectKey}/reqif-imports", s.handleUploadReqIF)
			r.Post("/projects/{projectKey}/word-imports", s.handleUploadWord)
			r.Get("/reqif-imports/{importID}", s.handleGetReqIF)
			r.Put("/reqif-imports/{importID}/choices", s.handleChooseReqIF)
			r.Post("/reqif-imports/{importID}/start", s.handleStartReqIF)
			r.Delete("/reqif-imports/{importID}", s.handleCancelReqIF)
			r.Get("/projects/{projectKey}/export.reqif", s.handleExportReqIF)
			r.Get("/projects/{projectKey}/export.reqifz", s.handleExportReqIFZ)
		})

		// A specification or a baseline as a document, for whoever views the project: read for the print view,
		// written as Word or Markdown, printed as PDF.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.documentsOn)
			r.Get("/projects/{projectKey}/document", s.handleGetProjectDocument)
			r.Get("/projects/{projectKey}/document.docx", s.handleProjectDocx)
			r.Get("/projects/{projectKey}/document.pdf", s.handleProjectPDF)
			r.Get("/projects/{projectKey}/document.md", s.handleProjectMarkdown)
			r.Get("/baselines/{baselineID}/document", s.handleGetBaselineDocument)
			r.Get("/baselines/{baselineID}/document.docx", s.handleBaselineDocx)
			r.Get("/baselines/{baselineID}/document.pdf", s.handleBaselinePDF)
			r.Get("/baselines/{baselineID}/document.md", s.handleBaselineMarkdown)
			r.Get("/projects/{projectKey}/document.html.zip", s.handleProjectSite)
			r.Get("/baselines/{baselineID}/document.html.zip", s.handleBaselineSite)
		})

		// Word templates (#214): read by the project's readers, or by everybody once shared; kept by its administrators.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.templatesOn)
			r.Get("/projects/{projectKey}/export-templates", s.handleListExportTemplates)
			r.Post("/projects/{projectKey}/export-templates", s.handleUploadExportTemplate)
			r.Get("/export-templates/{templateID}", s.handleGetExportTemplate)
			r.Patch("/export-templates/{templateID}", s.handleUpdateExportTemplate)
			r.Delete("/export-templates/{templateID}", s.handleDeleteExportTemplate)
			r.Get("/export-templates/{templateID}/file", s.handleDownloadExportTemplate)
		})

		// Test plans and executions (#251): read by the project's readers; plans kept by its editors, an execution by
		// its manager, results by its testers.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, s.testsOn)
			r.Get("/projects/{projectKey}/test-plans", s.handleListTestPlans)
			r.Post("/projects/{projectKey}/test-plans", s.handleCreateTestPlan)
			r.Get("/test-plans/{planID}", s.handleGetTestPlan)
			r.Patch("/test-plans/{planID}", s.handleUpdateTestPlan)
			r.Put("/test-plans/{planID}/cases", s.handleSetTestPlanCases)
			r.Delete("/test-plans/{planID}", s.handleDeleteTestPlan)
			r.Get("/projects/{projectKey}/test-executions", s.handleListTestExecutions)
			r.Post("/projects/{projectKey}/test-executions", s.handleStartTestExecution)
			r.Get("/test-executions/mine", s.handleListMyTestExecutions)
			r.Get("/test-executions/{executionID}", s.handleGetTestExecution)
			r.Patch("/test-executions/{executionID}", s.handleUpdateTestExecution)
			r.Post("/test-executions/{executionID}/close", s.handleCloseTestExecution)
			r.Get("/test-executions/{executionID}/cases/{reqKey}", s.handleGetTestExecutionCase)
			r.Put("/test-executions/{executionID}/cases/{reqKey}/result", s.handleRecordTestResult)
			r.Post("/test-executions/{executionID}/cases/{reqKey}/files", s.handleUploadTestExecutionFile)
			r.Post("/test-executions/{executionID}/cases/{reqKey}/issues", s.handleFileTestFailureIssue)
			r.Get("/test-execution-files/{fileID}", s.handleDownloadTestExecutionFile)
			r.Delete("/test-execution-files/{fileID}", s.handleDeleteTestExecutionFile)
			r.Get("/test-executions/{executionID}/report", s.handleGetTestExecutionReport)
			r.Get("/test-executions/{executionID}/report.docx", s.handleTestExecutionReportDocx)
			r.Get("/test-executions/{executionID}/report.pdf", s.handleTestExecutionReportPDF)
			r.Get("/requirements/{reqKey}/test-executions", s.handleListTestCaseResults)
		})

		// What the caller may do across the installation, and the people and groups a grant names.
		r.Group(func(r chi.Router) {
			r.Use(requireAuth)
			r.Get("/access/me", s.handleMyAccess)
			r.With(s.permsOn).Get("/people", s.handleListPeople)
			r.With(s.permsOn).Get("/groups", s.handleListGroups)
		})

		r.Group(func(r chi.Router) {
			r.Use(requireAuth)
			mountPending(r, false, false)
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		respondError(w, r, ErrNotFound("No such endpoint. The API lives under "+APIPrefix+"."))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		respondError(w, r, &APIError{
			Status:  http.StatusMethodNotAllowed,
			Code:    "method_not_allowed",
			Message: "That method is not allowed here.",
		})
	})
	// MCP runs each tool call through this router, as the caller.
	s.handler = r
	return r
}

// mountPending routes each pending operation to a 501 so clients can be written
// against it; the ones that are not public sit behind the sign-in.
func mountPending(r chi.Router, public, root bool) {
	for _, op := range operations {
		if op.pending && op.public == public && op.root == root {
			r.Method(op.method, op.path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				respondError(w, r, errNotImplemented)
			}))
		}
	}
}

// statusResponse is what the probes answer.
type statusResponse struct {
	Status string `json:"status"`
}

// readinessResponse adds how reads are being routed, for an operator.
type readinessResponse struct {
	Status  string   `json:"status"`
	Routing db.Stats `json:"routing"`
}

// handleLiveness touches nothing external on purpose: a database outage must
// not get the container killed and restarted, which would only make it worse.
func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, statusResponse{Status: "ok"})
}

// handleReadiness answers whether the process can serve traffic. Nobody has
// signed in to read it, so a failure says whether, not why.
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()

	if s.DB == nil {
		respondJSON(w, r, http.StatusServiceUnavailable, statusResponse{Status: "unavailable"})
		return
	}
	if err := s.DB.Ping(ctx); err != nil {
		loggerFrom(r.Context()).Error("not ready", "error", err)
		respondJSON(w, r, http.StatusServiceUnavailable, statusResponse{Status: "unavailable"})
		return
	}
	respondJSON(w, r, http.StatusOK, readinessResponse{Status: "ok", Routing: s.DB.Stats()})
}
