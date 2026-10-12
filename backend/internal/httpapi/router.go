package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/observability"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/version"
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
	Log            *slog.Logger
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
	r.Use(s.readYourWrites)

	// Liveness and readiness are deliberately outside the API and authentication.
	r.Get("/healthz", s.handleLiveness)
	r.Get("/readyz", s.handleReadiness)

	r.Route(APIPrefix, func(r chi.Router) {
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
