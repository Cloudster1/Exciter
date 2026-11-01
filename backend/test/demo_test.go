//go:build integration

package test

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
	"github.com/cloudster1/exciter/backend/internal/testexec"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// The suite demo (#270, #488): a small snapshot with its specifications built, linked and run against the stubs, the
// endpoints' refusals, and the demo's rows guarded straight through SQL.

// demoSpecs are the files of the fixture's specifications, by tool. Exciter's name issues and a guide page the
// armature-stub's EXC project and the stator-stub's EXC space know; Armature's name what neither has.
var demoSpecs = map[string]map[string]string{
	"ARM": {
		"spec/armature/10-boards.md": "# Functional requirements / Boards\n\n### FR-1 Rename a board\ntype: Functional\npriority: should\n" +
			"verification: Test\nissues: ARM-9\nguide: boards\n\nArmature shall show a renamed board under its new name.\n",
		"spec/armature/80-tests.md": "# Test specification\n\n### TC-1 Rename a board\ntype: Test case\nverifies: FR-1\nresult: passed\n\n" +
			"| Action | Expected |\n|---|---|\n| Rename the board. | It shows its new name. |\n",
	},
	"EXC": {
		"spec/exciter/01-introduction.md": "# Introduction\n\nWhat Exciter is for.\n",
		"spec/exciter/02-needs.md": "# Stakeholder needs\n\n### SN-1 Know what a change touches\ntype: Stakeholder need\npriority: must\n\n" +
			"An engineer needs to see what depends on a change.\n",
		"spec/exciter/10-traceability.md": "# Functional requirements / Traceability\n\nHow requirements are traced.\n\n" +
			"### FR-1 Link two requirements\ntype: Functional\npriority: must\nverification: Test\ncomponent: Traceability\nrefines: SN-1\n" +
			"issues: EXC-1, EXC-2\nguide: links\n\nExciter shall keep a typed link between two requirements.\n\nRationale: A type says how they relate.\n\n" +
			"### FR-2 Mark links suspect\ntype: Functional\npriority: must\nverification: Test\nrelates: FR-1\nissues: EXC-3\nguide: links\n\n" +
			"Exciter shall mark the links of a changed requirement suspect.\n\n## Changes\n\n" +
			"### FR-3 Clear a suspect mark\ntype: Non-functional\npriority: could\nverification: Inspection\nrefines: SN-1\n\n" +
			"Exciter shall let a person clear a suspect mark.\n",
		"spec/exciter/80-tests.md": "# Test specification\n\n" +
			"### TC-1 Link and read back\ntype: Test case\nverifies: FR-1\nautomated: yes\nresult: passed\n\n" +
			"Preconditions: Two requirements.\n\n| Action | Expected |\n|---|---|\n| Link them as `type`. | Both list the link. |\n| Reload. | |\n\n" +
			"Parameters: type = refines, relates\n\n" +
			"### TC-2 Change and clear\ntype: Test case\nverifies: FR-2, FR-3\nautomated: no\nresult: failed\nnote: The mark stayed after clearing it.\n\n" +
			"| Action | Expected |\n|---|---|\n| Change the second requirement. | The link is suspect. |\n",
	},
}

// demoFixture is a snapshot of Armature and Exciter with their specifications; EXC-3 is open.
func demoFixture(t *testing.T) demo.Snapshot {
	t.Helper()
	s := demo.Snapshot{Tools: []demo.Tool{
		{Repo: "Cloudster1/Armature", Key: "ARM", Name: "Armature", Commit: "a1b2c3d4e5f6a7b8", Taken: "2026-10-20", Issues: []demo.Issue{{Number: 9, Closed: true}}},
		{Repo: "Cloudster1/Exciter", Key: "EXC", Name: "Exciter", Commit: "0123456789abcdef", Taken: "2026-10-20", Issues: []demo.Issue{
			{Number: 1, Closed: true}, {Number: 2, Closed: true}, {Number: 3}}},
	}}
	for i, tool := range s.Tools {
		names := slices.Sorted(maps.Keys(demoSpecs[tool.Key]))
		for _, name := range names {
			f, err := demo.ParseSpec(name, demoSpecs[tool.Key][name])
			if err != nil {
				t.Fatal(err)
			}
			s.Tools[i].Spec = append(s.Tools[i].Spec, f)
		}
	}
	return s
}

// demoArchitecture is one drawing of Exciter's architecture, filed after the introduction.
func demoArchitecture() *demo.Architecture {
	return &demo.Architecture{Commit: "abc123", Source: "docs/architecture.md", Intro: "How Exciter is built.",
		Views: []demo.View{{Title: "Components", Statement: "The parts.\n\n```plantuml\n" + pumpDiagram + "\n```"}}}
}

func (h *harness) demoService(t *testing.T) *demo.Service {
	t.Helper()
	return demo.NewService(h.cluster, project.NewService(h.cluster).WithSeeder(risk.SeedProject),
		requirement.NewService(h.cluster).WithCoverage(trace.CoverageJudge{}), trace.NewService(h.cluster),
		baseline.NewService(h.cluster), testexec.NewService(h.cluster, nil), h.armature(t), h.stator(t))
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

// demoExcTree is Exciter's tree as the fixture files it: each section in the order of the files, the architecture
// after the introduction, and each item with its type, its status and its priority.
var demoExcTree = []string{
	"Introduction", "Architecture", "  Components [Architecture view, Approved]", "Stakeholder needs", "  Know what a change touches [Stakeholder need, Approved, must]",
	"Functional requirements", "  Traceability", "    Link two requirements [Functional, Verified, must]", "    Mark links suspect [Functional, Approved, must]",
	"    Changes", "      Clear a suspect mark [Non-functional, Approved, could]", "Test specification",
	"  Link and read back [Test case, Approved]", "  Change and clear [Test case, Approved]",
}

func TestTheSuiteDemoIsBuiltLinkedAndRun(t *testing.T) {
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

	fixture := demoFixture(t)
	fixture.Tools[1].Architecture = demoArchitecture()
	svc, plan := h.demoService(t), demo.Make(fixture)
	keys := map[string]string{"EXC": exc, "ARM": arm}
	made, err := svc.Build(home.ctx, home.user, plan, keys)
	if err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	// Armature's requirement and test case; Exciter's drawing, need, three requirements and two test cases.
	if made != 9 {
		t.Errorf("the demo made %d requirements, want 9", made)
	}
	ctx := context.Background()

	t.Run("the specification is filed by its sections, with types, statuses and priorities", func(t *testing.T) {
		if rows := h.demoTree(t, exc); !slices.Equal(rows, demoExcTree) {
			t.Errorf("the tree is\n%s\nwant\n%s", strings.Join(rows, "\n"), strings.Join(demoExcTree, "\n"))
		}
		var types []string
		rows, err := h.super.Query(ctx, `SELECT t.name || ':' || t.role FROM requirement_type t JOIN project p ON p.id = t.project_id
			WHERE p.key = $1 ORDER BY t.position`, exc)
		if err != nil {
			t.Fatal(err)
		}
		if types, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || !slices.Equal(types, []string{"Functional:requirement",
			"Non-functional:requirement", "Interface:requirement", "Use case:requirement", "Stakeholder need:requirement", "Test case:testCase",
			"Test procedure:requirement", "Architecture view:requirement"}) {
			t.Errorf("the types are %v (%v)", types, err)
		}
	})

	t.Run("an item carries its rationale, attributes and test steps", func(t *testing.T) {
		var rationale, verification, component, preconditions bool
		var steps, parameters int
		var automated *bool
		if err := h.super.QueryRow(ctx, `SELECT
			bool_or(r.title = 'Link two requirements' AND r.rationale::text LIKE '%A type says how they relate.%'),
			bool_or(r.title = 'Link two requirements' AND EXISTS (SELECT 1 FROM attribute_def d, jsonb_array_elements(d.options) o
				WHERE d.project_id = p.id AND d.name = 'Verification method' AND o->>'name' = 'Test' AND r.attributes->>(d.id::text) = o->>'id')),
			bool_or(r.title = 'Link two requirements' AND EXISTS (SELECT 1 FROM attribute_def d WHERE d.project_id = p.id
				AND d.name = 'Component' AND r.attributes->>(d.id::text) = 'Traceability')),
			bool_or(r.title = 'Link and read back' AND r.preconditions::text LIKE '%Two requirements.%'),
			max(jsonb_array_length(r.steps)) FILTER (WHERE r.title = 'Link and read back'),
			max(jsonb_array_length(r.parameters)) FILTER (WHERE r.title = 'Link and read back'),
			bool_or((SELECT (r.attributes->>(d.id::text))::boolean FROM attribute_def d WHERE d.project_id = p.id AND d.name = 'Automated'))
				FILTER (WHERE r.title = 'Link and read back')
			FROM requirement r JOIN project p ON p.id = r.project_id WHERE p.key = $1`, exc).Scan(
			&rationale, &verification, &component, &preconditions, &steps, &parameters, &automated); err != nil {
			t.Fatal(err)
		}
		if !rationale || !verification || !component || !preconditions || steps != 2 || parameters != 1 || automated == nil || !*automated {
			t.Errorf("rationale %v, verification %v, component %v, preconditions %v, %d steps, %d parameters, automated %v",
				rationale, verification, component, preconditions, steps, parameters, automated)
		}
	})

	linked, err := svc.Link(home.ctx, home.user, plan, keys)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("traces, issues and pages are linked, and what the partners lack is counted", func(t *testing.T) {
		// Seven trace links, EXC-1 to EXC-3 in Armature, the guide twice and the drawing's document; ARM-9 and
		// Armature's guide page are in neither stub.
		if linked.Links != 13 || linked.Skipped != 2 {
			t.Errorf("linking added %d and skipped %d, want 13 and 2: %v", linked.Links, linked.Skipped, linked.Notes)
		}
		if len(linked.Notes) != 2 || !strings.Contains(linked.Notes[0], "Armature") || !strings.Contains(linked.Notes[1], "Stator") {
			t.Errorf("the notes say %v", linked.Notes)
		}
		var traces []string
		rows, err := h.super.Query(ctx, `SELECT s.title || ' ' || lt.key || ' ' || d.title FROM trace_link l
			JOIN link_type lt ON lt.id = l.type_id JOIN requirement s ON s.id = l.source_id JOIN requirement d ON d.id = l.target_id
			JOIN project p ON p.id = s.project_id WHERE p.key = $1 ORDER BY 1`, exc)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"Change and clear verifies Clear a suspect mark", "Change and clear verifies Mark links suspect",
			"Clear a suspect mark refines Know what a change touches", "Link and read back verifies Link two requirements",
			"Link two requirements refines Know what a change touches", "Mark links suspect relates Link two requirements"}
		if traces, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || !slices.Equal(traces, want) {
			t.Errorf("the traces are\n%s\nwant\n%s (%v)", strings.Join(traces, "\n"), strings.Join(want, "\n"), err)
		}
		var issues []string
		rows, err = h.super.Query(ctx, `SELECT r.title || ' ' || l.issue_key FROM armature_issue_link l JOIN requirement r ON r.id = l.requirement_id
			JOIN project p ON p.id = r.project_id WHERE p.key = $1 AND l.relation = 'implements' ORDER BY 1`, exc)
		if err != nil {
			t.Fatal(err)
		}
		if issues, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || !slices.Equal(issues, []string{
			"Link two requirements EXC-1", "Link two requirements EXC-2", "Mark links suspect EXC-3"}) {
			t.Errorf("the issues linked are %v (%v)", issues, err)
		}
		var described int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM stator_page_link l JOIN requirement r ON r.id = l.requirement_id
			JOIN project p ON p.id = r.project_id WHERE p.key = $1 AND l.relation = 'describes'
			AND r.title IN ('Link two requirements', 'Mark links suspect')`, exc).Scan(&described); err != nil || described != 2 {
			t.Errorf("%d requirements are described by their guide page (%v), want 2", described, err)
		}
	})

	t.Run("linking again adds nothing twice", func(t *testing.T) {
		again, err := svc.Link(home.ctx, home.user, plan, keys)
		if err != nil || again.Links != 0 || again.Skipped != 2 {
			t.Errorf("linking again added %d and skipped %d (%v)", again.Links, again.Skipped, err)
		}
	})

	t.Run("each project's test cases run once against its baseline, and the run is closed", func(t *testing.T) {
		for range 2 {
			for _, tool := range []string{"ARM", "EXC"} {
				if err := svc.RunTests(home.ctx, home.user, plan, keys, tool); err != nil {
					t.Fatal(err)
				}
			}
		}
		var baselines, plans, executions int
		if err := h.super.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM baseline b JOIN project p ON p.id = b.project_id WHERE p.key = ANY($1) AND b.name = $2),
			(SELECT count(*) FROM test_plan t JOIN project p ON p.id = t.project_id WHERE p.key = ANY($1) AND t.title = $3),
			(SELECT count(*) FROM test_execution e JOIN project p ON p.id = e.project_id WHERE p.key = ANY($1) AND e.title = $4
				AND e.state = 'closed' AND e.baseline_name = $2)`, []string{exc, arm}, demo.BaselineName, demo.PlanTitle, demo.ExecutionTitle).Scan(
			&baselines, &plans, &executions); err != nil || baselines != 2 || plans != 2 || executions != 2 {
			t.Errorf("%d baselines, %d plans and %d closed runs (%v), want one each a project", baselines, plans, executions, err)
		}
		var results []string
		rows, err := h.super.Query(ctx, `SELECT r.title || ' ' || c.iteration || ' ' || c.outcome || coalesce(nullif(' ' || c.note, ' '), '')
			FROM test_execution_case c JOIN test_execution e ON e.id = c.execution_id JOIN requirement r ON r.id = c.requirement_id
			JOIN project p ON p.id = e.project_id WHERE p.key = $1 ORDER BY 1`, exc)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"Change and clear 1 failed The mark stayed after clearing it.", "Link and read back 1 passed", "Link and read back 2 passed"}
		if results, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || !slices.Equal(results, want) {
			t.Errorf("the results are %v (%v), want %v", results, err, want)
		}
		var version string
		if err := h.super.QueryRow(ctx, `SELECT e.version FROM test_execution e JOIN project p ON p.id = e.project_id WHERE p.key = $1`, exc).Scan(&version); err != nil || version != "0123456789ab" {
			t.Errorf("the run tested %q (%v), want the snapshot's commit", version, err)
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
	fixture := demoFixture(t)
	on := h
	if h.replica != nil {
		// Under load the replica falls behind the steps run back to back (#365). Here it is taken however far it
		// lags and its replay is held below, so a step that reads an earlier step's work from it fails every time.
		held := *h
		held.cluster = h.openCluster(t, 0, neverHealth)
		on = &held
	}
	svc := on.demoService(t).WithSnapshot(fixture, demoTopic, []string{arm, exc})
	steps := demo.StepCount(demo.Make(fixture), demo.KindBuild)
	if _, err := h.super.Exec(ctx, `INSERT INTO suite_demo (taken, project_keys, steps, requested_by) VALUES ('2026-10-20', $1, $2, $3)`,
		[]string{arm, exc}, steps, home.user); err != nil {
		t.Fatal(err)
	}
	h.cleanupExec(t, h.super, `DELETE FROM outbox_event WHERE topic = $1`, demoTopic)
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM outbox_event WHERE topic = $1`, demoTopic) })
	if h.replica != nil {
		h.settle(t)
		h.pauseReplay(t)
	}
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
		// Without partners only the trace links are made.
		if d.State != demo.StateDone || d.Step != steps || d.Requirements != 8 || d.Links != 7 || d.FinishedAt == nil {
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
		if again := read(t); again.Requirements != 8 {
			t.Errorf("the last step again made more: %+v", again)
		}
		var closed int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM test_execution e JOIN project p ON p.id = e.project_id
			WHERE p.key = ANY($1) AND e.state = 'closed'`, []string{arm, exc}).Scan(&closed); err != nil || closed != 2 {
			t.Errorf("%d runs are closed (%v), want one a project", closed, err)
		}
	})

	t.Run("linking again runs the link and test steps alone, and makes no second run", func(t *testing.T) {
		d, _, err := svc.Relink(home.ctx, home.user)
		if err != nil || d.State != demo.StateQueued || d.Kind != demo.KindLinks || d.Step != 0 || d.Steps != 4 {
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
		if d := read(t); d.State != demo.StateDone || d.Requirements != 8 || d.Links != 7 {
			t.Errorf("after linking again the demo is %+v", d)
		}
		var runs int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM test_execution e JOIN project p ON p.id = e.project_id WHERE p.key = ANY($1)`,
			[]string{arm, exc}).Scan(&runs); err != nil || runs != 2 {
			t.Errorf("%d runs (%v), want one a project", runs, err)
		}
	})
	if rows := h.demoTree(t, exc); len(rows) != len(demoExcTree)-2 {
		t.Errorf("the steps filed a tree of %d rows: %v", len(rows), rows)
	}
}

// demoTree reads a project's tree as the superuser: each node indented by depth, with its type, status and priority.
func (h *harness) demoTree(t *testing.T, key string) []string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		WITH RECURSIVE tree AS (
			SELECT r.id, r.title, r.kind, r.status_id, r.type_id, r.priority, 0 AS depth, ARRAY[r.rank::text, lpad(r.number::text, 10, '0')] AS path
			FROM requirement r JOIN project p ON p.id = r.project_id
			WHERE p.key = $1 AND r.parent_id IS NULL AND r.trashed_at IS NULL
			UNION ALL
			SELECT c.id, c.title, c.kind, c.status_id, c.type_id, c.priority, t.depth + 1, t.path || ARRAY[c.rank::text, lpad(c.number::text, 10, '0')]
			FROM requirement c JOIN tree t ON c.parent_id = t.id WHERE c.trashed_at IS NULL)
		SELECT repeat('  ', t.depth) || t.title ||
		       CASE WHEN t.kind = 'section' THEN '' ELSE ' [' || concat_ws(', ', ty.name, s.name, t.priority) || ']' END
		FROM tree t LEFT JOIN req_status s ON s.id = t.status_id LEFT JOIN requirement_type ty ON ty.id = t.type_id ORDER BY t.path`, key)
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
			s.Demo = h.demoService(t).WithSnapshot(demoFixture(t), demoTopic, keys)
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
		if relinked["state"] != "queued" || relinked["kind"] != "links" || relinked["steps"] != float64(4) {
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
	insert := `INSERT INTO suite_demo (taken, project_keys) VALUES ('2026-10-20', ARRAY['ARM', 'EXC', 'STA', 'ROT'])`

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

// Every drawing of the demo's architecture (#286) draws through the stack's Kroki, so nobody adding the demo meets a
// diagram that shows its source and a syntax error instead.
func TestEveryDrawingOfTheSuiteDemoDraws(t *testing.T) {
	f := newProjectFixture(t)
	if f.h.cfg.Kroki.URL == "" {
		t.Fatal("EXCITER_KROKI_URL is not set; run the suite with make test-integration against the running stack")
	}
	s, err := demo.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	drawn := 0
	for _, tool := range s.Tools {
		if tool.Architecture == nil {
			t.Errorf("%s has no architecture", tool.Name)
			continue
		}
		for _, v := range tool.Architecture.Views {
			sources, err := demo.DiagramSources(v.Statement)
			if err != nil {
				t.Fatal(err)
			}
			for _, src := range sources {
				got := f.plain.post(t, "/api/v1/diagrams", map[string]any{"language": "plantuml", "source": src})
				if svg, _ := got.Body["svg"].(string); got.Status != http.StatusOK || !strings.Contains(svg, "<svg") {
					t.Errorf("%s, %s: got %d: %s", tool.Name, v.Title, got.Status, got.Raw)
				}
				drawn++
			}
		}
	}
	t.Logf("%d drawings", drawn)
}
