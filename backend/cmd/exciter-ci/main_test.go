package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// fakeExciter answers an upload, a run that lands on the second read, and a trace check with the verdict given.
func fakeExciter(t *testing.T, verdict string) (*httptest.Server, *http.Request) {
	t.Helper()
	var reads atomic.Int32
	seen := &http.Request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer exciter_pat_ci" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"Sign in."}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/CHK/test-runs":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			*seen = *r
			if len(r.MultipartForm.File["file"]) != 2 || r.FormValue("commit") == "" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"error":{"code":"validation_failed","message":"Fix the form.","fields":{"commit":"Give the commit."}}}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"run":{"id":"r1","state":"queued"}}`))
		case r.URL.Path == "/api/v1/test-runs/r1":
			state := "running"
			if reads.Add(1) > 1 {
				state = "done"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"run": map[string]any{"id": "r1", "state": state, "cases": 3, "matched": 2,
				"counts":   map[string]int{"passed": 2, "failed": 1},
				"reported": []map[string]string{{"key": "CHK-R99", "test": "lights", "problem": "unknown"}}}})
		case r.URL.Path == "/api/v1/projects/CHK/trace-check":
			*seen = *r
			_ = json.NewEncoder(w).Encode(map[string]any{"check": map[string]any{"verdict": verdict, "branch": "main", "fail": []string{"failing"},
				"chains": false, "checked": 2, "evidence": map[string]int{"runs": 1},
				"failing": []map[string]any{{"requirement": map[string]string{"key": "CHK-R3"}, "name": "lights", "outcome": "failed"}},
				"totals":  map[string]int{"failing": 3}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

func pollEveryForTests(t *testing.T) {
	was := pollEvery
	pollEvery = time.Millisecond
	t.Cleanup(func() { pollEvery = was })
}

func TestResultsUploadsTheReportsWaitsAndSaysWhatVerifiesNothing(t *testing.T) {
	srv, seen := fakeExciter(t, "pass")
	dir := t.TempDir()
	for _, name := range []string{"a.xml", "b.xml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("<testsuite/>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	env := envOf(map[string]string{"EXCITER_URL": srv.URL, "EXCITER_TOKEN": "exciter_pat_ci", "EXCITER_PROJECT": "CHK",
		"GITHUB_ACTIONS": "true", "GITHUB_REPOSITORY": "acme/checkout", "GITHUB_SHA": "4f2c9a1e", "GITHUB_REF_NAME": "main",
		"GITHUB_SERVER_URL": "https://github.com", "GITHUB_RUN_ID": "7"})
	pollEveryForTests(t)
	code := run(context.Background(), []string{"results", filepath.Join(dir, "*.xml")}, env, &out, &errOut)
	if code != exitPass {
		t.Fatalf("results exits %d: %s %s", code, out.String(), errOut.String())
	}
	if seen.FormValue("repository") != "acme/checkout" || seen.FormValue("runUrl") != "https://github.com/acme/checkout/actions/runs/7" {
		t.Errorf("the form named %q and %q", seen.FormValue("repository"), seen.FormValue("runUrl"))
	}
	if !strings.Contains(out.String(), "3 test cases read, 2 name a requirement") || !strings.Contains(out.String(), "CHK-R99 verifies nothing (unknown)") {
		t.Errorf("results printed %s", out.String())
	}
}

func TestCheckExitsOnItsVerdictAndPrintsWhatFailed(t *testing.T) {
	for verdict, want := range map[string]int{"pass": exitPass, "fail": exitFail} {
		srv, seen := fakeExciter(t, verdict)
		var out, errOut bytes.Buffer
		env := envOf(map[string]string{"EXCITER_URL": srv.URL, "EXCITER_TOKEN": "exciter_pat_ci", "EXCITER_PROJECT": "CHK"})
		if code := run(context.Background(), []string{"check", "--fail", "failing", "--query", "type = Feature"}, env, &out, &errOut); code != want {
			t.Errorf("a %s exits %d: %s", verdict, code, errOut.String())
		}
		if seen.URL.Query().Get("fail") != "failing" || seen.URL.Query().Get("query") != "type = Feature" {
			t.Errorf("the check asked %s", seen.URL.RawQuery)
		}
		if !strings.Contains(out.String(), "failing CHK-R3: lights failed") || !strings.Contains(out.String(), "and 2 more failing") ||
			!strings.Contains(out.String(), "no trace model") {
			t.Errorf("the check printed %s", out.String())
		}
	}
}

func TestACommandThatCannotAskExitsTwoWithASentence(t *testing.T) {
	srv, _ := fakeExciter(t, "pass")
	for name, c := range map[string]struct {
		args []string
		env  map[string]string
		says string
	}{
		"no address":    {[]string{"check"}, map[string]string{"EXCITER_TOKEN": "t", "EXCITER_PROJECT": "CHK"}, "EXCITER_URL"},
		"no token":      {[]string{"check"}, map[string]string{"EXCITER_URL": srv.URL, "EXCITER_PROJECT": "CHK"}, "EXCITER_TOKEN"},
		"no reports":    {[]string{"results"}, map[string]string{"EXCITER_URL": srv.URL, "EXCITER_TOKEN": "t", "EXCITER_PROJECT": "CHK"}, "reports"},
		"a wrong token": {[]string{"check"}, map[string]string{"EXCITER_URL": srv.URL, "EXCITER_TOKEN": "nope", "EXCITER_PROJECT": "CHK"}, "Sign in."},
		"no command":    {nil, nil, "exciter-ci talks to Exciter"},
		"a bad command": {[]string{"deploy"}, nil, `no command "deploy"`},
	} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), c.args, envOf(c.env), &out, &errOut); code != exitBroken || !strings.Contains(errOut.String(), c.says) {
			t.Errorf("%s exits %d saying %s", name, code, errOut.String())
		}
	}
}

func TestWhereARunIsComesFromTheCIItRunsIn(t *testing.T) {
	gitlab := detect(envOf(map[string]string{"GITLAB_CI": "true", "CI_PROJECT_PATH": "acme/checkout", "CI_COMMIT_SHA": "abc1234",
		"CI_COMMIT_REF_NAME": "main", "CI_MERGE_REQUEST_SOURCE_BRANCH_NAME": "feature/x", "CI_JOB_NAME": "unit", "CI_JOB_URL": "https://gitlab.example/j/1"}))
	if gitlab != (where{"acme/checkout", "abc1234", "feature/x", "unit", "https://gitlab.example/j/1"}) {
		t.Errorf("GitLab reads %+v", gitlab)
	}
	pr := detect(envOf(map[string]string{"GITEA_ACTIONS": "true", "GITHUB_REPOSITORY": "acme/checkout", "GITHUB_SHA": "abc1234",
		"GITHUB_REF_NAME": "12/merge", "GITHUB_HEAD_REF": "feature/y"}))
	if pr.branch != "feature/y" || pr.runURL != "" {
		t.Errorf("a pull request reads %+v", pr)
	}
	if detect(envOf(nil)) != (where{}) {
		t.Error("a pipeline outside any CI reads something")
	}
}
