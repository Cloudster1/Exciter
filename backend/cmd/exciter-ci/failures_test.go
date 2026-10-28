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
)

// landing is an Exciter whose upload answers run r7, and whose run reads as run says, counting the reads.
type landing struct {
	run   map[string]any
	reads atomic.Int32
}

func (l *landing) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/projects/CHK/test-runs":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"run":{"id":"r7","state":"queued"}}`))
		case "/api/v1/test-runs/r7":
			l.reads.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"run": l.run})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// aReport writes one report and answers its path.
func aReport(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "unit.xml")
	if err := os.WriteFile(path, []byte("<testsuite/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func ciEnv(url string) func(string) string {
	return envOf(map[string]string{"EXCITER_URL": url, "EXCITER_TOKEN": "t", "EXCITER_PROJECT": "CHK"})
}

var runFlags = []string{"results", "--repository", "acme/checkout", "--commit", "4f2c9a1e", "--branch", "main"}

func TestResultsExitsTwoWhenTheRunCouldNotLand(t *testing.T) {
	l := &landing{run: map[string]any{"id": "r7", "state": "failed", "error": "The run could not be read onto its requirements."}}
	srv := l.serve(t)
	pollEveryForTests(t)
	var out, errOut bytes.Buffer
	code := run(context.Background(), append(append([]string{}, runFlags...), aReport(t)), ciEnv(srv.URL), &out, &errOut)
	if code != exitBroken || !strings.Contains(errOut.String(), "The run could not be read onto its requirements.") {
		t.Errorf("a failed run exits %d saying %q", code, errOut.String())
	}
	if strings.Contains(out.String(), "test cases read") {
		t.Errorf("a failed run printed counts: %s", out.String())
	}
}

func TestResultsWithoutWaitingExitsZeroAndNeverReadsTheRun(t *testing.T) {
	l := &landing{run: map[string]any{"id": "r7", "state": "queued"}}
	srv := l.serve(t)
	var out, errOut bytes.Buffer
	code := run(context.Background(), append(append([]string{}, runFlags...), "--wait", "0", aReport(t)), ciEnv(srv.URL), &out, &errOut)
	if code != exitPass || !strings.Contains(out.String(), "as run r7") {
		t.Errorf("--wait 0 exits %d: %s %s", code, out.String(), errOut.String())
	}
	if n := l.reads.Load(); n != 0 {
		t.Errorf("--wait 0 read the run %d times", n)
	}
}

func TestResultsExitsTwoWhenTheRunIsStillQueuedPastTheWait(t *testing.T) {
	l := &landing{run: map[string]any{"id": "r7", "state": "queued"}}
	srv := l.serve(t)
	pollEveryForTests(t)
	var out, errOut bytes.Buffer
	code := run(context.Background(), append(append([]string{}, runFlags...), "--wait", "20ms", aReport(t)), ciEnv(srv.URL), &out, &errOut)
	if code != exitBroken || !strings.Contains(errOut.String(), "still queued") || !strings.Contains(errOut.String(), "/test-runs/r7") {
		t.Errorf("a run past the wait exits %d saying %q", code, errOut.String())
	}
	if l.reads.Load() < 2 {
		t.Errorf("the run was read %d times while waiting", l.reads.Load())
	}
}

func TestResultsSaysWhenAPatternMatchesNoReport(t *testing.T) {
	l := &landing{}
	srv := l.serve(t)
	var out, errOut bytes.Buffer
	pattern := filepath.Join(t.TempDir(), "*.xml")
	if code := run(context.Background(), append(append([]string{}, runFlags...), pattern), ciEnv(srv.URL), &out, &errOut); code != exitBroken ||
		!strings.Contains(errOut.String(), "no report matches "+pattern) {
		t.Errorf("a pattern matching nothing exits %d saying %q", code, errOut.String())
	}
}

func TestARefusalIsPrintedWithItsFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"validation_failed","message":"Some fields need attention.","fields":{"commit":"Give the commit's SHA, 7 to 64 hex digits."}}}`))
	}))
	t.Cleanup(srv.Close)
	var out, errOut bytes.Buffer
	code := run(context.Background(), append(append([]string{}, runFlags...), aReport(t)), ciEnv(srv.URL), &out, &errOut)
	want := "exciter-ci: Exciter refused: Some fields need attention. commit: Give the commit's SHA, 7 to 64 hex digits."
	if code != exitBroken || !strings.Contains(errOut.String(), want) {
		t.Errorf("a refusal exits %d saying %q", code, errOut.String())
	}
}

func TestWhatIsNotReportedIsCounted(t *testing.T) {
	t.Run("results", func(t *testing.T) {
		l := &landing{run: map[string]any{"id": "r7", "state": "done", "cases": 300, "matched": 0,
			"reported": []map[string]string{{"key": "CHK-R99", "test": "engage", "problem": "unknown"}}, "unreported": 299}}
		srv := l.serve(t)
		pollEveryForTests(t)
		var out, errOut bytes.Buffer
		if code := run(context.Background(), append(append([]string{}, runFlags...), aReport(t)), ciEnv(srv.URL), &out, &errOut); code != exitPass {
			t.Fatalf("results exits %d: %s", code, errOut.String())
		}
		if !strings.Contains(out.String(), "and 299 more keys that verify nothing") {
			t.Errorf("results printed %s", out.String())
		}
	})

	t.Run("scan", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/v1/projects/CHK/code-tag-words":
				_, _ = w.Write([]byte(`{"words":{"implements":["implements"],"verifies":["verifies"],"projectKeys":["CHK"]}}`))
			case "/api/v1/projects/CHK/code-scans":
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"scan":{"id":"s1","tags":250,"linked":0,"reported":[],"unreported":50}}`))
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)
		var out, errOut bytes.Buffer
		args := []string{"scan", "--repository", "acme/checkout", "--commit", "4f2c9a1e", "--branch", "main", t.TempDir()}
		if code := run(context.Background(), args, ciEnv(srv.URL), &out, &errOut); code != exitPass {
			t.Fatalf("scan exits %d: %s", code, errOut.String())
		}
		if !strings.Contains(out.String(), "and 50 more tags that link nothing") {
			t.Errorf("scan printed %s", out.String())
		}
	})
}
