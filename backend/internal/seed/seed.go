// Package seed makes the installation on its first start. Every step finds
// what an earlier run made, so running it again changes nothing.
package seed

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/installation"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// Result says what a run made, as opposed to what it found already there.
type Result struct {
	// Org is the installation's organization, made or found.
	Org        tenant.Org
	OrgCreated bool
}

// Ensure makes the installation's organization on a first start, called name, and finds the one there on every
// later start, whatever an administrator renamed it to. Through the admin role, since it precedes any tenant.
func Ensure(ctx context.Context, cluster *db.Cluster, name string) (Result, error) {
	var res Result
	_, err := cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO org (slug, name) SELECT $1, $2 WHERE NOT EXISTS (SELECT 1 FROM org)
			RETURNING id, slug`, installation.Slug, name).Scan(&res.Org.ID, &res.Org.Slug)
		if err == nil {
			res.OrgCreated = true
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// The oldest, while the integration suite still makes organizations of its own beside it (#90).
		return tx.QueryRow(ctx, `SELECT id, slug FROM org ORDER BY created_at, slug LIMIT 1`).Scan(&res.Org.ID, &res.Org.Slug)
	})
	return res, err
}

// Member is one address let into the installation ahead of its first sign-in.
type Member struct {
	Email string
	Role  auth.OrgRole
}

// People is who the installation is seeded with: members named ahead of time,
// and the identity provider they sign in through, when there is one.
type People struct {
	Members  []Member
	Provider *oidc.Provider
}

// Bootstrapped is who EXCITER_BOOTSTRAP_MEMBERS and EXCITER_BOOTSTRAP_OIDC_*
// name: the installation's first people.
func Bootstrapped(cfg config.Bootstrap) People {
	var people People
	for _, m := range cfg.Members {
		people.Members = append(people.Members, Member{Email: m.Email, Role: auth.OrgRole(m.Role)})
	}
	if cfg.OIDCIssuer != "" {
		people.Provider = &oidc.Provider{
			Issuer:       cfg.OIDCIssuer,
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			CreateGroups: true,
			Enabled:      true,
		}
	}
	return people
}

// Populated says what Populate did, as opposed to what it found.
type Populated struct {
	Added            []Member
	ProviderCreated  bool
	ProviderExisting bool
}

// Populate lets the people into the organization and points it at the provider, once: a provider already there
// is an administrator's to change. ctx acts for the administrator the provider is configured in the name of.
func Populate(ctx context.Context, accounts *auth.Service, sso *oidc.Service, org tenant.Org, people People) (Populated, error) {
	var out Populated
	ctx = tenant.WithOrg(ctx, org)
	for _, m := range people.Members {
		added, err := accounts.EnsureMember(ctx, m.Email, m.Role)
		if err != nil {
			return out, err
		}
		if added {
			out.Added = append(out.Added, m)
		}
	}
	if people.Provider == nil {
		return out, nil
	}
	switch _, err := sso.Provider(ctx); {
	case err == nil:
		out.ProviderExisting = true
		return out, nil
	case !errors.Is(err, oidc.ErrNotConfigured):
		return out, err
	}
	if _, _, err := sso.Save(ctx, *people.Provider); err != nil {
		return out, err
	}
	out.ProviderCreated = true
	return out, nil
}
