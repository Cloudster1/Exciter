package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// clean starts every test from an environment with only the required setting,
// so a variable exported on the developer's machine cannot change the outcome.
func clean(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"EXCITER_ENV", "EXCITER_HTTP_ADDR", "EXCITER_LOG_LEVEL", "EXCITER_APP_URL", "EXCITER_CORS_ORIGINS",
		"EXCITER_REQUEST_TIMEOUT", "EXCITER_DB_REPLICA_URLS", "EXCITER_DB_ADMIN_URL", "EXCITER_DB_MAX_CONNS",
		"EXCITER_DB_PRIMARY_MAX_CONNS", "EXCITER_DB_REPLICA_MAX_CONNS",
		"EXCITER_DB_MIN_CONNS", "EXCITER_DB_CONN_MAX_LIFETIME", "EXCITER_DB_HEALTH_INTERVAL",
		"EXCITER_DB_MAX_REPLICA_LAG", "EXCITER_DB_REPLICA_LAG_SAMPLES", "EXCITER_READ_YOUR_WRITES_TTL",
		"EXCITER_VALKEY_URL", "EXCITER_METRICS_ADDR", "EXCITER_OTEL_ENDPOINT", "EXCITER_OTEL_SAMPLE_RATIO",
		"EXCITER_SECRET_KEY", "EXCITER_RETAIN_AUDIT", "EXCITER_SESSION_COOKIE", "EXCITER_SECURE_COOKIES", "EXCITER_SESSION_TTL",
		"EXCITER_BOOTSTRAP_ORG_SLUG", "EXCITER_BOOTSTRAP_ORG_NAME", "EXCITER_BOOTSTRAP_ADMIN_EMAIL", "EXCITER_BOOTSTRAP_ADMIN_PASSWORD",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("EXCITER_DB_PRIMARY_URL", "postgres://app@db/exciter")
}

func TestDefaults(t *testing.T) {
	clean(t)
	t.Setenv("EXCITER_METRICS_ADDR", DefaultMetricsAddr)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != EnvDevelopment || c.HTTPAddr != DefaultHTTPAddr || c.RequestTimeout != DefaultRequestTimeout {
		t.Errorf("process defaults = %+v", c)
	}
	if c.DB.PrimaryMaxConns != DefaultMaxConns || c.DB.ReplicaMaxConns != DefaultMaxConns || c.DB.MinConns != DefaultMinConns || c.DB.HealthInterval != DefaultHealthInterval {
		t.Errorf("pool defaults = %+v", c.DB)
	}
	if c.DB.AdminURL != c.DB.PrimaryURL {
		t.Errorf("the admin URL should fall back to the primary, got %q", c.DB.AdminURL)
	}
	if c.DB.ReplicaLagSamples != DefaultLagSamples || c.DB.ReadYourWritesTTL != DefaultReadYourWrites || c.Valkey.URL != "" {
		t.Errorf("replica and freshness defaults = %+v, %+v", c.DB, c.Valkey)
	}
	if c.Telemetry.MetricsAddr != DefaultMetricsAddr || c.Telemetry.SampleRatio != 1 {
		t.Errorf("telemetry defaults = %+v", c.Telemetry)
	}
}

func TestListsAreTrimmed(t *testing.T) {
	clean(t)
	t.Setenv("EXCITER_DB_REPLICA_URLS", " postgres://a , ,postgres://b ")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.DB.ReplicaURLs) != 2 || c.DB.ReplicaURLs[0] != "postgres://a" || c.DB.ReplicaURLs[1] != "postgres://b" {
		t.Fatalf("replicas = %q", c.DB.ReplicaURLs)
	}
}

func TestMetricsAddressBlankMeansOff(t *testing.T) {
	clean(t)
	t.Setenv("EXCITER_METRICS_ADDR", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Telemetry.MetricsAddr != "" {
		t.Fatalf("metrics address = %q, want it off", c.Telemetry.MetricsAddr)
	}
}

func TestEveryProblemIsReportedAtOnceAsSentences(t *testing.T) {
	clean(t)
	t.Setenv("EXCITER_DB_PRIMARY_URL", "")
	t.Setenv("EXCITER_ENV", "moon")
	t.Setenv("EXCITER_DB_MAX_CONNS", "many")
	t.Setenv("EXCITER_REQUEST_TIMEOUT", "soon")
	t.Setenv("EXCITER_OTEL_SAMPLE_RATIO", "7")

	_, err := Load()
	var cfgErr *Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("error = %v, want a *config.Error", err)
	}
	for _, want := range []string{
		"EXCITER_DB_PRIMARY_URL", "EXCITER_ENV", "EXCITER_DB_MAX_CONNS", "EXCITER_REQUEST_TIMEOUT",
		"EXCITER_OTEL_SAMPLE_RATIO",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should name %s:\n%v", want, err)
		}
	}
	for _, p := range cfgErr.Problems {
		if p == "" || p[0] < 'A' || p[0] > 'Z' || !strings.HasSuffix(p, ".") {
			t.Errorf("%q is not a sentence", p)
		}
	}
}

func TestPoolBoundsMustAgree(t *testing.T) {
	clean(t)
	t.Setenv("EXCITER_DB_MAX_CONNS", "2")
	t.Setenv("EXCITER_DB_MIN_CONNS", "5")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_DB_MIN_CONNS") {
		t.Fatalf("a minimum above the maximum should be refused: %v", err)
	}
	t.Setenv("EXCITER_DB_MAX_CONNS", "")
	t.Setenv("EXCITER_DB_MIN_CONNS", "")
	t.Setenv("EXCITER_DB_REPLICA_MAX_CONNS", "0")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_DB_REPLICA_MAX_CONNS must be at least 1.") {
		t.Fatalf("an empty read pool should be refused: %v", err)
	}
}

func TestThePoolsAreSizedApart(t *testing.T) {
	clean(t)
	t.Setenv("EXCITER_DB_MAX_CONNS", "12")
	t.Setenv("EXCITER_DB_REPLICA_MAX_CONNS", "40")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DB.PrimaryMaxConns != 12 || c.DB.ReplicaMaxConns != 40 {
		t.Fatalf("pools = %d write, %d read; want 12 from EXCITER_DB_MAX_CONNS and 40 of its own", c.DB.PrimaryMaxConns, c.DB.ReplicaMaxConns)
	}
}

func TestDurationsAreParsed(t *testing.T) {
	clean(t)
	t.Setenv("EXCITER_REQUEST_TIMEOUT", "45s")
	t.Setenv("EXCITER_DB_MAX_REPLICA_LAG", "250ms")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.RequestTimeout != 45*time.Second || c.DB.MaxReplicaLag != 250*time.Millisecond {
		t.Fatalf("durations = %s, %s", c.RequestTimeout, c.DB.MaxReplicaLag)
	}
}

func TestReplicaSamplesAndFreshnessMustBePositive(t *testing.T) {
	clean(t)
	t.Setenv("EXCITER_DB_REPLICA_LAG_SAMPLES", "0")
	t.Setenv("EXCITER_READ_YOUR_WRITES_TTL", "0s")
	_, err := Load()
	var cfgErr *Error
	if !errors.As(err, &cfgErr) || len(cfgErr.Problems) != 2 {
		t.Fatalf("want both settings refused, got %v", err)
	}
}

// testKey is 32 zero bytes, which is a well formed key and nothing more.
const testKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func TestTheSecretKeyIsThirtyTwoBytesAndRequiredOutsideDevelopment(t *testing.T) {
	clean(t)
	t.Setenv("EXCITER_SECRET_KEY", testKey)
	c, err := Load()
	if err != nil || len(c.SecretKey) != SecretKeyBytes {
		t.Fatalf("a good key = %d bytes, %v", len(c.SecretKey), err)
	}

	for _, bad := range []string{"short", "AAAA", "not base64 at all!"} {
		t.Setenv("EXCITER_SECRET_KEY", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_SECRET_KEY") {
			t.Errorf("key %q should be refused: %v", bad, err)
		}
	}

	t.Setenv("EXCITER_SECRET_KEY", "")
	t.Setenv("EXCITER_ENV", EnvProduction)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_SECRET_KEY") {
		t.Fatalf("production without a key should be refused: %v", err)
	}
}

func TestTheAuditLogIsKeptAYearAndNeverLessThanADay(t *testing.T) {
	clean(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RetainAudit != DefaultRetainAudit {
		t.Errorf("the audit log is kept %s by default, want %s", cfg.RetainAudit, DefaultRetainAudit)
	}
	for value, want := range map[string]time.Duration{"0": 0, "24h": 24 * time.Hour, "2160h": 2160 * time.Hour} {
		t.Setenv("EXCITER_RETAIN_AUDIT", value)
		if cfg, err := Load(); err != nil || cfg.RetainAudit != want {
			t.Errorf("EXCITER_RETAIN_AUDIT=%s reads as %s, %v", value, cfg.RetainAudit, err)
		}
	}
	for _, value := range []string{"23h", "-1h", "a while"} {
		t.Setenv("EXCITER_RETAIN_AUDIT", value)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_RETAIN_AUDIT") {
			t.Errorf("EXCITER_RETAIN_AUDIT=%s is let through: %v", value, err)
		}
	}
}

func TestSecureCookiesOutsideDevelopment(t *testing.T) {
	for _, env := range []string{EnvStaging, EnvProduction} {
		t.Run(env, func(t *testing.T) {
			clean(t)
			t.Setenv("EXCITER_ENV", env)
			t.Setenv("EXCITER_SECRET_KEY", testKey)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_SECURE_COOKIES") {
				t.Fatalf("insecure cookies in %s should be refused: %v", env, err)
			}
			t.Setenv("EXCITER_SECURE_COOKIES", "true")
			c, err := Load()
			if err != nil {
				t.Fatalf("with secure cookies %s should load: %v", env, err)
			}
			if c.IsProduction() != (env == EnvProduction) {
				t.Errorf("IsProduction = %v in %s", c.IsProduction(), env)
			}
		})
	}
}

func TestTheBootstrapAdminNeedsBothHalves(t *testing.T) {
	clean(t)
	t.Setenv("EXCITER_BOOTSTRAP_ADMIN_EMAIL", "admin@example.test")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_BOOTSTRAP_ADMIN_PASSWORD") {
		t.Fatalf("an email without a password should be refused: %v", err)
	}
	t.Setenv("EXCITER_BOOTSTRAP_ADMIN_PASSWORD", " a password with spaces ")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Bootstrap.AdminPassword != " a password with spaces " {
		t.Errorf("the password was altered: %q", c.Bootstrap.AdminPassword)
	}
}

func TestTheBootstrapOrganizationIsNamedOrDefaultsToTheDemo(t *testing.T) {
	clean(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Bootstrap.OrgSlug != "demo" || c.Bootstrap.OrgName != "Demo" {
		t.Errorf("default organization = %q %q", c.Bootstrap.OrgSlug, c.Bootstrap.OrgName)
	}
	t.Setenv("EXCITER_BOOTSTRAP_ORG_SLUG", "acme")
	t.Setenv("EXCITER_BOOTSTRAP_ORG_NAME", "Acme Corp")
	if c, err = Load(); err != nil || c.Bootstrap.OrgSlug != "acme" || c.Bootstrap.OrgName != "Acme Corp" {
		t.Fatalf("named organization = %+v, %v", c.Bootstrap, err)
	}
}

func TestABootstrapSlugTheDatabaseWouldRefuseIsRefusedAtOnce(t *testing.T) {
	for _, slug := range []string{"A", "ab", "-acme", "acme-", "Acme", "acme corp", "a_b_c"} {
		clean(t)
		t.Setenv("EXCITER_BOOTSTRAP_ORG_SLUG", slug)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_BOOTSTRAP_ORG_SLUG") {
			t.Errorf("slug %q should be refused by name: %v", slug, err)
		}
	}
}

func TestSignInDefaults(t *testing.T) {
	clean(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Auth.SessionTTL != DefaultSessionTTL || c.Auth.SessionCookie != DefaultSessionCookie || c.Auth.SecureCookies {
		t.Errorf("sign-in defaults = %+v", c.Auth)
	}
	t.Setenv("EXCITER_SESSION_TTL", "0s")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_SESSION_TTL") {
		t.Errorf("a session that ends at once should be refused: %v", err)
	}
}
