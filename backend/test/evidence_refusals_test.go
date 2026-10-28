//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/codetrace"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/testresult"
)

// What verification evidence refuses (#323): every upload and setting keeps an archived project as it was, each limit
// and field is refused with nothing written, and a run the worker cannot read ends failed rather than waiting.

// pastTheBodyLimit is how far past a limit a body goes so the server's slack for the form's framing cannot take it.
const pastTheBodyLimit = 1 << 20

// runsIn and scansIn count what a project holds, whatever state it is in.
func (s *baselineScene) runsIn(t *testing.T) int {
	t.Helper()
	return s.h.countRows(t, `SELECT count(*) FROM test_run WHERE project_id = $1`, s.projectID)
}

func (s *baselineScene) scansIn(t *testing.T) int {
	t.Helper()
	return s.h.countRows(t, `SELECT count(*) FROM code_scan WHERE project_id = $1`, s.projectID)
}

// uploadParts sends a run as multipart parts in the order given, each a form field or, named file, a report.
func uploadParts(t *testing.T, c *client, projectKey string, parts [][2]string) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for i, p := range parts {
		if p[0] == "file" {
			part, err := form.CreateFormFile("file", fmt.Sprintf("report-%d.xml", i))
			if err != nil {
				t.Fatal(err)
			}
			_, _ = part.Write([]byte(p[1]))
			continue
		}
		_ = form.WriteField(p[0], p[1])
	}
	_ = form.Close()
	req, err := http.NewRequest(http.MethodPost, c.api.srv.URL+"/api/v1/projects/"+projectKey+"/test-runs", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	return c.do(t, req)
}

// formParts are a run's fields as parts.
func formParts(fields map[string]string) [][2]string {
	var out [][2]string
	for k, v := range fields {
		out = append(out, [2]string{k, v})
	}
	return out
}

func TestEvidenceKeepsAnArchivedProjectAsItWas(t *testing.T) {
	s := newBaselineScene(t, "EAR")
	settings, words := "/api/v1/projects/"+s.key+"/evidence-settings", "/api/v1/projects/"+s.key+"/code-tag-words"
	want(t, s.owner.put(t, settings, map[string]any{"countingBranch": "release"}), http.StatusOK, "name the counting branch")
	want(t, s.owner.put(t, words, map[string]any{"implements": []string{"implements", "satisfies"}, "verifies": []string{"verifies"}}),
		http.StatusOK, "set the words")
	s.h.settle(t)
	savedWords := obj(t, want(t, s.owner.get(t, words), http.StatusOK, "read the words"), "words")
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/archive", nil), http.StatusOK, "archive the project")
	report := junit([2]string{"engage [verifies " + s.req("1") + "]", "passed"})

	refuse(t, "an editor uploads a run", uploadRun(t, s.bob, s.key, runForm("main"), report), http.StatusConflict, "project_archived")
	refuse(t, "the administrator uploads a run", uploadRun(t, s.owner, s.key, runForm("main"), report), http.StatusConflict, "project_archived")
	refuse(t, "an editor uploads a scan", s.bob.post(t, "/api/v1/projects/"+s.key+"/code-scans", scanBody("main", tag("a.go", 1, "implements", s.req("1")))),
		http.StatusConflict, "project_archived")
	refuse(t, "the administrator names another branch", s.owner.put(t, settings, map[string]any{"countingBranch": "main"}), http.StatusConflict, "project_archived")
	refuse(t, "the administrator sets other words", s.owner.put(t, words, map[string]any{"implements": []string{"realises"}, "verifies": []string{"checks"}}),
		http.StatusConflict, "project_archived")

	if runs, scans := s.runsIn(t), s.scansIn(t); runs != 0 || scans != 0 {
		t.Errorf("the archived project holds %d runs and %d scans", runs, scans)
	}
	if got := obj(t, want(t, s.carol.get(t, settings), http.StatusOK, "read the settings"), "settings"); got["countingBranch"] != "release" {
		t.Errorf("the counting branch reads %v", got)
	}
	if got := obj(t, want(t, s.owner.get(t, words), http.StatusOK, "read the words again"), "words"); fmt.Sprint(got) != fmt.Sprint(savedWords) {
		t.Errorf("the words read %v, were %v", got, savedWords)
	}
}

func TestATestRunUploadHoldsItsLimitsAndQueuesNothingRefused(t *testing.T) {
	s := newBaselineScene(t, "ERL")
	report := string(junit([2]string{"engage [verifies " + s.req("1") + "]", "passed"}))

	t.Run("more files than MaxRunFiles", func(t *testing.T) {
		parts := formParts(runForm("main"))
		for range testresult.MaxRunFiles + 1 {
			parts = append(parts, [2]string{"file", report})
		}
		if msg := fieldError(t, uploadParts(t, s.bob, s.key, parts), "file"); !strings.Contains(msg, fmt.Sprint(testresult.MaxRunFiles)) {
			t.Errorf("too many files are refused with %q", msg)
		}
	})

	t.Run("a body past MaxRunBytes", func(t *testing.T) {
		huge := bytes.Repeat([]byte("x"), testresult.MaxRunBytes+pastTheBodyLimit)
		refuse(t, "a body past the limit", uploadRun(t, s.bob, s.key, runForm("main"), huge), http.StatusRequestEntityTooLarge, "too_large")
	})

	t.Run("a form field past four KiB names the field", func(t *testing.T) {
		long := runForm("main")
		long["branch"] = strings.Repeat("b", 4<<10+1)
		if msg := fieldError(t, uploadRun(t, s.bob, s.key, long, []byte(report)), "branch"); !strings.Contains(msg, "too long") {
			t.Errorf("a field past the form's limit is refused with %q", msg)
		}
	})

	t.Run("a job past MaxNameLength", func(t *testing.T) {
		long := runForm("main")
		long["job"] = strings.Repeat("j", testresult.MaxNameLength+1)
		if msg := fieldError(t, uploadRun(t, s.bob, s.key, long, []byte(report)), "job"); !strings.Contains(msg, "job") {
			t.Errorf("a long job is refused with %q", msg)
		}
		long["job"] = strings.Repeat("j", testresult.MaxNameLength)
		run := ran(t, s.bob, s.key, long, []byte(report))
		if run["job"] != long["job"] || run["state"] != "done" {
			t.Errorf("a job of the limit reads %v", run)
		}
	})

	t.Run("JSON instead of multipart", func(t *testing.T) {
		refuse(t, "a JSON body", s.bob.post(t, "/api/v1/projects/"+s.key+"/test-runs", map[string]any{"repository": "acme/checkout", "commit": aCommit,
			"branch": "main"}), http.StatusBadRequest, "bad_request")
	})

	// The one run of the job at the limit is all that was ever queued.
	if n := s.runsIn(t); n != 1 {
		t.Errorf("the project holds %d runs, want only the one of the job at the limit", n)
	}
}

func TestARunTheWorkerCannotReadEndsFailedWithNothingLanded(t *testing.T) {
	s := newBaselineScene(t, "ERF")
	ctx := context.Background()
	var id uuid.UUID
	// A case whose name is a number is not what the upload writes, so the worker cannot read it.
	if err := s.h.super.QueryRow(ctx, `INSERT INTO test_run (project_id, repository, commit_sha, branch, cases, uploaded_by)
		VALUES ($1, 'acme/checkout', $2, 'main', '[{"name": 5, "keys": ["`+s.req("1")+`"]}]', $3) RETURNING id`, s.projectID, aCommit, s.bobID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"actorId": s.bobID, "runId": id})
	if err := testresult.Handler(testresult.NewService(s.h.cluster))(ctx, events.Event{Topic: testresult.TopicRequested, Payload: payload}); err != nil {
		t.Fatalf("the worker hands back a run it could not read as an error to retry: %v", err)
	}
	got := obj(t, want(t, s.bob.get(t, "/api/v1/test-runs/"+id.String()), http.StatusOK, "the uploader reads the run"), "run")
	if got["state"] != "failed" || !strings.HasPrefix(got["error"].(string), "The run could not be read onto its requirements.") ||
		got["cases"] != float64(0) || got["finishedAt"] == nil {
		t.Errorf("the run reads %v", got)
	}
	var cases string
	if err := s.h.super.QueryRow(ctx, `SELECT cases::text FROM test_run WHERE id = $1`, id).Scan(&cases); err != nil || cases != "[]" {
		t.Errorf("the run still holds its cases %s: %v", cases, err)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM test_result WHERE run_id = $1`, id); n != 0 {
		t.Errorf("a run that could not be read landed %d results", n)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM test_run_latest WHERE run_id = $1`, id); n != 0 {
		t.Error("a failed run counts as the latest")
	}
}

func TestACodeScanSaysWhatDoesNotHoldAndKeepsNothing(t *testing.T) {
	s := newBaselineScene(t, "ERS")
	scans := "/api/v1/projects/" + s.key + "/code-scans"
	r1 := s.req("1")

	t.Run("a field it does not know", func(t *testing.T) {
		body := scanBody("main", tag("a.go", 1, "implements", r1))
		body["comment"] = "scanned overnight"
		r := s.bob.post(t, scans, body)
		refuse(t, "an unknown field", r, http.StatusBadRequest, "bad_request")
		if msg := refusalMessage(t, r); !strings.Contains(msg, "comment") {
			t.Errorf("an unknown field is refused with %q", msg)
		}
	})

	t.Run("a body past MaxScanBytes", func(t *testing.T) {
		body := scanBody("main")
		body["repository"] = strings.Repeat("r", codetrace.MaxScanBytes+pastTheBodyLimit)
		refuse(t, "a body past the limit", s.bob.post(t, scans, body), http.StatusRequestEntityTooLarge, "too_large")
	})

	t.Run("each field names itself", func(t *testing.T) {
		for _, field := range []string{"repository", "commit", "branch"} {
			body := scanBody("main", tag("a.go", 1, "implements", r1))
			delete(body, field)
			fieldError(t, s.bob.post(t, scans, body), field)
		}
		fieldError(t, s.bob.post(t, scans, scanBody("main", tag(" ", 1, "implements", r1))), "tags[0].path")
		fieldError(t, s.bob.post(t, scans, scanBody("main", tag("a.go", 1, "", r1))), "tags[0].word")
		noPath := scanBody("main", tag("a.go", 1, "implements", r1))
		noPath["fileUrl"] = "https://git.example/acme/checkout/blob/{commit}#L{line}"
		fieldError(t, s.bob.post(t, scans, noPath), "fileUrl")
	})

	if n := s.scansIn(t); n != 0 {
		t.Errorf("the refused scans left %d scans", n)
	}
}

// tagPlaces reads a page of the project's tags as path:line.
func tagPlaces(t *testing.T, c *client, path string) ([]string, float64) {
	t.Helper()
	got := want(t, c.get(t, path), http.StatusOK, "list the tags")
	var out []string
	for _, each := range list(t, got, "tags") {
		tg := each.(map[string]any)
		out = append(out, fmt.Sprintf("%s %s:%v", tg["branch"], tg["path"], tg["line"]))
	}
	return out, got.Body["total"].(float64)
}

func TestTheProjectsCodeTagsArePagedAndKeptToOneBranch(t *testing.T) {
	s := newBaselineScene(t, "ERP")
	const pages, pageSize = 3, 25
	many := make([]map[string]any, 0, pages*pageSize-1)
	for i := range cap(many) {
		many = append(many, tag(fmt.Sprintf("ghosts/%03d.go", i), 1, "implements", s.key+"-R9"+fmt.Sprint(i)))
	}
	scanned(t, s.bob, s.key, scanBody("main", many...))
	scanned(t, s.bob, s.key, scanBody("feature/lights", tag("lights/ghost.go", 4, "implements", s.key+"-R999")))
	tags := "/api/v1/projects/" + s.key + "/code-tags"

	t.Run("limit and offset walk the pages, each tag once, the total the same on each", func(t *testing.T) {
		seen := map[string]bool{}
		for page := range pages {
			got, total := tagPlaces(t, s.carol, fmt.Sprintf("%s?limit=%d&offset=%d", tags, pageSize, page*pageSize))
			if total != float64(len(many)) {
				t.Errorf("page %d counts %v tags, want %d", page, total, len(many))
			}
			if want := min(pageSize, len(many)-page*pageSize); len(got) != want {
				t.Errorf("page %d holds %d tags, want %d", page, len(got), want)
			}
			for _, place := range got {
				if seen[place] || !strings.HasPrefix(place, "main ") {
					t.Errorf("page %d lists %s again or from another branch", page, place)
				}
				seen[place] = true
			}
		}
		if len(seen) != len(many) {
			t.Errorf("the pages list %d tags, want %d", len(seen), len(many))
		}
		if got, _ := tagPlaces(t, s.carol, tags); len(got) != codetrace.DefaultLimit {
			t.Errorf("a list without a limit holds %d tags, want %d", len(got), codetrace.DefaultLimit)
		}
	})

	t.Run("a limit past MaxLimit", func(t *testing.T) {
		fieldError(t, s.carol.get(t, fmt.Sprintf("%s?limit=%d", tags, codetrace.MaxLimit+1)), "limit")
	})

	t.Run("a branch lists its own tags", func(t *testing.T) {
		got, total := tagPlaces(t, s.carol, tags+"?branch="+url.QueryEscape("feature/lights"))
		if total != 1 || fmt.Sprint(got) != "[feature/lights lights/ghost.go:4]" {
			t.Errorf("the feature branch lists %v of %v", got, total)
		}
	})
}

func TestARequirementsCodeIsReadOneBranchAtATime(t *testing.T) {
	s := newBaselineScene(t, "ERB")
	r1 := s.req("1")
	scanned(t, s.bob, s.key, scanBody("main", tag("brakes/engage.go", 12, "implements", r1)))
	scanned(t, s.bob, s.key, scanBody("feature/abs", tag("brakes/abs.go", 7, "implements", r1), tag("brakes/abs_test.go", 2, "verifies", r1)))
	links := "/api/v1/requirements/" + r1 + "/code-links"
	branches := func(r response) []string {
		var out []string
		for _, each := range list(t, r, "links") {
			l := each.(map[string]any)
			out = append(out, fmt.Sprintf("%s %s:%v counts=%v", l["branch"], l["path"], l["line"], l["counts"]))
		}
		return out
	}

	all := want(t, s.carol.get(t, links), http.StatusOK, "read every branch")
	sameList(t, "every branch, the counting one first", branches(all),
		"main brakes/engage.go:12 counts=true", "feature/abs brakes/abs.go:7 counts=false", "feature/abs brakes/abs_test.go:2 counts=false")
	one := want(t, s.carol.get(t, links+"?branch="+url.QueryEscape("feature/abs")), http.StatusOK, "read one branch")
	sameList(t, "the feature branch", branches(one), "feature/abs brakes/abs.go:7 counts=false", "feature/abs brakes/abs_test.go:2 counts=false")
	if one.Body["countingBranch"] != "main" {
		t.Errorf("one branch's links name the counting branch %v", one.Body["countingBranch"])
	}
	refuse(t, "a person outside the project", s.owner.api.as(t, s.h.addPerson(t, "guest")).get(t, links), http.StatusNotFound, "not_found")
}

func TestTagWordsAndTheCountingBranchHoldTheirLimits(t *testing.T) {
	s := newBaselineScene(t, "ERW")
	words, settings := "/api/v1/projects/"+s.key+"/code-tag-words", "/api/v1/projects/"+s.key+"/evidence-settings"
	want(t, s.owner.put(t, words, map[string]any{"implements": []string{"implements", "satisfies"}, "verifies": []string{"verifies"}}),
		http.StatusOK, "set the words")
	want(t, s.owner.put(t, settings, map[string]any{"countingBranch": "release"}), http.StatusOK, "name the counting branch")
	s.h.settle(t)
	savedWords := obj(t, want(t, s.owner.get(t, words), http.StatusOK, "read the words"), "words")
	savedSettings := obj(t, want(t, s.owner.get(t, settings), http.StatusOK, "read the settings"), "settings")

	t.Run("more than MaxWords for one relation, or none", func(t *testing.T) {
		eleven := make([]string, codetrace.MaxWords+1)
		for i := range eleven {
			eleven[i] = "word" + string(rune('a'+i))
		}
		fieldError(t, s.owner.put(t, words, map[string]any{"implements": eleven, "verifies": []string{"verifies"}}), "implements")
		fieldError(t, s.owner.put(t, words, map[string]any{"implements": []string{"implements"}, "verifies": []string{}}), "verifies")
		if got := obj(t, want(t, s.owner.get(t, words), http.StatusOK, "read the words again"), "words"); fmt.Sprint(got) != fmt.Sprint(savedWords) {
			t.Errorf("the words read %v, were %v", got, savedWords)
		}
	})

	t.Run("a counting branch past MaxNameLength, and a reader from outside", func(t *testing.T) {
		fieldError(t, s.owner.put(t, settings, map[string]any{"countingBranch": strings.Repeat("b", testresult.MaxNameLength+1)}), "countingBranch")
		if got := obj(t, want(t, s.owner.get(t, settings), http.StatusOK, "read the settings again"), "settings"); fmt.Sprint(got) != fmt.Sprint(savedSettings) {
			t.Errorf("the settings read %v, were %v", got, savedSettings)
		}
		refuse(t, "a person outside the project", s.owner.api.as(t, s.h.addPerson(t, "guest")).get(t, settings), http.StatusNotFound, "not_found")
	})
}

func TestTheTraceCheckFailsOnGapsOnlyWhenAsked(t *testing.T) {
	s := newBaselineScene(t, "ERG")
	r1 := s.req("1")
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/coverage-rules", map[string]any{"verified": true, "verifiedFrom": []string{"tests"}}),
		http.StatusOK, "verify by tests")
	s.h.drained(t)
	check := "/api/v1/projects/" + s.key + "/trace-check?query=" + url.QueryEscape("key = "+r1)

	t.Run("a requirement verified by no passing test is a gap", func(t *testing.T) {
		got := obj(t, want(t, s.bob.get(t, check+"&fail=gaps"), http.StatusOK, "check the gaps"), "check")
		gaps := got["gaps"].([]any)
		if got["verdict"] != "fail" || got["checked"] != float64(1) || len(gaps) != 1 || got["totals"].(map[string]any)["gaps"] != float64(1) {
			t.Fatalf("the check reads %v", got)
		}
		gap := gaps[0].(map[string]any)
		if gap["requirement"].(map[string]any)["key"] != r1 || gap["dimension"] != "verified" || gap["level"] != "none" {
			t.Errorf("the gap reads %v", gap)
		}
	})

	t.Run("the same project passes a check that fails on failing tests alone", func(t *testing.T) {
		got := obj(t, want(t, s.bob.get(t, check+"&fail=failing"), http.StatusOK, "check the failing"), "check")
		if got["verdict"] != "pass" || len(got["gaps"].([]any)) != 1 {
			t.Errorf("the check reads %v", got)
		}
	})

	t.Run("a query that does not parse is refused with where it stops", func(t *testing.T) {
		r := s.bob.get(t, "/api/v1/projects/"+s.key+"/trace-check?query="+url.QueryEscape("key = = "+r1))
		refuse(t, "a query that does not parse", r, http.StatusBadRequest, "bad_query")
		if r.Status == http.StatusBadRequest {
			if e := obj(t, r, "error"); e["position"] == nil || e["message"] == "" {
				t.Errorf("the refusal reads %v", e)
			}
		}
	})
}

// The evidence tools answer an assistant what the routes they wrap answer, one branch when asked (#323).
func TestAnAssistantReadsTheEvidenceAsTheRoutesAnswerIt(t *testing.T) {
	s := newBaselineScene(t, "ERM")
	r1 := s.req("1")
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/coverage-rules", map[string]any{"verified": true, "verifiedFrom": []string{"tests"}}),
		http.StatusOK, "verify by tests")
	ran(t, s.bob, s.key, runForm("main"), junit([2]string{"engage [verifies " + r1 + "]", "passed"}))
	ran(t, s.bob, s.key, runForm("feature/abs"), junit([2]string{"engage [verifies " + r1 + "]", "failed"}))
	scanned(t, s.bob, s.key, scanBody("main", tag("brakes/engage.go", 12, "implements", r1)))
	scanned(t, s.bob, s.key, scanBody("feature/abs", tag("brakes/abs.go", 7, "implements", r1)))
	_, secret := makeToken(t, s.owner, map[string]any{"name": "assistant"})
	s.h.drained(t)
	a := connectAssistant(t, s.owner.api, secret)
	reqPath := "/api/v1/requirements/" + r1

	for _, branch := range []string{"", "feature/abs"} {
		args, query := map[string]any{"reqKey": r1}, ""
		if branch != "" {
			args["branch"], query = branch, "?branch="+url.QueryEscape(branch)
		}
		results := a.call(t, "get_test_results", args)
		sameAnswer(t, "get_test_results "+branch, results, want(t, s.owner.get(t, reqPath+"/test-results"+query), http.StatusOK, "read the results"))
		code := a.call(t, "get_code_links", args)
		sameAnswer(t, "get_code_links "+branch, code, want(t, s.owner.get(t, reqPath+"/code-links"+query), http.StatusOK, "read the code"))
		if branch != "" && (len(results["results"].([]any)) != 1 || len(code["links"].([]any)) != 1) {
			t.Errorf("one branch answers %v and %v", results, code)
		}
	}

	check := a.call(t, "run_trace_check", map[string]any{"projectKey": s.key, "branch": "feature/abs", "fail": []string{"failing", "gaps"}})
	sameAnswer(t, "run_trace_check", check, want(t, s.owner.get(t, "/api/v1/projects/"+s.key+"/trace-check?branch=feature%2Fabs&fail=failing&fail=gaps"),
		http.StatusOK, "check the trace"))
	if got := check["check"].(map[string]any); got["verdict"] != "fail" || got["branch"] != "feature/abs" || len(got["failing"].([]any)) != 1 {
		t.Errorf("run_trace_check answers %v", got)
	}
	if msg := a.refused(t, "run_trace_check", map[string]any{"projectKey": s.key, "fail": []string{"feelings"}}); msg == "" {
		t.Error("an unknown reason is refused without a sentence")
	}
}
