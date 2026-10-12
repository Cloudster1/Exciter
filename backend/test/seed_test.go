//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/seed"
)

// The seed runs on every start of the stack, so a second run must find what the
// first made and change nothing, whatever an administrator renamed it to since.
func TestTheSeedFindsWhatItMadeBefore(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	slug := "seeded-" + h.suffix()
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM org WHERE slug = $1`, slug) })

	first, err := seed.Ensure(ctx, h.cluster, slug, "Seeded")
	if err != nil || !first.OrgCreated {
		t.Fatalf("the first run = %+v, %v; want the organization made", first, err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE org SET name = 'Renamed' WHERE slug = $1`, slug); err != nil {
		t.Fatal(err)
	}
	again, err := seed.Ensure(ctx, h.cluster, slug, "Seeded")
	if err != nil || again.OrgCreated {
		t.Fatalf("the second run = %+v, %v; want nothing made", again, err)
	}
	var name string
	if err := h.super.QueryRow(ctx, `SELECT name FROM org WHERE slug = $1`, slug).Scan(&name); err != nil || name != "Renamed" {
		t.Errorf("the organization is called %q (%v) after the second run, want the administrator's name kept", name, err)
	}
}
