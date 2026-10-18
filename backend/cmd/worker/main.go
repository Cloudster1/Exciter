// Command worker runs everything that happens outside a request: the outbox, whose topics each feature routes
// to its handler as it lands, notifications and their mail, the audit log's retention, and the removal of files
// whose rows are gone.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/attachment"
	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/mail"
	"github.com/cloudster1/exciter/backend/internal/netguard"
	"github.com/cloudster1/exciter/backend/internal/notify"
	"github.com/cloudster1/exciter/backend/internal/oauth"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/observability"
	"github.com/cloudster1/exciter/backend/internal/quality"
	"github.com/cloudster1/exciter/backend/internal/reqifio"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/review"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/secret"
	"github.com/cloudster1/exciter/backend/internal/stator"
	"github.com/cloudster1/exciter/backend/internal/testexec"
	"github.com/cloudster1/exciter/backend/internal/testresult"
	"github.com/cloudster1/exciter/backend/internal/trace"
	"github.com/cloudster1/exciter/backend/internal/version"
	"github.com/cloudster1/exciter/backend/internal/webhook"
)

const (
	// flushTimeout bounds sending the traces still in hand at shutdown.
	flushTimeout = 5 * time.Second
	// valkeyWait bounds the first answer from Valkey at start.
	valkeyWait = 5 * time.Second
)

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

	armatures, stators, err := partnerServices(cfg, cluster, log)
	if err != nil {
		return err
	}
	store, err := objectstore.Open(objectstore.Config{
		Endpoint: cfg.S3.Endpoint, Bucket: cfg.S3.Bucket, AccessKey: cfg.S3.AccessKey,
		SecretKey: cfg.S3.SecretKey, Region: cfg.S3.Region, UseSSL: cfg.S3.UseSSL,
	})
	if err != nil {
		return err
	}
	// A topic without a handler is marked done as it arrives; each feature routes its own here.
	requirements, links := requirement.NewService(cluster).WithCoverage(trace.CoverageJudge{}), trace.NewService(cluster)
	imports := csvio.NewService(cluster, requirements, links, search.NewService(cluster))
	exchanges := reqifio.NewService(cluster, requirements, attribute.NewService(cluster), links,
		attachment.NewService(cluster, store, requirements).WithMaxSize(cfg.UploadLimit).WithLogger(log), store)
	handlers := events.NewMux(nil).Route(csvio.TopicRequested, csvio.Handler(imports)).
		Route(reqifio.TopicRequested, reqifio.Handler(exchanges)).
		Route(armature.TopicLinks, armature.NewLinkSync(armatures, log)).
		Route(stator.TopicLinks, stator.NewLinkSync(stators, log)).
		Route(testresult.TopicRequested, testresult.Handler(testresult.NewService(cluster))).
		Route(quality.TopicRecheck, quality.Handler(quality.NewService(cluster, requirement.LocateForQuality)))
	var mailer mail.Mailer
	if cfg.Mail.SMTPAddr == "" {
		log.Warn("mail is off: EXCITER_SMTP_ADDR is not set, so notifications are written in the app only")
	} else {
		mailer = mail.SMTPMailer{Addr: cfg.Mail.SMTPAddr, From: cfg.Mail.From}
		go notify.NewDigester(cluster, mailer, cfg.AppBaseURL, log).Run(ctx)
	}
	go review.NewReminder(cluster, log).Run(ctx)
	go testexec.NewReminder(cluster, log).Run(ctx)
	fanOut := notify.NewFanOut(cluster, mailer, cfg.AppBaseURL, log)
	for _, topic := range fanOut.Topics() {
		handlers.Route(topic, fanOut)
	}
	valkey, err := openValkey(ctx, cfg)
	if err != nil {
		return err
	}
	if valkey != nil {
		defer valkey.Close()
		forget := trace.NewCoverageForgetter(cluster, trace.NewCoverageCache(valkey, log))
		for _, topic := range trace.CoverageTopics {
			handlers.Before(topic, forget)
		}
	}
	// Webhooks queue their deliveries ahead of the rest of a topic's work, since a second run queues none again.
	box, err := secretBox(cfg)
	if err != nil {
		return err
	}
	hooks := webhook.NewService(cluster, box, webhook.Options{AppURL: cfg.AppBaseURL, Allow: netguard.ParseAllow(cfg.OutboundAllow), Log: log})
	for _, topic := range webhook.Topics {
		handlers.Before(topic, hooks)
	}
	go webhook.NewSender(hooks, log).Run(ctx)
	go events.NewWorker(cluster, handlers, log).Run(ctx)
	go audit.NewRetention(cluster, cfg.RetainAudit, log).Run(ctx)
	go testresult.NewRetention(cluster, log).Run(ctx)
	// Computed attributes (#213): what the database marked is worked out again, project by project.
	go attribute.NewRecomputer(cluster, log, attribute.DefaultRecomputeInterval).Run(ctx)
	if objectstore.IsUnavailable(store) {
		log.Warn("files are not removed: EXCITER_S3_ENDPOINT is not set")
	} else {
		files := attachment.NewService(cluster, store, nil).WithLogger(log)
		go attachment.NewReaper(files, log, attachment.DefaultReapInterval).Run(ctx)
		go oauth.NewSweeper(cluster, log).Run(ctx)
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
// partnerServices reach Armature and Stator as the person whose change link sync works on; the worker caches nothing.
func partnerServices(cfg config.Config, cluster *db.Cluster, log *slog.Logger) (*armature.Service, *stator.Service, error) {
	box, err := secretBox(cfg)
	if err != nil {
		return nil, nil, err
	}
	allow := netguard.ParseAllow(cfg.OutboundAllow)
	development := cfg.Env == config.EnvDevelopment
	armatures := armature.NewService(cluster, box, armature.NewClient(allow, cfg.ArmatureBackchannel), nil,
		armature.Options{AppURL: cfg.AppBaseURL, Allow: allow, Development: development, Log: log})
	stators := stator.NewService(cluster, box, stator.NewClient(allow, cfg.StatorBackchannel), nil,
		stator.Options{AppURL: cfg.AppBaseURL, Allow: allow, Development: development, Log: log})
	return armatures, stators, nil
}

// secretBox opens what is sealed with EXCITER_SECRET_KEY; nil without one, when nothing sealed opens.
func secretBox(cfg config.Config) (*secret.Box, error) {
	if cfg.SecretKey == nil {
		return nil, nil
	}
	return secret.New(cfg.SecretKey)
}

// openValkey connects to EXCITER_VALKEY_URL, where the api caches coverage, or returns nil when it is blank.
func openValkey(ctx context.Context, cfg config.Config) (*redis.Client, error) {
	if cfg.Valkey.URL == "" {
		return nil, nil
	}
	opts, err := redis.ParseURL(cfg.Valkey.URL)
	if err != nil {
		return nil, fmt.Errorf("EXCITER_VALKEY_URL is not a valid Valkey URL, such as redis://:password@valkey:6379/0: %w", err)
	}
	client := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, valkeyWait)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("connect to Valkey: %w", err)
	}
	return client, nil
}
