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
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/label"
	"github.com/cloudster1/exciter/backend/internal/observability"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/perm"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/theme"
	"github.com/cloudster1/exciter/backend/internal/trace"
	"github.com/cloudster1/exciter/backend/internal/version"
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
	// Accounts signs people in and out and keeps the organization's members;
	// nil answers every sign-in route that it is not set up.
	Accounts *auth.Service
	// OIDC runs sign-in through an organization's identity provider; nil answers its routes that it is not set
	// up. OIDCCallbackURL is the address to register with a provider.
	OIDC            *oidc.Service
	OIDCCallbackURL string
	// Audit reads the record for administrators and keeps exports; nil refuses
	// both. AuditRetention is how long the worker keeps an entry, zero forever.
	Audit          *audit.Service
	AuditRetention time.Duration
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
	// Armature connects the organization to Armature and holds each member's token; nil answers its routes that
	// it is not set up.
	Armature *armature.Service
	// Imports moves requirements in and out of CSV files; nil answers their routes that they are not set up.
	Imports *csvio.Service
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
	// TestOrgs serves /test/orgs for the browser suite when it is set, to
	// callers with TestToken; nil leaves those paths unrouted.
	TestOrgs  TestOrgs
	TestToken string

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

	r.Route(APIPrefix, func(r chi.Router) {
		r.Use(wholeOrg(r))
		r.Get("/openapi.json", s.handleOpenAPI)
		r.Get("/meta", s.handleMeta)
		r.Post("/auth/login", s.handleLogin)
		r.Get("/auth/oidc/{orgSlug}/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)
		mountPending(r, true)

		r.Group(func(r chi.Router) {
			r.Use(requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Patch("/auth/me", s.handleUpdateMe)
			r.Post("/auth/switch-org", s.handleSwitchOrg)
		})

		r.Group(func(r chi.Router) {
			r.Use(requireAdmin)
			r.Get("/users", s.handleListMembers)
			r.Post("/users", s.handleCreateMember)
			r.Patch("/users/{userID}", s.handleSetMemberRole)
			r.Delete("/users/{userID}", s.handleRemoveMember)
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

		// Themes are a person's in an organization; the examples are anybody's who is signed in.
		r.With(requireAuth).Get("/themes/examples", s.handleThemeExamples)
		r.Group(func(r chi.Router) {
			r.Use(requireOrg)
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

		// Projects: every member reads those they may view; the organization's administrators make them.
		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.projectsOn)
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
			r.Use(requireOrg, s.requirementsOn)
			r.Post("/projects/{projectKey}/requirements", s.handleCreateRequirement)
			r.Get("/projects/{projectKey}/requirements", s.handleListGrid)
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
			r.Post("/requirements/{reqKey}/copy", s.handleCopyRequirement)
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
			r.Use(requireOrg, s.attributesOn)
			r.Get("/attribute-kinds", s.handleListAttributeKinds)
			r.Get("/projects/{projectKey}/types", s.handleListRequirementTypes)
			r.Post("/projects/{projectKey}/types", s.handleCreateRequirementType)
			r.Patch("/projects/{projectKey}/types/{typeID}", s.handleUpdateRequirementType)
			r.Delete("/projects/{projectKey}/types/{typeID}", s.handleDeleteRequirementType)
			r.Get("/projects/{projectKey}/attributes", s.handleListAttributes)
			r.Post("/projects/{projectKey}/attributes", s.handleCreateAttribute)
			r.Patch("/projects/{projectKey}/attributes/{attributeID}", s.handleUpdateAttribute)
			r.Delete("/projects/{projectKey}/attributes/{attributeID}", s.handleDeleteAttribute)
		})

		// Statuses and workflows: read by whoever views their project, shaped by its administrators, taken by its editors.
		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.workflowsOn)
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
		})

		// Labels: seen with their requirements, put on and taken off by who edits them.
		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.labelsOn)
			r.Get("/requirements/{reqKey}/labels", s.handleListRequirementLabels)
			r.Post("/requirements/{reqKey}/labels", s.handleAddRequirementLabel)
			r.Delete("/requirements/{reqKey}/labels/{labelName}", s.handleRemoveRequirementLabel)
			r.Get("/labels", s.handleListLabels)
			r.Get("/labels/{labelName}/requirements", s.handleListLabelledRequirements)
		})

		// Trace links: read by whoever views both ends, made by whoever edits one; link types shaped by the
		// organization's administrators, or a project's for its own.
		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.traceOn)
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
		})

		// Attachments: seen with their requirements, uploaded and deleted by who edits them.
		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.attachmentsOn)
			r.Get("/requirements/{reqKey}/attachments", s.handleListAttachments)
			r.Post("/requirements/{reqKey}/attachments", s.handleUploadAttachment)
			r.Get("/attachments/{attachmentID}", s.handleDownloadAttachment)
			r.Delete("/attachments/{attachmentID}", s.handleDeleteAttachment)
		})

		// Personal access tokens: a person's own, made only from a session, and the organization's for its
		// administrators. The assistants' endpoint runs each tool as the HTTP call it stands for.
		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.accountsOn)
			r.Get("/tokens", s.handleListAPITokens)
			r.With(requireSession).Post("/tokens", s.handleCreateAPIToken)
			r.Delete("/tokens/{tokenID}", s.handleRevokeAPIToken)
			r.With(requireAdmin).Get("/org/tokens", s.handleListOrgAPITokens)
			r.With(requireAdmin).Delete("/org/tokens/{tokenID}", s.handleRevokeOrgAPIToken)
		})
		r.With(requireOrg).Post("/mcp", s.handleMCP)

		// Armature: the organization's connection for its administrators, each member's own token for them.
		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.armatureOn)
			r.With(requireAdmin).Get("/armature/connection", s.handleGetArmatureConnection)
			r.With(requireAdmin).Put("/armature/connection", s.handleSaveArmatureConnection)
			r.With(requireAdmin).Delete("/armature/connection", s.handleRemoveArmatureConnection)
			r.Get("/armature/account", s.handleGetArmatureAccount)
			r.With(requireSession).Put("/armature/account/token", s.handleStoreArmatureToken)
			r.Post("/armature/account/check", s.handleCheckArmatureAccount)
			r.With(requireSession).Delete("/armature/account/token", s.handleRemoveArmatureToken)
		})

		// CSV import and export: a file into a project its administrators run, mapped and started by who uploaded it,
		// and a project's grid out as a file for whoever views it.
		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.importsOn)
			r.Post("/projects/{projectKey}/import", s.handleUploadImport)
			r.Get("/imports/{importID}", s.handleGetImport)
			r.Put("/imports/{importID}/mapping", s.handleMapImport)
			r.Post("/imports/{importID}/start", s.handleStartImport)
			r.Delete("/imports/{importID}", s.handleCancelImport)
			r.Get("/projects/{projectKey}/export.csv", s.handleExportCSV)
		})

		// What the caller may do across the organization, and the people and groups a grant names.
		r.Group(func(r chi.Router) {
			r.Use(requireOrg)
			r.Get("/access/me", s.handleMyAccess)
			r.With(s.permsOn).Get("/people", s.handleListPeople)
			r.With(s.permsOn).Get("/groups", s.handleListGroups)
		})

		if s.TestOrgs != nil {
			r.Group(func(r chi.Router) {
				r.Use(s.requireTestToken)
				r.Post("/test/orgs", s.handleCreateTestOrg)
				r.Delete("/test/orgs/{orgSlug}", s.handleDeleteTestOrg)
			})
		}

		r.Group(func(r chi.Router) {
			r.Use(requireOrg)
			mountPending(r, false)
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
func mountPending(r chi.Router, public bool) {
	for _, op := range operations {
		if op.pending && op.public == public {
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

// handleMeta names the product and build, which Armature and Stator read to
// tell an Exciter apart from any other address an administrator typed.
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, metaResponse{Product: ProductName, Build: version.Current()})
}
