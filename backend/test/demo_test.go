//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/demo"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/risk"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// The suite demo (#270): a small snapshot built and linked against the stubs, the endpoints' refusals, and the
// demo's rows guarded straight through SQL.

// demoFixture is a snapshot whose Exciter issues the armature-stub's EXC project and the stator-stub's EXC space
// know, beside one of Armature's that neither has.
func demoFixture() demo.Snapshot {
	story := func(s string) string { return "## User story\n\nAs a tester, I want " + s + ".\n" }
	return demo.Snapshot{Tools: []demo.Tool{
		{Repo: "Cloudster1/Armature", Key: "ARM", Name: "Armature", Taken: "2026-10-20", Issues: []demo.Issue{
			{Number: 9, Title: "Boards", Closed: true, Body: story("boards")},
		}},
		{Repo: "Cloudster1/Exciter", Key: "EXC", Name: "Exciter", Taken: "2026-10-20", Issues: []demo.Issue{
			{Number: 1, Title: "Projects", Closed: true, Milestone: "M2 Armature integration",
				Body: story("projects") + "\n## Acceptance criteria\n\n- [x] **Making one:** an administrator makes a project.\n- [x] **Keys:** each has a key.\n"},
			{Number: 2, Title: "Requirements", Milestone: "M2 Armature integration", Body: story("requirements") + "\n## Depends on\n\n- #1\n"},
			{Number: 3, Title: "Trace links", Closed: true, Milestone: "Backlog", Body: story("links") + "\nDepends on: Cloudster1/Armature#9.\n"},
			{Number: 4, Title: "Not in Armature", Body: story("nothing")},
		}},
	}}
}

func (h *harness) demoService(t *testing.T) *demo.Service {
	t.Helper()
	return demo.NewService(h.cluster, project.NewService(h.cluster).WithSeeder(risk.SeedProject),
		requirement.NewService(h.cluster).WithCoverage(trace.CoverageJudge{}), trace.NewService(h.cluster),
		baseline.NewService(h.cluster), h.armature(t), h.stator(t))
}

// keepSuiteDemo starts the test with no suite demo and puts back the one there was, with its projects' records.
func (h *harness) keepSuiteDemo(t *testing.T) {
	t.Helper()
	var row []byte
	if err := h.super.QueryRow(context.Background(), `SELECT coalesce(jsonb_agg(d), '[]') FROM suite_demo d`).Scan(&row); err != nil {
		t.Fatal(err)
	}
	h.cleanupExec(t, h.super, `DELETE FROM suite_demo`)
	t.Cleanup(func() {
		h.cleanupExec(t, h.super, `DELETE FROM suite_demo`)
		h.cleanupExec(t, h.super, `INSERT INTO suite_demo SELECT * FROM jsonb_populate_recordset(NULL::suite_demo, $1::jsonb)`, row)
	})
}

func TestTheSuiteDemoIsBuiltAndLinked(t *testing.T) {
	exc, arm := projectKey(t, "DEX"), projectKey(t, "DAR")
	h := newHarness(t)
	h.keepArmature(t)
	h.keepStator(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "suite-demo")
	tenant := stubTenant(t)
	stubControl(t, http.MethodDelete, tenant, nil)
	statorStub(t, http.MethodDelete, tenant, nil)
	statorOffers(t)
	owner := api.as(t, home.user)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	// Only the stub's demo person sees its EXC project, as only the demo's people would see Armature's.
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(tenant, "demo")}), http.StatusOK, "connect to Armature")
	want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	want(t, owner.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "alice")}), http.StatusOK, "connect to Stator")

	svc, plan := h.demoService(t), demo.Make(demoFixture())
	keys := map[string]string{"EXC": exc, "ARM": arm}
	made, err := svc.Build(home.ctx, home.user, plan, keys)
	if err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	// Each project's About, Exciter's four issues and two criteria, and Armature's one.
	if made != 9 {
		t.Errorf("the demo made %d requirements, want 9", made)
	}

	t.Run("the issues are filed by milestone in their order, with their criteria", func(t *testing.T) {
		rows := h.demoTree(t, exc)
		want := []string{
			"Introduction", "  About Exciter", "Functional requirements", "  M2 Armature integration", "    Projects [Approved, M2 Armature integration]",
			"      Making one [Approved]", "      Keys [Approved]", "    Requirements [Draft, M2 Armature integration]", "  Backlog",
			"    Trace links [Approved, Backlog]", "  Without a milestone", "    Not in Armature [Draft]", "Non-functional requirements", "Interfaces",
		}
		if !slices.Equal(rows, want) {
			t.Errorf("the tree is\n%s\nwant\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
		}
	})

	linked, err := svc.Link(home.ctx, home.user, plan, keys)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("dependencies, issues and pages are linked, and what the partners lack is counted", func(t *testing.T) {
		// Two dependencies, EXC-1 to EXC-3 in Armature, and four pages: the M2 contract twice, README and architecture.
		if linked.Links != 9 || linked.Skipped != 4 {
			t.Errorf("linking added %d and skipped %d, want 9 and 4: %v", linked.Links, linked.Skipped, linked.Notes)
		}
		if len(linked.Notes) != 2 || !strings.Contains(linked.Notes[0], "Armature") || !strings.Contains(linked.Notes[1], "Stator") {
			t.Errorf("the notes say %v", linked.Notes)
		}
		var crossProject int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM trace_link l
			JOIN requirement s ON s.id = l.source_id JOIN project sp ON sp.id = s.project_id
			JOIN requirement d ON d.id = l.target_id JOIN project dp ON dp.id = d.project_id
			WHERE sp.key = $1 AND dp.key = $2`, exc, arm).Scan(&crossProject); err != nil || crossProject != 1 {
			t.Errorf("%d links from %s to %s (%v), want Trace links to Boards", crossProject, exc, arm, err)
		}
		var issues []string
		rows, err := h.super.Query(context.Background(), `SELECT l.issue_key FROM armature_issue_link l JOIN requirement r ON r.id = l.requirement_id
			JOIN project p ON p.id = r.project_id WHERE p.key = $1 AND l.relation = 'implements' ORDER BY l.issue_key`, exc)
		if err != nil {
			t.Fatal(err)
		}
		if issues, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || !slices.Equal(issues, []string{"EXC-1", "EXC-2", "EXC-3"}) {
			t.Errorf("the issues linked are %v (%v)", issues, err)
		}
	})

	t.Run("linking again adds nothing twice", func(t *testing.T) {
		again, err := svc.Link(home.ctx, home.user, plan, keys)
		if err != nil || again.Links != 0 || again.Skipped != 4 {
			t.Errorf("linking again added %d and skipped %d (%v)", again.Links, again.Skipped, err)
		}
	})

	t.Run("a baseline named after the snapshot is taken once", func(t *testing.T) {
		for range 2 {
			if err := svc.Baseline(home.ctx, home.user, plan, keys); err != nil {
				t.Fatal(err)
			}
		}
		var n int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM baseline b JOIN project p ON p.id = b.project_id
			WHERE p.key = ANY($1) AND b.name = 'Snapshot 2026-10-20'`, []string{exc, arm}).Scan(&n); err != nil || n != 2 {
			t.Errorf("%d baselines (%v), want one a project", n, err)
		}
	})
}

// demoTree reads a project's tree as the superuser: each node indented by depth, with its status and Release.
func (h *harness) demoTree(t *testing.T, key string) []string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		WITH RECURSIVE tree AS (
			SELECT r.id, r.title, r.kind, r.status_id, r.attributes, 0 AS depth, ARRAY[r.rank::text, lpad(r.number::text, 10, '0')] AS path
			FROM requirement r JOIN project p ON p.id = r.project_id
			WHERE p.key = $1 AND r.parent_id IS NULL AND r.trashed_at IS NULL
			UNION ALL
			SELECT c.id, c.title, c.kind, c.status_id, c.attributes, t.depth + 1, t.path || ARRAY[c.rank::text, lpad(c.number::text, 10, '0')]
			FROM requirement c JOIN tree t ON c.parent_id = t.id WHERE c.trashed_at IS NULL)
		SELECT repeat('  ', t.depth) || t.title ||
		       CASE WHEN t.kind = 'section' THEN '' ELSE ' [' || concat_ws(', ', s.name,
		           (SELECT v #>> '{}' FROM jsonb_each(t.attributes) AS a(k, v) LIMIT 1)) || ']' END
		FROM tree t LEFT JOIN req_status s ON s.id = t.status_id ORDER BY t.path`, key)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTheSuiteDemoIsAddedOnce(t *testing.T) {
	h := newHarness(t)
	h.keepSuiteDemo(t)
	api := newAPIServer(t, h)
	admin := api.as(t, h.owner(t, "demo-once").user)
	member := api.as(t, h.addPerson(t, "member"))
	ctx := context.Background()

	refuse(t, "a member reading the demo", member.get(t, "/api/v1/installation/suite-demo"), http.StatusForbidden, "forbidden")
	refuse(t, "a member adding the demo", member.post(t, "/api/v1/installation/suite-demo", nil), http.StatusForbidden, "forbidden")
	if got := obj(t, want(t, admin.get(t, "/api/v1/installation/suite-demo"), http.StatusOK, "read no demo")); got["demo"] != nil {
		t.Errorf("with none added the demo reads %v", got)
	}
	refuse(t, "linking a demo nobody added", admin.post(t, "/api/v1/installation/suite-demo/links", nil), http.StatusNotFound, "not_found")

	t.Run("a key another project holds is refused", func(t *testing.T) {
		var taken bool
		if err := h.super.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM project WHERE key = $1)`, demo.Keys[1]).Scan(&taken); err != nil {
			t.Fatal(err)
		}
		// A stack the demo was added to holds its key already; another makes the project here, and takes it away.
		if !taken {
			want(t, admin.post(t, "/api/v1/projects", map[string]any{"key": demo.Keys[1], "name": "Taken"}), http.StatusCreated, "take the key")
			t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM project WHERE key = $1`, demo.Keys[1]) })
		}
		if msg := fieldError(t, admin.post(t, "/api/v1/installation/suite-demo", nil), "key"); !strings.Contains(msg, demo.Keys[1]) {
			t.Errorf("the refusal says %q", msg)
		}
	})

	if _, err := h.super.Exec(ctx, `INSERT INTO suite_demo (taken, project_keys, state, kind) VALUES ('2026-10-20', $1, 'queued', 'build')`, demo.Keys); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	t.Run("a demo added already is read, and neither added nor linked again while it is built", func(t *testing.T) {
		got := obj(t, want(t, admin.get(t, "/api/v1/installation/suite-demo"), http.StatusOK, "read the demo"), "demo")
		if got["state"] != "queued" || got["taken"] != "2026-10-20" || len(got["projectKeys"].([]any)) != len(demo.Keys) {
			t.Errorf("the demo reads %v", got)
		}
		refuse(t, "adding it again", admin.post(t, "/api/v1/installation/suite-demo", nil), http.StatusConflict, "conflict")
		refuse(t, "linking it while queued", admin.post(t, "/api/v1/installation/suite-demo/links", nil), http.StatusConflict, "conflict")
	})
}

// Straight through SQL as exciter_app: the demo is one row an administrator alone reads and writes, it moves
// forward only, and what it was made from stays.
func TestRawSQLKeepsTheSuiteDemoToAdministrators(t *testing.T) {
	h := newHarness(t)
	h.keepSuiteDemo(t)
	admin := h.owner(t, "demo-sql")
	member := h.addPerson(t, "member")
	ctx := context.Background()
	insert := `INSERT INTO suite_demo (taken, project_keys) VALUES ('2026-10-20', ARRAY['ARM', 'EXC', 'STA'])`

	asMember := h.appConn(t, member)
	if n := affects(t, asMember, `UPDATE suite_demo SET message = 'x'`); n != 0 {
		t.Errorf("a member changed %d demo rows", n)
	}
	if _, err := asMember.Exec(ctx, insert); !isPolicyViolation(err) {
		t.Errorf("a member added the demo: %v", err)
	}
	unscoped := h.appConn(t)
	if _, err := unscoped.Exec(ctx, insert); !isPolicyViolation(err) {
		t.Errorf("a connection acting for nobody added the demo: %v", err)
	}

	asAdmin := h.appConn(t, admin.user)
	if _, err := asAdmin.Exec(ctx, insert); err != nil {
		t.Fatalf("an administrator adds the demo: %v", err)
	}
	if _, err := asAdmin.Exec(ctx, insert); !isKeyTaken(err) {
		t.Errorf("a second demo was added: %v", err)
	}
	var seen int
	if err := asMember.QueryRow(ctx, `SELECT count(*) FROM suite_demo`).Scan(&seen); err != nil || seen != 0 {
		t.Errorf("a member reads %d demo rows (%v)", seen, err)
	}
	for what, sql := range map[string]string{
		"skipping running":             `UPDATE suite_demo SET state = 'done'`,
		"changing its projects":        `UPDATE suite_demo SET project_keys = ARRAY['ONE']`,
		"changing its kind while busy": `UPDATE suite_demo SET kind = 'links', state = 'running'`,
	} {
		if _, err := asAdmin.Exec(ctx, sql); !isCheckViolation(err) {
			t.Errorf("%s was not refused: %v", what, err)
		}
	}
	var pgErr *pgconn.PgError
	if _, err := asAdmin.Exec(ctx, `DELETE FROM suite_demo`); !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Errorf("an administrator deleted the demo: %v", err)
	}
	record := `INSERT INTO suite_demo_requirement (requirement_id, project_id, tool, issue, criterion) VALUES ($1, $2, 'EXC', 1, 0)`
	if _, err := asMember.Exec(ctx, record, uuid.New(), uuid.New()); !isPolicyViolation(err) {
		t.Errorf("a member kept a record of the demo: %v", err)
	}
	if _, err := asAdmin.Exec(ctx, record, uuid.New(), uuid.New()); !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Errorf("a record of a requirement that is none was kept: %v", err)
	}
}
