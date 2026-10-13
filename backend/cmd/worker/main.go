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

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/attachment"
	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/netguard"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/observability"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/secret"
	"github.com/cloudster1/exciter/backend/internal/trace"
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

	armatures, err := armatureService(cfg, cluster, log)
	if err != nil {
		return err
	}
	// A topic without a handler is marked done as it arrives; each feature routes its own here.
	imports := csvio.NewService(cluster, requirement.NewService(cluster), trace.NewService(cluster), search.NewService(cluster))
	handlers := events.NewMux(nil).Route(csvio.TopicRequested, csvio.Handler(imports)).
		Route(armature.TopicLinks, armature.NewLinkSync(armatures, log))
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

// armatureService calls Armature as the person an event names, through the guard as the api does; without
// EXCITER_SECRET_KEY no stored token opens, and every link waits for one.
func armatureService(cfg config.Config, cluster *db.Cluster, log *slog.Logger) (*armature.Service, error) {
	var box *secret.Box
	if cfg.SecretKey != nil {
		var err error
		if box, err = secret.New(cfg.SecretKey); err != nil {
			return nil, err
		}
	}
	allow := netguard.ParseAllow(cfg.OutboundAllow)
	return armature.NewService(cluster, box, armature.NewClient(allow, cfg.ArmatureBackchannel), nil,
		armature.Options{AppURL: cfg.AppBaseURL, Allow: allow, Development: cfg.Env == config.EnvDevelopment, Log: log}), nil
}
