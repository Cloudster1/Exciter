//go:build integration

package test

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/testexec"
)

// projectID is the id of the scene's project.
func (s *testCaseScene) projectID(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := s.f.h.super.QueryRow(t.Context(), `SELECT id FROM project WHERE key = $1`, s.key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// reqID is the id of the scene's requirement of a number.
func (s *testCaseScene) reqID(t *testing.T, number int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := s.f.h.super.QueryRow(t.Context(), `SELECT id FROM requirement WHERE number = $2 AND project_id = (SELECT id FROM project WHERE key = $1)`,
		s.key, number).Scan(&id); err != nil {
		t.Fatalf("find %s-R%d: %v", s.key, number, err)
	}
	return id
}

// executions counts the executions of the scene's project, and plans its plans.
func (s *testCaseScene) executions(t *testing.T) int {
	t.Helper()
	return s.f.h.countRows(t, `SELECT count(*) FROM test_execution WHERE project_id = $1`, s.projectID(t))
}

func (s *testCaseScene) plans(t *testing.T) int {
	t.Helper()
	return s.f.h.countRows(t, `SELECT count(*) FROM test_plan WHERE project_id = $1`, s.projectID(t))
}

// elsewhere is a second project the scene's owner edits, with a type of test cases, an empty plan and a baseline.
type elsewhere struct {
	key, testType, plan, baseline string
}

func (s *testCaseScene) elsewhere(t *testing.T, prefix string) elsewhere {
	t.Helper()
	owner := s.f.owner
	e := elsewhere{key: projectKey(t, prefix)}
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": e.key, "name": "Elsewhere"}), http.StatusCreated, "make another project")
	e.testType = obj(t, want(t, owner.post(t, "/api/v1/projects/"+e.key+"/types", map[string]any{
		"name": "Test case", "icon": "requirement", "colour": "#4f6bed", "role": "testCase"}), http.StatusCreated, "make its type of test cases"), "type")["id"].(string)
	want(t, owner.post(t, "/api/v1/projects/"+e.key+"/requirements", map[string]any{"title": "Brake on ice", "typeId": e.testType,
		"testCase": map[string]any{"steps": []any{map[string]any{"action": statementSaying("Brake.")}}}}), http.StatusCreated, "write a case there")
	e.plan = obj(t, want(t, owner.post(t, "/api/v1/projects/"+e.key+"/test-plans", map[string]any{"title": "Ice", "scope": "cases",
		"cases": []string{e.key + "-R1"}}), http.StatusCreated, "make a plan there"), "plan")["id"].(string)
	e.baseline = obj(t, want(t, owner.post(t, "/api/v1/projects/"+e.key+"/baselines", map[string]any{"name": "Ice 1"}), http.StatusCreated,
		"take a baseline there"), "baseline")["id"].(string)
	return e
}

// tester names a person as a tester.
func tester(id uuid.UUID) map[string]any { return map[string]any{"subjectType": "user", "userId": id} }

// due is a due date d from now, as the API reads one.
func due(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339) }

// An execution starts only over a plan and baseline of its project with something to run, for 1 to MaxTesters
// testers named once, due within a year, by an editor; nothing is started otherwise (#324).
func TestAnExecutionStartsOnlyOverWhatItCanRun(t *testing.T) {
	s := newExecutionScene(t, "TXS")
	owner, guest := s.f.owner, s.readerOf(t)
	other := s.elsewhere(t, "TXSO")
	onlyNew := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/test-plans", map[string]any{"title": "Only the new case", "scope": "cases",
		"cases": []string{s.key + "-R3"}}), http.StatusCreated, "a plan of the case the baseline lacks"), "plan")["id"].(string)
	start := func(c *client, change map[string]any) response {
		body := map[string]any{"title": "Run", "planId": s.plan, "testers": []any{tester(s.f.plain.user)}}
		for k, v := range change {
			body[k] = v
		}
		return c.post(t, "/api/v1/projects/"+s.key+"/test-executions", body)
	}
	crowd := make([]any, testexec.MaxTesters+1)
	for i := range crowd {
		crowd[i] = tester(uuid.New())
	}
	before := s.executions(t)

	for _, tc := range []struct {
		what   string
		change map[string]any
		field  string
	}{
		{"no tester", map[string]any{"testers": []any{}}, "testers"},
		{"one tester too many", map[string]any{"testers": crowd}, "testers"},
		{"the same tester twice", map[string]any{"testers": []any{tester(s.f.plain.user), tester(s.f.plain.user)}}, "testers"},
		{"a tester who is a role", map[string]any{"testers": []any{map[string]any{"subjectType": "role", "userId": s.f.plain.user}}}, "testers"},
		{"a due date gone by", map[string]any{"dueAt": due(-time.Hour)}, "dueAt"},
		{"a due date over a year ahead", map[string]any{"dueAt": due(testexec.MaxDueAhead + 24*time.Hour)}, "dueAt"},
		{"a plan of another project", map[string]any{"planId": other.plan}, "planId"},
		{"a plan nobody has", map[string]any{"planId": uuid.NewString()}, "planId"},
		{"a baseline of another project", map[string]any{"baselineId": other.baseline}, "baselineId"},
	} {
		refuse422(t, tc.what, start(owner, tc.change), tc.field)
	}
	msg := refuse422(t, "a plan whose cases the baseline does not hold", start(owner, map[string]any{"planId": onlyNew, "baselineId": s.baseline}), "planId")
	if !strings.HasPrefix(msg, "The plan holds no test case to run here.") {
		t.Errorf("the refusal says %q", msg)
	}
	refuse(t, "a reader starts one", start(guest, nil), http.StatusForbidden, "forbidden")
	if n := s.executions(t); n != before {
		t.Errorf("the refusals started %d executions", n-before)
	}
}

// A plan holds a known scope, a query that finds its cases, at most MaxPlanCases test cases of its own project, and
// is changed by whoever edits the project alone; a refused plan is neither made nor changed (#324).
func TestAPlanRefusesWhatItCannotHold(t *testing.T) {
	s := newExecutionScene(t, "TXP")
	owner, guest := s.f.owner, s.readerOf(t)
	other := s.elsewhere(t, "TXPO")
	plans := "/api/v1/projects/" + s.key + "/test-plans"
	byQuery := obj(t, want(t, owner.post(t, plans, map[string]any{"title": "Every case", "scope": "query", "query": `type = "Test case"`}),
		http.StatusCreated, "a plan by query"), "plan")["id"].(string)
	before := s.plans(t)

	t.Run("a scope, a query and cases it cannot hold are refused on their field", func(t *testing.T) {
		refuse422(t, "a mixed scope", owner.post(t, plans, map[string]any{"title": "Mixed", "scope": "mixed"}), "scope")
		refuse422(t, "a plan by query with no query", owner.post(t, plans, map[string]any{"title": "Empty", "scope": "query"}), "query")
		refuse422(t, "a case of another project", owner.post(t, plans, map[string]any{"title": "Ice", "scope": "cases", "cases": []string{other.key + "-R1"}}), "cases")
		// A query that does not parse is refused as every query is, with where it goes wrong.
		r := owner.post(t, plans, map[string]any{"title": "Broken", "scope": "query", "query": `type = = "Test case"`})
		refuse(t, "a query that does not parse", r, http.StatusBadRequest, "bad_query")
		if r.Status == http.StatusBadRequest && obj(t, r, "error")["position"] == nil {
			t.Errorf("the refusal names no position: %s", r.Raw)
		}
		if n := s.plans(t); n != before {
			t.Errorf("the refusals made %d plans", n-before)
		}
	})

	t.Run("more cases than a plan holds are refused, and the plan keeps its own", func(t *testing.T) {
		// Written straight into the table: five hundred saves through the API would only make the test slow.
		rows, err := s.f.h.super.Query(t.Context(), `
			INSERT INTO requirement (project_id, title, statement, normative_hash, created_by, type_id)
			SELECT $1, 'Case ' || i, '{"type":"doc"}', 'h', $2, $3 FROM generate_series(1, $4::int) i RETURNING number`,
			s.projectID(t), s.f.home.user, s.testType, testexec.MaxPlanCases+1)
		if err != nil {
			t.Fatal(err)
		}
		var keys []string
		for rows.Next() {
			var n int
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			keys = append(keys, s.key+"-R"+strconv.Itoa(n))
		}
		rows.Close()
		s.f.h.settle(t)
		refuse422(t, "a new plan of one case too many", owner.post(t, plans, map[string]any{"title": "Everything", "scope": "cases", "cases": keys}), "cases")
		refuse422(t, "one case too many set on a plan", owner.put(t, "/api/v1/test-plans/"+s.plan+"/cases", map[string]any{"cases": keys}), "cases")
		sameList(t, "the plan's cases", planKeys(t, owner, s.plan), s.key+"-R2", s.key+"-R3")
		if n := s.plans(t); n != before {
			t.Errorf("the refusals made %d plans", n-before)
		}
	})

	t.Run("a query on a plan of picked cases is refused, and so is a title of nothing", func(t *testing.T) {
		refuse422(t, "a query on a picked plan", owner.patch(t, "/api/v1/test-plans/"+s.plan, map[string]any{"query": `type = "Test case"`}), "query")
		refuse422(t, "an empty title", owner.patch(t, "/api/v1/test-plans/"+s.plan, map[string]any{"title": " "}), "title")
		refuse(t, "a query on a plan by query that does not parse", owner.patch(t, "/api/v1/test-plans/"+byQuery, map[string]any{"query": "type ="}),
			http.StatusBadRequest, "bad_query")
		got := obj(t, want(t, owner.get(t, "/api/v1/test-plans/"+s.plan), http.StatusOK, "read the plan"), "plan")
		if got["title"] != "Wet road" || got["scope"] != "cases" || got["query"] != "" {
			t.Errorf("the refused changes left %v", got)
		}
		if q := obj(t, want(t, owner.get(t, "/api/v1/test-plans/"+byQuery), http.StatusOK, "read the plan by query"), "plan")["query"]; q != `type = "Test case"` {
			t.Errorf("the plan by query reads %v", q)
		}
	})

	t.Run("a reader neither sets a plan's cases nor deletes it", func(t *testing.T) {
		refuse(t, "a reader sets the cases", guest.put(t, "/api/v1/test-plans/"+s.plan+"/cases", map[string]any{"cases": []string{s.key + "-R2"}}),
			http.StatusForbidden, "forbidden")
		refuse(t, "a reader deletes it", guest.delete(t, "/api/v1/test-plans/"+s.plan), http.StatusForbidden, "forbidden")
		sameList(t, "the plan's cases", planKeys(t, owner, s.plan), s.key+"-R2", s.key+"-R3")
	})
}

// planKeys are the keys of a plan's cases, in order.
func planKeys(t *testing.T, c *client, plan string) []string {
	t.Helper()
	var out []string
	for _, each := range obj(t, want(t, c.get(t, "/api/v1/test-plans/"+plan), http.StatusOK, "read the plan"), "plan")["cases"].([]any) {
		out = append(out, each.(map[string]any)["key"].(string))
	}
	return out
}

// Nothing of tests changes in an archived project: no plan, execution, result or file is written, and its manager
// and editors are told it is archived rather than that it is not theirs (#324).
func TestAnArchivedProjectTakesNoPlanExecutionOrResult(t *testing.T) {
	s := newExecutionScene(t, "TXA")
	owner, plain := s.f.owner, s.f.plain
	kept := obj(t, want(t, plain.uploadAs(t, "/api/v1/test-executions/"+s.execution+"/cases/"+s.key+"-R2/files?iteration=1", "kept.png", "image/png", aPicture(t)),
		http.StatusCreated, "put a file on the case"), "file")["id"].(string)
	want(t, owner.put(t, "/api/v1/projects/"+s.key+"/archive", nil), http.StatusOK, "archive the project")
	plans, executions := s.plans(t), s.executions(t)
	base := "/api/v1/test-executions/" + s.execution

	refuse(t, "a plan", owner.post(t, "/api/v1/projects/"+s.key+"/test-plans", map[string]any{"title": "Late", "scope": "cases"}), http.StatusConflict, "project_archived")
	refuse(t, "an execution", owner.post(t, "/api/v1/projects/"+s.key+"/test-executions", map[string]any{"title": "Late", "planId": s.plan,
		"testers": []any{tester(plain.user)}}), http.StatusConflict, "project_archived")
	refuse(t, "a result", plain.put(t, base+"/cases/"+s.key+"-R2/result", map[string]any{"iteration": 1, "outcome": "passed"}), http.StatusConflict, "project_archived")
	refuse(t, "a file", plain.uploadAs(t, base+"/cases/"+s.key+"-R2/files?iteration=1", "late.png", "image/png", aPicture(t)), http.StatusConflict, "project_archived")
	refuse(t, "a file deleted", plain.delete(t, "/api/v1/test-execution-files/"+kept), http.StatusConflict, "project_archived")
	refuse(t, "a change of the execution", owner.patch(t, base, map[string]any{"version": "9"}), http.StatusConflict, "project_archived")
	refuse(t, "the execution closed", owner.post(t, base+"/close", map[string]any{}), http.StatusConflict, "project_archived")
	refuse(t, "the plan renamed", owner.patch(t, "/api/v1/test-plans/"+s.plan, map[string]any{"title": "Late"}), http.StatusConflict, "project_archived")
	refuse(t, "the plan's cases set", owner.put(t, "/api/v1/test-plans/"+s.plan+"/cases", map[string]any{"cases": []string{s.key + "-R2"}}),
		http.StatusConflict, "project_archived")
	refuse(t, "the plan deleted", owner.delete(t, "/api/v1/test-plans/"+s.plan), http.StatusConflict, "project_archived")

	if p, e := s.plans(t), s.executions(t); p != plans || e != executions {
		t.Errorf("the archived project went from %d plans and %d executions to %d and %d", plans, executions, p, e)
	}
	e := obj(t, want(t, owner.get(t, base), http.StatusOK, "read the execution"), "execution")
	if e["state"] != "open" || e["version"] != "2.3" || e["progress"].(map[string]any)["notRun"] != float64(2) {
		t.Errorf("the execution in the archived project reads %v", e)
	}
	if n := s.f.h.countRows(t, `SELECT count(*) FROM test_execution_file WHERE execution_id = $1`, s.execution); n != 1 {
		t.Errorf("the execution holds %d files, want the one put there before", n)
	}
	sameList(t, "the plan's cases", planKeys(t, owner, s.plan), s.key+"-R2", s.key+"-R3")
}

// A result names an outcome there is, an iteration and a case the execution runs; a case's own outcome outranks
// what its steps say (#324).
func TestAResultIsRecordedOnlyForWhatTheExecutionRuns(t *testing.T) {
	s := newExecutionScene(t, "TXR")
	plain := s.f.plain
	result := func(key string) string { return "/api/v1/test-executions/" + s.execution + "/cases/" + key + "/result" }

	refuse422(t, "an outcome nobody has", plain.put(t, result(s.key+"-R2"), map[string]any{"iteration": 1, "outcome": "maybe"}), "outcome")
	refuse(t, "an iteration the case lacks", plain.put(t, result(s.key+"-R2"), map[string]any{"iteration": 3, "outcome": "passed"}), http.StatusNotFound, "not_found")
	refuse(t, "a requirement the execution does not run", plain.put(t, result(s.key+"-R1"), map[string]any{"iteration": 1, "outcome": "passed"}),
		http.StatusNotFound, "not_found")
	refuse(t, "a case the baseline left out", plain.put(t, result(s.key+"-R3"), map[string]any{"iteration": 1, "outcome": "passed"}), http.StatusNotFound, "not_found")
	if n := s.f.h.countRows(t, `SELECT count(*) FROM test_execution_case WHERE execution_id = $1 AND outcome IS NOT NULL`, s.execution); n != 0 {
		t.Errorf("the refusals recorded %d case iterations", n)
	}

	got := obj(t, want(t, plain.put(t, result(s.key+"-R2"), map[string]any{"iteration": 2, "outcome": "blocked", "steps": []any{
		map[string]any{"step": 1, "outcome": "passed"}, map[string]any{"step": 2, "outcome": "passed"}}}), http.StatusOK, "block a case whose steps passed"), "case")
	if got["run"].(map[string]any)["outcome"] != "blocked" {
		t.Errorf("the case's own outcome gave way to its steps: %v", got["run"])
	}
	for _, each := range got["results"].([]any) {
		if r := each.(map[string]any); r["outcome"] != "passed" {
			t.Errorf("step %v reads %v", r["step"], r["outcome"])
		}
	}
	if outcome, _, _ := runOf(t, plain, s.execution, s.key+"-R2", 1); outcome != nil {
		t.Errorf("the other iteration reads %v", outcome)
	}
}

// A file is something, at most MaxFileMB, on a step the case has, at most MaxFilesPerStep to a step, put there by a
// tester while the execution is open; it downloads as an attachment the browser never sniffs (#324).
func TestAFileIsRecordedWithinItsBounds(t *testing.T) {
	s := newExecutionScene(t, "TXF")
	owner, plain, guest := s.f.owner, s.f.plain, s.readerOf(t)
	files := func(query string) string {
		return "/api/v1/test-executions/" + s.execution + "/cases/" + s.key + "-R2/files?iteration=1" + query
	}
	held := func() int {
		return s.f.h.countRows(t, `SELECT count(*) FROM test_execution_file WHERE execution_id = $1`, s.execution)
	}

	t.Run("an empty file, one too large and a step the case lacks are refused", func(t *testing.T) {
		refuse422(t, "an empty file", plain.uploadAs(t, files(""), "empty.txt", "text/plain", nil), "file")
		refuse(t, "a file past the limit", plain.uploadAs(t, files(""), "huge.bin", "application/octet-stream", make([]byte, testexec.MaxFileBytes+1)),
			http.StatusRequestEntityTooLarge, "too_large")
		refuse422(t, "a step the case lacks", plain.uploadAs(t, files("&step=9"), "nine.png", "image/png", aPicture(t)), "step")
		refuse(t, "a reader uploads", guest.uploadAs(t, files(""), "mine.png", "image/png", aPicture(t)), http.StatusForbidden, "forbidden")
		if n := held(); n != 0 {
			t.Errorf("the refusals left %d files", n)
		}
	})

	t.Run("a step holds MaxFilesPerStep files and no more", func(t *testing.T) {
		for i := range testexec.MaxFilesPerStep {
			want(t, plain.uploadAs(t, files("&step=1"), fmt.Sprintf("shot-%d.png", i), "image/png", aPicture(t)), http.StatusCreated, "put a file on step 1")
		}
		refuse422(t, "one file too many on the step", plain.uploadAs(t, files("&step=1"), "one-more.png", "image/png", aPicture(t)), "file")
		want(t, plain.uploadAs(t, files("&step=2"), "other-step.png", "image/png", aPicture(t)), http.StatusCreated, "another step still takes one")
		if n := held(); n != testexec.MaxFilesPerStep+1 {
			t.Errorf("the execution holds %d files", n)
		}
	})

	var page string
	t.Run("a page sent as HTML downloads as an attachment that is not sniffed", func(t *testing.T) {
		page = obj(t, want(t, plain.uploadAs(t, files(""), "trap.html", "text/html", []byte("<script>alert(1)</script>")), http.StatusCreated,
			"put a page on the case"), "file")["id"].(string)
		got := want(t, guest.get(t, "/api/v1/test-execution-files/"+page), http.StatusOK, "a reader downloads it")
		if d := got.Header.Get("Content-Disposition"); !strings.HasPrefix(d, "attachment") {
			t.Errorf("the page downloads as %q", d)
		}
		if n := got.Header.Get("X-Content-Type-Options"); n != "nosniff" {
			t.Errorf("X-Content-Type-Options is %q", n)
		}
		if !bytes.Equal(got.Raw, []byte("<script>alert(1)</script>")) {
			t.Error("the page came back changed")
		}
	})

	t.Run("after closing nothing is put on a step or taken off it", func(t *testing.T) {
		want(t, owner.post(t, "/api/v1/test-executions/"+s.execution+"/close", map[string]any{}), http.StatusOK, "close it")
		before := held()
		refuse(t, "a file after closing", plain.uploadAs(t, files(""), "late.png", "image/png", aPicture(t)), http.StatusConflict, "execution_closed")
		refuse(t, "a file deleted after closing", plain.delete(t, "/api/v1/test-execution-files/"+page), http.StatusConflict, "execution_closed")
		if n := held(); n != before {
			t.Errorf("the closed execution went from %d files to %d", before, n)
		}
	})
}

// Changing and closing an execution refuse a due date gone by, a title of nothing and a comment over the limit,
// and change nothing then (#324).
func TestAnExecutionIsChangedAndClosedWithinItsBounds(t *testing.T) {
	s := newExecutionScene(t, "TXC")
	owner := s.f.owner
	base := "/api/v1/test-executions/" + s.execution
	refuse422(t, "a due date gone by", owner.patch(t, base, map[string]any{"dueAt": due(-time.Hour)}), "dueAt")
	refuse422(t, "a title of nothing", owner.patch(t, base, map[string]any{"title": " "}), "title")
	refuse422(t, "a version too long", owner.patch(t, base, map[string]any{"version": strings.Repeat("v", testexec.MaxVersionLength+1)}), "version")
	refuse422(t, "a close comment too long", owner.post(t, base+"/close", map[string]any{"comment": strings.Repeat("c", testexec.MaxDescriptionLength+1)}), "comment")
	e := obj(t, want(t, owner.get(t, base), http.StatusOK, "read it"), "execution")
	if e["state"] != "open" || e["title"] != "Release 1 on wet roads" || e["version"] != "2.3" {
		t.Errorf("the refusals left %v", e)
	}
}
