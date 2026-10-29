// Command seed makes the installation and what EXCITER_BOOTSTRAP_* names. Bare it is a development stack's, `seed
// demo` adds the suite demo to one, and `seed bootstrap` is for production.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/netguard"
	"github.com/cloudster1/exciter/backend/internal/observability"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/secret"
	"github.com/cloudster1/exciter/backend/internal/seed"
	"github.com/cloudster1/exciter/backend/internal/stator"
)

// bootstrapAdminName is what the first administrator is called until they say otherwise.
const bootstrapAdminName = "Administrator"

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

// mode is what a run of seed makes: a development stack's installation, that with the suite demo, or a real one's.
type mode int

const (
	development mode = iota
	withDemo
	bootstrap
)

// parseMode reads the command before the configuration, so a typo is refused whatever the environment holds.
func parseMode(args []string) (mode, error) {
	if len(args) == 0 {
		return development, nil
	}
	switch args[0] {
	case "demo":
		return withDemo, nil
	case "bootstrap":
		return bootstrap, nil
	}
	return development, fmt.Errorf("unknown command %q; run seed bare or seed demo on a development stack, or seed bootstrap", args[0])
}

// allowed refuses a mode the configuration cannot carry out, before anything connects.
func (m mode) allowed(cfg config.Config) error {
	if cfg.IsProduction() && m != bootstrap {
		return errors.New("bare, seed is for a development stack; in production run seed bootstrap, which asks for a first administrator")
	}
	if m == bootstrap && cfg.Bootstrap.AdminEmail == "" {
		return errors.New("seed bootstrap needs EXCITER_BOOTSTRAP_ADMIN_EMAIL and EXCITER_BOOTSTRAP_ADMIN_PASSWORD: without them nobody could sign in to the installation it makes")
	}
	return nil
}

func run() error {
	asked, err := parseMode(os.Args[1:])
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := asked.allowed(cfg); err != nil {
		return err
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

	res, err := seed.Ensure(ctx, cluster, cfg.Name)
	if err != nil {
		return err
	}
	log.Info("installation ready", "name", cfg.Name, "created", res.Created)

	// Somebody has to be able to get in before any identity provider is set up, and that somebody is named by
	// the operator rather than invented here.
	accounts := auth.NewService(cluster, auth.DefaultPasswordParams(), cfg.Auth.SessionTTL)
	if boot.AdminEmail != "" {
		made, err := accounts.EnsureAdmin(ctx, boot.AdminEmail, bootstrapAdminName, boot.AdminPassword)
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
	if err := populate(ctx, cfg, cluster, accounts, log); err != nil || asked != withDemo {
		return err
	}
	return addDemo(ctx, cluster, log)
}

// populate lets the named members in ahead of their first sign-in and points the installation at its provider,
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
	sso := oidc.NewService(cluster, box, cfg.Auth.OIDCRedirectURL)
	done, err := seed.Populate(ctx, accounts, sso, seed.Bootstrapped(boot))
	if err != nil {
		return err
	}
	for _, m := range done.Added {
		log.Info("member let in ahead of sign-in", "email", m.Email, "role", m.Role)
	}
	switch {
	case done.ProviderCreated:
		log.Info("identity provider configured", "issuer", boot.OIDCIssuer)
	case done.ProviderExisting:
		log.Info("the installation's identity provider is already set up")
	}
	return connectPartners(ctx, cfg, cluster, box, log)
}

// connectPartners connects Armature and Stator where none is connected yet, so a chart can wire the suite; an
// address an administrator set since is theirs and stays.
func connectPartners(ctx context.Context, cfg config.Config, cluster *db.Cluster, box *secret.Box, log *slog.Logger) error {
	boot := cfg.Bootstrap
	if boot.ArmatureURL == "" && boot.StatorURL == "" {
		return nil
	}
	if _, ok := db.UserFrom(ctx); !ok {
		return errors.New("EXCITER_BOOTSTRAP_ARMATURE_URL and EXCITER_BOOTSTRAP_STATOR_URL need EXCITER_BOOTSTRAP_ADMIN_EMAIL: only an administrator may connect a partner")
	}
	allow := netguard.ParseAllow(cfg.OutboundAllow)
	appURL, dev := cfg.AppBaseURL, cfg.Env == config.EnvDevelopment
	partners := []struct {
		name, url string
		connect   func(context.Context, string, string) (bool, error)
	}{
		{"Armature", boot.ArmatureURL, armature.NewService(cluster, box, armature.NewClient(allow, cfg.ArmatureBackchannel), nil,
			armature.Options{AppURL: appURL, Allow: allow, Development: dev, Log: log}).ConnectFirst},
		{"Stator", boot.StatorURL, stator.NewService(cluster, box, stator.NewClient(allow, cfg.StatorBackchannel), nil,
			stator.Options{AppURL: appURL, Allow: allow, Development: dev, Log: log}).ConnectFirst},
	}
	for _, p := range partners {
		if p.url == "" {
			continue
		}
		made, err := p.connect(ctx, p.url, "")
		if err != nil {
			return fmt.Errorf("connect %s at %s: %w", p.name, p.url, err)
		}
		log.Info("partner ready", "partner", p.name, "url", p.url, "connected", made)
	}
	return nil
}
