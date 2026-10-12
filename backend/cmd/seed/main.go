// Command seed makes the demo organization of a development stack; from #5 on it
// also makes its first administrator and lets the realm's people in.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/observability"
	"github.com/cloudster1/exciter/backend/internal/seed"
)

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.IsProduction() {
		return errors.New("seed makes the development organization, which has no place in production")
	}
	log := observability.NewLogger(cfg.LogLevel, cfg.IsProduction())
	slog.SetDefault(log)

	ctx := context.Background()
	cluster, err := db.Open(ctx, cfg.DB, log)
	if err != nil {
		return err
	}
	defer cluster.Close()

	res, err := seed.Run(ctx, cluster)
	if err != nil {
		return err
	}
	log.Info("seed done", "org", seed.DemoOrgSlug, "created", res.OrgCreated)
	return nil
}
