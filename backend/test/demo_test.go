//go:build integration

package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/demo"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
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
			"Introduction", "  About Exciter [Approved]", "Functional requirements", "  M2 Armature integration", "    Projects [Approved, M2 Armature integration]",
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

// demoTopic is where a test's run queues its steps, which the stack's worker does not hear.
const demoTopic = "suite_demo.test"

func TestTheSuiteDemoIsBuiltAStepAtATime(t *testing.T) {
	exc, arm := projectKey(t, "DSX"), projectKey(t, "DSA")
	h := newHarness(t)
	h.keepSuiteDemo(t)
	// No partner is connected, so every link to one waits and says why.
	h.keepArmature(t)
	h.keepStator(t)
	home := h.owner(t, "suite-steps")
	ctx := context.Background()
	fixture := demoFixture()
	svc := h.demoService(t).WithSnapshot(fixture, demoTopic, []string{arm, exc})
	steps := demo.StepCount(demo.Make(fixture), demo.KindBuild)
	if _, err := h.super.Exec(ctx, `INSERT INTO suite_demo (taken, project_keys, steps, requested_by) VALUES ('2026-10-20', $1, $2, $3)`,
		[]string{arm, exc}, steps, home.user); err != nil {
		t.Fatal(err)
	}
	h.cleanupExec(t, h.super, `DELETE FROM outbox_event WHERE topic = $1`, demoTopic)
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM outbox_event WHERE topic = $1`, demoTopic) })
	read := func(t *testing.T) *demo.Demo {
		t.Helper()
		d, err := svc.Status(db.PinPrimary(home.ctx))
		if err != nil || d == nil {
			t.Fatalf("read the demo: %v %v", d, err)
		}
		return d
	}

	t.Run("a step that is not next does nothing", func(t *testing.T) {
		if err := svc.Step(home.ctx, home.user, 1); err != nil {
			t.Fatal(err)
		}
		if d := read(t); d.State != demo.StateQueued || d.Step != 0 {
			t.Errorf("the demo is %s at step %d", d.State, d.Step)
		}
	})

	t.Run("each step queues the next, and the last ends the run", func(t *testing.T) {
		for at := range steps {
			if err := svc.Step(home.ctx, home.user, at); err != nil {
				t.Fatalf("step %d: %v", at, err)
			}
			if at == 0 {
				if d := read(t); d.State != demo.StateRunning || d.Step != 1 {
					t.Errorf("after the first step the demo is %s at step %d", d.State, d.Step)
				}
			}
		}
		d := read(t)
		if d.State != demo.StateDone || d.Step != steps || d.Requirements != 9 || d.Links != 2 || d.FinishedAt == nil {
			t.Errorf("the demo ended as %+v", d)
		}
		for _, partner := range []string{"Armature is not connected", "Stator is not connected"} {
			if strings.Count(d.Message, partner) != 1 {
				t.Errorf("the message says %q, want %q once", d.Message, partner)
			}
		}
		var queued []int
		rows, err := h.super.Query(ctx, `SELECT (payload->>'step')::int FROM outbox_event WHERE topic = $1 ORDER BY created_at`, demoTopic)
		if err != nil {
			t.Fatal(err)
		}
		if queued, err = pgx.CollectRows(rows, pgx.RowTo[int]); err != nil || len(queued) != steps-1 || queued[0] != 1 || queued[len(queued)-1] != steps-1 {
			t.Errorf("the steps queued are %v (%v)", queued, err)
		}
		if err := svc.Step(home.ctx, home.user, steps-1); err != nil {
			t.Fatal(err)
		}
		if again := read(t); again.Requirements != 9 {
			t.Errorf("the last step again made more: %+v", again)
		}
	})

	t.Run("linking again runs the link steps alone", func(t *testing.T) {
		d, _, err := svc.Relink(home.ctx, home.user)
		if err != nil || d.State != demo.StateQueued || d.Kind != demo.KindLinks || d.Step != 0 || d.Steps != 3 {
			t.Fatalf("relinking queued %+v (%v)", d, err)
		}
		if _, _, err := svc.Relink(home.ctx, home.user); !errors.Is(err, demo.ErrBusy) {
			t.Errorf("linking twice at once: %v", err)
		}
		for at := range d.Steps {
			if err := svc.Step(home.ctx, home.user, at); err != nil {
				t.Fatal(err)
			}
		}
		if d := read(t); d.State != demo.StateDone || d.Requirements != 9 || d.Links != 2 {
			t.Errorf("after linking again the demo is %+v", d)
		}
	})
	if rows := h.demoTree(t, exc); len(rows) != 14 {
		t.Errorf("the steps filed a tree of %d rows: %v", len(rows), rows)
	}
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
	owner := h.owner(t, "demo-once")
	admin := api.as(t, owner.user)
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

	t.Run("an administrator adds it, and links it again once it is done", func(t *testing.T) {
		// A small snapshot under keys of the test's own, on a topic the stack's worker leaves alone.
		keys := []string{projectKey(t, "DOA"), projectKey(t, "DOX")}
		fixed := newAPIServerWith(t, h, func(_ *apiServer, s *httpapi.Server) {
			s.Demo = h.demoService(t).WithSnapshot(demoFixture(), demoTopic, keys)
		})
		t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM outbox_event WHERE topic = $1`, demoTopic) })
		by := fixed.as(t, owner.user)
		added := obj(t, want(t, by.post(t, "/api/v1/installation/suite-demo", nil), http.StatusAccepted, "add the demo"), "demo")
		if added["state"] != "queued" || added["kind"] != "build" || fmt.Sprint(added["projectKeys"]) != fmt.Sprint(keys) || added["step"] != float64(0) {
			t.Errorf("the demo added reads %v", added)
		}
		var audited int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = $1 AND actor_user_id = $2`,
			audit.ActionSuiteDemoAdded, owner.user).Scan(&audited); err != nil || audited != 1 {
			t.Errorf("%d audit entries (%v), want one", audited, err)
		}
		refuse(t, "adding it twice", by.post(t, "/api/v1/installation/suite-demo", nil), http.StatusConflict, "conflict")
		if _, err := h.super.Exec(ctx, `UPDATE suite_demo SET state = 'running'`); err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(ctx, `UPDATE suite_demo SET state = 'done'`); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		relinked := obj(t, want(t, by.post(t, "/api/v1/installation/suite-demo/links", nil), http.StatusAccepted, "link it again"), "demo")
		if relinked["state"] != "queued" || relinked["kind"] != "links" || relinked["steps"] != float64(3) {
			t.Errorf("linking again reads %v", relinked)
		}
		h.cleanupExec(t, h.super, `DELETE FROM suite_demo`)
		h.settle(t)
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
	if _, err := asAdmin.Exec(ctx, `UPDATE suite_demo SET state = 'running', steps = 5, step = 2`); err != nil {
		t.Fatalf("the worker takes two steps: %v", err)
	}
	if _, err := asAdmin.Exec(ctx, `UPDATE suite_demo SET step = 1`); !isCheckViolation(err) {
		t.Errorf("a running demo went back a step: %v", err)
	}
	if _, err := asAdmin.Exec(ctx, `UPDATE suite_demo SET step = 6`); !isCheckViolation(err) {
		t.Errorf("a demo went past its last step: %v", err)
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
