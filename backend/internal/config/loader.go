package config

import (
	"encoding/base64"
	"fmt"
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
