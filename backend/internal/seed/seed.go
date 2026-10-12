// Package seed makes the development database worth opening. Every step finds
// what an earlier run made, so running it again changes nothing.
package seed

import (
	"context"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Seeded names, fixed so a second run finds what the first made.
const (
	DemoOrgSlug = "demo"
	DemoOrgName = "Demo"
)

// Result says what a run made, as opposed to what it found already there.
type Result struct {
	OrgCreated bool
}

// Run seeds the cluster through the admin role, since making an organization
// precedes any tenant.
func Run(ctx context.Context, cluster *db.Cluster) (Result, error) {
	return Ensure(ctx, cluster, DemoOrgSlug, DemoOrgName)
}

// Ensure makes the organization of that slug when there is none, and leaves an
// existing one, whatever it is now called, as it is.
func Ensure(ctx context.Context, cluster *db.Cluster, slug, name string) (Result, error) {
	var res Result
	_, err := cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2) ON CONFLICT (slug) DO NOTHING`, slug, name)
		res.OrgCreated = tag.RowsAffected() == 1
		return err
	})
	return res, err
}
