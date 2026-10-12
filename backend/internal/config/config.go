// Package config loads and validates process configuration from the
// environment. Every setting is an EXCITER_* variable; there is no file.
package config

import (
	"fmt"
	"strings"
	"time"
)

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
	// DefaultRetainAudit and MinRetainAudit are audit.DefaultRetention and
	// audit.MinRetention, which a test holds the two to.
	DefaultRetainAudit = 365 * 24 * time.Hour
	MinRetainAudit     = 24 * time.Hour
	// SecretKeyBytes is the AES-256 key length EXCITER_SECRET_KEY decodes to.
	SecretKeyBytes = 32
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

	DB        DB
	Valkey    Valkey
	Telemetry Telemetry

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
		Telemetry: Telemetry{
			MetricsAddr:  l.strOrBlank("EXCITER_METRICS_ADDR", DefaultMetricsAddr),
			OTLPEndpoint: l.str("EXCITER_OTEL_ENDPOINT", ""),
			SampleRatio:  l.float("EXCITER_OTEL_SAMPLE_RATIO", 1),
		},
	}
	c.RetainAudit = l.duration("EXCITER_RETAIN_AUDIT", DefaultRetainAudit)
	c.SecretKey = l.secretKey("EXCITER_SECRET_KEY", c.Env)

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
	if c.RetainAudit != 0 && c.RetainAudit < MinRetainAudit {
		l.problem(fmt.Sprintf("EXCITER_RETAIN_AUDIT is %s, but the audit log keeps every entry for at least %.0fh; set it to that or longer, such as 8760h, or to 0 to keep the log forever.", c.RetainAudit, MinRetainAudit.Hours()))
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

// IsProduction reports whether this is the production deployment.
func (c Config) IsProduction() bool { return c.Env == EnvProduction }
