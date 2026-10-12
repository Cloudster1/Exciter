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

func main() {
	if err := run(); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"up"}
	}
	command := args[0]

	pool, err := waitForPrimary(ctx, cfg.DB.PrimaryURL)
	if err != nil {
		return err
	}
	defer pool.Close()

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
		if len(args) < 2 {
			return errors.New("up-to needs a version, such as: migrate up-to 1")
		}
		v, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("%q is not a migration version; give its number, such as 1", args[1])
		}
		if err := goose.UpToContext(ctx, sqlDB, ".", v, allowMissing); err != nil {
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
	default:
		return fmt.Errorf("%q is not a migrate command; use up, up-to, down, status or version", command)
	}

	v, err := goose.GetDBVersionContext(ctx, sqlDB)
	if err != nil {
		return err
	}
	slog.Info("migrations applied", "command", command, "schema_version", v)
	return nil
}

func waitForPrimary(ctx context.Context, url string) (*pgxpool.Pool, error) {
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
		case <-time.After(connectPause):
		}
	}
	return nil, fmt.Errorf("the database did not answer after %d attempts: %w", connectAttempts, lastErr)
}

// stdoutLog adapts goose's logger to stdout for the status command.
type stdoutLog struct{}

func (stdoutLog) Fatalf(format string, v ...any) { fmt.Printf(format, v...); os.Exit(1) }
func (stdoutLog) Printf(format string, v ...any) { fmt.Printf(format, v...) }
