// Command migrate applies the embedded goose migrations (up, up-to N, down, status,
// version) through EXCITER_DB_PRIMARY_URL, which must name the schema's owner.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/migrations"
)

// The migrate container may start alongside Postgres rather than after it, so
// it waits this long for the database before giving up.
const (
	connectAttempts = 30
	connectPause    = time.Second
	connectTimeout  = 3 * time.Second
)

// commands are what migrate does, in the order its refusal names them.
var commands = []string{"up", "up-to", "down", "status", "version"}

// request is one run of migrate: a command, and for up-to the version to stop at.
type request struct {
	command string
	version int64
}

// parseArgs reads the command before anything connects, so a typo is refused at once and not after the wait.
func parseArgs(args []string) (request, error) {
	if len(args) == 0 {
		return request{command: "up"}, nil
	}
	r := request{command: args[0]}
	switch r.command {
	case "up", "down", "status", "version":
		return r, nil
	case "up-to":
		if len(args) < 2 {
			return r, errors.New("up-to needs a version, such as: migrate up-to 1")
		}
		v, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return r, fmt.Errorf("%q is not a migration version; give its number, such as 1", args[1])
		}
		r.version = v
		return r, nil
	}
	last := len(commands) - 1
	return r, fmt.Errorf("%q is not a migrate command; use %s or %s", r.command, strings.Join(commands[:last], ", "), commands[last])
}

func main() {
	if err := run(); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	asked, err := parseArgs(os.Args[1:])
	if err != nil {
		return err
	}
	command := asked.command
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pool, err := waitForPrimary(ctx, cfg.DB.PrimaryURL, connectPause)
	if err != nil {
		return err
	}
	defer pool.Close()

	if command == "up" || command == "up-to" || command == "down" {
		if err := migrations.CheckOwner(ctx, pool); err != nil {
			return err
		}
	}

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	// Each feature reserves its own range of migration numbers, so a branch
	// can land a lower number after a higher one is already applied.
	allowMissing := goose.WithAllowMissing()

	switch command {
	case "up":
		if err := goose.UpContext(ctx, sqlDB, ".", allowMissing); err != nil {
			return err
		}
	case "up-to":
		if err := goose.UpToContext(ctx, sqlDB, ".", asked.version, allowMissing); err != nil {
			return err
		}
	case "down":
		if err := goose.DownContext(ctx, sqlDB, "."); err != nil {
			return err
		}
	case "status":
		goose.SetLogger(stdoutLog{})
		return goose.StatusContext(ctx, sqlDB, ".")
	case "version":
		v, err := goose.GetDBVersionContext(ctx, sqlDB)
		if err != nil {
			return err
		}
		fmt.Println(v)
		return nil
	}

	v, err := goose.GetDBVersionContext(ctx, sqlDB)
	if err != nil {
		return err
	}
	slog.Info("migrations applied", "command", command, "schema_version", v)
	return nil
}

// waitForPrimary tries connectAttempts times, pause apart, and then gives up saying how often it tried.
func waitForPrimary(ctx context.Context, url string, pause time.Duration) (*pgxpool.Pool, error) {
	var lastErr error
	for i := range connectAttempts {
		pool, err := pgxpool.New(ctx, url)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
			err = pool.Ping(pingCtx)
			cancel()
			if err == nil {
				return pool, nil
			}
			pool.Close()
		}
		lastErr = err
		slog.Info("waiting for database", "attempt", i+1, "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pause):
		}
	}
	return nil, fmt.Errorf("the database did not answer after %d attempts: %w", connectAttempts, lastErr)
}

// stdoutLog adapts goose's logger to stdout for the status command.
type stdoutLog struct{}

func (stdoutLog) Fatalf(format string, v ...any) { fmt.Printf(format, v...); os.Exit(1) }
func (stdoutLog) Printf(format string, v ...any) { fmt.Printf(format, v...) }
