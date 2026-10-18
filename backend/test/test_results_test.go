//go:build integration

package test

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/testresult"
)

// Test results from CI (#200): a run's JUnit files land on the requirements their cases name, at the revision current
// when it was uploaded, and coverage, queries, baselines and the trace check read them.

const aCommit = "4f2c9a1e7b3d"

// junit is a report of cases, each a name and how it ended.
func junit(cases ...[2]string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><testsuites><testsuite name="brakes">`)
	for _, c := range cases {
		fmt.Fprintf(&b, `<testcase classname="brakes" name=%q>`, c[0])
		switch c[1] {
		case "failed":
			b.WriteString(`<failure message="lights stayed off"/>`)
		case "errored":
			b.WriteString(`<error message="no pedal"/>`)
		case "skipped":
			b.WriteString(`<skipped/>`)
		}
		b.WriteString(`</testcase>`)
	}
	b.WriteString(`</testsuite></testsuites>`)
	return []byte(b.String())
}

// uploadRun sends a run's form and files to a project, as exciter-ci does.
func uploadRun(t *testing.T, c *client, projectKey string, fields map[string]string, files ...[]byte) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = form.WriteField(k, v)
	}
	for i, f := range files {
		part, err := form.CreateFormFile("file", fmt.Sprintf("report-%d.xml", i))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(f)
	}
	_ = form.Close()
	req, err := http.NewRequest(http.MethodPost, c.api.srv.URL+"/api/v1/projects/"+projectKey+"/test-runs", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	return c.do(t, req)
}

func runForm(branch string) map[string]string {
	return map[string]string{"repository": "acme/checkout", "commit": aCommit, "branch": branch, "runUrl": "https://ci.example/runs/7"}
}

// ran uploads a run and waits for the stack's worker to land it, answering the run.
func ran(t *testing.T, c *client, projectKey string, fields map[string]string, files ...[]byte) map[string]any {
	t.Helper()
	made := obj(t, want(t, uploadRun(t, c, projectKey, fields, files...), http.StatusCreated, "upload a run"), "run")
	if made["state"] != "queued" {
		t.Fatalf("a new run reads %v", made)
	}
	deadline := time.Now().Add(importWait)
	for {
		got := obj(t, want(t, c.get(t, "/api/v1/test-runs/"+made["id"].(string)), http.StatusOK, "read the run"), "run")
		if state := got["state"]; state == "done" || state == "failed" {
			c.api.h.settle(t)
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run is still %v after %s", got["state"], importWait)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// resultsOf reads a requirement's results as test: outcome, revision and whether it is outdated.
func resultsOf(t *testing.T, c *client, reqKey, query string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, each := range list(t, want(t, c.get(t, "/api/v1/requirements/"+reqKey+"/test-results"+query), http.StatusOK, "read the results"), "results") {
		r := each.(map[string]any)
		out[r["branch"].(string)+" "+r["test"].(string)] = fmt.Sprintf("%v r%v outdated=%v counts=%v", r["outcome"], r["revision"], r["outdated"], r["counts"])
	}
	return out
}

func TestTestResultsLandOnTheRequirementsTheyNameAndTurnOutdated(t *testing.T) {
	s := newBaselineScene(t, "TRS")
	r1, r2, r3 := s.req("1"), s.req("2"), s.req("3")
	report := junit([2]string{"engage [verifies " + r1 + "]", "passed"}, [2]string{"lights [verifies " + r3 + ", " + s.key + "-R99]", "failed"},
		[2]string{"heading [verifies " + r2 + "]", "passed"}, [2]string{"untraced", "skipped"})

	var run map[string]any
	t.Run("an editor uploads a run and the worker lands it, saying what verifies nothing", func(t *testing.T) {
		run = ran(t, s.bob, s.key, runForm("main"), report)
		counts := run["counts"].(map[string]any)
		if run["state"] != "done" || run["cases"] != float64(4) || run["matched"] != float64(2) || counts["passed"] != float64(2) ||
			counts["failed"] != float64(1) || counts["skipped"] != float64(1) || run["commit"] != aCommit || run["uploadedBy"].(map[string]any)["id"] != s.bobID.String() {
			t.Fatalf("the run reads %v", run)
		}
		reported := map[string]string{}
		for _, each := range run["reported"].([]any) {
			r := each.(map[string]any)
			reported[r["key"].(string)] = r["problem"].(string)
		}
		if len(reported) != 2 || reported[r2] != "section" || reported[s.key+"-R99"] != "unknown" {
			t.Errorf("the run reports %v", reported)
		}
		if got := resultsOf(t, s.carol, r1, ""); got["main engage [verifies "+r1+"]"] != "passed r1 outdated=false counts=true" || len(got) != 1 {
			t.Errorf("R1's results read %v", got)
		}
		if got := resultsOf(t, s.carol, r3, ""); got["main lights [verifies "+r3+", "+s.key+"-R99]"] != "failed r1 outdated=false counts=true" {
			t.Errorf("R3's results read %v", got)
		}
	})

	t.Run("a run is its uploader's and the administrators' to read, and an editor's to upload", func(t *testing.T) {
		id := run["id"].(string)
		want(t, s.owner.get(t, "/api/v1/test-runs/"+id), http.StatusOK, "the administrator reads the run")
		refuse(t, "a reader reads somebody's run", s.carol.get(t, "/api/v1/test-runs/"+id), http.StatusNotFound, "not_found")
		refuse(t, "an unknown run", s.bob.get(t, "/api/v1/test-runs/"+uuid.NewString()), http.StatusNotFound, "not_found")
		refuse(t, "a reader uploads", uploadRun(t, s.carol, s.key, runForm("main"), report), http.StatusForbidden, "forbidden")
		refuse(t, "results of a requirement nobody has", s.carol.get(t, "/api/v1/requirements/"+s.key+"-R99/test-results"), http.StatusNotFound, "not_found")
	})

	t.Run("an upload says which field or file does not hold", func(t *testing.T) {
		bad := runForm("main")
		bad["commit"] = "not-a-sha"
		fieldError(t, uploadRun(t, s.bob, s.key, bad, report), "commit")
		bad = runForm("main")
		bad["runUrl"] = "javascript:alert(1)"
		fieldError(t, uploadRun(t, s.bob, s.key, bad, report), "runUrl")
		bad = runForm("main")
		delete(bad, "repository")
		fieldError(t, uploadRun(t, s.bob, s.key, bad, report), "repository")
		fieldError(t, uploadRun(t, s.bob, s.key, runForm("main")), "file")
		if msg := fieldError(t, uploadRun(t, s.bob, s.key, runForm("main"), []byte("<html/>")), "file"); !strings.Contains(msg, "report-0.xml") {
			t.Errorf("a stray file is refused with %q", msg)
		}
	})

	t.Run("a revision past the result makes it outdated, and a later run current again", func(t *testing.T) {
		want(t, s.owner.patch(t, "/api/v1/requirements/"+r1, map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}), http.StatusOK, "revise R1")
		if got := resultsOf(t, s.carol, r1, ""); got["main engage [verifies "+r1+"]"] != "passed r1 outdated=true counts=true" {
			t.Errorf("R1's result after a revision reads %v", got)
		}
		ran(t, s.bob, s.key, runForm("main"), junit([2]string{"engage [verifies " + r1 + "]", "passed"}))
		if got := resultsOf(t, s.carol, r1, ""); got["main engage [verifies "+r1+"]"] != "passed r2 outdated=false counts=true" {
			t.Errorf("R1's result after a new run reads %v", got)
		}
		if got := resultsOf(t, s.carol, r3, ""); len(got) != 0 {
			t.Errorf("a test the latest run no longer holds still reads %v", got)
		}
	})

	t.Run("a branch's results show beside the counting branch's and count only once it is named", func(t *testing.T) {
		ran(t, s.bob, s.key, runForm("feature/lights"), junit([2]string{"engage [verifies " + r1 + "]", "failed"}))
		got := resultsOf(t, s.carol, r1, "")
		if got["feature/lights engage [verifies "+r1+"]"] != "failed r2 outdated=false counts=false" || len(got) != 2 {
			t.Errorf("R1 reads %v", got)
		}
		if only := resultsOf(t, s.carol, r1, "?branch=feature/lights"); len(only) != 1 {
			t.Errorf("one branch reads %v", only)
		}
		settings := "/api/v1/projects/" + s.key + "/evidence-settings"
		if got := obj(t, want(t, s.carol.get(t, settings), http.StatusOK, "read the settings"), "settings"); got["countingBranch"] != "main" {
			t.Errorf("a new project counts %v", got)
		}
		refuse(t, "an editor names the counting branch", s.bob.put(t, settings, map[string]any{"countingBranch": "release"}), http.StatusForbidden, "forbidden")
		fieldError(t, s.owner.put(t, settings, map[string]any{"countingBranch": "  "}), "countingBranch")
		if got := obj(t, want(t, s.owner.put(t, settings, map[string]any{"countingBranch": "feature/lights"}), http.StatusOK, "name the branch"), "settings"); got["countingBranch"] != "feature/lights" {
			t.Errorf("the settings read %v", got)
		}
		s.owner.api.h.settle(t)
		if got := resultsOf(t, s.carol, r1, ""); got["feature/lights engage [verifies "+r1+"]"] != "failed r2 outdated=false counts=true" {
			t.Errorf("after naming the branch R1 reads %v", got)
		}
		want(t, s.owner.put(t, settings, map[string]any{"countingBranch": "main"}), http.StatusOK, "count main again")
	})
}

func TestCoverageQueriesAndTheTraceCheckFollowTheEvidence(t *testing.T) {
	s := newBaselineScene(t, "TRC")
	r1, r3 := s.req("1"), s.req("3")
	rules := "/api/v1/projects/" + s.key + "/coverage-rules"
	fieldError(t, s.owner.put(t, rules, map[string]any{"verified": true, "verifiedFrom": []string{"opinions"}}), "verifiedFrom")
	rule := obj(t, want(t, s.owner.put(t, rules, map[string]any{"verified": true, "verifiedFrom": []string{"tests"}}), http.StatusOK, "verify by tests"), "rule")
	if fmt.Sprint(rule["verifiedFrom"]) != "[tests]" {
		t.Fatalf("the rule reads %v", rule)
	}
	ran(t, s.bob, s.key, runForm("main"), junit([2]string{"engage [verifies " + r1 + "]", "passed"}, [2]string{"lights [verifies " + r3 + "]", "failed"}))

	verified := func(query string) []string {
		t.Helper()
		return rowKeys(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/requirements?kind=requirement&query="+url.QueryEscape(query)), http.StatusOK, "query "+query))
	}
	levels := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		for _, each := range list(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/coverage"), http.StatusOK, "read the coverage"), "rows") {
			r := each.(map[string]any)
			out[r["requirement"].(map[string]any)["key"].(string)] = r["verified"].(string)
		}
		return out
	}

	t.Run("passing, current tests verify in full, failing ones in part, and links alone no more", func(t *testing.T) {
		if got := levels(); got[r1] != "full" || got[r3] != "partial" {
			t.Errorf("coverage reads %v", got)
		}
		if got := verified("verified = full"); fmt.Sprint(got) != "["+r1+"]" {
			t.Errorf("verified = full keeps %v", got)
		}
		if got := verified("testResult = failed"); fmt.Sprint(got) != "["+r3+"]" {
			t.Errorf("testResult = failed keeps %v", got)
		}
	})

	t.Run("the matrix lays out the tests as columns and a model of tests breaks its chains at a failing one", func(t *testing.T) {
		m := obj(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/matrix?kind=requirement&columns=tests"), http.StatusOK, "the tests' matrix"), "matrix")
		cells := map[string]string{}
		for _, each := range m["cells"].([]any) {
			c := each.(map[string]any)
			row := m["rows"].([]any)[int(c["row"].(float64))].(map[string]any)["key"].(string)
			column := m["columns"].([]any)[int(c["column"].(float64))].(map[string]any)["test"].(map[string]any)["name"].(string)
			cells[row+" "+column] = c["links"].([]any)[0].(map[string]any)["name"].(string)
		}
		if len(m["columns"].([]any)) != 2 || cells[r1+" engage [verifies "+r1+"]"] != "passed" || cells[r3+" lights [verifies "+r3+"]"] != "failed" {
			t.Errorf("the matrix reads %v", cells)
		}
		refuse(t, "a repository for issue columns", s.carol.get(t, "/api/v1/projects/"+s.key+"/matrix?columns=issues&columnRepository=acme/checkout"),
			http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a baseline's matrix of tests", s.carol.get(t, "/api/v1/baselines/"+uuid.NewString()+"/matrix?columns=tests"),
			http.StatusUnprocessableEntity, "validation_failed")

		typeID := obj(t, want(t, s.carol.get(t, "/api/v1/requirements/"+r1), http.StatusOK, "read R1"), "requirement")["type"].(map[string]any)["id"].(string)
		model := map[string]any{"levels": []map[string]any{
			{"name": "Requirement", "kind": "requirements", "typeIds": []string{typeID}},
			{"name": "Test", "kind": "tests", "steps": []map[string]any{{"relation": "verifies"}}},
		}}
		want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/trace-model", model), http.StatusOK, "end the model in tests")
		chains := obj(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/trace-chains"), http.StatusOK, "follow the chains"), "chains")
		broken := map[string]bool{}
		for _, each := range chains["lines"].([]any) {
			line := each.(map[string]any)
			broken[chains["roots"].([]any)[int(line["root"].(float64))].(map[string]any)["key"].(string)] = line["broken"].(bool)
		}
		if broken[r1] || !broken[r3] {
			t.Errorf("the chains break at %v", broken)
		}
		check := obj(t, want(t, s.bob.get(t, "/api/v1/projects/"+s.key+"/trace-check?fail=broken"), http.StatusOK, "check the chains"), "check")
		if check["chains"] != true || check["verdict"] != "fail" || len(check["broken"].([]any)) != 1 {
			t.Errorf("the check of the chains reads %v", check)
		}
		want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/trace-model", map[string]any{"levels": []any{}}), http.StatusOK, "clear the model")
	})

	t.Run("the trace check fails on failing tests and passes a filter that leaves them out", func(t *testing.T) {
		check := "/api/v1/projects/" + s.key + "/trace-check"
		got := obj(t, want(t, s.bob.get(t, check), http.StatusOK, "check the trace"), "check")
		if got["verdict"] != "fail" || got["branch"] != "main" || got["chains"] != false || len(got["failing"].([]any)) != 1 ||
			got["evidence"].(map[string]any)["runs"] != float64(1) {
			t.Errorf("the check reads %v", got)
		}
		passing := obj(t, want(t, s.bob.get(t, check+"?fail=failing&query="+url.QueryEscape("key = "+r1)), http.StatusOK, "check R1"), "check")
		if passing["verdict"] != "pass" || passing["checked"] != float64(1) {
			t.Errorf("R1 alone reads %v", passing)
		}
		empty := obj(t, want(t, s.bob.get(t, check+"?branch=nowhere&fail=failing"), http.StatusOK, "check a branch without runs"), "check")
		if empty["verdict"] != "pass" || empty["evidence"].(map[string]any)["runs"] != float64(0) || empty["branch"] != "nowhere" {
			t.Errorf("a branch without runs reads %v", empty)
		}
		refuse(t, "an unknown reason", s.bob.get(t, check+"?fail=feelings"), http.StatusBadRequest, "bad_request")
		refuse(t, "a guest of the installation checks", s.owner.api.as(t, s.h.addPerson(t, "guest")).get(t, check), http.StatusNotFound, "not_found")

		want(t, s.owner.patch(t, "/api/v1/requirements/"+r1, map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}), http.StatusOK, "revise R1")
		outdated := obj(t, want(t, s.bob.get(t, check+"?fail=outdated&query="+url.QueryEscape("key = "+r1)), http.StatusOK, "check R1 outdated"), "check")
		if outdated["verdict"] != "fail" || len(outdated["outdated"].([]any)) != 1 {
			t.Errorf("an outdated R1 reads %v", outdated)
		}
		if got := verified("testOutdated > 0"); fmt.Sprint(got) != "["+r1+"]" {
			t.Errorf("testOutdated > 0 keeps %v", got)
		}
		if got := levels(); got[r1] != "partial" {
			t.Errorf("an outdated test verifies R1 %s", got[r1])
		}
	})

	t.Run("a baseline records the evidence it held, and a signature covers it", func(t *testing.T) {
		b := obj(t, want(t, s.owner.post(t, s.baselines, map[string]any{"name": "With evidence"}), http.StatusCreated, "take a baseline"), "baseline")
		var held, outdated int
		if err := s.h.super.QueryRow(context.Background(), `SELECT count(*), count(*) FILTER (WHERE revision = 1 AND outcome = 'passed')
			FROM baseline_evidence WHERE baseline_id = $1`, b["id"]).Scan(&held, &outdated); err != nil {
			t.Fatal(err)
		}
		if held != 2 || outdated != 1 {
			t.Errorf("the baseline holds %d results, %d of R1 at its first revision", held, outdated)
		}
		var lines int
		if err := s.h.appConn(t, s.ownerID).QueryRow(context.Background(), `SELECT count(*) FROM baseline_content_rows($1) WHERE kind = 'evidence'`,
			b["id"]).Scan(&lines); err != nil || lines != 2 {
			t.Errorf("the signed content holds %d lines of evidence: %v", lines, err)
		}
	})
}

func TestAnEvidenceTokenUploadsAndChecksAndNothingElse(t *testing.T) {
	s := newBaselineScene(t, "TRT")
	refuse(t, "two scopes at once", s.bob.post(t, "/api/v1/tokens", map[string]any{"name": "ci", "scopes": []string{"read", "evidence"}}),
		http.StatusUnprocessableEntity, "validation_failed")
	_, secret := makeToken(t, s.bob, map[string]any{"name": "ci", "scopes": []string{"evidence"}})
	ci := s.bob.api.withToken(s.bobID, secret)
	run := ran(t, ci, s.key, runForm("main"), junit([2]string{"engage [verifies " + s.req("1") + "]", "passed"}))
	if run["via"].(map[string]any)["name"] != "ci" {
		t.Errorf("a run uploaded with a token reads via %v", run["via"])
	}
	want(t, ci.get(t, "/api/v1/auth/me"), http.StatusOK, "the token says whose it is")
	want(t, ci.get(t, "/api/v1/projects/"+s.key+"/trace-check"), http.StatusOK, "the token checks the trace")
	refuse(t, "the token reads results", ci.get(t, "/api/v1/requirements/"+s.req("1")+"/test-results"), http.StatusForbidden, "evidence_token")
	refuse(t, "the token writes a requirement", ci.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Sneaked in"}),
		http.StatusForbidden, "evidence_token")
	refuse(t, "the token speaks MCP", ci.post(t, "/api/v1/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}),
		http.StatusForbidden, "evidence_token")
}

func TestAnAppGrantedEvidenceUploadsOutsideMCPAndNotAtIt(t *testing.T) {
	h := newHarness(t)
	a := newOAuthServer(t, h)
	owner := h.owner(t, "oauth evidence")
	person := a.as(t, owner.user)
	key := projectKey(t, "TRO")
	want(t, person.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Traced"}), http.StatusCreated, "make a project")
	want(t, person.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage"}), http.StatusCreated, "write R1")
	c, _, _ := register(t, a, newAddress(), nil)
	back, err := consent(t, person, c.authorizeURL(map[string]string{"scope": "evidence"}), map[string]any{"allow": true})
	if err != nil {
		t.Fatal(err)
	}
	tokens := c.exchange(t, back.Query().Get("code"))
	if tokens["scope"] != "evidence" {
		t.Errorf("the grant's tokens are for %v", tokens["scope"])
	}
	app := a.withToken(owner.user, tokens["access_token"].(string))
	run := ran(t, app, key, runForm("main"), junit([2]string{"engage [verifies " + key + "-R1]", "passed"}))
	if run["matched"] != float64(1) {
		t.Errorf("the app's run reads %v", run)
	}
	resp := mcpAs(t, a, tokens["access_token"].(string), "tools/list", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("an evidence token at MCP answers %d", resp.StatusCode)
	}
	refuse(t, "the app reads a project", app.get(t, "/api/v1/projects/"+key), http.StatusForbidden, "evidence_token")
	if to := sentTo(t, c.authorizeURL(map[string]string{"scope": "mcp evidence"})); to.Query().Get("error") != "invalid_scope" {
		t.Errorf("evidence asked beside mcp goes back to %s", to)
	}
}

func TestOldRunsArePrunedButNeverTheLatest(t *testing.T) {
	s := newBaselineScene(t, "TRP")
	latest := ran(t, s.bob, s.key, runForm("main"), junit([2]string{"engage [verifies " + s.req("1") + "]", "passed"}))
	ctx := context.Background()
	for i := range testresult.KeptRuns + 2 {
		if _, err := s.h.super.Exec(ctx, `
			INSERT INTO test_run (project_id, repository, commit_sha, branch, state, cases, uploaded_by, uploaded_at, finished_at)
			VALUES ($1, 'acme/checkout', $2, 'main', 'done', '[]', $3, now() - make_interval(hours => $4), now())`,
			s.projectID, aCommit, s.bobID, i+1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.h.super.Exec(ctx, `
		INSERT INTO test_run (project_id, repository, commit_sha, branch, state, cases, uploaded_by, uploaded_at, finished_at)
		VALUES ($1, 'acme/checkout', $2, 'stale', 'done', '[]', $3, now() - make_interval(days => $4), now())`,
		s.projectID, aCommit, s.bobID, testresult.IdleBranchDays+1); err != nil {
		t.Fatal(err)
	}
	if _, err := testresult.NewRetention(s.h.cluster, discard()).Once(ctx); err != nil {
		t.Fatal(err)
	}
	var kept, stale int
	var latestKept bool
	if err := s.h.super.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE branch = 'main'), count(*) FILTER (WHERE branch = 'stale'), bool_or(id = $2)
		FROM test_run WHERE project_id = $1`, s.projectID, latest["id"]).Scan(&kept, &stale, &latestKept); err != nil {
		t.Fatal(err)
	}
	if kept != testresult.KeptRuns || stale != 0 || !latestKept {
		t.Errorf("after pruning main keeps %d runs, the stale branch %d, the latest %v", kept, stale, latestKept)
	}
}

// The database holds runs and results to their rules for every writer, not only the service.
func TestRawSQLGuardsTestResults(t *testing.T) {
	s := newBaselineScene(t, "TRQ")
	ctx := context.Background()
	ran(t, s.bob, s.key, runForm("main"), junit([2]string{"engage [verifies " + s.req("1") + "]", "passed"}))
	insertRun := `INSERT INTO test_run (project_id, repository, commit_sha, branch, cases, uploaded_by) VALUES ($1, 'r', $2, 'main', '[]', $3) RETURNING id`

	reader := s.h.appConn(t, s.carolID)
	if _, err := reader.Exec(ctx, insertRun, s.projectID, aCommit, s.carolID); !isPolicyViolation(err) {
		t.Errorf("a reader uploads a run: %v", err)
	}
	editor := s.h.appConn(t, s.bobID)
	if _, err := editor.Exec(ctx, insertRun, s.projectID, aCommit, s.ownerID); !isPolicyViolation(err) {
		t.Errorf("an editor uploads in somebody's name: %v", err)
	}
	var mine uuid.UUID
	if err := editor.QueryRow(ctx, insertRun, s.projectID, aCommit, s.bobID).Scan(&mine); err != nil {
		t.Fatal(err)
	}
	insertResult := `INSERT INTO test_result (run_id, project_id, requirement_id, suite, name, outcome, revision) VALUES ($1, $2, $3, 's', $4, 'passed', 99)`
	if _, err := editor.Exec(ctx, insertResult, mine, s.projectID, s.r3ID, "while queued"); !isPolicyViolation(err) {
		t.Errorf("a result lands on a run that is not running: %v", err)
	}
	if _, err := editor.Exec(ctx, `UPDATE test_run SET state = 'running' WHERE id = $1`, mine); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.Exec(ctx, insertResult, mine, s.projectID, s.r3ID, "stamped"); err != nil {
		t.Fatal(err)
	}
	var stamped int
	if err := s.h.super.QueryRow(ctx, `SELECT revision FROM test_result WHERE run_id = $1`, mine).Scan(&stamped); err != nil || stamped != 1 {
		t.Errorf("a result sent as revision 99 is stamped %d: %v", stamped, err)
	}
	if _, err := editor.Exec(ctx, `UPDATE test_result SET outcome = 'failed' WHERE run_id = $1`, mine); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("a result is changed: %v", err)
	}
	if _, err := editor.Exec(ctx, `DELETE FROM test_result WHERE run_id = $1`, mine); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("a result is removed: %v", err)
	}
	if _, err := editor.Exec(ctx, `UPDATE test_run SET state = 'queued' WHERE id = $1`, mine); !refusedBy(err, "test_run_state") {
		t.Errorf("a run moves back: %v", err)
	}
	if _, err := editor.Exec(ctx, `INSERT INTO test_run_latest (project_id, repository, branch, job, run_id, uploaded_at) VALUES ($1, 'r', 'main', '', $2, now())`,
		s.projectID, mine); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("an editor names the latest run: %v", err)
	}
	if _, err := editor.Exec(ctx, `INSERT INTO evidence_settings (project_id, counting_branch, updated_by) VALUES ($1, 'x', $2)`, s.projectID, s.bobID); !isPolicyViolation(err) {
		t.Errorf("an editor names the counting branch: %v", err)
	}
	stranger := s.h.appConn(t, s.h.addPerson(t, "guest"))
	var seen int
	if err := stranger.QueryRow(ctx, `SELECT count(*) FROM test_result WHERE project_id = $1`, s.projectID).Scan(&seen); err != nil || seen != 0 {
		t.Errorf("a stranger reads %d results: %v", seen, err)
	}

	b := obj(t, want(t, s.owner.post(t, s.baselines, map[string]any{"name": "Frozen"}), http.StatusCreated, "take a baseline"), "baseline")
	owner := s.h.appConn(t, s.ownerID)
	if _, err := owner.Exec(ctx, `DELETE FROM baseline_evidence WHERE baseline_id = $1`, b["id"]); err == nil {
		t.Error("a baseline's evidence was removed")
	}
	if _, err := owner.Exec(ctx, `INSERT INTO baseline_evidence (baseline_id, requirement_id, kind, repository, branch, name, outcome, revision, commit_sha)
		VALUES ($1, $2, 'test', 'r', 'main', 'later', 'passed', 1, $3)`, b["id"], s.r3ID, aCommit); err == nil {
		t.Error("evidence was added to a baseline after it was taken")
	}
}

func TestAResultLinksToItsCommitInArmature(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "evidence-armature")
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	owner := api.as(t, home.user)
	key := projectKey(t, "TRA")
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "the owner connects as alice")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Traced"}), http.StatusCreated, "make a project")
	want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage"}), http.StatusCreated, "write R1")
	want(t, owner.post(t, "/api/v1/requirements/"+key+"-R1/armature-links", map[string]any{"issueKey": "CP-1", "relation": "implements"}),
		http.StatusCreated, "link CP-1")
	stubControl(t, http.MethodPost, slug+"/issues/CP-1/commits", map[string]any{"repository": "acme/checkout", "sha": aCommit + "0000",
		"url": "https://git.example/acme/checkout/commit/" + aCommit})
	ran(t, owner, key, runForm("main"), junit([2]string{"engage [verifies " + key + "-R1]", "passed"}))
	results := list(t, want(t, owner.get(t, "/api/v1/requirements/"+key+"-R1/test-results"), http.StatusOK, "read the results"), "results")
	if len(results) != 1 {
		t.Fatalf("R1 has %d results", len(results))
	}
	link, _ := results[0].(map[string]any)["armature"].(map[string]any)
	if link["issueKey"] != "CP-1" || link["url"] != "https://git.example/acme/checkout/commit/"+aCommit {
		t.Errorf("the result's commit links to %v", link)
	}
}
