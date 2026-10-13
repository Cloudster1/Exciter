// Command worker runs everything that happens outside a request: the outbox, whose topics each feature routes
// to its handler as it lands, the audit log's retention, and the removal of files whose rows are gone.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudster1/exciter/backend/internal/attachment"
	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/observability"
	"github.com/cloudster1/exciter/backend/internal/version"
)

// flushTimeout bounds sending the traces still in hand at shutdown.
const flushTimeout = 5 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("worker exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel, cfg.IsProduction())
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	tel, err := observability.Setup(ctx, observability.Config{
		Service: "exciter-worker", Env: cfg.Env, MetricsAddr: cfg.Telemetry.MetricsAddr,
		OTLPEndpoint: cfg.Telemetry.OTLPEndpoint, SampleRatio: cfg.Telemetry.SampleRatio,
	}, log)
	if err != nil {
		return err
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), flushTimeout)
		defer cancel()
		if err := tel.Shutdown(flushCtx); err != nil {
			log.Warn("traces still in hand were not all sent", "error", err)
		}
	}()

	cluster, err := db.Open(ctx, cfg.DB, log, db.WithQueryTracer(tel.QueryTracer))
	if err != nil {
		return err
	}
	defer cluster.Close()
	if err := tel.Register(observability.NewClusterCollector(cluster)); err != nil {
		return err
	}

	// No topic has a handler yet, so an event is marked done as it arrives; each feature routes its own here.
	handlers := events.NewMux(nil)
	go events.NewWorker(cluster, handlers, log).Run(ctx)
	go audit.NewRetention(cluster, cfg.RetainAudit, log).Run(ctx)
	store, err := objectstore.Open(objectstore.Config{
		Endpoint: cfg.S3.Endpoint, Bucket: cfg.S3.Bucket, AccessKey: cfg.S3.AccessKey,
		SecretKey: cfg.S3.SecretKey, Region: cfg.S3.Region, UseSSL: cfg.S3.UseSSL,
	})
	if err != nil {
		return err
	}
	if objectstore.IsUnavailable(store) {
		log.Warn("files are not removed: EXCITER_S3_ENDPOINT is not set")
	} else {
		files := attachment.NewService(cluster, store, nil).WithLogger(log)
		go attachment.NewReaper(files, log, attachment.DefaultReapInterval).Run(ctx)
	}

	build := version.Current()
	log.Info("worker started", "env", cfg.Env, "version", build.Version, "commit", build.Commit)
	// The worker serves nothing but its metrics, which are also its health check's sign of life.
	if err := tel.Serve(ctx); err != nil {
		log.Warn("metrics listener stopped", "error", err)
	}
	<-ctx.Done()
	log.Info("worker stopped cleanly")
	return nil
}
