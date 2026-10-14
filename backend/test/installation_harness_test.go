//go:build integration

package test

import (
	"context"
	"crypto/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/installation"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// Every test works in the database's one installation (#90) and keeps apart from the others by what it makes:
// people with addresses of their own, projects under keys of its own, and names nobody else uses.

var (
	installationOnce sync.Once
	theInstallation  tenant.Org
	installationErr  error
)

// installation is the organization the stack's seed made, under the address it always gives it.
func (h *harness) installation(t *testing.T) tenant.Org {
	t.Helper()
	installationOnce.Do(func() {
		installationErr = h.super.QueryRow(context.Background(),
			`SELECT id, slug FROM org WHERE slug = $1`, installation.Slug,
		).Scan(&theInstallation.ID, &theInstallation.Slug)
	})
	if installationErr != nil {
		t.Fatalf("the stack has no installation (%v). Start it with make stack-up, whose seed makes one.", installationErr)
	}
	return theInstallation
}

// markLength is how many characters a test's mark adds to a key, leaving up to four for the key's own letters.
const markLength = 6

var marks sync.Map

// mark is the top-level test's own: its subtests share it, so a key made in one is the key in the next.
func mark(t *testing.T) string {
	name, _, _ := strings.Cut(t.Name(), "/")
	// rand.Text's letters, A to Z and 2 to 7, are all a key may hold.
	got, _ := marks.LoadOrStore(name, rand.Text()[:markLength])
	return got.(string)
}

// projectKey is a project key no other test, run or person draws: what the test calls WAL is WAL and its mark.
func projectKey(t *testing.T, prefix string) string {
	t.Helper()
	k := prefix + mark(t)
	if !project.ValidKey(k) {
		t.Fatalf("%q makes the key %q, which is no project key; keep a test's prefixes to four letters", prefix, k)
	}
	return k
}

// sweepProjects deletes the projects under the test's keys, current or former, as the superuser past every rule,
// newest first: a requirement moved into a later project keeps revisions naming the earlier one's types.
func (h *harness) sweepProjects(t *testing.T) {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT id FROM project WHERE key LIKE '%' || $1
		   OR id IN (SELECT project_id FROM project_key_alias WHERE key LIKE '%' || $1)
		ORDER BY created_at DESC`, mark(t))
	if err != nil {
		t.Errorf("find the test's projects to remove: %v", err)
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		t.Errorf("find the test's projects to remove: %v", err)
		return
	}
	for _, id := range ids {
		h.cleanupExec(t, h.super, `DELETE FROM project WHERE id = $1`, id)
	}
}

// ours keeps the keys under the test's mark, since a list the whole installation reads holds other tests' too.
func ours(t *testing.T, keys []string) []string {
	t.Helper()
	var out []string
	for _, k := range keys {
		if strings.HasSuffix(strings.SplitN(k, "-", 2)[0], mark(t)) {
			out = append(out, k)
		}
	}
	return out
}

// withID is the row of a list answer with the id, or nil, for lists other tests' rows share.
func withID(rows []any, id uuid.UUID) map[string]any {
	for _, row := range rows {
		if each, ok := row.(map[string]any); ok && each["id"] == id.String() {
			return each
		}
	}
	return nil
}

// keepLinkTypes puts the link types for all projects back as they were once the test ends: the ones it made go,
// the ones it changed or removed return. Its projects go first, so no link of theirs holds a type back.
func (h *harness) keepLinkTypes(t *testing.T) {
	t.Helper()
	inst := h.installation(t)
	var saved []byte
	if err := h.super.QueryRow(context.Background(),
		`SELECT coalesce(jsonb_agg(l), '[]') FROM link_type l WHERE org_id = $1 AND project_id IS NULL`, inst.ID).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.sweepProjects(t)
		h.cleanupExec(t, h.super, `
			DELETE FROM link_type WHERE org_id = $1 AND project_id IS NULL
			   AND id NOT IN (SELECT (e->>'id')::uuid FROM jsonb_array_elements($2::jsonb) e)`, inst.ID, saved)
		h.cleanupExec(t, h.super, `
			UPDATE link_type l SET name = s.name, forward = s.forward, backward = s.backward, acyclic = s.acyclic,
			       suspect_end = s.suspect_end, counts = s.counts, retired = s.retired
			FROM jsonb_populate_recordset(NULL::link_type, $1::jsonb) s WHERE l.id = s.id`, saved)
		h.cleanupExec(t, h.super, `
			INSERT INTO link_type SELECT * FROM jsonb_populate_recordset(NULL::link_type, $1::jsonb) ON CONFLICT (id) DO NOTHING`, saved)
	})
}

// keepDefaultTheme puts the default theme for everyone back once the test ends.
func (h *harness) keepDefaultTheme(t *testing.T) {
	t.Helper()
	inst := h.installation(t)
	var saved *uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT default_theme_id FROM org WHERE id = $1`, inst.ID).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.cleanupExec(t, h.super, `UPDATE org SET default_theme_id = $2 WHERE id = $1`, inst.ID, saved)
	})
}

// keepArmature starts the test with the installation connected to no Armature, as each connects it to a stub of its
// own, and puts the connection and its members' tokens back once the test ends.
func (h *harness) keepArmature(t *testing.T) {
	t.Helper()
	inst := h.installation(t)
	var connection, tokens []byte
	if err := h.super.QueryRow(context.Background(), `
		SELECT (SELECT coalesce(jsonb_agg(c), '[]') FROM armature_connection c WHERE org_id = $1),
		       (SELECT coalesce(jsonb_agg(k), '[]') FROM armature_token k WHERE org_id = $1)`, inst.ID).Scan(&connection, &tokens); err != nil {
		t.Fatal(err)
	}
	h.cleanupExec(t, h.super, `DELETE FROM armature_connection WHERE org_id = $1`, inst.ID)
	h.settle(t)
	t.Cleanup(func() {
		h.cleanupExec(t, h.super, `DELETE FROM armature_connection WHERE org_id = $1`, inst.ID)
		h.cleanupExec(t, h.super, `INSERT INTO armature_connection SELECT * FROM jsonb_populate_recordset(NULL::armature_connection, $1::jsonb)`, connection)
		h.cleanupExec(t, h.super, `
			INSERT INTO armature_token SELECT * FROM jsonb_populate_recordset(NULL::armature_token, $1::jsonb) k
			WHERE EXISTS (SELECT 1 FROM app_user WHERE id = k.user_id)`, tokens)
	})
}

// singleSignOn hands the test the installation with no identity provider, group roles or waiting people, and puts
// them back once it ends, with the standing and groups of the realm's people, which the seed lets in and a test's
// sign-ins change.
func (h *harness) singleSignOn(t *testing.T) tenant.Org {
	t.Helper()
	inst := h.installation(t)
	ctx := context.Background()
	people := []string{aliceEmail, bobEmail, carolEmail, daveEmail}
	var provider, roles, waiting, groups, members, memberships, accounts []byte
	if err := h.super.QueryRow(ctx, `
		SELECT (SELECT coalesce(jsonb_agg(p), '[]') FROM oidc_provider p WHERE org_id = $1),
		       (SELECT coalesce(jsonb_agg(r), '[]') FROM oidc_group_role r WHERE org_id = $1),
		       (SELECT coalesce(jsonb_agg(w), '[]') FROM org_join_request w WHERE org_id = $1),
		       (SELECT coalesce(jsonb_agg(g.id), '[]') FROM groups g WHERE org_id = $1),
		       (SELECT coalesce(jsonb_agg(m), '[]') FROM org_member m JOIN app_user u ON u.id = m.user_id
		         WHERE m.org_id = $1 AND u.email = ANY($2)),
		       (SELECT coalesce(jsonb_agg(gm), '[]') FROM group_member gm JOIN app_user u ON u.id = gm.user_id
		         WHERE gm.org_id = $1 AND u.email = ANY($2)),
		       (SELECT coalesce(jsonb_agg(u.email), '[]') FROM app_user u WHERE u.email = ANY($2))`, inst.ID, people,
	).Scan(&provider, &roles, &waiting, &groups, &members, &memberships, &accounts); err != nil {
		t.Fatal(err)
	}
	h.cleanupExec(t, h.super, `DELETE FROM oidc_provider WHERE org_id = $1`, inst.ID)
	h.cleanupExec(t, h.super, `DELETE FROM org_join_request WHERE org_id = $1`, inst.ID)
	h.started(t)
	h.settle(t)
	t.Cleanup(func() {
		h.cleanupExec(t, h.super, `DELETE FROM groups WHERE org_id = $1 AND id NOT IN (SELECT (e #>> '{}')::uuid FROM jsonb_array_elements($2::jsonb) e)`, inst.ID, groups)
		h.cleanupExec(t, h.super, `DELETE FROM oidc_provider WHERE org_id = $1`, inst.ID)
		h.cleanupExec(t, h.super, `INSERT INTO oidc_provider SELECT * FROM jsonb_populate_recordset(NULL::oidc_provider, $1::jsonb)`, provider)
		h.cleanupExec(t, h.super, `INSERT INTO oidc_group_role SELECT * FROM jsonb_populate_recordset(NULL::oidc_group_role, $1::jsonb)`, roles)
		h.cleanupExec(t, h.super, `DELETE FROM org_join_request WHERE org_id = $1`, inst.ID)
		h.cleanupExec(t, h.super, `
			INSERT INTO org_join_request SELECT * FROM jsonb_populate_recordset(NULL::org_join_request, $1::jsonb) w
			WHERE EXISTS (SELECT 1 FROM app_user WHERE id = w.user_id)`, waiting)
		// Updated in place rather than taken away and given back, which would take their grants and tokens along.
		h.cleanupExec(t, h.super, `
			UPDATE org_member m SET org_role = s.org_role, role_source = s.role_source
			FROM jsonb_populate_recordset(NULL::org_member, $1::jsonb) s WHERE m.org_id = s.org_id AND m.user_id = s.user_id`, members)
		h.cleanupExec(t, h.super, `
			DELETE FROM org_member m USING app_user u WHERE u.id = m.user_id AND m.org_id = $1 AND u.email = ANY($2)
			   AND m.user_id NOT IN (SELECT (e->>'user_id')::uuid FROM jsonb_array_elements($3::jsonb) e)`, inst.ID, people, members)
		h.cleanupExec(t, h.super, `
			INSERT INTO org_member SELECT * FROM jsonb_populate_recordset(NULL::org_member, $1::jsonb) s
			WHERE EXISTS (SELECT 1 FROM app_user WHERE id = s.user_id) ON CONFLICT DO NOTHING`, members)
		h.cleanupExec(t, h.super, `
			DELETE FROM group_member gm USING app_user u WHERE u.id = gm.user_id AND gm.org_id = $1 AND u.email = ANY($2)
			   AND (gm.group_id, gm.user_id) NOT IN (SELECT (e->>'group_id')::uuid, (e->>'user_id')::uuid FROM jsonb_array_elements($3::jsonb) e)`,
			inst.ID, people, memberships)
		h.cleanupExec(t, h.super, `
			INSERT INTO group_member SELECT * FROM jsonb_populate_recordset(NULL::group_member, $1::jsonb) s
			WHERE EXISTS (SELECT 1 FROM groups WHERE id = s.group_id) ON CONFLICT DO NOTHING`, memberships)
		// A sign-in or a letting in made the account of somebody the realm has and the seed had not.
		h.cleanupExec(t, h.super, `
			DELETE FROM app_user WHERE email = ANY($1)
			   AND email::text NOT IN (SELECT e #>> '{}' FROM jsonb_array_elements($2::jsonb) e)`, people, accounts)
		h.settle(t)
	})
	return inst
}

// outsider is an account that belongs to no installation, as somebody waiting to be let in has.
func (h *harness) outsider(t *testing.T, label string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	email := label + "-" + h.suffix() + "@example.test"
	if err := h.super.QueryRow(context.Background(), `INSERT INTO app_user (email, name) VALUES ($1, $2) RETURNING id`, email, label).Scan(&id); err != nil {
		t.Fatalf("create %s: %v", email, err)
	}
	h.later(t, id)
	return id
}

var (
	peopleMu sync.Mutex
	people   = map[string][]uuid.UUID{}
)

// later notes a person the test made, for forgetPeople to remove when the whole test ends.
func (h *harness) later(t *testing.T, id uuid.UUID) {
	name, _, _ := strings.Cut(t.Name(), "/")
	peopleMu.Lock()
	defer peopleMu.Unlock()
	people[name] = append(people[name], id)
}

// forgetPeople removes the people the test made, which newHarness asks for once its projects are gone.
func (h *harness) forgetPeople(t *testing.T) {
	t.Helper()
	name, _, _ := strings.Cut(t.Name(), "/")
	peopleMu.Lock()
	ids := people[name]
	delete(people, name)
	peopleMu.Unlock()
	if len(ids) > 0 {
		h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE id = ANY($1)`, ids)
	}
}

var starts sync.Map

// started is when the top-level test began, by the database's clock, so it reads its own entries of records the
// whole installation shares; the first call notes it.
func (h *harness) started(t *testing.T) time.Time {
	t.Helper()
	name, _, _ := strings.Cut(t.Name(), "/")
	if at, ok := starts.Load(name); ok {
		return at.(time.Time)
	}
	var now time.Time
	if err := h.super.QueryRow(context.Background(), `SELECT now()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	at, _ := starts.LoadOrStore(name, now)
	return at.(time.Time)
}

// keepInstallationName puts the installation's name back once the test ends.
func (h *harness) keepInstallationName(t *testing.T) {
	t.Helper()
	inst := h.installation(t)
	var name string
	if err := h.super.QueryRow(context.Background(), `SELECT name FROM org WHERE id = $1`, inst.ID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.cleanupExec(t, h.super, `UPDATE org SET name = $2 WHERE id = $1`, inst.ID, name) })
}

// stubTenant is the Armature stub's tenant of the test's own, which its tokens name, so a test never meets another's
// issues or remote links in the stub.
func stubTenant(t *testing.T) string {
	return "exciter-" + strings.ToLower(mark(t))
}
