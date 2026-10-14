//go:build integration

package test

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// beforeTheSchemaLosesTheOrganization is the last migration before 00410, which drops the tenant policies (#106).
const beforeTheSchemaLosesTheOrganization = 403

// What names the organization until #108 drops the column: its own table's functions, the session's column, the
// one-row keys and foreign keys of the organization, the provider and the connection, and objects' keys (#107).
var (
	organizationsOwnFunctions = []string{"current_org_id", "link_type_seed", "session_keeps_its_opening"}
	organizationsOwnKeys      = []string{"armature_connection_pkey", "oidc_provider_pkey", "attachment_tombstone_own_prefix"}
)

// Nothing in the schema asks which organization a row is of: no policy, function, key or index names org_id or
// current_org_id(), apart from what the organization's own table and row keep until #108.
func TestNothingInTheSchemaAsksForTheOrganization(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	names := func(sql string, args ...any) []string {
		t.Helper()
		rows, err := h.super.Query(ctx, sql, args...)
		if err != nil {
			t.Fatal(err)
		}
		found, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return found
	}
	asks := `'\morg_id\M|current_org_id'`
	for what, found := range map[string][]string{
		"policies": names(`SELECT tablename || '.' || policyname FROM pg_policies
			WHERE schemaname = 'public' AND coalesce(qual, '') || coalesce(with_check, '') ~ ` + asks),
		"functions": names(`SELECT proname FROM pg_proc
			WHERE pronamespace = 'public'::regnamespace AND prosrc ~ `+asks+` AND proname <> ALL ($1)`, organizationsOwnFunctions),
		"keys": names(`SELECT conname FROM pg_constraint c
			WHERE connamespace = 'public'::regnamespace AND conname <> ALL ($1) AND contype IN ('p', 'u', 'f', 'x', 'c')
			  AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey) AND a.attname = 'org_id')
			  AND NOT (c.contype = 'f' AND c.conkey = ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid = c.conrelid AND attname = 'org_id')]
			           AND c.confrelid IN ('org'::regclass, 'oidc_provider'::regclass, 'armature_connection'::regclass))`, organizationsOwnKeys),
		"indexes": names(`SELECT c.relname FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
			WHERE c.relnamespace = 'public'::regnamespace
			  AND NOT EXISTS (SELECT 1 FROM pg_constraint k WHERE k.conindid = i.indexrelid)
			  AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = i.indrelid AND a.attnum = ANY (i.indkey) AND a.attname = 'org_id')`),
	} {
		if len(found) > 0 {
			t.Errorf("%d %s still ask for the organization: %s", len(found), what, strings.Join(found, ", "))
		}
	}
	// A name kept for #108 that no longer names the organization would hide a slip; the list has to stay true.
	for _, name := range organizationsOwnFunctions {
		if len(names(`SELECT proname FROM pg_proc WHERE pronamespace = 'public'::regnamespace AND proname = $1 AND prosrc ~ `+asks, name)) == 0 {
			t.Errorf("%s is kept for #108 but no longer names the organization; take it off the list", name)
		}
	}
}

// populate gives a scratch database at the step before an installation of every kind of row the triggers make and
// guard, written as the superuser with the organization named as it was then.
func populate(t *testing.T, super *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	var org, owner, member, project, token uuid.UUID
	steps := []struct {
		sql  string
		args []any
		into []any
	}{
		{`INSERT INTO org (slug, name) VALUES ('exciter', 'Exciter') RETURNING id`, nil, []any{&org}},
		{`INSERT INTO app_user (email, name, password_hash) VALUES ('owner@example.test', 'Olive', 'x') RETURNING id`, nil, []any{&owner}},
		{`INSERT INTO app_user (email, name) VALUES ('member@example.test', 'Max') RETURNING id`, nil, []any{&member}},
	}
	for _, s := range steps {
		if err := super.QueryRow(ctx, s.sql, s.args...).Scan(s.into...); err != nil {
			t.Fatalf("%s: %v", s.sql, err)
		}
	}
	if err := super.QueryRow(ctx, `INSERT INTO project (org_id, key, name, created_by) VALUES ($1, 'POP', 'Populated', $2) RETURNING id`,
		org, owner).Scan(&project); err != nil {
		t.Fatalf("make the project: %v", err)
	}
	var first, second uuid.UUID
	for _, sql := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'owner'), ($1, $3, 'member')`, []any{org, owner, member}},
		{`INSERT INTO groups (org_id, name) VALUES ($1, 'Reviewers')`, []any{org}},
		{`INSERT INTO group_member (org_id, group_id, user_id) SELECT $1, id, $2 FROM groups`, []any{org, member}},
		{`INSERT INTO theme (org_id, owner_id, name, shared) VALUES ($1, $2, 'House', true)`, []any{org, owner}},
		{`INSERT INTO user_theme (org_id, user_id, theme_id) SELECT $1, $2, id FROM theme`, []any{org, member}},
		{`UPDATE org SET default_theme_id = (SELECT id FROM theme)`, nil},
		{`INSERT INTO audit_log (org_id, actor_user_id, action, target_type) VALUES ($1, $2, 'audit.exported', 'org')`, []any{org, owner}},
		{`INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, 'test.populated', '{}')`, []any{org}},
	} {
		if _, err := super.Exec(ctx, sql.q, sql.args...); err != nil {
			t.Fatalf("%s: %v", sql.q, err)
		}
	}
	for _, into := range []*uuid.UUID{&first, &second} {
		if err := super.QueryRow(ctx, `INSERT INTO requirement (org_id, project_id, title, statement, normative_hash, created_by)
			VALUES ($1, $2, 'Populated', '{"type":"doc"}', 'h', $3) RETURNING id`, org, project, owner).Scan(into); err != nil {
			t.Fatalf("write a requirement: %v", err)
		}
		if _, err := super.Exec(ctx, `INSERT INTO requirement_revision (org_id, requirement_id, number, title, statement, hash, created_by)
			VALUES ($1, $2, 1, 'Populated', '{"type":"doc"}', 'h', $3)`, org, *into, owner); err != nil {
			t.Fatalf("revise it: %v", err)
		}
	}
	for _, sql := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO trace_link (org_id, type_id, source_id, target_id, created_by)
			SELECT $1, id, $2, $3, $4 FROM link_type WHERE key = 'derives'`, []any{org, first, second, owner}},
		{`INSERT INTO requirement_label (org_id, requirement_id, name, created_by) VALUES ($1, $2, 'populated', $3)`, []any{org, first, owner}},
		{`UPDATE project SET key = 'POPX' WHERE id = $1`, []any{project}},
	} {
		if _, err := super.Exec(ctx, sql.q, sql.args...); err != nil {
			t.Fatalf("%s: %v", sql.q, err)
		}
	}
	hash := sha256.Sum256([]byte(uuid.NewString()))
	if err := super.QueryRow(ctx, `INSERT INTO api_token (org_id, user_id, name, token_hash, scopes) VALUES ($1, $2, 'nightly', $3, '{read}') RETURNING id`,
		org, owner, hash[:]).Scan(&token); err != nil {
		t.Fatalf("make a token: %v", err)
	}
}

// rowCounts is how many rows each table of the schema holds.
func rowCounts(t *testing.T, super *pgxpool.Pool) map[string]int64 {
	t.Helper()
	ctx := context.Background()
	rows, err := super.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'goose_db_version'`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int64{}
	for _, table := range tables {
		var n int64
		if err := super.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{table}.Sanitize()).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[table] = n
	}
	return out
}

// schemaOf is every policy, key, index, trigger and function of the schema as the catalog prints them.
func schemaOf(t *testing.T, super *pgxpool.Pool) []string {
	t.Helper()
	rows, err := super.Query(context.Background(), `
		SELECT 'policy ' || tablename || ' ' || policyname || ' ' || permissive || ' ' || array_to_string(roles, ',') || ' ' || cmd
		       || ' ' || coalesce(qual, '') || ' ' || coalesce(with_check, '') FROM pg_policies WHERE schemaname = 'public'
		UNION ALL SELECT 'constraint ' || conrelid::regclass || ' ' || conname || ' ' || pg_get_constraintdef(oid)
		FROM pg_constraint WHERE connamespace = 'public'::regnamespace
		UNION ALL SELECT 'index ' || pg_get_indexdef(indexrelid) FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
		WHERE c.relnamespace = 'public'::regnamespace
		UNION ALL SELECT 'trigger ' || pg_get_triggerdef(oid) FROM pg_trigger WHERE NOT tgisinternal
		UNION ALL SELECT 'function ' || pg_get_functiondef(oid) || coalesce(proacl::text, '') FROM pg_proc
		WHERE pronamespace = 'public'::regnamespace AND prokind = 'f'
		UNION ALL SELECT 'default ' || table_name || ' ' || column_name || ' ' || coalesce(column_default, '')
		FROM information_schema.columns WHERE table_schema = 'public'
		UNION ALL SELECT 'grant ' || table_name || ' ' || grantee || ' ' || privilege_type
		FROM information_schema.role_table_grants WHERE table_schema = 'public'
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// An installation populated before the schema lost the organization keeps every row through the upgrade, and
// stepping back gives the schema of before, with the rows still there, from which it upgrades again.
func TestTheUpgradeKeepsEveryRowAndStepsBack(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	provider, super, _ := scratchDatabase(t, h, "exciter_schema_without_organization_test")
	if _, err := provider.UpTo(ctx, beforeTheSchemaLosesTheOrganization); err != nil {
		t.Fatalf("migrate to %d: %v", beforeTheSchemaLosesTheOrganization, err)
	}
	populate(t, super)
	before, schemaBefore := rowCounts(t, super), schemaOf(t, super)

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("upgrade a populated installation: %v", err)
	}
	if after := rowCounts(t, super); !sameCounts(t, "the upgrade", before, after) {
		return
	}
	var key string
	if err := super.QueryRow(ctx, `SELECT key FROM project`).Scan(&key); err != nil || key != "POPX" {
		t.Errorf("the project is %q (%v) after the upgrade", key, err)
	}

	if _, err := provider.DownTo(ctx, beforeTheSchemaLosesTheOrganization); err != nil {
		t.Fatalf("step back to %d: %v", beforeTheSchemaLosesTheOrganization, err)
	}
	if schemaAfter := schemaOf(t, super); strings.Join(schemaAfter, "\n") != strings.Join(schemaBefore, "\n") {
		t.Errorf("stepping back gives another schema:\n%s", firstDifference(schemaBefore, schemaAfter))
	}
	sameCounts(t, "stepping back", before, rowCounts(t, super))
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("upgrade again after stepping back: %v", err)
	}
}

func sameCounts(t *testing.T, what string, before, after map[string]int64) bool {
	t.Helper()
	same := true
	for table, n := range before {
		if after[table] != n {
			t.Errorf("%s left %d rows in %s, want %d", what, after[table], table, n)
			same = false
		}
	}
	return same
}

// firstDifference names where two sorted listings part, for a readable failure.
func firstDifference(a, b []string) string {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return "before: " + a[i] + "\nafter:  " + b[i]
		}
	}
	return "one listing is longer than the other"
}
