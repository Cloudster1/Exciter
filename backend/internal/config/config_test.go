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
		"EXCITER_OIDC_REDIRECT_URL", "EXCITER_OIDC_BACKCHANNEL", "EXCITER_TRUSTED_PROXIES",
		"EXCITER_BOOTSTRAP_OIDC_ISSUER", "EXCITER_BOOTSTRAP_OIDC_CLIENT_ID", "EXCITER_BOOTSTRAP_OIDC_CLIENT_SECRET", "EXCITER_BOOTSTRAP_MEMBERS",
		"EXCITER_S3_ENDPOINT", "EXCITER_S3_BUCKET", "EXCITER_S3_ACCESS_KEY", "EXCITER_S3_SECRET_KEY",
		"EXCITER_S3_REGION", "EXCITER_S3_USE_SSL", "EXCITER_TEST_ENDPOINTS", "EXCITER_TEST_ENDPOINTS_TOKEN",
		"EXCITER_UPLOAD_LIMIT", "EXCITER_OUTBOUND_ALLOW", "EXCITER_ARMATURE_BACKCHANNEL",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("EXCITER_DB_PRIMARY_URL", "postgres://app@db/exciter")
}

func TestTheUploadLimitReadsSizes(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int64
	}{{"50MB", 50 * mib}, {"2g", 2 * gib}, {"512 kb", 512 * kib}, {"1048576", mib}, {"", DefaultUploadLimit}} {
		clean(t)
		t.Setenv("EXCITER_UPLOAD_LIMIT", tc.text)
		c, err := Load()
		if err != nil {
			t.Fatalf("%q: %v", tc.text, err)
		}
		if c.UploadLimit != tc.want {
			t.Errorf("%q read as %d, want %d", tc.text, c.UploadLimit, tc.want)
		}
	}
	for _, bad := range []string{"lots", "-5MB", "0", "99999999999GB"} {
		clean(t)
		t.Setenv("EXCITER_UPLOAD_LIMIT", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_UPLOAD_LIMIT") {
			t.Errorf("%q should be refused by name, got %v", bad, err)
		}
	}
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
	if c.Auth.OIDCRedirectURL != DefaultAppBaseURL+OIDCCallbackPath {
		t.Errorf("the callback defaults to %q, want it under the web client", c.Auth.OIDCRedirectURL)
	}
	if len(c.Auth.OIDCBackchannel) != 0 || len(c.TrustedProxies) != 0 {
		t.Errorf("development without rewrites or proxies = %v, %v", c.Auth.OIDCBackchannel, c.TrustedProxies)
	}
	t.Setenv("EXCITER_APP_URL", "https://requirements.example.test/")
	if c, err := Load(); err != nil || c.Auth.OIDCRedirectURL != "https://requirements.example.test"+OIDCCallbackPath {
		t.Errorf("the callback follows the web client to %q (%v)", c.Auth.OIDCRedirectURL, err)
	}
	t.Setenv("EXCITER_OIDC_REDIRECT_URL", "/only/a/path")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_OIDC_REDIRECT_URL") {
		t.Errorf("a callback that is no absolute URL should be refused: %v", err)
	}
	t.Setenv("EXCITER_OIDC_REDIRECT_URL", "")
	t.Setenv("EXCITER_SESSION_TTL", "0s")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_SESSION_TTL") {
		t.Errorf("a session that ends at once should be refused: %v", err)
	}
}

func TestBackchannelRewritesAreParsed(t *testing.T) {
	clean(t)
	t.Setenv("EXCITER_OIDC_BACKCHANNEL", "http://localhost:8180/=http://keycloak:8080, https://a.test=https://b.test/")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Auth.OIDCBackchannel["http://localhost:8180"] != "http://keycloak:8080" || c.Auth.OIDCBackchannel["https://a.test"] != "https://b.test" || len(c.Auth.OIDCBackchannel) != 2 {
		t.Fatalf("rewrites = %v", c.Auth.OIDCBackchannel)
	}
	t.Setenv("EXCITER_ARMATURE_BACKCHANNEL", "http://localhost:8190=http://armature-stub:8080")
	if c, err := Load(); err != nil || c.ArmatureBackchannel["http://localhost:8190"] != "http://armature-stub:8080" {
		t.Fatalf("the Armature backchannel = %v, %v", c.ArmatureBackchannel, err)
	}
	t.Setenv("EXCITER_OIDC_BACKCHANNEL", "keycloak")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_OIDC_BACKCHANNEL") {
		t.Fatalf("a malformed rewrite should be refused: %v", err)
	}
}

func TestBootstrapMembersAreParsed(t *testing.T) {
	clean(t)
	t.Setenv("EXCITER_BOOTSTRAP_MEMBERS", "alice@suite.test=admin, bob@suite.test = member")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []BootstrapMember{{"alice@suite.test", "admin"}, {"bob@suite.test", "member"}}
	if len(c.Bootstrap.Members) != 2 || c.Bootstrap.Members[0] != want[0] || c.Bootstrap.Members[1] != want[1] {
		t.Fatalf("members = %+v", c.Bootstrap.Members)
	}
	for _, bad := range []string{"alice@suite.test", "alice=admin", "alice@suite.test=king"} {
		t.Setenv("EXCITER_BOOTSTRAP_MEMBERS", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_BOOTSTRAP_MEMBERS") {
			t.Errorf("%q should be refused: %v", bad, err)
		}
	}
}

func TestTheBootstrapProviderNeedsAClientAKeyForItsSecretAndAnAdministrator(t *testing.T) {
	clean(t)
	t.Setenv("EXCITER_BOOTSTRAP_ADMIN_EMAIL", "admin@exciter.test")
	t.Setenv("EXCITER_BOOTSTRAP_ADMIN_PASSWORD", "a long enough password")
	t.Setenv("EXCITER_BOOTSTRAP_OIDC_ISSUER", "http://localhost:8180/realms/suite-dev")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_BOOTSTRAP_OIDC_CLIENT_ID") {
		t.Fatalf("an issuer without a client = %v", err)
	}
	t.Setenv("EXCITER_BOOTSTRAP_OIDC_CLIENT_ID", "exciter")
	t.Setenv("EXCITER_BOOTSTRAP_OIDC_CLIENT_SECRET", "exciter-dev-secret")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_SECRET_KEY") {
		t.Fatalf("a secret with no key to seal it = %v", err)
	}
	t.Setenv("EXCITER_SECRET_KEY", testKey)
	c, err := Load()
	if err != nil || c.Bootstrap.OIDCClientID != "exciter" || c.Bootstrap.OIDCClientSecret != "exciter-dev-secret" {
		t.Fatalf("bootstrap provider = %+v, %v", c.Bootstrap, err)
	}
	t.Setenv("EXCITER_BOOTSTRAP_ADMIN_EMAIL", "")
	t.Setenv("EXCITER_BOOTSTRAP_ADMIN_PASSWORD", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_BOOTSTRAP_ADMIN_EMAIL") {
		t.Fatalf("a provider configured in nobody's name = %v", err)
	}
}

func TestTrustedProxiesAreAddressRanges(t *testing.T) {
	clean(t)
	t.Setenv("EXCITER_TRUSTED_PROXIES", " 172.16.0.0/12, 10.1.2.3/8 ,fd00::/8")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range c.TrustedProxies {
		got = append(got, p.String())
	}
	if strings.Join(got, " ") != "172.16.0.0/12 10.0.0.0/8 fd00::/8" {
		t.Fatalf("trusted proxies = %v", got)
	}
	for _, bad := range []string{"172.16.0.1", "nginx", "10.0.0.0/33"} {
		t.Setenv("EXCITER_TRUSTED_PROXIES", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_TRUSTED_PROXIES") {
			t.Errorf("%q should be refused: %v", bad, err)
		}
	}
}

// A bucket with no credentials would be refused on the first upload, long
// after the operator has stopped watching the start-up.
func TestAnEndpointWithoutCredentialsIsRefused(t *testing.T) {
	clean(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.S3.Endpoint != "" || c.S3.Bucket != DefaultS3Bucket || c.S3.Region != DefaultS3Region || c.S3.UseSSL {
		t.Errorf("storage defaults = %+v", c.S3)
	}
	t.Setenv("EXCITER_S3_ENDPOINT", "seaweedfs:8333")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_S3_ACCESS_KEY") {
		t.Fatalf("an endpoint without keys = %v, want the keys asked for", err)
	}
	t.Setenv("EXCITER_S3_ACCESS_KEY", "exciter")
	t.Setenv("EXCITER_S3_SECRET_KEY", "secret")
	t.Setenv("EXCITER_S3_BUCKET", "elsewhere")
	t.Setenv("EXCITER_S3_REGION", "eu-central-1")
	t.Setenv("EXCITER_S3_USE_SSL", "true")
	c, err = Load()
	if err != nil {
		t.Fatalf("a complete bucket was refused: %v", err)
	}
	if c.S3 != (S3{Endpoint: "seaweedfs:8333", Bucket: "elsewhere", AccessKey: "exciter", SecretKey: "secret", Region: "eu-central-1", UseSSL: true}) {
		t.Errorf("storage = %+v", c.S3)
	}
}

func TestTheTestEndpointsNeedATokenAProviderAndNoProduction(t *testing.T) {
	clean(t)
	if c, err := Load(); err != nil || c.TestEndpoints.Enabled {
		t.Fatalf("the test endpoints are on by default: %+v, %v", c.TestEndpoints, err)
	}
	t.Setenv("EXCITER_TEST_ENDPOINTS", "1")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "EXCITER_TEST_ENDPOINTS_TOKEN") || !strings.Contains(err.Error(), "EXCITER_BOOTSTRAP_OIDC_ISSUER") {
		t.Fatalf("on without a token or a provider = %v", err)
	}
	t.Setenv("EXCITER_TEST_ENDPOINTS_TOKEN", "short")
	t.Setenv("EXCITER_BOOTSTRAP_ADMIN_EMAIL", "admin@exciter.test")
	t.Setenv("EXCITER_BOOTSTRAP_ADMIN_PASSWORD", "a long enough password")
	t.Setenv("EXCITER_BOOTSTRAP_OIDC_ISSUER", "http://localhost:8180/realms/suite-dev")
	t.Setenv("EXCITER_BOOTSTRAP_OIDC_CLIENT_ID", "exciter")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_TEST_ENDPOINTS_TOKEN") {
		t.Fatalf("a short token = %v", err)
	}
	t.Setenv("EXCITER_TEST_ENDPOINTS_TOKEN", "a token long enough to use")
	c, err := Load()
	if err != nil || !c.TestEndpoints.Enabled || c.TestEndpoints.Token != "a token long enough to use" {
		t.Fatalf("on with a token = %+v, %v", c.TestEndpoints, err)
	}
	t.Setenv("EXCITER_ENV", EnvProduction)
	t.Setenv("EXCITER_SECURE_COOKIES", "true")
	t.Setenv("EXCITER_SECRET_KEY", testKey)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXCITER_TEST_ENDPOINTS is on in production") {
		t.Fatalf("on in production = %v", err)
	}
}
