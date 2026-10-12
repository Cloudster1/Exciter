// Package config loads and validates process configuration from the
// environment. Every setting is an EXCITER_* variable; there is no file.
package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// bootstrapSlug is the shape the org table's org_slug_shape check accepts.
var bootstrapSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

// Environments a deployment may declare itself as.
const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Defaults, named so the tests and the documentation can quote them.
const (
	DefaultHTTPAddr        = ":8080"
	DefaultMetricsAddr     = ":9090"
	DefaultAppBaseURL      = "http://localhost:5173"
	DefaultRequestTimeout  = 30 * time.Second
	DefaultMaxConns        = 20
	DefaultMinConns        = 2
	DefaultConnMaxLifetime = time.Hour
	DefaultHealthInterval  = 5 * time.Second
	DefaultMaxReplicaLag   = 2 * time.Second
	DefaultLagSamples      = 3
	DefaultReadYourWrites  = 30 * time.Second
	DefaultSessionCookie   = "exciter_session"
	DefaultSessionTTL      = 720 * time.Hour
	// DefaultBootstrapOrgSlug and DefaultBootstrapOrgName are seed.DemoOrgSlug
	// and seed.DemoOrgName, which a test holds the two to.
	DefaultBootstrapOrgSlug = "demo"
	DefaultBootstrapOrgName = "Demo"
	// DefaultRetainAudit and MinRetainAudit are audit.DefaultRetention and
	// audit.MinRetention, which a test holds the two to.
	DefaultRetainAudit = 365 * 24 * time.Hour
	MinRetainAudit     = 24 * time.Hour
	// SecretKeyBytes is the AES-256 key length EXCITER_SECRET_KEY decodes to.
	SecretKeyBytes = 32
	// OIDCCallbackPath is where an identity provider sends the browser back,
	// under the web client's origin, which proxies the API.
	OIDCCallbackPath = "/api/v1/auth/oidc/callback"
	// DefaultS3Bucket and DefaultS3Region are where uploaded files go when
	// only the endpoint and its keys are named.
	DefaultS3Bucket = "exciter-files"
	DefaultS3Region = "us-east-1"
)

// Config is the fully resolved configuration for every binary.
type Config struct {
	Env      string
	HTTPAddr string
	LogLevel string
	// AppBaseURL is where the web client is served; the same-site check
	// accepts writes from it.
	AppBaseURL string
	// CORSOrigins may call the API with credentials from another origin. Empty
	// outside production means the development web server's own origins.
	CORSOrigins []string
	// RequestTimeout bounds every request's handler.
	RequestTimeout time.Duration
	// TrustedProxies are the peers whose X-Real-IP header names the client,
	// such as the web container's nginx; empty trusts nobody's.
	TrustedProxies []netip.Prefix

	DB        DB
	Valkey    Valkey
	Auth      Auth
	Telemetry Telemetry
	Bootstrap Bootstrap
	S3        S3
	// TestEndpoints serves the throwaway organizations of the browser suite.
	TestEndpoints TestEndpoints

	// RetainAudit is how long the worker keeps an audit entry; zero keeps
	// every one forever.
	RetainAudit time.Duration

	// SecretKey encrypts secrets stored in the database, such as a person's
	// Armature token. Nil in development when it is not set.
	SecretKey []byte
}

// DB describes the Postgres cluster: one writable primary and optional read
// replicas, so a developer can run against a single instance.
type DB struct {
	PrimaryURL  string
	ReplicaURLs []string
	// AdminURL connects as exciter_admin, exempt from row level security by
	// policy, for signup, login and cross-tenant jobs. Defaults to PrimaryURL.
	AdminURL string

	// PrimaryMaxConns bounds the write pool, and the admin pool beside it;
	// ReplicaMaxConns bounds each read pool.
	PrimaryMaxConns int32
	ReplicaMaxConns int32
	MinConns        int32
	ConnMaxLifetime time.Duration
	HealthInterval  time.Duration
	MaxReplicaLag   time.Duration
	// ReplicaLagSamples is how many connections of a replica's pool each
	// health pass asks, so behind a load balanced service it sees several.
	ReplicaLagSamples int
	// ReadYourWritesTTL is how long a reader's last write position keeps
	// their reads off any replica that has not replayed it.
	ReadYourWritesTTL time.Duration
}

// Auth holds the session cookie's settings and how sign-in reaches identity
// providers.
type Auth struct {
	SessionCookie string
	SecureCookies bool
	SessionTTL    time.Duration
	// OIDCRedirectURL is the callback address providers are told to send the
	// browser back to.
	OIDCRedirectURL string
	// OIDCBackchannel maps a provider's public origin to the one this process
	// reaches it at, for a stack where the two differ.
	OIDCBackchannel map[string]string
}

// Bootstrap is what cmd/seed sets up: the organization, its first local
// administrator, and an identity provider with its first members when named.
type Bootstrap struct {
	OrgSlug       string
	OrgName       string
	AdminEmail    string
	AdminPassword string

	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string

	// Members are let into the organization ahead of their first sign-in,
	// so a development stack needs nobody to approve them.
	Members []BootstrapMember
}

// BootstrapMember is one address and the standing it is given.
type BootstrapMember struct {
	Email string
	Role  string
}

// TestEndpoints make and remove throwaway organizations under /api/v1/test.
// Off, they answer 404 like any path that does not exist.
type TestEndpoints struct {
	Enabled bool
	// Token is the shared secret every call carries, so an endpoint switched
	// on by accident is still closed.
	Token string
}

// MinTestTokenLength keeps the test endpoints' token from being guessable.
const MinTestTokenLength = 16

// S3 is the bucket uploaded files live in: theme assets now, attachments
// later. A blank endpoint leaves uploads off, refused with the setting to fix.
type S3 struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
}

// Valkey is the shared store behind read-your-writes. A blank URL keeps the
// positions in the process, which is only right for a single api process.
type Valkey struct {
	URL string
}

// Telemetry is the metrics listener and the trace collector. Both are off when
// their address is blank, and a blank set on purpose is honoured.
type Telemetry struct {
	// MetricsAddr serves /metrics on its own port, never through the API.
	MetricsAddr string
	// OTLPEndpoint is the collector's traces URL; empty means no traces leave.
	OTLPEndpoint string
	// SampleRatio is the share of new traces kept, 0 to 1.
	SampleRatio float64
}

// Error lists every problem Load found, so an operator fixes them in one go.
type Error struct {
	Problems []string
}

func (e *Error) Error() string {
	return "Exciter cannot start until its configuration is fixed:\n  - " + strings.Join(e.Problems, "\n  - ")
}

// Load reads configuration from the environment, applies defaults and validates
// the result. It returns every problem it finds at once rather than the first.
func Load() (Config, error) {
	l := &loader{}
	// EXCITER_DB_MAX_CONNS sizes both pools unless one is sized on its own.
	maxConns := l.integer("EXCITER_DB_MAX_CONNS", DefaultMaxConns)
	c := Config{
		Env:            l.str("EXCITER_ENV", EnvDevelopment),
		HTTPAddr:       l.str("EXCITER_HTTP_ADDR", DefaultHTTPAddr),
		LogLevel:       l.str("EXCITER_LOG_LEVEL", "info"),
		AppBaseURL:     strings.TrimSuffix(l.str("EXCITER_APP_URL", DefaultAppBaseURL), "/"),
		CORSOrigins:    splitList(l.str("EXCITER_CORS_ORIGINS", "")),
		RequestTimeout: l.duration("EXCITER_REQUEST_TIMEOUT", DefaultRequestTimeout),
		DB: DB{
			PrimaryURL:      l.str("EXCITER_DB_PRIMARY_URL", ""),
			ReplicaURLs:     splitList(l.str("EXCITER_DB_REPLICA_URLS", "")),
			AdminURL:        l.str("EXCITER_DB_ADMIN_URL", ""),
			PrimaryMaxConns: int32(l.integer("EXCITER_DB_PRIMARY_MAX_CONNS", maxConns)),
			ReplicaMaxConns: int32(l.integer("EXCITER_DB_REPLICA_MAX_CONNS", maxConns)),
			MinConns:        int32(l.integer("EXCITER_DB_MIN_CONNS", DefaultMinConns)),
			ConnMaxLifetime: l.duration("EXCITER_DB_CONN_MAX_LIFETIME", DefaultConnMaxLifetime),
			HealthInterval:  l.duration("EXCITER_DB_HEALTH_INTERVAL", DefaultHealthInterval),
			MaxReplicaLag:   l.duration("EXCITER_DB_MAX_REPLICA_LAG", DefaultMaxReplicaLag),

			ReplicaLagSamples: l.integer("EXCITER_DB_REPLICA_LAG_SAMPLES", DefaultLagSamples),
			ReadYourWritesTTL: l.duration("EXCITER_READ_YOUR_WRITES_TTL", DefaultReadYourWrites),
		},
		Valkey: Valkey{
			URL: l.str("EXCITER_VALKEY_URL", ""),
		},
		Auth: Auth{
			SessionCookie: l.str("EXCITER_SESSION_COOKIE", DefaultSessionCookie),
			SecureCookies: l.boolean("EXCITER_SECURE_COOKIES", false),
			SessionTTL:    l.duration("EXCITER_SESSION_TTL", DefaultSessionTTL),
		},
		Bootstrap: Bootstrap{
			OrgSlug:    l.str("EXCITER_BOOTSTRAP_ORG_SLUG", DefaultBootstrapOrgSlug),
			OrgName:    l.str("EXCITER_BOOTSTRAP_ORG_NAME", DefaultBootstrapOrgName),
			AdminEmail: l.str("EXCITER_BOOTSTRAP_ADMIN_EMAIL", ""),
			// Not trimmed: a password is exactly what was typed.
			AdminPassword: l.raw("EXCITER_BOOTSTRAP_ADMIN_PASSWORD"),

			OIDCIssuer:       l.str("EXCITER_BOOTSTRAP_OIDC_ISSUER", ""),
			OIDCClientID:     l.str("EXCITER_BOOTSTRAP_OIDC_CLIENT_ID", ""),
			OIDCClientSecret: l.str("EXCITER_BOOTSTRAP_OIDC_CLIENT_SECRET", ""),
			Members:          l.members("EXCITER_BOOTSTRAP_MEMBERS"),
		},
		S3: S3{
			Endpoint:  l.str("EXCITER_S3_ENDPOINT", ""),
			Bucket:    l.str("EXCITER_S3_BUCKET", DefaultS3Bucket),
			AccessKey: l.str("EXCITER_S3_ACCESS_KEY", ""),
			SecretKey: l.str("EXCITER_S3_SECRET_KEY", ""),
			Region:    l.str("EXCITER_S3_REGION", DefaultS3Region),
			UseSSL:    l.boolean("EXCITER_S3_USE_SSL", false),
		},
		TestEndpoints: TestEndpoints{
			Enabled: l.boolean("EXCITER_TEST_ENDPOINTS", false),
			Token:   l.str("EXCITER_TEST_ENDPOINTS_TOKEN", ""),
		},
		Telemetry: Telemetry{
			MetricsAddr:  l.strOrBlank("EXCITER_METRICS_ADDR", DefaultMetricsAddr),
			OTLPEndpoint: l.str("EXCITER_OTEL_ENDPOINT", ""),
			SampleRatio:  l.float("EXCITER_OTEL_SAMPLE_RATIO", 1),
		},
	}
	c.RetainAudit = l.duration("EXCITER_RETAIN_AUDIT", DefaultRetainAudit)
	c.SecretKey = l.secretKey("EXCITER_SECRET_KEY", c.Env)
	c.Auth.OIDCRedirectURL = l.str("EXCITER_OIDC_REDIRECT_URL", c.AppBaseURL+OIDCCallbackPath)
	c.Auth.OIDCBackchannel = l.rewrites("EXCITER_OIDC_BACKCHANNEL")
	c.TrustedProxies = l.prefixes("EXCITER_TRUSTED_PROXIES")

	if c.DB.PrimaryURL == "" {
		l.problem("Set EXCITER_DB_PRIMARY_URL to the connection URL of the primary database.")
	}
	switch c.Env {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		l.problem(fmt.Sprintf("EXCITER_ENV is %q; set it to development, staging or production.", c.Env))
	}
	for _, pool := range []struct {
		key string
		max int32
	}{{"EXCITER_DB_PRIMARY_MAX_CONNS", c.DB.PrimaryMaxConns}, {"EXCITER_DB_REPLICA_MAX_CONNS", c.DB.ReplicaMaxConns}} {
		if pool.max < 1 {
			l.problem(pool.key + " must be at least 1.")
		} else if c.DB.MinConns > pool.max {
			l.problem(fmt.Sprintf("EXCITER_DB_MIN_CONNS (%d) exceeds %s (%d); lower the first or raise the second.", c.DB.MinConns, pool.key, pool.max))
		}
	}
	if c.DB.ReplicaLagSamples < 1 {
		l.problem("EXCITER_DB_REPLICA_LAG_SAMPLES must be at least 1.")
	}
	if c.DB.ReadYourWritesTTL <= 0 {
		l.problem("EXCITER_READ_YOUR_WRITES_TTL must be longer than zero, such as 30s.")
	}
	if c.RequestTimeout <= 0 {
		l.problem("EXCITER_REQUEST_TIMEOUT must be longer than zero, such as 30s.")
	}
	// Staging is reached over a network, so it is held to what production is held to.
	if c.Env != EnvDevelopment && !c.Auth.SecureCookies {
		l.problem("Set EXCITER_SECURE_COOKIES to true outside development, so the session cookie never travels in clear.")
	}
	if c.Auth.SessionTTL <= 0 {
		l.problem("EXCITER_SESSION_TTL must be longer than zero, such as 720h.")
	}
	if !bootstrapSlug.MatchString(c.Bootstrap.OrgSlug) {
		l.problem(fmt.Sprintf("EXCITER_BOOTSTRAP_ORG_SLUG is %q; use 3 to 40 lowercase letters, digits and hyphens, not starting or ending with a hyphen.", c.Bootstrap.OrgSlug))
	}
	if c.Bootstrap.OrgName == "" {
		l.problem("EXCITER_BOOTSTRAP_ORG_NAME is empty; give the organization a name.")
	}
	if (c.Bootstrap.AdminEmail == "") != (c.Bootstrap.AdminPassword == "") {
		l.problem("Set both EXCITER_BOOTSTRAP_ADMIN_EMAIL and EXCITER_BOOTSTRAP_ADMIN_PASSWORD, or neither.")
	}
	if c.Bootstrap.AdminEmail != "" && !strings.Contains(c.Bootstrap.AdminEmail, "@") {
		l.problem(fmt.Sprintf("EXCITER_BOOTSTRAP_ADMIN_EMAIL is %q; set it to an email address.", c.Bootstrap.AdminEmail))
	}
	if u, err := url.Parse(c.Auth.OIDCRedirectURL); err != nil || u.Scheme == "" || u.Host == "" {
		l.problem(fmt.Sprintf("EXCITER_OIDC_REDIRECT_URL is %q; set it to an absolute URL ending in %s.", c.Auth.OIDCRedirectURL, OIDCCallbackPath))
	}
	c.checkBootstrapProvider(l)
	c.checkTestEndpoints(l)
	if c.RetainAudit != 0 && c.RetainAudit < MinRetainAudit {
		l.problem(fmt.Sprintf("EXCITER_RETAIN_AUDIT is %s, but the audit log keeps every entry for at least %.0fh; set it to that or longer, such as 8760h, or to 0 to keep the log forever.", c.RetainAudit, MinRetainAudit.Hours()))
	}
	if c.S3.Endpoint != "" && (c.S3.AccessKey == "" || c.S3.SecretKey == "") {
		l.problem("EXCITER_S3_ENDPOINT is set, so set EXCITER_S3_ACCESS_KEY and EXCITER_S3_SECRET_KEY as well.")
	}
	if c.Telemetry.SampleRatio < 0 || c.Telemetry.SampleRatio > 1 {
		l.problem("EXCITER_OTEL_SAMPLE_RATIO must be between 0 and 1.")
	}
	if c.DB.AdminURL == "" {
		c.DB.AdminURL = c.DB.PrimaryURL
	}
	if len(l.problems) > 0 {
		return Config{}, &Error{Problems: l.problems}
	}
	return c, nil
}

// checkBootstrapProvider refuses a provider the seed could not configure: one without its client, a secret with
// no key to seal it, or nobody whose name it is configured in, since only an administrator configures one.
func (c Config) checkBootstrapProvider(l *loader) {
	b := c.Bootstrap
	if (b.OIDCIssuer == "") != (b.OIDCClientID == "") {
		l.problem("Set both EXCITER_BOOTSTRAP_OIDC_ISSUER and EXCITER_BOOTSTRAP_OIDC_CLIENT_ID, or neither.")
	}
	if b.OIDCClientSecret != "" && c.SecretKey == nil {
		l.problem("EXCITER_BOOTSTRAP_OIDC_CLIENT_SECRET is set but EXCITER_SECRET_KEY is not; set the key so the secret can be stored sealed.")
	}
	if b.OIDCIssuer != "" && b.AdminEmail == "" {
		l.problem("EXCITER_BOOTSTRAP_OIDC_ISSUER is set but EXCITER_BOOTSTRAP_ADMIN_EMAIL is not; set the administrator the provider is configured in the name of.")
	}
}

// checkTestEndpoints refuses the test endpoints in production, without a token hard to guess, or without the
// provider every throwaway organization signs in through.
func (c Config) checkTestEndpoints(l *loader) {
	if !c.TestEndpoints.Enabled {
		return
	}
	if c.Env == EnvProduction {
		l.problem("EXCITER_TEST_ENDPOINTS is on in production, where it would let anybody with its token make and delete organizations. Turn it off.")
	}
	if len(c.TestEndpoints.Token) < MinTestTokenLength {
		l.problem(fmt.Sprintf("EXCITER_TEST_ENDPOINTS is on, so set EXCITER_TEST_ENDPOINTS_TOKEN to a secret of at least %d characters.", MinTestTokenLength))
	}
	if c.Bootstrap.OIDCIssuer == "" {
		l.problem("EXCITER_TEST_ENDPOINTS is on, so set EXCITER_BOOTSTRAP_OIDC_ISSUER and its client: every throwaway organization signs in through that provider.")
	}
}

// IsProduction reports whether this is the production deployment.
func (c Config) IsProduction() bool { return c.Env == EnvProduction }
