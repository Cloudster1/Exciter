package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// setConfigSQL binds a row level security variable for the current transaction.
// SET LOCAL takes no bind parameter; set_config does, and its true makes it local.
const setConfigSQL = `SELECT set_config($1, $2, true)`

// currentLSNSQL reads the primary's current write ahead log insert position.
const currentLSNSQL = `SELECT pg_current_wal_insert_lsn()::text`

// Write runs fn in a read-write transaction on the primary and returns the WAL
// position reflecting it, or its refusal, for PinLSN to keep later reads fresh.
func (c *Cluster) Write(ctx context.Context, fn func(context.Context, DBTX) error) (LSN, error) {
	err := c.inTx(ctx, c.primary, pgx.TxOptions{AccessMode: pgx.ReadWrite}, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, tx)
	})
	if err != nil {
		return c.refusedLSN(ctx), err
	}
	return c.committedLSN(ctx, c.primary)
}

// refusedLSN is where the primary stood when a write was refused, or zero,
// which pins nothing, when it cannot say.
func (c *Cluster) refusedLSN(ctx context.Context) LSN {
	// A refusal such as an edit conflict tells the caller of somebody else's
	// newer write, and the read they make next to see it must not miss it.
	lsn, err := c.committedLSN(ctx, c.primary)
	if err != nil {
		return 0
	}
	return lsn
}

// committedLSN is read after the commit, never inside the transaction: the
// commit record lands past any position read before it.
func (c *Cluster) committedLSN(ctx context.Context, pool *pgxpool.Pool) (LSN, error) {
	var text string
	if err := pool.QueryRow(ctx, currentLSNSQL).Scan(&text); err != nil {
		return 0, fmt.Errorf("read wal position: %w", err)
	}
	return ParseLSN(text)
}

// readOnly keeps one snapshot for a whole read: with one per statement, a replica
// replaying in between answers with a requirement newer than its own permissions.
var readOnly = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}

// Read runs fn in a read-only transaction on a healthy, caught-up replica when
// there is one, otherwise on the primary.
func (c *Cluster) Read(ctx context.Context, fn func(context.Context, DBTX) error) error {
	var on beginner = c.primary
	if conn := c.readConn(ctx); conn != nil {
		defer conn.Release()
		on = conn
	}
	return c.inTx(ctx, on, readOnly, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, tx)
	})
}

// beginner is a pool or one connection taken from it.
type beginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// ReadPrimary is Read pinned to the primary.
func (c *Cluster) ReadPrimary(ctx context.Context, fn func(context.Context, DBTX) error) error {
	return c.Read(PinPrimary(ctx), fn)
}

// inTx opens a transaction, names who it acts for, runs fn and commits.
func (c *Cluster) inTx(
	ctx context.Context,
	on beginner,
	opts pgx.TxOptions,
	fn func(context.Context, pgx.Tx) error,
) error {
	tx, err := on.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Rollback is a no-op once the transaction has committed, so this is safe
	// on every path including panics.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// The permission policies ask who acts; a transaction acting for nobody reaches nothing they wall.
	if user, ok := UserFrom(ctx); ok {
		if _, err := tx.Exec(ctx, setConfigSQL, UserVar, user.String()); err != nil {
			return fmt.Errorf("apply the acting person: %w", err)
		}
		if projects, only := ProjectsFrom(ctx); only {
			if _, err := tx.Exec(ctx, setConfigSQL, TokenProjectsVar, projectsSetting(projects)); err != nil {
				return fmt.Errorf("apply the token's projects: %w", err)
			}
		}
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// WriteAdmin runs fn on the primary as the role exempt from row level security,
// for work that runs before anybody acts, such as signing in.
func (c *Cluster) WriteAdmin(ctx context.Context, fn func(context.Context, DBTX) error) (LSN, error) {
	err := c.inTx(ctx, c.admin, pgx.TxOptions{AccessMode: pgx.ReadWrite}, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, tx)
	})
	if err != nil {
		return 0, err
	}
	return c.committedLSN(ctx, c.admin)
}

// ReadAdmin is WriteAdmin read-only. It reads the primary: its callers are
// authentication paths, where a stale answer is worse than a dearer one.
func (c *Cluster) ReadAdmin(ctx context.Context, fn func(context.Context, DBTX) error) error {
	return c.inTx(ctx, c.admin, readOnly, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, tx)
	})
}
