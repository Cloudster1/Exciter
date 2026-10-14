// Package seed makes the installation on its first start. Every step finds
// what an earlier run made, so running it again changes nothing.
package seed

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/oidc"
)

// Result says what a run made, as opposed to what it found already there.
type Result struct {
	// ID is the installation's, made or found.
	ID      uuid.UUID
	Created bool
}

// Ensure makes the installation on a first start, called name, and finds the one there on every later start,
// whatever an administrator renamed it to. Through the admin role, since it precedes anybody acting.
func Ensure(ctx context.Context, cluster *db.Cluster, name string) (Result, error) {
	var res Result
	_, err := cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO installation (name) SELECT $1 WHERE NOT EXISTS (SELECT 1 FROM installation)
			RETURNING id`, name).Scan(&res.ID)
		if err == nil {
			res.Created = true
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return tx.QueryRow(ctx, `SELECT id FROM installation`).Scan(&res.ID)
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

// Populate lets the people into the installation and points it at the provider, once: a provider already there
// is an administrator's to change. ctx acts for the administrator the provider is configured in the name of.
func Populate(ctx context.Context, accounts *auth.Service, sso *oidc.Service, people People) (Populated, error) {
	var out Populated
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
