//go:build integration

package test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/cloudster1/exciter/backend/migrations"
)

const (
	plainOwner         = "exciter_plain_owner_test"
	plainOwnerPassword = "plain-owner-test"
)

// An owner that is no superuser, as CNPG makes it, owns the permission functions; without BYPASSRLS they read the
// walled tables through FORCE and find nobody, so an owner account may not save a provider (#271). The migrate
// command refuses such an owner, and once it is BYPASSRLS the database lets the owner account through.
func TestTheMigrationsNeedAnOwnerExemptFromRowSecurity(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// Registered before the scratch database, so it is dropped after the database it owns.
	dropRole := func() { h.cleanupExec(t, h.super, "DROP ROLE IF EXISTS "+plainOwner) }
	dropRole()
	t.Cleanup(dropRole)
	if _, err := h.super.Exec(ctx, "CREATE ROLE "+plainOwner+" LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD '"+plainOwnerPassword+"'"); err != nil {
		t.Fatalf("create the owner: %v", err)
	}
	_, super, at := scratchDatabase(t, h, "exciter_migration_owner_test")
	if _, err := super.Exec(ctx, "ALTER DATABASE exciter_migration_owner_test OWNER TO "+plainOwner); err != nil {
		t.Fatalf("hand the database to the owner: %v", err)
	}

	u, err := url.Parse(at(os.Getenv(envSuperuser)))
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(plainOwner, plainOwnerPassword)
	owner, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(owner.Close)

	if err := migrations.CheckOwner(ctx, owner); err == nil || !strings.Contains(err.Error(), `ALTER ROLE "`+plainOwner+`" BYPASSRLS`) {
		t.Fatalf("the check let an owner without BYPASSRLS through, or did not say what to grant: %v", err)
	}

	sqlDB := stdlib.OpenDBFromPool(owner)
	t.Cleanup(func() { _ = sqlDB.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS, goose.WithAllowOutofOrder(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate as the owner: %v", err)
	}
	var admin string
	if err := super.QueryRow(ctx, `
		INSERT INTO app_user (email, name, role) VALUES ('owner@migration-owner.test', 'Owner', 'member')
		RETURNING id::text`).Scan(&admin); err != nil {
		t.Fatalf("make an owner account: %v", err)
	}
	if _, err := super.Exec(ctx, `INSERT INTO role_assignment (role, user_id) VALUES ('administrator', $1)`, admin); err != nil {
		t.Fatalf("make the owner an administrator: %v", err)
	}
	app, err := pgxpool.New(ctx, at(os.Getenv(envPrimary)))
	if err != nil {
		t.Fatalf("connect as exciter_app: %v", err)
	}
	t.Cleanup(app.Close)
	saveProvider := func() error {
		return pgx.BeginFunc(ctx, app, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true)`, admin); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO oidc_provider (issuer, client_id) VALUES ('https://sso.example.com', 'exciter')`)
			if err == nil {
				err = errRolledBack
			}
			return err
		})
	}

	var pgErr *pgconn.PgError
	if err := saveProvider(); !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("under an owner without BYPASSRLS the owner account saved a provider, or failed otherwise: %v", err)
	}

	if _, err := h.super.Exec(ctx, "ALTER ROLE "+plainOwner+" BYPASSRLS"); err != nil {
		t.Fatal(err)
	}
	if err := migrations.CheckOwner(ctx, owner); err != nil {
		t.Fatalf("the check refused an owner with BYPASSRLS: %v", err)
	}
	if err := saveProvider(); !errors.Is(err, errRolledBack) {
		t.Fatalf("under an owner with BYPASSRLS the owner account may not save a provider: %v", err)
	}
}

// errRolledBack ends a transaction that did what it tried, so the scratch database keeps nothing of it.
var errRolledBack = errors.New("rolled back on purpose")
