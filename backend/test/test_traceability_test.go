//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// Test executions in traceability (#252): a CI upload fills the open executions holding the test cases it verifies,
// coverage counts test cases by their last results, and queries, chains, the matrix and baselines read them.

// passCase records a case's iteration as passed, as its tester.
func passCase(t *testing.T, c *client, execution, key string, iteration int) {
	t.Helper()
	want(t, c.put(t, "/api/v1/test-executions/"+execution+"/cases/"+key+"/result", map[string]any{"iteration": iteration, "outcome": "passed"}),
		http.StatusOK, "pass the case")
}

// runOf reads one iteration of a case in an execution: its outcome, source and the CI result's name.
func runOf(t *testing.T, c *client, execution, key string, iteration int) (outcome, source, ci any) {
	t.Helper()
	run := caseOf(t, c, execution, key, iteration)["run"].(map[string]any)
	if from, ok := run["ci"].(map[string]any); ok {
		ci = from["name"]
	}
	return run["outcome"], run["source"], ci
}

// An upload on the counting branch fills every iteration of the cases it verifies in the open executions, the worst
// of its results with errored as failed; a tester's result, another branch and a closed execution stay as they are.
func TestACIUploadFillsTheOpenTestExecutions(t *testing.T) {
	s := newExecutionScene(t, "TTF")
	owner, plain := s.f.owner, s.f.plain
	r2 := s.key + "-R2"
	night := "wet at night [verifies " + r2 + "]"
	ran(t, owner, s.key, runForm("main"), junit([2]string{"wet [verifies " + r2 + "]", "passed"}, [2]string{night, "errored"}))
	for _, n := range []int{1, 2} {
		if outcome, source, ci := runOf(t, plain, s.execution, r2, n); outcome != "failed" || source != "ci" || ci != night {
			t.Errorf("iteration %d reads %v from %v (%v)", n, outcome, source, ci)
		}
	}
	e := obj(t, want(t, plain.get(t, "/api/v1/test-executions/"+s.execution), http.StatusOK, "read the execution"), "execution")
	if p := e["progress"].(map[string]any); p["failed"] != float64(2) || p["notRun"] != float64(0) {
		t.Errorf("the execution's progress reads %v", p)
	}

	passCase(t, plain, s.execution, r2, 1)
	ran(t, owner, s.key, runForm("main"), junit([2]string{"wet [verifies " + r2 + "]", "passed"}))
	if outcome, source, _ := runOf(t, plain, s.execution, r2, 1); outcome != "passed" || source != "tester" {
		t.Errorf("the tester's result became %v from %v", outcome, source)
	}
	if outcome, source, _ := runOf(t, plain, s.execution, r2, 2); outcome != "passed" || source != "ci" {
		t.Errorf("the newer upload left the second iteration %v from %v", outcome, source)
	}

	ran(t, owner, s.key, runForm("feature"), junit([2]string{"wet [verifies " + r2 + "]", "failed"}))
	if outcome, _, _ := runOf(t, plain, s.execution, r2, 2); outcome != "passed" {
		t.Errorf("an upload on another branch made the second iteration %v", outcome)
	}

	want(t, owner.post(t, "/api/v1/test-executions/"+s.execution+"/close", map[string]any{}), http.StatusOK, "close the execution")
	ran(t, owner, s.key, runForm("main"), junit([2]string{"wet [verifies " + r2 + "]", "skipped"}))
	if outcome, _, _ := runOf(t, plain, s.execution, r2, 2); outcome != "passed" {
		t.Errorf("an upload changed a closed execution to %v", outcome)
	}
}

// Coverage from runs is full when each verifying case last passed at its current revision through a link not suspect;
// the query language, the chains, the matrix and a baseline read the same last results.
func TestTestExecutionsCountForCoverageQueriesChainsAndBaselines(t *testing.T) {
	s := newExecutionScene(t, "TTC")
	owner, plain, reader := s.f.owner, s.f.plain, s.readerOf(t)
	r1, r2 := s.key+"-R1", s.key+"-R2"
	rules := "/api/v1/projects/" + s.key + "/coverage-rules"
	want(t, owner.put(t, rules, map[string]any{"verified": true, "verifiedFrom": []string{"runs"}}), http.StatusOK, "count test executions")
	want(t, owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Warn the driver"}), http.StatusCreated, "write R4")
	verified := func(t *testing.T, key, level string) {
		t.Helper()
		var got string
		for _, each := range list(t, want(t, reader.get(t, "/api/v1/projects/"+s.key+"/coverage"), http.StatusOK, "read the coverage"), "rows") {
			if r := each.(map[string]any); r["requirement"].(map[string]any)["key"] == key {
				got = r["verified"].(string)
			}
		}
		kept := rowKeys(t, want(t, reader.get(t, "/api/v1/projects/"+s.key+"/requirements?query="+url.QueryEscape("verified = "+level)), http.StatusOK, "query"))
		if got != level || !slices.Contains(kept, key) {
			t.Errorf("%s is %s in the coverage and the query keeps %v, want %s", key, got, kept, level)
		}
	}
	query := func(t *testing.T, q string) []string {
		t.Helper()
		return rowKeys(t, want(t, reader.get(t, "/api/v1/projects/"+s.key+"/requirements?query="+url.QueryEscape(q)), http.StatusOK, "query "+q))
	}

	t.Run("a case not run yet, or run at an older revision, verifies in part", func(t *testing.T) {
		verified(t, r1, "partial")
		verified(t, s.key+"-R4", "none")
		passCase(t, plain, s.execution, r2, 1)
		passCase(t, plain, s.execution, r2, 2)
		verified(t, r1, "partial")
		sameList(t, "runOutdated = true", query(t, "runOutdated = true"), r2)
	})

	var current string
	t.Run("a case passed at its current revision verifies in full", func(t *testing.T) {
		current = obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/test-executions", map[string]any{"title": "Current", "planId": s.plan,
			"testers": []any{map[string]any{"subjectType": "user", "userId": plain.user}}}), http.StatusCreated, "start an execution of the current cases"),
			"execution")["id"].(string)
		passCase(t, plain, current, r2, 1)
		passCase(t, plain, current, r2, 2)
		verified(t, r1, "full")
		sameList(t, "testCase = true", query(t, "testCase = true"), r2, s.key+"-R3")
		sameList(t, "runResult = passed", query(t, "runResult = passed"), r2)
		sameList(t, "runResult = none AND testCase = true", query(t, "runResult = none AND testCase = true"), s.key+"-R3")
		sameList(t, "runOutdated = true", query(t, "runOutdated = true"))
		sameList(t, "casesResult = passed", query(t, "casesResult = passed"), r1)
	})

	t.Run("the matrix and the chains mark a case with its last result", func(t *testing.T) {
		m := obj(t, want(t, reader.get(t, "/api/v1/projects/"+s.key+"/matrix"), http.StatusOK, "the matrix"), "matrix")
		marks := map[string]any{}
		for _, each := range m["rows"].([]any) {
			r := each.(map[string]any)
			marks[r["key"].(string)] = r["lastRun"]
		}
		if run, ok := marks[r2].(map[string]any); !ok || run["outcome"] != "passed" || run["executionId"] != current || run["outdated"] != false {
			t.Errorf("the matrix marks R2 with %v", marks[r2])
		}
		if marks[r1] != nil {
			t.Errorf("the matrix marks a requirement that is no test case with %v", marks[r1])
		}
		if run, ok := marks[s.key+"-R3"].(map[string]any); !ok || run["outcome"] != "none" || run["executionId"] != nil {
			t.Errorf("the matrix marks a case never run with %v", marks[s.key+"-R3"])
		}
		verifies := ""
		for _, each := range list(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/link-types"), http.StatusOK, "the link types"), "linkTypes") {
			if lt := each.(map[string]any); lt["key"] == "verifies" {
				verifies = lt["id"].(string)
			}
		}
		want(t, owner.put(t, "/api/v1/projects/"+s.key+"/trace-model", map[string]any{"levels": []map[string]any{
			{"name": "Requirement", "kind": "requirements", "typeIds": []string{s.plain}},
			{"name": "Test case", "kind": "requirements", "typeIds": []string{s.testType}, "steps": []map[string]any{{"typeId": verifies, "direction": "incoming"}}},
		}}), http.StatusOK, "end the model in test cases")
		chains := obj(t, want(t, reader.get(t, "/api/v1/projects/"+s.key+"/trace-chains"), http.StatusOK, "follow the chains"), "chains")
		var mark any
		for _, each := range chains["lines"].([]any) {
			nodes := each.(map[string]any)["nodes"].([]any)
			if len(nodes) == 2 {
				mark = nodes[1].(map[string]any)["lastRun"]
			}
		}
		if run, ok := mark.(map[string]any); !ok || run["outcome"] != "passed" {
			t.Errorf("the chain marks the case with %v", mark)
		}
	})

	t.Run("a baseline records the cases' last results, and one taken before holds none", func(t *testing.T) {
		b := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/baselines", map[string]any{"name": "Tested"}), http.StatusCreated, "take a baseline"), "baseline")
		var outcome, execution string
		if err := s.f.h.super.QueryRow(context.Background(), `SELECT outcome, execution_id::text FROM baseline_evidence WHERE baseline_id = $1 AND kind = 'run'`,
			b["id"]).Scan(&outcome, &execution); err != nil || outcome != "passed" || execution != current {
			t.Errorf("the baseline recorded %s in %s: %v", outcome, execution, err)
		}
		var lines, before int
		if err := s.f.h.appConn(t, s.f.owner.user).QueryRow(context.Background(), `SELECT count(*) FROM baseline_content_rows($1) WHERE hash LIKE 'run %'`,
			b["id"]).Scan(&lines); err != nil || lines != 1 {
			t.Errorf("the signed content holds %d lines of runs: %v", lines, err)
		}
		if err := s.f.h.super.QueryRow(context.Background(), `SELECT count(*) FROM baseline_evidence WHERE baseline_id = $1 AND kind = 'run'`,
			s.baseline).Scan(&before); err != nil || before != 0 {
			t.Errorf("a baseline taken before any run holds %d: %v", before, err)
		}
	})

	t.Run("a suspect link or a revised case verifies in part again", func(t *testing.T) {
		want(t, owner.patch(t, "/api/v1/requirements/"+r1, map[string]any{"title": "Stop within 35 m", "baseRevision": 1}), http.StatusOK, "revise R1")
		verified(t, r1, "partial")
		sameList(t, "casesResult = passed", query(t, "casesResult = passed"), r1)
	})
}

// The fill is the uploader's, of a run landing, and nobody writes a CI result into an execution by hand.
func TestRawSQLKeepsCIResultsToTheirUploads(t *testing.T) {
	s := newExecutionScene(t, "TTG")
	ctx := context.Background()
	r2 := s.key + "-R2"
	run := ran(t, s.f.owner, s.key, runForm("main"), junit([2]string{"wet [verifies " + r2 + "]", "passed"}))
	owner := s.f.h.appConn(t, s.f.owner.user)
	if _, err := owner.Exec(ctx, `SELECT * FROM test_execution_take_ci($1)`, run["id"]); err == nil {
		t.Error("a run that is done filled the executions again")
	}
	if _, err := owner.Exec(ctx, `SELECT * FROM test_execution_take_ci($1)`, uuid.New()); err == nil {
		t.Error("a run nobody has filled the executions")
	}
	tester := s.f.h.appConn(t, s.f.plain.user)
	if _, err := tester.Exec(ctx, `UPDATE test_execution_case SET ci_name = 'forged' WHERE execution_id = $1`, s.execution); err == nil {
		t.Error("a tester wrote a CI result's name by hand")
	}
	if tag, err := tester.Exec(ctx, `UPDATE test_execution_case SET outcome = 'passed', source = 'ci', recorded_by = $2, recorded_at = now()
		WHERE execution_id = $1`, s.execution, s.f.plain.user); err == nil && tag.RowsAffected() != 0 {
		t.Error("a tester recorded a result as though CI had")
	}
}
