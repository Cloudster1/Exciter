package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/demo"
	"github.com/cloudster1/exciter/backend/internal/netguard"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/risk"
	"github.com/cloudster1/exciter/backend/internal/secret"
	"github.com/cloudster1/exciter/backend/internal/stator"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

const (
	// demoWait bounds how long the seed waits for a demo the worker took up first.
	demoWait = 10 * time.Minute
	// demoPoll is how often it looks.
	demoPoll = 2 * time.Second
)

// addDemo adds the suite demo (#270), or links it again when it is there, as the bootstrap administrator, and
// waits until it is done.
func addDemo(ctx context.Context, cfg config.Config, cluster *db.Cluster, log *slog.Logger) error {
	actor, ok := db.UserFrom(ctx)
	if !ok {
		return errors.New("seed demo needs EXCITER_BOOTSTRAP_ADMIN_EMAIL: only an administrator may add the suite demo")
	}
	var box *secret.Box
	if cfg.SecretKey != nil {
		var err error
		if box, err = secret.New(cfg.SecretKey); err != nil {
			return err
		}
	}
	allow := netguard.ParseAllow(cfg.OutboundAllow)
	appURL, dev := cfg.AppBaseURL, cfg.Env == config.EnvDevelopment
	svc := demo.NewService(cluster, project.NewService(cluster).WithSeeder(risk.SeedProject),
		requirement.NewService(cluster).WithCoverage(trace.CoverageJudge{}), trace.NewService(cluster), baseline.NewService(cluster),
		armature.NewService(cluster, box, armature.NewClient(allow, cfg.ArmatureBackchannel), nil,
			armature.Options{AppURL: appURL, Allow: allow, Development: dev, Log: log}),
		stator.NewService(cluster, box, stator.NewClient(allow, cfg.StatorBackchannel), nil,
			stator.Options{AppURL: appURL, Allow: allow, Development: dev, Log: log}))

	d, err := svc.Status(db.PinPrimary(ctx))
	switch {
	case err != nil:
		return err
	case d == nil:
		_, _, err = svc.Start(ctx, actor)
	case d.State == demo.StateDone || d.State == demo.StateFailed:
		_, _, err = svc.Relink(ctx, actor)
	}
	if err != nil {
		return fmt.Errorf("add the suite demo: %w", err)
	}
	log.Info("building the suite demo; this takes a minute or two")
	if err := svc.Run(ctx, actor); err != nil {
		return err
	}
	for deadline := time.Now().Add(demoWait); time.Now().Before(deadline); time.Sleep(demoPoll) {
		d, err := svc.Status(db.PinPrimary(ctx))
		if err != nil {
			return err
		}
		switch d.State {
		case demo.StateDone:
			log.Info("suite demo ready", "projects", d.ProjectKeys, "requirements", d.Requirements, "links", d.Links,
				"skipped", d.Skipped, "note", d.Message)
			return nil
		case demo.StateFailed:
			return errors.New(d.Message)
		}
	}
	return errors.New("the suite demo is still being built; look at it in the installation settings")
}
