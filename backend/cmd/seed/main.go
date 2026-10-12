// Command seed makes a database worth opening. Bare, it makes the demo organization of a development stack; `seed
// bootstrap` makes the organization EXCITER_BOOTSTRAP_* names, its first administrator, provider and members.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/observability"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/secret"
	"github.com/cloudster1/exciter/backend/internal/seed"
)

// bootstrapAdminName is what the first administrator is called until they say otherwise.
const bootstrapAdminName = "Administrator"

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	bootstrap := len(os.Args) > 1 && os.Args[1] == "bootstrap"
	if len(os.Args) > 1 && !bootstrap {
		return fmt.Errorf("unknown command %q; run seed bare for the development organization, or seed bootstrap", os.Args[1])
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.IsProduction() && !bootstrap {
		return errors.New("seed makes the development organization, which has no place in production; run seed bootstrap instead")
	}
	if bootstrap && cfg.Bootstrap.AdminEmail == "" {
		return errors.New("seed bootstrap needs EXCITER_BOOTSTRAP_ADMIN_EMAIL and EXCITER_BOOTSTRAP_ADMIN_PASSWORD: without them nobody could sign in to the organization it makes")
	}
	boot := cfg.Bootstrap
	log := observability.NewLogger(cfg.LogLevel, cfg.IsProduction())
	slog.SetDefault(log)

	ctx := context.Background()
	cluster, err := db.Open(ctx, cfg.DB, log)
	if err != nil {
		return err
	}
	defer cluster.Close()

	res, err := seed.Ensure(ctx, cluster, boot.OrgSlug, boot.OrgName)
	if err != nil {
		return err
	}
	log.Info("organization ready", "org", boot.OrgSlug, "created", res.OrgCreated)

	// Somebody has to be able to get in before any identity provider is set up, and that somebody is named by
	// the operator rather than invented here.
	accounts := auth.NewService(cluster, auth.DefaultPasswordParams(), cfg.Auth.SessionTTL)
	if boot.AdminEmail != "" {
		made, err := accounts.EnsureAdmin(ctx, boot.OrgSlug, boot.AdminEmail, bootstrapAdminName, boot.AdminPassword)
		if err != nil {
			return err
		}
		log.Info("administrator ready", "email", boot.AdminEmail, "created", made)
		admin, err := accounts.AccountID(ctx, boot.AdminEmail)
		if err != nil {
			return err
		}
		ctx = db.WithUser(ctx, admin)
	}
	return populate(ctx, cfg, cluster, accounts, log)
}

// populate lets the named members in ahead of their first sign-in and points the organization at its provider,
// configured in the bootstrap administrator's name, since only an administrator may configure one.
func populate(ctx context.Context, cfg config.Config, cluster *db.Cluster, accounts *auth.Service, log *slog.Logger) error {
	boot := cfg.Bootstrap
	var box *secret.Box
	if cfg.SecretKey != nil {
		var err error
		if box, err = secret.New(cfg.SecretKey); err != nil {
			return err
		}
	}
	org, err := accounts.OrgBySlug(ctx, boot.OrgSlug)
	if err != nil {
		return err
	}
	sso := oidc.NewService(cluster, box, cfg.Auth.OIDCRedirectURL)
	done, err := seed.Populate(ctx, accounts, sso, *org, seed.Bootstrapped(boot))
	if err != nil {
		return err
	}
	for _, m := range done.Added {
		log.Info("member let in ahead of sign-in", "email", m.Email, "role", m.Role, "org", boot.OrgSlug)
	}
	switch {
	case done.ProviderCreated:
		log.Info("identity provider configured", "org", boot.OrgSlug, "issuer", boot.OIDCIssuer)
	case done.ProviderExisting:
		log.Info("the organization's identity provider is already set up", "org", boot.OrgSlug)
	}
	return nil
}
