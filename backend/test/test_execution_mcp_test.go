//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"
)

// An assistant reads plans and executions, starts one, records and closes it with the tools, each answering what
// the API answers; a reader's assistant neither starts nor records (#324).
func TestAnAssistantRunsATestExecution(t *testing.T) {
	s := newExecutionScene(t, "TXM")
	owner, plain, guest := s.f.owner, s.f.plain, s.readerOf(t)
	_, managerSecret := makeToken(t, owner, map[string]any{"name": "manager's assistant"})
	_, testerSecret := makeToken(t, plain, map[string]any{"name": "tester's assistant"})
	_, readerSecret := makeToken(t, guest, map[string]any{"name": "reader's assistant"})
	s.f.h.settle(t)
	byManager, byTester, byReader := connectAssistant(t, s.f.api, managerSecret), connectAssistant(t, s.f.api, testerSecret), connectAssistant(t, s.f.api, readerSecret)
	r2 := s.key + "-R2"

	t.Run("plans read as the API reads them", func(t *testing.T) {
		sameAnswer(t, "list_test_plans", byManager.call(t, "list_test_plans", map[string]any{"projectKey": s.key}),
			want(t, owner.get(t, "/api/v1/projects/"+s.key+"/test-plans"), http.StatusOK, "list the plans"))
		sameAnswer(t, "get_test_plan", byManager.call(t, "get_test_plan", map[string]any{"planID": s.plan}),
			want(t, owner.get(t, "/api/v1/test-plans/"+s.plan), http.StatusOK, "read the plan"))
	})

	var started string
	t.Run("an execution started by the tool is the one the API reads", func(t *testing.T) {
		e := byManager.call(t, "start_test_execution", map[string]any{"projectKey": s.key, "title": "By the assistant", "planId": s.plan,
			"version": "2.4", "testers": []any{tester(plain.user)}})["execution"].(map[string]any)
		started = e["id"].(string)
		// A write through a token is not one the session's reads wait for.
		s.f.h.settle(t)
		read := obj(t, want(t, owner.get(t, "/api/v1/test-executions/"+started), http.StatusOK, "read it"), "execution")
		if read["title"] != "By the assistant" || read["version"] != "2.4" || read["state"] != "open" || len(read["cases"].([]any)) != len(e["cases"].([]any)) {
			t.Errorf("the tool started %v, the API reads %v", e, read)
		}
	})

	t.Run("executions and cases read as the API reads them", func(t *testing.T) {
		sameAnswer(t, "list_test_executions", byManager.call(t, "list_test_executions", map[string]any{"projectKey": s.key, "state": "open"}),
			want(t, owner.get(t, "/api/v1/projects/"+s.key+"/test-executions?state=open"), http.StatusOK, "list the open executions"))
		sameAnswer(t, "list_my_test_executions", byTester.call(t, "list_my_test_executions", map[string]any{}),
			want(t, plain.get(t, "/api/v1/test-executions/mine"), http.StatusOK, "the tester's own"))
		sameAnswer(t, "get_test_execution", byTester.call(t, "get_test_execution", map[string]any{"executionID": started}),
			want(t, plain.get(t, "/api/v1/test-executions/"+started), http.StatusOK, "read it"))
		sameAnswer(t, "get_test_execution_case", byTester.call(t, "get_test_execution_case", map[string]any{"executionID": started, "reqKey": r2, "iteration": 2}),
			want(t, plain.get(t, "/api/v1/test-executions/"+started+"/cases/"+r2+"?iteration=2"), http.StatusOK, "read the case"))
	})

	t.Run("a reader's assistant neither starts nor records", func(t *testing.T) {
		before := s.executions(t)
		if msg := byReader.refused(t, "start_test_execution", map[string]any{"projectKey": s.key, "title": "Mine", "planId": s.plan,
			"testers": []any{tester(guest.user)}}); !strings.HasSuffix(msg, ".") {
			t.Errorf("start_test_execution refused with %q", msg)
		}
		if msg := byReader.refused(t, "record_test_result", map[string]any{"executionID": started, "reqKey": r2, "iteration": 1, "outcome": "passed"}); !strings.HasSuffix(msg, ".") {
			t.Errorf("record_test_result refused with %q", msg)
		}
		if n := s.executions(t); n != before {
			t.Errorf("the reader's assistant started %d executions", n-before)
		}
		if outcome, _, _ := runOf(t, plain, started, r2, 1); outcome != nil {
			t.Errorf("the reader's assistant recorded %v", outcome)
		}
	})

	t.Run("a result recorded and an execution closed by the tools are what the API reads", func(t *testing.T) {
		if msg := byTester.refused(t, "record_test_result", map[string]any{"executionID": started, "reqKey": r2, "iteration": 1, "outcome": "maybe"}); !strings.Contains(msg,
			"outcome: Record passed, failed, blocked or skipped.") {
			t.Errorf("an outcome nobody has is refused with %q", msg)
		}
		// The case runs at its current revision, which has one step.
		recorded := byTester.call(t, "record_test_result", map[string]any{"executionID": started, "reqKey": r2, "iteration": 1,
			"steps": []any{map[string]any{"step": 1, "outcome": "failed", "note": "It slid."}}})
		s.f.h.settle(t)
		sameAnswer(t, "record_test_result", recorded, want(t, plain.get(t, "/api/v1/test-executions/"+started+"/cases/"+r2+"?iteration=1"),
			http.StatusOK, "read the case"))
		if run := recorded["case"].(map[string]any)["run"].(map[string]any); run["outcome"] != "failed" {
			t.Errorf("the recorded case reads %v", run)
		}
		closed := byManager.call(t, "close_test_execution", map[string]any{"executionID": started, "comment": "Done by the assistant."})["execution"].(map[string]any)
		if closed["state"] != "closed" || closed["closeComment"] != "Done by the assistant." {
			t.Errorf("close_test_execution answered %v", closed)
		}
		s.f.h.settle(t)
		sameAnswer(t, "get_test_case_results", byTester.call(t, "get_test_case_results", map[string]any{"reqKey": r2}),
			want(t, plain.get(t, "/api/v1/requirements/"+r2+"/test-executions"), http.StatusOK, "the case's results"))
	})
}

// A failure is filed only with Armature connected, by a tester, on an issue type and a step there are; a refusal
// reaches neither Armature nor the execution, and the tool files what the API files (#324).
func TestAFailureIssueIsRefusedBeforeItReachesArmature(t *testing.T) {
	s := newExecutionScene(t, "TXI")
	h := s.f.h
	h.keepArmature(t)
	plain, guest := s.f.plain, s.readerOf(t)
	_, secret := makeToken(t, plain, map[string]any{"name": "tester's assistant"})
	h.settle(t)
	assistant := connectAssistant(t, s.f.api, secret)
	base := "/api/v1/test-executions/" + s.execution + "/cases/" + s.key + "-R2"
	want(t, plain.put(t, base+"/result", map[string]any{"iteration": 1, "steps": []any{map[string]any{"step": 2, "outcome": "failed", "note": "It slid."}}}),
		http.StatusOK, "fail a step")
	s.f.h.settle(t)
	issue := func(c *client, change map[string]any) response {
		body := map[string]any{"iteration": 1, "step": 2, "armatureProject": "cp"}
		for k, v := range change {
			body[k] = v
		}
		return c.post(t, base+"/issues", body)
	}
	filed := func() int {
		return h.countRows(t, `SELECT count(*) FROM test_execution_issue WHERE execution_id = $1`, s.execution)
	}

	t.Run("with no Armature connected nothing is filed", func(t *testing.T) {
		refuse(t, "no Armature", issue(plain, nil), http.StatusConflict, "armature_not_configured")
		if msg := assistant.refused(t, "file_test_failure_issue", map[string]any{"executionID": s.execution, "reqKey": s.key + "-R2", "iteration": 1,
			"armatureProject": "cp"}); !strings.Contains(msg, "Armature") {
			t.Errorf("file_test_failure_issue refused with %q", msg)
		}
	})

	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	want(t, s.f.owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, plain.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "the tester connects")

	t.Run("a reader, an issue type that is no id and a step the case lacks are refused before Armature", func(t *testing.T) {
		refuse(t, "a reader files one", issue(guest, nil), http.StatusForbidden, "forbidden")
		refuse422(t, "an issue type that is no id", issue(plain, map[string]any{"typeId": "bug"}), "typeId")
		refuse422(t, "step 0", issue(plain, map[string]any{"step": 0}), "step")
		refuse422(t, "step 101", issue(plain, map[string]any{"step": 101}), "step")
		if got := stubSearch(t, slug, "bob", s.key+"-R2"); len(got) != 0 {
			t.Errorf("the refusals filed %v in Armature", got)
		}
		if n := filed(); n != 0 {
			t.Errorf("the refusals recorded %d issues", n)
		}
	})

	t.Run("the tool files the issue the case then lists", func(t *testing.T) {
		got := assistant.call(t, "file_test_failure_issue", map[string]any{"executionID": s.execution, "reqKey": s.key + "-R2", "iteration": 1, "step": 2,
			"armatureProject": "cp"})["issue"].(map[string]any)
		s.f.h.settle(t)
		issues := caseOf(t, plain, s.execution, s.key+"-R2", 1)["issues"].([]any)
		if len(issues) != 1 || issues[0].(map[string]any)["key"] != got["key"] || issues[0].(map[string]any)["step"] != float64(2) {
			t.Errorf("the tool filed %v, the case lists %v", got, issues)
		}
		if found := stubSearch(t, slug, "bob", s.key+"-R2"); len(found) != 1 {
			t.Errorf("Armature holds %d issues of the case", len(found))
		}
	})
}
