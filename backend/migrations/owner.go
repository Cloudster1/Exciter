package migrations

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Querier is what CheckOwner asks through: a pool, a connection or a transaction.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// CheckOwner refuses a migrating role that row level security would wall: the permission functions run as their
// owner, through FORCE, and with no policy naming it every administrator check would answer false (#271).
func CheckOwner(ctx context.Context, q Querier) error {
	var role string
	var exempt bool
	err := q.QueryRow(ctx, `SELECT rolname, rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`).
		Scan(&role, &exempt)
	if err != nil {
		return fmt.Errorf("read what the migrating role may do: %w", err)
	}
	if !exempt {
		name := pgx.Identifier{role}.Sanitize()
		return fmt.Errorf("the migrations run as %s, which is neither a superuser nor BYPASSRLS, so the permission "+
			"checks it owns would find nobody. Run ALTER ROLE %s BYPASSRLS as a superuser, then migrate again", name, name)
	}
	return nil
}
