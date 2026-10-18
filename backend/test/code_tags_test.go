//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/codetrace"
)

// Code tags (#201): a scan's tags land on the requirements they name, keep the revision they were written against
// from scan to scan, turn outdated past it or orphaned without a requirement, and count for coverage, queries,
// chains, baselines and the trace check.

const fileTemplate = "https://git.example/acme/checkout/blob/{commit}/{path}#L{line}"

func tag(path string, line int, word, key string, revision ...int) map[string]any {
	out := map[string]any{"path": path, "line": line, "word": word, "key": key}
	if len(revision) > 0 {
		out["revision"] = revision[0]
	}
	return out
}

func scanBody(branch string, tags ...map[string]any) map[string]any {
	if tags == nil {
		tags = []map[string]any{}
	}
	return map[string]any{"repository": "acme/checkout", "commit": aCommit, "branch": branch, "fileUrl": fileTemplate, "tags": tags}
}

func scanned(t *testing.T, c *client, projectKey string, body map[string]any) map[string]any {
	t.Helper()
	got := obj(t, want(t, c.post(t, "/api/v1/projects/"+projectKey+"/code-scans", body), http.StatusCreated, "upload a scan"), "scan")
	c.api.h.settle(t)
	return got
}

// codeOf reads a requirement's code as path:line relation revision outdated.
func codeOf(t *testing.T, c *client, reqKey string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, each := range list(t, want(t, c.get(t, "/api/v1/requirements/"+reqKey+"/code-links"), http.StatusOK, "read the code"), "links") {
		l := each.(map[string]any)
		out[fmt.Sprintf("%s:%v", l["path"], l["line"])] = fmt.Sprintf("%v r%v outdated=%v", l["relation"], l["revision"], l["outdated"])
	}
	return out
}

func TestCodeTagsLandOnRequirementsAndCarryTheirRevision(t *testing.T) {
	s := newBaselineScene(t, "CTS")
	r1, r2, r3 := s.req("1"), s.req("2"), s.req("3")
	first := scanBody("main",
		tag("brakes/engage.go", 12, "implements", r1),
		tag("brakes/engage_test.go", 3, "verifies", r3),
		tag("brakes/doc.go", 1, "implements", r2),
		tag("brakes/ghost.go", 7, "implements", s.key+"-R99"),
		tag("brakes/old.go", 2, "satisfies", r1),
		tag("brakes/later.go", 9, "implements", r1, 5))

	t.Run("an editor uploads a scan, and it says what links nothing", func(t *testing.T) {
		got := scanned(t, s.bob, s.key, first)
		problems := map[string]string{}
		for _, each := range got["reported"].([]any) {
			r := each.(map[string]any)
			problems[r["path"].(string)] = r["problem"].(string)
		}
		if got["tags"] != float64(6) || got["linked"] != float64(2) || problems["brakes/doc.go"] != "section" || problems["brakes/ghost.go"] != "unknown" ||
			problems["brakes/old.go"] != "word" || problems["brakes/later.go"] != "revision" {
			t.Errorf("the scan reads %v, its problems %v", got, problems)
		}
		code := codeOf(t, s.carol, r1)
		if code["brakes/engage.go:12"] != "implements r1 outdated=false" || len(code) != 1 {
			t.Errorf("R1's code reads %v", code)
		}
		link := list(t, want(t, s.carol.get(t, "/api/v1/requirements/"+r1+"/code-links"), http.StatusOK, "read the code"), "links")[0].(map[string]any)
		if link["url"] != "https://git.example/acme/checkout/blob/"+aCommit+"/brakes/engage.go#L12" {
			t.Errorf("the link leads to %v", link["url"])
		}
		id := got["id"].(string)
		want(t, s.owner.get(t, "/api/v1/code-scans/"+id), http.StatusOK, "the administrator reads the scan")
		refuse(t, "a reader reads somebody's scan", s.carol.get(t, "/api/v1/code-scans/"+id), http.StatusNotFound, "not_found")
		refuse(t, "a reader uploads", s.carol.post(t, "/api/v1/projects/"+s.key+"/code-scans", first), http.StatusForbidden, "forbidden")
	})

	t.Run("a scan says which field does not hold, and refuses one too large", func(t *testing.T) {
		bad := scanBody("main")
		bad["fileUrl"] = "javascript:alert(1)"
		fieldError(t, s.bob.post(t, "/api/v1/projects/"+s.key+"/code-scans", bad), "fileUrl")
		fieldError(t, s.bob.post(t, "/api/v1/projects/"+s.key+"/code-scans", scanBody("main", tag("a.go", 0, "implements", r1))), "tags[0].line")
		fieldError(t, s.bob.post(t, "/api/v1/projects/"+s.key+"/code-scans", scanBody("main", tag("a.go", 1, "implements", "brakes"))), "tags[0].key")
		many := make([]map[string]any, codetrace.MaxScanTags+1)
		for i := range many {
			many[i] = tag("a.go", i+1, "implements", r1)
		}
		refuse(t, "a scan past the limit", s.bob.post(t, "/api/v1/projects/"+s.key+"/code-scans", scanBody("main", many...)), http.StatusRequestEntityTooLarge, "too_large")
	})

	t.Run("a revision outdates a tag, a scan again keeps its revision, and naming the current one makes it current", func(t *testing.T) {
		want(t, s.owner.patch(t, "/api/v1/requirements/"+r1, map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}), http.StatusOK, "revise R1")
		if code := codeOf(t, s.carol, r1); code["brakes/engage.go:12"] != "implements r1 outdated=true" {
			t.Errorf("after a revision R1's code reads %v", code)
		}
		moved := scanBody("main", tag("brakes/engage.go", 20, "implements", r1), tag("brakes/engage_test.go", 3, "verifies", r3))
		scanned(t, s.bob, s.key, moved)
		if code := codeOf(t, s.carol, r1); code["brakes/engage.go:20"] != "implements r1 outdated=true" || len(code) != 1 {
			t.Errorf("a tag that moved, scanned again, reads %v", code)
		}
		if keys := rowKeys(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/requirements?query="+url.QueryEscape("codeOutdated > 0")), http.StatusOK, "query")); fmt.Sprint(keys) != "["+r1+"]" {
			t.Errorf("codeOutdated > 0 keeps %v", keys)
		}
		scanned(t, s.bob, s.key, scanBody("main", tag("brakes/engage.go", 20, "implements", r1, 2), tag("brakes/engage_test.go", 3, "verifies", r3)))
		if code := codeOf(t, s.carol, r1); code["brakes/engage.go:20"] != "implements r2 outdated=false" {
			t.Errorf("a tag naming the current revision reads %v", code)
		}
	})

	t.Run("the project lists its outdated and orphaned tags", func(t *testing.T) {
		scanned(t, s.bob, s.key, scanBody("main", tag("brakes/engage.go", 12, "implements", r1, 1), tag("brakes/ghost.go", 7, "implements", s.key+"-R99"),
			tag("brakes/old.go", 2, "satisfies", r1)))
		tags := list(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/code-tags"), http.StatusOK, "list the tags"), "tags")
		states := map[string]string{}
		for _, each := range tags {
			tg := each.(map[string]any)
			states[tg["path"].(string)] = tg["state"].(string)
		}
		if states["brakes/engage.go"] != "outdated" || states["brakes/ghost.go"] != "orphaned" || len(states) != 2 {
			t.Errorf("the project lists %v", states)
		}
		orphaned := list(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/code-tags?state=orphaned"), http.StatusOK, "list orphaned"), "tags")
		if len(orphaned) != 1 {
			t.Errorf("orphaned lists %v", orphaned)
		}
		fieldError(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/code-tags?state=lost"), "state")
	})

	t.Run("the words are read when a tag is, so changing them needs no new scan", func(t *testing.T) {
		words := "/api/v1/projects/" + s.key + "/code-tag-words"
		got := obj(t, want(t, s.bob.get(t, words), http.StatusOK, "read the words"), "words")
		if fmt.Sprint(got["implements"]) != "[implements]" || fmt.Sprint(got["projectKeys"]) != "["+s.key+"]" {
			t.Errorf("the default words read %v", got)
		}
		refuse(t, "an editor sets the words", s.bob.put(t, words, map[string]any{"implements": []string{"implements", "satisfies"}, "verifies": []string{"verifies"}}),
			http.StatusForbidden, "forbidden")
		fieldError(t, s.owner.put(t, words, map[string]any{"implements": []string{"1x"}, "verifies": []string{"verifies"}}), "implements")
		fieldError(t, s.owner.put(t, words, map[string]any{"implements": []string{"checks"}, "verifies": []string{"Checks"}}), "verifies")
		want(t, s.owner.put(t, words, map[string]any{"implements": []string{"implements", "satisfies"}, "verifies": []string{"verifies"}}), http.StatusOK, "set the words")
		s.owner.api.h.settle(t)
		if code := codeOf(t, s.carol, r1); code["brakes/old.go:2"] == "" {
			t.Errorf("a tag of a word set later reads %v", code)
		}
	})
}

func TestCodeCountsForCoverageChainsBaselinesAndTheTraceCheck(t *testing.T) {
	s := newBaselineScene(t, "CTC")
	r1, r3 := s.req("1"), s.req("3")
	rules := "/api/v1/projects/" + s.key + "/coverage-rules"
	want(t, s.owner.put(t, rules, map[string]any{"implemented": true, "implementedFrom": []string{"code"}, "verified": true, "verifiedFrom": []string{"code"}}),
		http.StatusOK, "count code")
	fieldError(t, s.owner.put(t, rules, map[string]any{"implemented": true, "implementedFrom": []string{"tests"}}), "implementedFrom")
	scanned(t, s.bob, s.key, scanBody("main", tag("brakes/engage.go", 12, "implements", r1), tag("brakes/engage_test.go", 3, "verifies", r1),
		tag("lights/lights.go", 4, "implements", r3), tag("brakes/ghost.go", 7, "implements", s.key+"-R99")))

	t.Run("current tags implement and verify in full, and the query language counts them", func(t *testing.T) {
		levels := map[string]string{}
		for _, each := range list(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/coverage"), http.StatusOK, "read the coverage"), "rows") {
			r := each.(map[string]any)
			levels[r["requirement"].(map[string]any)["key"].(string)] = r["implemented"].(string) + " " + r["verified"].(string)
		}
		if levels[r1] != "full full" || levels[r3] != "full none" {
			t.Errorf("coverage reads %v", levels)
		}
		if keys := rowKeys(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/requirements?query="+url.QueryEscape("codeLink = 0 AND kind = requirement")), http.StatusOK, "query")); len(keys) != 0 {
			t.Errorf("codeLink = 0 keeps %v", keys)
		}
	})

	t.Run("the matrix lays out the files, and a model ending in code follows the tags", func(t *testing.T) {
		m := obj(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/matrix?kind=requirement&columns=code"), http.StatusOK, "the code matrix"), "matrix")
		if len(m["columns"].([]any)) != 3 || m["columns"].([]any)[0].(map[string]any)["code"].(map[string]any)["path"] != "brakes/engage.go" {
			t.Errorf("the code matrix's columns are %v", m["columns"])
		}
		typeID := obj(t, want(t, s.carol.get(t, "/api/v1/requirements/"+r1), http.StatusOK, "read R1"), "requirement")["type"].(map[string]any)["id"].(string)
		want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/trace-model", map[string]any{"levels": []map[string]any{
			{"name": "Requirement", "kind": "requirements", "typeIds": []string{typeID}},
			{"name": "Code", "kind": "code", "steps": []map[string]any{{"relation": "implements"}}},
		}}), http.StatusOK, "end the model in code")
		chains := obj(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/trace-chains"), http.StatusOK, "follow the chains"), "chains")
		if chains["broken"] != float64(0) || len(chains["lines"].([]any)) != 2 {
			t.Errorf("the chains read %v", chains)
		}
		want(t, s.owner.patch(t, "/api/v1/requirements/"+r3, map[string]any{"title": "Brake lights follow the pedal at once", "baseRevision": 1}), http.StatusOK, "revise R3")
		check := obj(t, want(t, s.bob.get(t, "/api/v1/projects/"+s.key+"/trace-check?fail=broken,outdated,orphaned"), http.StatusOK, "check"), "check")
		totals := check["totals"].(map[string]any)
		if check["verdict"] != "fail" || totals["broken"] != float64(1) || totals["outdated"] != float64(1) || totals["orphaned"] != float64(1) ||
			check["evidence"].(map[string]any)["scans"] != float64(1) {
			t.Errorf("the check reads %v", check)
		}
		want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/trace-model", map[string]any{"levels": []any{}}), http.StatusOK, "clear the model")
	})

	t.Run("a baseline records the code it held", func(t *testing.T) {
		b := obj(t, want(t, s.owner.post(t, s.baselines, map[string]any{"name": "With code"}), http.StatusCreated, "take a baseline"), "baseline")
		var code int
		if err := s.h.super.QueryRow(context.Background(), `SELECT count(*) FROM baseline_evidence WHERE baseline_id = $1 AND kind = 'code'`, b["id"]).Scan(&code); err != nil || code != 3 {
			t.Errorf("the baseline holds %d tags: %v", code, err)
		}
	})
}

func TestAScanTellsOwnersOnceOfWhatItBrought(t *testing.T) {
	s := newBaselineScene(t, "CTN")
	r1 := s.req("1")
	scanned(t, s.bob, s.key, scanBody("main", tag("brakes/engage.go", 12, "implements", r1)))
	want(t, s.owner.patch(t, "/api/v1/requirements/"+r1, map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}), http.StatusOK, "revise R1")
	codeTold := func() []map[string]any {
		t.Helper()
		s.h.drained(t)
		var out []map[string]any
		for _, each := range list(t, want(t, s.owner.get(t, "/api/v1/notifications"), http.StatusOK, "list notifications"), "notifications") {
			n := each.(map[string]any)
			if strings.HasPrefix(n["kind"].(string), "code_") {
				out = append(out, n)
			}
		}
		return out
	}
	scanned(t, s.bob, s.key, scanBody("main", tag("brakes/engage.go", 12, "implements", r1)))
	got := codeTold()
	if len(got) != 1 || got[0]["kind"] != "code_outdated" || got[0]["codeScan"].(map[string]any)["branch"] != "main" || got[0]["requirement"] != nil {
		t.Fatalf("the owner was told %v", got)
	}
	scanned(t, s.bob, s.key, scanBody("main", tag("brakes/engage.go", 14, "implements", r1)))
	if again := codeTold(); len(again) != 1 {
		t.Errorf("a scan bringing nothing new told the owner again: %v", again)
	}
	scanned(t, s.bob, s.key, scanBody("feature/x", tag("brakes/ghost.go", 1, "implements", s.key+"-R99")))
	if other := codeTold(); len(other) != 1 {
		t.Errorf("a branch that does not count told the owner: %v", other)
	}
	scanned(t, s.bob, s.key, scanBody("main", tag("brakes/engage.go", 14, "implements", r1), tag("brakes/ghost.go", 1, "implements", s.key+"-R99")))
	if orphaned := codeTold(); len(orphaned) != 2 || orphaned[0]["kind"] != "code_orphaned" {
		t.Errorf("an unknown key told the administrator %v", orphaned)
	}
}

func TestAnEvidenceTokenScansAndReadsTheWordsAlone(t *testing.T) {
	s := newBaselineScene(t, "CTT")
	_, secret := makeToken(t, s.bob, map[string]any{"name": "scanner", "scopes": []string{"evidence"}})
	ci := s.bob.api.withToken(s.bobID, secret)
	want(t, ci.get(t, "/api/v1/projects/"+s.key+"/code-tag-words"), http.StatusOK, "the token reads the words")
	got := scanned(t, ci, s.key, scanBody("main", tag("a.go", 1, "implements", s.req("1"))))
	if got["via"].(map[string]any)["name"] != "scanner" {
		t.Errorf("a scan through a token reads via %v", got["via"])
	}
	want(t, ci.get(t, "/api/v1/code-scans/"+got["id"].(string)), http.StatusOK, "the token reads its scan")
	refuse(t, "the token reads code links", ci.get(t, "/api/v1/requirements/"+s.req("1")+"/code-links"), http.StatusForbidden, "evidence_token")
	refuse(t, "the token sets the words", ci.put(t, "/api/v1/projects/"+s.key+"/code-tag-words", map[string]any{"implements": []string{"x"}, "verifies": []string{"y"}}),
		http.StatusForbidden, "evidence_token")
}

// The database holds scans and tags to their rules for every writer, not only the service.
func TestRawSQLGuardsCodeTags(t *testing.T) {
	s := newBaselineScene(t, "CTQ")
	ctx := context.Background()
	reader := s.h.appConn(t, s.carolID)
	if _, err := reader.Exec(ctx, `SELECT code_scan_replace($1, 'r', $2, 'main', '')`, s.projectID, aCommit); !refusedBy(err, "code_scan_forbidden") {
		t.Errorf("a reader makes a scan: %v", err)
	}
	editor := s.h.appConn(t, s.bobID)
	if _, err := editor.Exec(ctx, `INSERT INTO code_scan (project_id, repository, commit_sha, branch, uploaded_by) VALUES ($1, 'r', $2, 'main', $3)`,
		s.projectID, aCommit, s.bobID); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("a scan is made past code_scan_replace: %v", err)
	}
	tx, err := editor.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var scan uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT code_scan_replace($1, 'r', $2, 'main', '')`, s.projectID, aCommit).Scan(&scan); err != nil {
		t.Fatal(err)
	}
	insertTag := `INSERT INTO code_tag (scan_id, project_id, path, line, word, key, requirement_id, revision) VALUES ($1, $2, $3, 1, 'implements', 'K-R1', $4, $5)`
	if _, err := tx.Exec(ctx, insertTag, scan, s.projectID, "ok.go", s.r3ID, 1); err != nil {
		t.Fatalf("the uploader writes a tag in the transaction that made the scan: %v", err)
	}
	if _, err := tx.Exec(ctx, `SAVEPOINT ahead`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, insertTag, scan, s.projectID, "ahead.go", s.r3ID, 9); !isPolicyViolation(err) {
		t.Errorf("a tag names a revision the requirement does not have: %v", err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Error("a transaction with a refused write committed")
	}
	if err := editor.QueryRow(ctx, `SELECT code_scan_replace($1, 'r', $2, 'main', '')`, s.projectID, aCommit).Scan(&scan); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.Exec(ctx, insertTag, scan, s.projectID, "late.go", s.r3ID, 1); !isPolicyViolation(err) {
		t.Errorf("a tag is added to a scan after its transaction: %v", err)
	}
	if tag, err := editor.Exec(ctx, `UPDATE code_scan SET tags = 99 WHERE id = $1`, scan); err != nil || tag.RowsAffected() != 0 {
		t.Errorf("a scan's counts changed after its transaction: %v, %d rows", err, tag.RowsAffected())
	}
	if _, err := editor.Exec(ctx, `INSERT INTO code_tag_words (project_id, implements, verifies, updated_by) VALUES ($1, '{a}', '{b}', $2)`, s.projectID, s.bobID); !isPolicyViolation(err) {
		t.Errorf("an editor sets the tag words: %v", err)
	}
	stranger := s.h.appConn(t, s.h.addPerson(t, "guest"))
	var seen int
	if err := stranger.QueryRow(ctx, `SELECT count(*) FROM code_tag WHERE project_id = $1`, s.projectID).Scan(&seen); err != nil || seen != 0 {
		t.Errorf("a stranger reads %d tags: %v", seen, err)
	}
}
