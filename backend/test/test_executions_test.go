//go:build integration

package test

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// executionScene is a test-case scene with a plan of its two cases, a baseline taken before the second existed and
// before the first changed, and an execution of the plan against it that plain tests.
type executionScene struct {
	*testCaseScene
	plan, baseline, execution string
}

func newExecutionScene(t *testing.T, prefix string) *executionScene {
	t.Helper()
	s := &executionScene{testCaseScene: newTestCaseScene(t, prefix)}
	owner := s.f.owner
	s.baseline = obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/baselines", map[string]any{"name": "Release 1"}), http.StatusCreated, "take a baseline"), "baseline")["id"].(string)
	want(t, owner.patch(t, "/api/v1/requirements/"+s.key+"-R2", map[string]any{"baseRevision": 1, "testCase": map[string]any{"steps": []any{
		map[string]any{"action": statementSaying("Drive faster.")}}}}), http.StatusOK, "change the case after the baseline")
	want(t, owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Warn on a wet road", "typeId": s.testType,
		"testCase": map[string]any{"steps": []any{map[string]any{"action": statementSaying("Look at the dashboard."), "expected": statementSaying("A light shows.")}}}}),
		http.StatusCreated, "write a second case")
	s.plan = obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/test-plans", map[string]any{"title": "Wet road", "scope": "cases",
		"cases": []string{s.key + "-R2", s.key + "-R3"}}), http.StatusCreated, "make a plan"), "plan")["id"].(string)
	started := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/test-executions", map[string]any{"title": "Release 1 on wet roads",
		"planId": s.plan, "baselineId": s.baseline, "version": "2.3", "configuration": "Winter tyres",
		"dueAt":   time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339),
		"testers": []any{map[string]any{"subjectType": "user", "userId": s.f.plain.user}}}), http.StatusCreated, "start an execution"), "execution")
	s.execution = started["id"].(string)
	return s
}

func caseOf(t *testing.T, c *client, execution, key string, iteration int) map[string]any {
	t.Helper()
	return obj(t, want(t, c.get(t, "/api/v1/test-executions/"+execution+"/cases/"+key+"?iteration="+strconv.Itoa(iteration)), http.StatusOK, "read the case"), "case")
}

// readerOf lets the scene's guest read the project and nothing more, a reader who neither edits nor tests.
func (s *testCaseScene) readerOf(t *testing.T) *client {
	t.Helper()
	var id uuid.UUID
	if err := s.f.h.super.QueryRow(t.Context(), `SELECT id FROM project WHERE key = $1`, s.key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	s.f.h.grantAs(t, id, s.f.guest.user, "viewer")
	return s.f.guest
}

// An execution pins its plan's cases at the revisions the baseline holds, once per iteration; a tester records step
// by step, puts files on steps, and a closed execution never changes again; its report reads the same.
func TestATestExecutionRunsAPlanStepByStep(t *testing.T) {
	s := newExecutionScene(t, "TEA")
	owner, plain, guest := s.f.owner, s.f.plain, s.readerOf(t)

	e := obj(t, want(t, plain.get(t, "/api/v1/test-executions/"+s.execution), http.StatusOK, "a tester reads the execution"), "execution")
	if e["progress"].(map[string]any)["total"] != float64(2) || len(e["excluded"].([]any)) != 1 || e["can"].(map[string]any)["record"] != true {
		t.Fatalf("the execution pins the two iterations of the case the baseline holds and leaves the other out: %v", e)
	}
	two := caseOf(t, plain, s.execution, s.key+"-R2", 2)
	if run := two["run"].(map[string]any); run["revision"] != float64(1) || run["values"].(map[string]any)["Speed"] != "50" {
		t.Errorf("the second iteration runs revision 1 at 50: %v", run)
	}
	sameList(t, "the pinned steps", stepTexts(t, map[string]any{"steps": two["steps"]}), "Drive at the speed.", "Press the pedal.")

	result := "/api/v1/test-executions/" + s.execution + "/cases/" + s.key + "-R2/result"
	refuse(t, "somebody who does not test it", guest.put(t, result, map[string]any{"iteration": 1, "steps": []any{map[string]any{"step": 1, "outcome": "passed"}}}),
		http.StatusForbidden, "forbidden")
	refuse422(t, "a step the case lacks", plain.put(t, result, map[string]any{"iteration": 1, "steps": []any{map[string]any{"step": 3, "outcome": "passed"}}}), "steps[0]")
	half := obj(t, want(t, plain.put(t, result, map[string]any{"iteration": 1, "steps": []any{map[string]any{"step": 1, "outcome": "passed"}}}),
		http.StatusOK, "record the first step"), "case")
	if half["run"].(map[string]any)["outcome"] != nil {
		t.Errorf("a case with a step open has no outcome: %v", half["run"])
	}
	failed := obj(t, want(t, plain.put(t, result, map[string]any{"iteration": 1, "steps": []any{map[string]any{"step": 2, "outcome": "failed", "note": "It slid."}}}),
		http.StatusOK, "fail the second step"), "case")
	if run := failed["run"].(map[string]any); run["outcome"] != "failed" || run["source"] != "tester" || run["recordedBy"].(map[string]any)["id"] != plain.user.String() {
		t.Errorf("a failed step fails the case: %v", run)
	}
	file := obj(t, want(t, plain.uploadAs(t, "/api/v1/test-executions/"+s.execution+"/cases/"+s.key+"-R2/files?iteration=1&step=2", "slide.png", "image/png", aPicture(t)),
		http.StatusCreated, "put a file on the step"), "file")
	got := want(t, guest.get(t, "/api/v1/test-execution-files/"+file["id"].(string)), http.StatusOK, "a reader downloads it")
	if !bytes.Equal(got.Raw, aPicture(t)) {
		t.Error("the file came back changed")
	}
	refuse(t, "somebody else deletes the file", owner.delete(t, "/api/v1/test-execution-files/"+file["id"].(string)), http.StatusForbidden, "forbidden")
	refuse422(t, "an issue for a case that did not fail", plain.post(t, "/api/v1/test-executions/"+s.execution+"/cases/"+s.key+"-R2/issues",
		map[string]any{"iteration": 2, "armatureProject": "CP"}), "iteration")

	refuse(t, "a tester changes the execution", plain.patch(t, "/api/v1/test-executions/"+s.execution, map[string]any{"version": "2.4"}), http.StatusForbidden, "forbidden")
	want(t, owner.patch(t, "/api/v1/test-executions/"+s.execution, map[string]any{"version": "2.4"}), http.StatusOK, "the manager changes the version")
	mine := list(t, want(t, plain.get(t, "/api/v1/test-executions/mine"), http.StatusOK, "the tester's executions"), "executions")
	if len(mine) == 0 || mine[0].(map[string]any)["id"] != s.execution {
		t.Errorf("the tester's own list: %v", mine)
	}
	results := list(t, want(t, guest.get(t, "/api/v1/requirements/"+s.key+"-R2/test-executions"), http.StatusOK, "the case's results"), "results")
	if len(results) != 2 || results[0].(map[string]any)["outcome"] != "failed" {
		t.Errorf("the case's results: %v", results)
	}

	refuse(t, "a tester closes it", plain.post(t, "/api/v1/test-executions/"+s.execution+"/close", map[string]any{}), http.StatusForbidden, "forbidden")
	closed := obj(t, want(t, owner.post(t, "/api/v1/test-executions/"+s.execution+"/close", map[string]any{"comment": "Wet roads done."}),
		http.StatusOK, "close it"), "execution")
	if closed["state"] != "closed" || closed["closeComment"] != "Wet roads done." {
		t.Errorf("the closed execution: %v", closed)
	}
	refuse(t, "a result after closing", plain.put(t, result, map[string]any{"iteration": 2, "outcome": "passed"}), http.StatusConflict, "execution_closed")
	refuse(t, "a change after closing", owner.patch(t, "/api/v1/test-executions/"+s.execution, map[string]any{"title": "Again"}), http.StatusConflict, "execution_closed")

	report := obj(t, want(t, guest.get(t, "/api/v1/test-executions/"+s.execution+"/report"), http.StatusOK, "read the report"), "report")
	if len(report["cases"].([]any)) != 2 {
		t.Errorf("the report holds %d case iterations", len(report["cases"].([]any)))
	}
	word := unzipped(t, want(t, owner.get(t, "/api/v1/test-executions/"+s.execution+"/report.docx"), http.StatusOK, "the report as Word").Raw)["word/document.xml"]
	for _, wanted := range []string{"Release 1 on wet roads", "It slid.", "Press the pedal.", "Winter tyres", "slide.png"} {
		if !bytes.Contains(word, []byte(wanted)) {
			t.Errorf("the Word report lacks %q", wanted)
		}
	}
	pdf := want(t, owner.get(t, "/api/v1/test-executions/"+s.execution+"/report.pdf"), http.StatusOK, "the report as PDF")
	if !bytes.HasPrefix(pdf.Raw, []byte("%PDF-")) {
		t.Error("the PDF report is no PDF")
	}
	if n := s.f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'test_execution.recorded' AND target_id = $1`, s.execution); n != 2 {
		t.Errorf("%d recordings audited, want 2", n)
	}
	deadline := time.Now().Add(importWait)
	for {
		told := s.f.h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND execution_id = $2 AND kind = 'execution_assigned'`, plain.user, s.execution)
		if told == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the tester was told %d times that they test", told)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// A plan holds test cases of its project, picked or found by a query; an execution's testers may comment; whoever
// does not edit the project makes no plan.
func TestATestPlanHoldsTestCases(t *testing.T) {
	s := newTestCaseScene(t, "TEB")
	owner := s.f.owner
	s.readerOf(t)
	refuse422(t, "a requirement that is no test case", owner.post(t, "/api/v1/projects/"+s.key+"/test-plans", map[string]any{"title": "Mixed", "scope": "cases",
		"cases": []string{s.key + "-R1"}}), "cases")
	refuse(t, "somebody who does not edit", s.f.guest.post(t, "/api/v1/projects/"+s.key+"/test-plans", map[string]any{"title": "Mine", "scope": "cases",
		"cases": []string{s.key + "-R2"}}), http.StatusForbidden, "forbidden")
	byQuery := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/test-plans", map[string]any{"title": "All cases", "scope": "query",
		"query": `type = "Test case"`}), http.StatusCreated, "a plan by query"), "plan")
	if cases := byQuery["cases"].([]any); len(cases) != 1 || cases[0].(map[string]any)["key"] != s.key+"-R2" {
		t.Errorf("the query finds %v", cases)
	}
	id := byQuery["id"].(string)
	refuse422(t, "cases set on a plan by query", owner.put(t, "/api/v1/test-plans/"+id+"/cases", map[string]any{"cases": []string{s.key + "-R2"}}), "cases")
	want(t, owner.patch(t, "/api/v1/test-plans/"+id, map[string]any{"title": "Every case"}), http.StatusOK, "rename it")
	refuse(t, "somebody outside reads it", s.f.outside.get(t, "/api/v1/test-plans/"+id), http.StatusNotFound, "not_found")
	refuse(t, "somebody who does not edit changes it", s.f.guest.patch(t, "/api/v1/test-plans/"+id, map[string]any{"title": "Mine"}), http.StatusForbidden, "forbidden")
	plans := list(t, want(t, s.f.guest.get(t, "/api/v1/projects/"+s.key+"/test-plans"), http.StatusOK, "a reader lists the plans"), "plans")
	if len(plans) != 1 {
		t.Errorf("the project lists %d plans", len(plans))
	}
	refuse422(t, "a tester outside the project", owner.post(t, "/api/v1/projects/"+s.key+"/test-executions", map[string]any{"title": "Run", "planId": id,
		"testers": []any{map[string]any{"subjectType": "user", "userId": s.f.outside.user}}}), "testers")
	refuse(t, "an execution's state nobody has", owner.get(t, "/api/v1/projects/"+s.key+"/test-executions?state=paused"), http.StatusUnprocessableEntity, "validation_failed")
	refuse(t, "an execution nobody has", owner.get(t, "/api/v1/test-executions/"+uuid.NewString()), http.StatusNotFound, "not_found")
	refuse(t, "a case's results of a key nobody has", owner.get(t, "/api/v1/requirements/"+s.key+"-R99/test-executions"), http.StatusNotFound, "not_found")
	refuse(t, "a file nobody has", owner.get(t, "/api/v1/test-execution-files/"+uuid.NewString()), http.StatusNotFound, "not_found")
	refuse(t, "a file of a case nobody has", owner.uploadAs(t, "/api/v1/test-executions/"+uuid.NewString()+"/cases/"+s.key+"-R2/files", "a.png", "image/png", aPicture(t)),
		http.StatusNotFound, "not_found")
	refuse(t, "a case of an execution nobody has", owner.get(t, "/api/v1/test-executions/"+uuid.NewString()+"/cases/"+s.key+"-R2"), http.StatusNotFound, "not_found")
	refuse(t, "a report of an execution nobody has", owner.get(t, "/api/v1/test-executions/"+uuid.NewString()+"/report"), http.StatusNotFound, "not_found")
	refuse(t, "a Word report of an execution nobody has", owner.get(t, "/api/v1/test-executions/"+uuid.NewString()+"/report.docx"), http.StatusNotFound, "not_found")
	refuse(t, "a PDF report of an execution nobody has", owner.get(t, "/api/v1/test-executions/"+uuid.NewString()+"/report.pdf"), http.StatusNotFound, "not_found")
	refuse(t, "a list of a project nobody has", owner.get(t, "/api/v1/projects/NOPE"+s.key+"/test-plans"), http.StatusNotFound, "not_found")
	refuse(t, "the executions of a project nobody has", owner.get(t, "/api/v1/projects/NOPE"+s.key+"/test-executions"), http.StatusNotFound, "not_found")
	refuse(t, "my executions signed out", (&client{api: s.f.api, http: &http.Client{}}).get(t, "/api/v1/test-executions/mine"), http.StatusUnauthorized, "unauthorized")
	want(t, owner.delete(t, "/api/v1/test-plans/"+id), http.StatusNoContent, "delete it")
	refuse(t, "a plan deleted", owner.delete(t, "/api/v1/test-plans/"+id), http.StatusNotFound, "not_found")
}

// A tester files a failed case as an Armature issue with their token, linked with relates to the case and what it
// verifies.
func TestAFailedCaseIsFiledAsAnIssue(t *testing.T) {
	s := newExecutionScene(t, "TEC")
	h := s.f.h
	h.keepArmature(t)
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	want(t, s.f.owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, s.f.plain.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "the tester connects")
	base := "/api/v1/test-executions/" + s.execution + "/cases/" + s.key + "-R2"
	want(t, s.f.plain.put(t, base+"/result", map[string]any{"iteration": 1, "steps": []any{map[string]any{"step": 2, "outcome": "failed", "note": "It slid."}}}),
		http.StatusOK, "fail a step")
	issue := obj(t, want(t, s.f.plain.post(t, base+"/issues", map[string]any{"iteration": 1, "step": 2, "armatureProject": "cp"}), http.StatusCreated, "file it"), "issue")
	key := issue["key"].(string)
	links := list(t, want(t, s.f.plain.get(t, "/api/v1/requirements/"+s.key+"-R1/armature-links"), http.StatusOK, "the requirement's issues"), "links")
	if len(links) != 1 || links[0].(map[string]any)["issueKey"] != key || links[0].(map[string]any)["relation"] != "relates" {
		t.Errorf("the verified requirement is linked with relates: %v", links)
	}
	if c := caseOf(t, s.f.plain, s.execution, s.key+"-R2", 1); len(c["issues"].([]any)) != 1 {
		t.Errorf("the case lists %v", c["issues"])
	}
	stub := stubIssue(t, slug, "bob", key)
	if !strings.Contains(stub["summary"].(string), s.key+"-R2") {
		t.Errorf("the issue's summary: %v", stub["summary"])
	}
}

// Straight through SQL as exciter_app: cases are pinned by the start alone, results are recorded by testers as
// themselves while the execution is open, and a plan holds test cases only.
func TestRawSQLKeepsExecutionsToTheirTesters(t *testing.T) {
	s := newExecutionScene(t, "TED")
	ctx := context.Background()
	var caseID, plainReq uuid.UUID
	for n, into := range map[int]*uuid.UUID{2: &caseID, 1: &plainReq} {
		if err := s.f.h.super.QueryRow(ctx, `SELECT id FROM requirement WHERE number = $2 AND project_id = (SELECT id FROM project WHERE key = $1)`, s.key, n).Scan(into); err != nil {
			t.Fatal(err)
		}
	}
	tester := s.f.h.appConn(t, s.f.plain.user)
	if _, err := tester.Exec(ctx, `INSERT INTO test_execution_case (execution_id, requirement_id, iteration, position, revision) VALUES ($1, $2, 9, 0, 1)`,
		s.execution, caseID); err == nil {
		t.Error("a tester pinned a case by hand")
	}
	if _, err := tester.Exec(ctx, `SELECT test_execution_start($1, ARRAY[$2::uuid])`, s.execution, caseID); err == nil {
		t.Error("a tester started the execution again")
	}
	if _, err := tester.Exec(ctx, `UPDATE test_execution_case SET outcome = 'passed', source = 'tester', recorded_by = $3, recorded_at = now()
		WHERE execution_id = $1 AND requirement_id = $2`, s.execution, caseID, s.f.owner.user); err == nil {
		t.Error("a tester recorded in somebody else's name")
	}
	reader := s.f.h.appConn(t, s.readerOf(t).user)
	tag, err := reader.Exec(ctx, `UPDATE test_execution_case SET outcome = 'passed', source = 'tester', recorded_by = $3, recorded_at = now()
		WHERE execution_id = $1 AND requirement_id = $2`, s.execution, caseID, s.f.guest.user)
	if err == nil && tag.RowsAffected() != 0 {
		t.Error("somebody who does not test recorded a result")
	}
	owner := s.f.h.appConn(t, s.f.owner.user)
	if _, err := owner.Exec(ctx, `UPDATE test_execution SET state = 'closed', closed_at = now() WHERE id = $1`, s.execution); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `UPDATE test_execution SET title = 'Again' WHERE id = $1`, s.execution); err == nil {
		t.Error("a closed execution changed")
	}
	if tag, err := owner.Exec(ctx, `UPDATE test_execution_case SET outcome = 'passed', source = 'tester', recorded_by = $3, recorded_at = now()
		WHERE execution_id = $1 AND requirement_id = $2`, s.execution, caseID, s.f.owner.user); err == nil && tag.RowsAffected() != 0 {
		t.Error("a result was recorded in a closed execution")
	}
	if _, err := owner.Exec(ctx, `INSERT INTO test_plan_case (plan_id, requirement_id, position) VALUES ($1, $2, 9)`, s.plan, plainReq); err == nil {
		t.Error("a plan took a requirement that is no test case")
	}
}
