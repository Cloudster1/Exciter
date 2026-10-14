// Command api serves Exciter's HTTP API. Run with -healthcheck it probes the
// local readiness endpoint instead, for the container's health check.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/attachment"
	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/comment"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/freshness"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/installation"
	"github.com/cloudster1/exciter/backend/internal/label"
	"github.com/cloudster1/exciter/backend/internal/netguard"
	"github.com/cloudster1/exciter/backend/internal/notify"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/observability"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/perm"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/secret"
	"github.com/cloudster1/exciter/backend/internal/theme"
	"github.com/cloudster1/exciter/backend/internal/trace"
	"github.com/cloudster1/exciter/backend/internal/version"
	"github.com/cloudster1/exciter/backend/internal/watch"
	"github.com/cloudster1/exciter/backend/internal/workflow"
)

// Server timeouts. The write timeout outlasts the request timeout, so a slow
// handler is answered by its own deadline rather than cut off mid-reply.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 60 * time.Second
	writeTimeout      = 120 * time.Second
	idleTimeout       = 120 * time.Second
	shutdownGrace     = 25 * time.Second
	healthcheckWait   = 3 * time.Second
)

// valkeyPrefix keeps read-your-writes keys apart from anything else in Valkey.
const valkeyPrefix = "exciter:ryw:"

// devOrigins are where the web client's development server runs.
var devOrigins = []string{"http://localhost:5173", "http://127.0.0.1:5173"}

func main() {
	if err := run(); err != nil {
		slog.Error("api exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// The container health check runs the binary itself, so it needs no curl in
	// the image and no drift between what the check probes and what we serve.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		return healthcheck()
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel, cfg.IsProduction())
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	tel, err := observability.Setup(ctx, telemetryConfig(cfg, "exciter-api"), log)
	if err != nil {
		return err
	}

	cluster, err := db.Open(ctx, cfg.DB, log, db.WithQueryTracer(tel.QueryTracer))
	if err != nil {
		return err
	}
	defer cluster.Close()
	// Outside development the permission walls are the database's to keep, and a
	// role that ignores it would make every policy decoration.
	if cfg.Env != config.EnvDevelopment {
		if err := cluster.RefuseSuperuser(ctx); err != nil {
			return err
		}
	}
	if err := tel.Register(observability.NewClusterCollector(cluster)); err != nil {
		return err
	}

	valkey, err := openValkey(ctx, cfg)
	if err != nil {
		return err
	}
	if valkey != nil {
		defer valkey.Close()
	}

	store, err := fileStore(ctx, cfg, log)
	if err != nil {
		return err
	}

	accounts := auth.NewService(cluster, auth.DefaultPasswordParams(), cfg.Auth.SessionTTL)
	var box *secret.Box
	if cfg.SecretKey != nil {
		if box, err = secret.New(cfg.SecretKey); err != nil {
			return err
		}
	} else {
		log.Warn("EXCITER_SECRET_KEY is not set, so an identity provider's client secret cannot be stored")
	}
	sso := oidc.NewService(cluster, box, cfg.Auth.OIDCRedirectURL).WithHTTPClient(oidc.Backchannel(cfg.Auth.OIDCBackchannel))
	var cache *armature.Cache
	if valkey != nil {
		cache = armature.NewCache(valkey, log)
	} else {
		log.Warn("EXCITER_VALKEY_URL is not set, so every view asks Armature afresh")
	}
	allow := netguard.ParseAllow(cfg.OutboundAllow)
	armatures := armature.NewService(cluster, box, armature.NewClient(allow, cfg.ArmatureBackchannel), cache,
		armature.Options{AppURL: cfg.AppBaseURL, Allow: allow, Development: cfg.Env == config.EnvDevelopment, Log: log})
	requirements := requirement.NewService(cluster)
	links := trace.NewService(cluster)
	finder := search.NewService(cluster)
	server := &httpapi.Server{
		DB:              cluster,
		Auth:            accounts,
		Accounts:        accounts,
		OIDC:            sso,
		OIDCCallbackURL: cfg.Auth.OIDCRedirectURL,
		TrustedProxies:  cfg.TrustedProxies,
		Audit:           audit.NewService(cluster),
		AuditRetention:  cfg.RetainAudit,
		Installation:    installation.NewService(cluster),
		Themes:          theme.NewService(cluster, store),
		Projects:        project.NewService(cluster).WithSeeder(requirement.Seed),
		Perms:           perm.NewService(cluster),
		Requirements:    requirements,
		Search:          finder,
		Attributes:      attribute.NewService(cluster),
		Workflows:       workflow.NewService(cluster),
		Labels:          label.NewService(cluster),
		Trace:           links,
		Imports:         csvio.NewService(cluster, requirements, links, finder),
		Attachments:     attachment.NewService(cluster, store, requirements).WithMaxSize(cfg.UploadLimit).WithLogger(log),
		Armature:        armatures,
		Comments:        comment.NewService(cluster),
		Watches:         watch.NewService(cluster),
		Notifications:   notify.NewService(cluster),
		Log:             log,
		Telemetry:       tel,
		Fresh:           freshnessTracker(cfg, valkey, log),
		CookieName:      cfg.Auth.SessionCookie,
		Secure:          cfg.Auth.SecureCookies,
		AppBaseURL:      cfg.AppBaseURL,
		CheckOrigin:     cfg.IsProduction(),
		RequestTimeout:  cfg.RequestTimeout,
	}
	origins := cfg.CORSOrigins
	if len(origins) == 0 && !cfg.IsProduction() {
		origins = devOrigins
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Routes(origins),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	// Exactly one value is ever sent on this channel. Closing it instead would
	// make the select below read a nil and report a clean exit for a failure.
	serveErr := make(chan error, 1)
	go func() {
		build := version.Current()
		log.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env, "version", build.Version, "commit", build.Commit)
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()
	go func() {
		if err := tel.Serve(ctx); err != nil {
			log.Warn("metrics listener stopped", "error", err)
		}
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		return errors.New("http server stopped serving without being asked to")
	case <-ctx.Done():
		log.Info("shutdown requested, draining connections")
	}

	// Give in-flight requests a chance to finish before the pools close.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := tel.Shutdown(shutdownCtx); err != nil {
		log.Warn("traces still in hand were not all sent", "error", err)
	}
	log.Info("api stopped cleanly")
	return nil
}

func telemetryConfig(cfg config.Config, service string) observability.Config {
	return observability.Config{
		Service:      service,
		Env:          cfg.Env,
		MetricsAddr:  cfg.Telemetry.MetricsAddr,
		OTLPEndpoint: cfg.Telemetry.OTLPEndpoint,
		SampleRatio:  cfg.Telemetry.SampleRatio,
	}
}

// openValkey connects to EXCITER_VALKEY_URL, or returns nil when it is blank.
func openValkey(ctx context.Context, cfg config.Config) (*redis.Client, error) {
	if cfg.Valkey.URL == "" {
		return nil, nil
	}
	opts, err := redis.ParseURL(cfg.Valkey.URL)
	if err != nil {
		return nil, fmt.Errorf("EXCITER_VALKEY_URL is not a valid Valkey URL, such as redis://:password@valkey:6379/0: %w", err)
	}
	client := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, healthcheckWait)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("connect to Valkey: %w", err)
	}
	return client, nil
}

// fileStore connects to the configured bucket, making it on a fresh stack, or
// returns the store that refuses uploads with the setting to fix.
func fileStore(ctx context.Context, cfg config.Config, log *slog.Logger) (objectstore.Store, error) {
	store, err := objectstore.Open(objectstore.Config{
		Endpoint: cfg.S3.Endpoint, Bucket: cfg.S3.Bucket, AccessKey: cfg.S3.AccessKey,
		SecretKey: cfg.S3.SecretKey, Region: cfg.S3.Region, UseSSL: cfg.S3.UseSSL,
	})
	if err != nil {
		return nil, err
	}
	switch s := store.(type) {
	case objectstore.Unavailable:
		log.Warn("uploads are off: EXCITER_S3_ENDPOINT is not set")
	case *objectstore.S3Store:
		if err := s.EnsureBucket(ctx); err != nil {
			return nil, fmt.Errorf("file bucket: %w", err)
		}
	}
	return store, nil
}

// freshnessTracker keeps read-your-writes positions in Valkey, so they hold
// across api processes, or in this process when no Valkey is configured.
func freshnessTracker(cfg config.Config, valkey *redis.Client, log *slog.Logger) httpapi.Freshness {
	if valkey == nil {
		if len(cfg.DB.ReplicaURLs) > 0 {
			log.Warn("EXCITER_VALKEY_URL is not set, so read-your-writes holds within this process only; run one api process or set it")
		}
		return freshness.NewMemoryTracker(cfg.DB.ReadYourWritesTTL)
	}
	return freshness.NewValkeyTracker(valkey, cfg.DB.ReadYourWritesTTL, valkeyPrefix)
}

// healthcheck probes the local readiness endpoint and fails unless it is ready,
// which is exactly what the container runtime wants.
func healthcheck() error {
	addr := os.Getenv("EXCITER_HTTP_ADDR")
	if addr == "" {
		addr = config.DefaultHTTPAddr
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	client := &http.Client{Timeout: healthcheckWait}
	resp, err := client.Get("http://" + addr + "/readyz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness returned %s", resp.Status)
	}
	return nil
}
