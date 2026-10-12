//go:build integration

package test

import (
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

// This file sorts last, so by now every operation must have been answered with a success and with a refusal:
// an endpoint nobody exercised is an endpoint whose description nobody has checked.
func TestEveryOperationWasExercisedBothWays(t *testing.T) {
	if runFiltered() {
		t.Skip("a filtered run cannot cover the whole surface")
	}
	neverSucceeded, neverRefused := apiContract.uncovered()
	// A GET with no input and no session cannot be refused; there is nothing
	// to refuse it for.
	unrefusable := map[string]bool{"GET /healthz": true, "GET /readyz": true, "GET /openapi.json": true, "GET /meta": true}
	// A pending operation answers 501 until it is built, so it cannot succeed.
	pending := map[string]bool{}
	for _, r := range httpapi.Catalog() {
		if r.Pending {
			pending[r.Method+" "+r.Path] = true
		}
	}
	neverSucceeded = without(neverSucceeded, pending)
	neverRefused = without(without(neverRefused, unrefusable), pending)
	if len(neverSucceeded) > 0 {
		t.Errorf("%d operations were never answered successfully:\n  %s", len(neverSucceeded), strings.Join(neverSucceeded, "\n  "))
	}
	if len(neverRefused) > 0 {
		t.Errorf("%d operations were never refused:\n  %s", len(neverRefused), strings.Join(neverRefused, "\n  "))
	}
}

func without(keys []string, drop map[string]bool) []string {
	var kept []string
	for _, key := range keys {
		if !drop[key] {
			kept = append(kept, key)
		}
	}
	return kept
}
