//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"
)

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// Nothing behind a sign-in is reachable without one: every operation that is
// not public, called anonymously with placeholder parameters, answers 401.
func TestEveryEndpointRefusesAnAnonymousCaller(t *testing.T) {
	h := newHarness(t)
	nobody := newAPIServer(t, h).anonymous()
	for _, r := range apiContract.routes {
		if r.Public {
			continue
		}
		path := r.Path
		for strings.Contains(path, "{") {
			open, end := strings.IndexByte(path, '{'), strings.IndexByte(path, '}')
			path = path[:open] + "00000000-0000-0000-0000-000000000000" + path[end+1:]
		}
		var body any
		if r.Method != http.MethodGet && r.Method != http.MethodDelete {
			body = map[string]any{}
		}
		if resp := nobody.call(t, r.Method, "/api/v1"+path, body); resp.Status != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d to nobody, want 401", r.Method, r.Path, resp.Status)
		}
	}
}
