//go:build integration

package test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/seed"
)

// catalogParts read the catalog of the public schema, each as sorted lines, so two snapshots compare line by line.
// The toolchain container has no pg_dump, and these cover what it would print of a schema.
var catalogParts = map[string]string{
	"relations": `
		SELECT c.relname || ' ' || c.relkind::text || ' rls=' || c.relrowsecurity || ' force=' || c.relforcerowsecurity ||
		       ' acl=' || COALESCE(array_to_string(ARRAY(SELECT unnest(c.relacl)::text ORDER BY 1), ','), '')
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')`,
	"columns": `
		SELECT c.relname || '.' || a.attname || ' ' || format_type(a.atttypid, a.atttypmod) ||
		       CASE WHEN a.attnotnull THEN ' not null' ELSE '' END ||
		       COALESCE(' default ' || pg_get_expr(d.adbin, d.adrelid), '') ||
		       CASE WHEN a.attgenerated::text <> '' THEN ' generated' ELSE '' END ||
		       ' acl=' || COALESCE(array_to_string(ARRAY(SELECT unnest(a.attacl)::text ORDER BY 1), ','), '')
		FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p', 'v', 'm', 'f') AND a.attnum > 0 AND NOT a.attisdropped`,
	"constraints": `
		SELECT c.relname || ' ' || k.conname || ' ' || pg_get_constraintdef(k.oid)
		FROM pg_constraint k JOIN pg_class c ON c.oid = k.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'`,
	"indexes": `SELECT indexdef FROM pg_indexes WHERE schemaname = 'public'`,
	"functions": `
		SELECT pg_get_functiondef(p.oid) || ' acl=' || COALESCE(array_to_string(ARRAY(SELECT unnest(p.proacl)::text ORDER BY 1), ','), '')
		FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = 'public' AND p.prokind IN ('f', 'p')
		  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = p.oid AND d.deptype = 'e')`,
	"triggers": `
		SELECT pg_get_triggerdef(t.oid) || CASE t.tgenabled WHEN 'D' THEN ' disabled' ELSE '' END
		FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND NOT t.tgisinternal`,
	"policies": `
		SELECT tablename || ' ' || policyname || ' ' || permissive || ' ' || array_to_string(roles, ',') || ' ' || cmd ||
		       ' using ' || COALESCE(qual, '') || ' check ' || COALESCE(with_check, '')
		FROM pg_policies WHERE schemaname = 'public'`,
	"views": `SELECT viewname || ' ' || definition FROM pg_views WHERE schemaname = 'public'`,
	"types": `
		SELECT t.typname || ' ' || t.typtype::text || ' ' || COALESCE(array_to_string(ARRAY(
		           SELECT e.enumlabel FROM pg_enum e WHERE e.enumtypid = t.oid ORDER BY e.enumsortorder), ','), '')
		FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE n.nspname = 'public' AND t.typtype IN ('e', 'd', 'c')
		  AND NOT EXISTS (SELECT 1 FROM pg_class c WHERE c.reltype = t.oid AND c.relkind <> 'c')`,
	"extensions":         `SELECT extname FROM pg_extension`,
	"default privileges": `SELECT defaclrole::regrole::text || ' ' || defaclobjtype::text || ' ' || array_to_string(defaclacl, ',') FROM pg_default_acl`,
}

// catalogOf is the catalog's account of the schema, part by part.
func catalogOf(t *testing.T, pool *pgxpool.Pool) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for part, sql := range catalogParts {
		rows, err := pool.Query(context.Background(), `SELECT line FROM (`+sql+`) AS s(line) ORDER BY 1`)
		if err != nil {
			t.Fatalf("read the %s: %v", part, err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			out[part] = append(out[part], line)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("read the %s: %v", part, err)
		}
	}
	return out
}

// catalogDiff names the lines one snapshot has and the other lacks.
func catalogDiff(before, after map[string][]string) string {
	var b strings.Builder
	for part := range catalogParts {
		was, is := map[string]bool{}, map[string]bool{}
		for _, l := range before[part] {
			was[l] = true
		}
		for _, l := range after[part] {
			is[l] = true
		}
		for _, l := range before[part] {
			if !is[l] {
				fmt.Fprintf(&b, "%s, gone: %.300s\n", part, l)
			}
		}
		for _, l := range after[part] {
			if !was[l] {
				fmt.Fprintf(&b, "%s, new: %.300s\n", part, l)
			}
		}
	}
	return b.String()
}

// Every migration runs down, one at a time to nothing, and up again to the very same schema, so a Down is known to
// work before an incident needs it.
func TestEveryMigrationRunsDownAndUpAgain(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	provider, super, at := scratchDatabase(t, h, "exciter_migrations_down_test")
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	// Stepping back past the installation's tables needs the installation, which a first start makes, and every
	// database an incident steps back is one Exciter has started on.
	cfg := h.cfg.DB
	cfg.PrimaryURL, cfg.AdminURL, cfg.ReplicaURLs = at(cfg.PrimaryURL), at(cfg.AdminURL), nil
	cluster, err := db.Open(ctx, cfg, discard())
	if err != nil {
		t.Fatalf("open the scratch database: %v", err)
	}
	if _, err := seed.Ensure(ctx, cluster, "Round trip"); err != nil {
		t.Fatalf("start on the scratch database: %v", err)
	}
	cluster.Close()
	first := catalogOf(t, super)
	if len(first["relations"]) == 0 || len(first["policies"]) == 0 {
		t.Fatalf("the migrated schema reads %d relations and %d policies; the snapshot sees nothing", len(first["relations"]), len(first["policies"]))
	}
	top, err := provider.GetDBVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	applied := len(provider.ListSources())

	steps := 0
	for {
		v, err := provider.GetDBVersion(ctx)
		if err != nil {
			t.Fatalf("read the version after %d steps down: %v", steps, err)
		}
		if v == 0 {
			break
		}
		if _, err := provider.Down(ctx); err != nil {
			t.Fatalf("migration %d does not run down: %v", v, err)
		}
		steps++
		if steps > applied {
			t.Fatalf("still at version %d after %d steps down, more than the %d migrations", v, steps, applied)
		}
	}
	if steps != applied {
		t.Errorf("went down %d steps from %d, want one for each of the %d migrations", steps, top, applied)
	}
	if n := h.countRowsIn(t, super, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p', 'v', 'm') AND c.relname <> 'goose_db_version'`); n != 0 {
		t.Errorf("at version 0, %d tables and views are left", n)
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	if diff := catalogDiff(first, catalogOf(t, super)); diff != "" {
		t.Errorf("up, down to nothing and up again changed the schema:\n%s", diff)
	}
}

// countRowsIn is countRows on another database's pool.
func (h *harness) countRowsIn(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}
