package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/demo"
)

const (
	// demoWait bounds how long the seed waits for the worker to build the demo.
	demoWait = 10 * time.Minute
	// demoPoll is how often it looks.
	demoPoll = 2 * time.Second
)

// addDemo adds the suite demo (#270), or links it again when it is there, as the bootstrap administrator, and
// waits while the stack's worker builds it.
func addDemo(ctx context.Context, cluster *db.Cluster, log *slog.Logger) error {
	actor, ok := db.UserFrom(ctx)
	if !ok {
		return errors.New("seed demo needs EXCITER_BOOTSTRAP_ADMIN_EMAIL: only an administrator may add the suite demo")
	}
	// Adding and following the demo reads and writes its row alone; the worker does the rest.
	svc := demo.NewService(cluster, nil, nil, nil, nil, nil, nil)
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
	log.Info("the worker is building the suite demo; this takes a few minutes")
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
