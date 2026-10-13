package config

import (
	"encoding/base64"
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// loader reads variables and collects what it could not read, instead of
// quietly falling back to a default the operator did not ask for.
type loader struct {
	problems []string
}

func (l *loader) problem(p string) { l.problems = append(l.problems, p) }

func (l *loader) str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

// strOrBlank is str for a setting where blank means off: set to nothing, it
// stays nothing rather than falling back to the default.
func (l *loader) strOrBlank(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(v)
	}
	return def
}

// raw is a value exactly as the environment holds it, for a password.
func (l *loader) raw(key string) string { return os.Getenv(key) }

func (l *loader) boolean(key string, def bool) bool {
	v := l.str(key, "")
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.problem(fmt.Sprintf("%s is %q; set it to true or false.", key, v))
		return def
	}
	return b
}

func (l *loader) integer(key string, def int) int {
	v := l.str(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.problem(fmt.Sprintf("%s is %q; set it to a whole number such as %d.", key, v, def))
		return def
	}
	return n
}

func (l *loader) float(key string, def float64) float64 {
	v := l.str(key, "")
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		l.problem(fmt.Sprintf("%s is %q; set it to a number such as %g.", key, v, def))
		return def
	}
	return f
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v := l.str(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.problem(fmt.Sprintf("%s is %q; set it to a duration such as %s.", key, v, def))
		return def
	}
	return d
}

func (l *loader) size(key string, def int64) int64 {
	v := l.str(key, "")
	if v == "" {
		return def
	}
	n, err := ParseSize(v)
	if err != nil {
		l.problem(fmt.Sprintf("%s is %q; set it to a size such as 50MB.", key, v))
		return def
	}
	return n
}

// The units a size may be written in.
const (
	kib = 1 << 10
	mib = 1 << 20
	gib = 1 << 30
)

// ParseSize reads a byte count with an optional unit, as Stator does: B, K, KB, M, MB, G or GB, in either case,
// with or without a space.
func ParseSize(text string) (int64, error) {
	text = strings.ToUpper(strings.TrimSpace(text))
	units := []struct {
		suffix string
		scale  int64
	}{{"GB", gib}, {"G", gib}, {"MB", mib}, {"M", mib}, {"KB", kib}, {"K", kib}, {"B", 1}}
	scale := int64(1)
	for _, u := range units {
		if strings.HasSuffix(text, u.suffix) {
			text = strings.TrimSpace(strings.TrimSuffix(text, u.suffix))
			scale = u.scale
			break
		}
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n <= 0 || n > math.MaxInt64/scale {
		return 0, fmt.Errorf("%q is not a size such as 50MB", text)
	}
	return n * scale, nil
}

// secretKey reads the encryption key. Development may run without one, and then
// cannot store secrets; a deployment reached over a network must set it.
func (l *loader) secretKey(key, env string) []byte {
	v := l.str(key, "")
	if v == "" {
		if env != EnvDevelopment {
			l.problem(fmt.Sprintf("Set %s to %d random bytes in base64, such as the output of openssl rand -base64 %d; it encrypts stored secrets.", key, SecretKeyBytes, SecretKeyBytes))
		}
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(v, "="))
	}
	if err != nil || len(decoded) != SecretKeyBytes {
		l.problem(fmt.Sprintf("%s must be %d random bytes in base64, such as the output of openssl rand -base64 %d.", key, SecretKeyBytes, SecretKeyBytes))
		return nil
	}
	return decoded
}

// members parses "email=role,email=role", each role owner, admin or member.
func (l *loader) members(key string) []BootstrapMember {
	var out []BootstrapMember
	for _, pair := range splitList(l.str(key, "")) {
		email, role, _ := strings.Cut(pair, "=")
		email, role = strings.TrimSpace(email), strings.TrimSpace(role)
		if !strings.Contains(email, "@") || (role != "owner" && role != "admin" && role != "member") {
			l.problem(fmt.Sprintf("%s has %q; write each entry as email=role, with the role owner, admin or member.", key, pair))
			continue
		}
		out = append(out, BootstrapMember{Email: email, Role: role})
	}
	return out
}

// rewrites parses "public=reachable,public=reachable", refusing a pair it
// cannot read rather than quietly dropping it.
func (l *loader) rewrites(key string) map[string]string {
	out := map[string]string{}
	for _, pair := range splitList(l.str(key, "")) {
		from, to, ok := strings.Cut(pair, "=")
		from, to = strings.TrimSuffix(strings.TrimSpace(from), "/"), strings.TrimSuffix(strings.TrimSpace(to), "/")
		fromURL, errFrom := url.Parse(from)
		toURL, errTo := url.Parse(to)
		if !ok || errFrom != nil || errTo != nil || fromURL.Host == "" || toURL.Host == "" {
			l.problem(fmt.Sprintf("%s has %q; write each entry as public-origin=reachable-origin, such as http://localhost:8180=http://keycloak:8080.", key, pair))
			continue
		}
		out[from] = to
	}
	return out
}

// prefixes parses a comma separated list of CIDRs, refusing one it cannot read
// rather than trusting less, or more, than the operator meant.
func (l *loader) prefixes(key string) []netip.Prefix {
	var out []netip.Prefix
	for _, each := range splitList(l.str(key, "")) {
		prefix, err := netip.ParsePrefix(each)
		if err != nil {
			l.problem(fmt.Sprintf("%s has %q; write each entry as an address range in CIDR notation, such as 172.16.0.0/12.", key, each))
			continue
		}
		out = append(out, prefix.Masked())
	}
	return out
}

// splitList parses a comma separated list, trimming blanks.
func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
