//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Duplicates (#208): requirements alike are found while writing and paired on a project page, and a pair is
// dismissed until either side is revised, linked, or merged with its links kept on the side that stays.

// pairsOf reads a project's pairs as "A B".
func pairsOf(t *testing.T, c *client, projectKey, query string) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, want(t, c.get(t, "/api/v1/projects/"+projectKey+"/duplicates"+query), http.StatusOK, "list the pairs"), "pairs") {
		p := each.(map[string]any)
		keys := []string{p["a"].(map[string]any)["key"].(string), p["b"].(map[string]any)["key"].(string)}
		slices.Sort(keys)
		out = append(out, strings.Join(keys, " "))
	}
	return out
}

func similarKeys(t *testing.T, r response) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, r, "similar") {
		out = append(out, each.(map[string]any)["requirement"].(map[string]any)["key"].(string))
	}
	return out
}

func sideOf(key string, revision int) map[string]any {
	return map[string]any{"key": key, "revision": revision}
}

func TestDuplicatesAreFoundDismissedLinkedAndMerged(t *testing.T) {
	w := newCommentWorld(t, "DUP")
	reqs := "/api/v1/projects/" + w.key + "/requirements"
	write := func(title string) string {
		t.Helper()
		return obj(t, want(t, w.bob.post(t, reqs, map[string]any{"title": title}), http.StatusCreated, "write "+title), "requirement")["key"].(string)
	}
	r1 := w.reqKey
	r2 := write("Brakes engage within 100 milliseconds")
	r3 := write("Paint the bodywork red")
	pair := strings.Join(slices.Sorted(slices.Values([]string{r1, r2})), " ")

	t.Run("a requirement and a draft list what they are like, above the threshold", func(t *testing.T) {
		got := similarKeys(t, want(t, w.carol.get(t, "/api/v1/requirements/"+r1+"/similar"), http.StatusOK, "R1's similar"))
		if !slices.Contains(got, r2) || slices.Contains(got, r3) {
			t.Errorf("R1 is like %v", got)
		}
		draft := map[string]any{"title": "Brakes engage within 100 ms"}
		got = similarKeys(t, want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/similar", draft), http.StatusOK, "bob's draft"))
		if !slices.Contains(got, r1) || !slices.Contains(got, r2) {
			t.Errorf("the draft is like %v", got)
		}
		draft["excludeKey"] = r1
		if got := similarKeys(t, want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/similar", draft), http.StatusOK, "bob revises R1")); slices.Contains(got, r1) {
			t.Errorf("a requirement being revised is like itself: %v", got)
		}
		refuse(t, "carol checking a draft", w.carol.post(t, "/api/v1/projects/"+w.key+"/similar", draft), http.StatusForbidden, "forbidden")
		// The route writes nothing, so the statement's own sentence is all a refusal leaves.
		nonsense := map[string]any{"title": "x", "statement": map[string]any{"type": "nonsense"}}
		if msg := fieldError(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/similar", nonsense), "statement"); !strings.Contains(msg, `"type":"doc"`) {
			t.Errorf("a statement that is no document is refused with %q", msg)
		}
		if got := pairsOf(t, w.carol, w.key, ""); !slices.Contains(got, pair) {
			t.Errorf("the pairs are %v", got)
		}
		if got := pairsOf(t, w.carol, w.key, "?query="+url.QueryEscape(`title ~ "Paint"`)); len(got) != 0 {
			t.Errorf("narrowed to the painting, the pairs are %v", got)
		}
	})

	t.Run("a dismissed pair stays away until either side is revised", func(t *testing.T) {
		dismiss := "/api/v1/projects/" + w.key + "/duplicates/dismiss"
		body := map[string]any{"a": sideOf(r1, 1), "b": sideOf(r2, 1), "reason": "One is the pedal, the other the lever."}
		refuse(t, "carol dismissing", w.carol.post(t, dismiss, body), http.StatusForbidden, "forbidden")
		refuse(t, "a stale side", w.bob.post(t, dismiss, map[string]any{"a": sideOf(r1, 9), "b": sideOf(r2, 1), "reason": "x"}), http.StatusConflict, "stale_revision")
		fieldError(t, w.bob.post(t, dismiss, map[string]any{"a": sideOf(r1, 1), "b": sideOf(r1, 1), "reason": "x"}), "b")
		fieldError(t, w.bob.post(t, dismiss, map[string]any{"a": sideOf(r1, 1), "b": sideOf(r2, 1), "reason": " "}), "reason")
		decision := obj(t, want(t, w.bob.post(t, dismiss, body), http.StatusCreated, "bob dismisses"), "decision")
		if decision["kind"] != "dismissed" {
			t.Errorf("the decision reads %v", decision)
		}
		if got := pairsOf(t, w.carol, w.key, ""); slices.Contains(got, pair) {
			t.Errorf("dismissed, the pairs are %v", got)
		}
		if got := similarKeys(t, want(t, w.carol.get(t, "/api/v1/requirements/"+r1+"/similar"), http.StatusOK, "R1's similar")); slices.Contains(got, r2) {
			t.Errorf("dismissed, R1 is still like %v", got)
		}
		want(t, w.bob.patch(t, "/api/v1/requirements/"+r2, map[string]any{"title": "Brakes engage within 100 milliseconds.", "baseRevision": 1}),
			http.StatusOK, "bob revises R2")
		if got := pairsOf(t, w.carol, w.key, ""); !slices.Contains(got, pair) {
			t.Errorf("revised, the pairs are %v", got)
		}
	})

	t.Run("a linked pair stays away while the link lasts", func(t *testing.T) {
		got := obj(t, want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/duplicates/link",
			map[string]any{"source": sideOf(r2, 2), "target": sideOf(r1, 1), "linkTypeKey": "relates"}), http.StatusCreated, "bob links them"), "decision")
		if got["kind"] != "linked" || got["linkId"] == nil {
			t.Errorf("the decision reads %v", got)
		}
		if got := pairsOf(t, w.carol, w.key, ""); slices.Contains(got, pair) {
			t.Errorf("linked, the pairs are %v", got)
		}
		want(t, w.bob.delete(t, "/api/v1/links/"+got["linkId"].(string)), http.StatusNoContent, "bob removes the link")
		if got := pairsOf(t, w.carol, w.key, ""); !slices.Contains(got, pair) {
			t.Errorf("unlinked, the pairs are %v", got)
		}
	})

	t.Run("a merge trashes one side and keeps its links on the other", func(t *testing.T) {
		r4 := write("Lights shall dim within 2 seconds")
		r5 := write("Lights shall dim within 2 seconds at night")
		r6 := write("Dashboard shows the light level")
		want(t, w.bob.post(t, "/api/v1/requirements/"+r6+"/links", map[string]any{"typeKey": "derives", "otherKey": r5}), http.StatusCreated, "R6 derives from R5")
		want(t, w.bob.post(t, "/api/v1/requirements/"+r5+"/links", map[string]any{"typeKey": "relates", "otherKey": r4}), http.StatusCreated, "R5 relates to R4")
		merge := "/api/v1/projects/" + w.key + "/duplicates/merge"
		body := map[string]any{"keep": sideOf(r4, 1), "trash": sideOf(r5, 1), "comment": "Night is a case of it."}
		refuse(t, "carol merging", w.carol.post(t, merge, body), http.StatusForbidden, "forbidden")
		got := obj(t, want(t, w.bob.post(t, merge, body), http.StatusOK, "bob merges"), "merge")
		var moved, skipped int
		for _, each := range got["links"].([]any) {
			if l := each.(map[string]any); l["skipped"] == nil || l["skipped"] == "" {
				moved++
			} else {
				skipped++
			}
		}
		if moved != 1 || skipped != 1 || got["decision"].(map[string]any)["keptKey"] != r4 {
			t.Errorf("the merge reads %v", got)
		}
		var others []string
		for _, each := range list(t, want(t, w.carol.get(t, "/api/v1/requirements/"+r4+"/links"), http.StatusOK, "R4's links"), "links") {
			others = append(others, each.(map[string]any)["other"].(map[string]any)["key"].(string))
		}
		if !slices.Contains(others, r6) {
			t.Errorf("R4 links to %v", others)
		}
		if n := w.h.countRows(t, `SELECT count(*) FROM requirement r JOIN project p ON p.id = r.project_id
			WHERE p.key || '-R' || r.number = $1 AND r.trashed_at IS NOT NULL`, r5); n != 1 {
			t.Errorf("R5 is not in the trash")
		}
	})

	t.Run("the threshold is the administrators'", func(t *testing.T) {
		settings := "/api/v1/projects/" + w.key + "/duplicate-settings"
		if got := obj(t, want(t, w.carol.get(t, settings), http.StatusOK, "read the threshold"), "settings"); got["threshold"].(float64) < 0.59 || got["threshold"].(float64) > 0.61 {
			t.Errorf("the threshold starts at %v", got)
		}
		refuse(t, "bob setting it", w.bob.put(t, settings, map[string]any{"threshold": 0.9}), http.StatusForbidden, "forbidden")
		fieldError(t, w.owner.put(t, settings, map[string]any{"threshold": 0.1}), "threshold")
		want(t, w.owner.put(t, settings, map[string]any{"threshold": 0.95}), http.StatusOK, "the owner raises it")
		if got := pairsOf(t, w.carol, w.key, ""); slices.Contains(got, pair) {
			t.Errorf("at 0.95 the pairs are %v", got)
		}
	})
}

// Straight through SQL as exciter_app: pairs are the database's alone, and a decision names two requirements of
// its project, a merge's side that went in the trash, by an editor in their own name.
func TestRawSQLKeepsDuplicatesHonest(t *testing.T) {
	w := newCommentWorld(t, "DUR")
	ctx := context.Background()
	r2 := obj(t, want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/requirements", map[string]any{"title": "Brakes engage within 100 milliseconds"}),
		http.StatusCreated, "write R2"), "requirement")
	a, b := uuid.MustParse(w.reqID), uuid.MustParse(r2["id"].(string))
	low, high := a, b
	if strings.Compare(b.String(), a.String()) < 0 {
		low, high = b, a
	}
	other := projectKey(t, "DUO")
	want(t, w.owner.post(t, "/api/v1/projects", map[string]any{"key": other, "name": "Other"}), http.StatusCreated, "another project")
	foreign := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+other+"/requirements", map[string]any{"title": "Elsewhere"}), http.StatusCreated, "a requirement elsewhere"), "requirement")
	f := uuid.MustParse(foreign["id"].(string))

	editor := w.h.appConn(t, w.bobID)
	refusedOutright(t, editor, "an editor writes a pair", `INSERT INTO requirement_similarity (project_id, low_id, high_id, score) VALUES ($1, $2, $3, 0.9)`,
		w.projectID, low, high)
	refusedOutright(t, editor, "an editor rescores a pair", `UPDATE requirement_similarity SET score = 0.1 WHERE low_id = $1`, low)
	refusedOutright(t, editor, "a merge whose side is not in the trash", `INSERT INTO duplicate_decision (project_id, low_id, high_id, low_rev, high_rev, kind, kept_id, decided_by)
		VALUES ($1, $2, $3, 1, 1, 'merged', $2, $4)`, w.projectID, low, high, w.bobID)
	flow, fhigh := a, f
	if strings.Compare(f.String(), a.String()) < 0 {
		flow, fhigh = f, a
	}
	refusedOutright(t, editor, "a decision across projects", `INSERT INTO duplicate_decision (project_id, low_id, high_id, low_rev, high_rev, kind, reason, decided_by)
		VALUES ($1, $2, $3, 1, 1, 'dismissed', 'x', $4)`, w.projectID, flow, fhigh, w.bobID)
	refusedOutright(t, editor, "a decision at a revision the side never had", `INSERT INTO duplicate_decision (project_id, low_id, high_id, low_rev, high_rev, kind, reason, decided_by)
		VALUES ($1, $2, $3, 9, 1, 'dismissed', 'x', $4)`, w.projectID, low, high, w.bobID)
	refusedOutright(t, editor, "a decision in another's name", `INSERT INTO duplicate_decision (project_id, low_id, high_id, low_rev, high_rev, kind, reason, decided_by)
		VALUES ($1, $2, $3, 1, 1, 'dismissed', 'x', $4)`, w.projectID, low, high, w.carolID)
	viewer := w.h.appConn(t, w.carolID)
	refusedOutright(t, viewer, "a viewer dismisses", `INSERT INTO duplicate_decision (project_id, low_id, high_id, low_rev, high_rev, kind, reason, decided_by)
		VALUES ($1, $2, $3, 1, 1, 'dismissed', 'x', $4)`, w.projectID, low, high, w.carolID)
	if _, err := editor.Exec(ctx, `INSERT INTO duplicate_decision (project_id, low_id, high_id, low_rev, high_rev, kind, reason, decided_by)
		VALUES ($1, $2, $3, 1, 1, 'dismissed', 'Not the same.', $4)`, w.projectID, low, high, w.bobID); err != nil {
		t.Errorf("an editor may dismiss a pair: %v", err)
	}
	refusedOutright(t, editor, "an editor rewrites a decision", `UPDATE duplicate_decision SET reason = 'x' WHERE low_id = $1`, low)
}
