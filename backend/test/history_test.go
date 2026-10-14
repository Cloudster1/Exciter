//go:build integration

package test

import (
	"net/http"
	"testing"
)

// A requirement's history lists every revision, compares any two, restores an old one as a new revision, and
// merges with the changes that wrote none.
func TestHistoryOverTheAPI(t *testing.T) {
	hisKey := projectKey(t, "HIS")
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": hisKey, "name": "History"}), http.StatusCreated, "make a project")
	priority := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+hisKey+"/attributes", map[string]any{
		"name": "Risk", "kind": "select", "options": []any{map[string]any{"name": "Low"}, map[string]any{"name": "High"}},
	}), http.StatusCreated, "define an attribute"), "attribute")
	risk := priority["id"].(string)
	low, high := optionID(t, priority, "Low"), optionID(t, priority, "High")
	editor := f.plain
	want(t, editor.post(t, "/api/v1/projects/"+hisKey+"/requirements", map[string]any{
		"title": "Pay", "statement": statementSaying("The shopper pays at once."), "attributes": map[string]any{risk: low}, "comment": "First.",
	}), http.StatusCreated, "write revision 1")
	want(t, editor.patch(t, "/api/v1/requirements/"+hisKey+"-R1", map[string]any{
		"statement": statementSaying("The shopper pays later."), "attributes": map[string]any{risk: high}, "baseRevision": 1, "comment": "Later.",
	}), http.StatusOK, "write revision 2")
	want(t, editor.patch(t, "/api/v1/requirements/"+hisKey+"-R1", map[string]any{"priority": "must"}), http.StatusOK, "a logged change")

	t.Run("the history lists revisions, the latest first", func(t *testing.T) {
		refuse(t, "a guest reads the history", f.guest.get(t, "/api/v1/requirements/"+hisKey+"-R1/revisions"), http.StatusNotFound, "not_found")
		page := want(t, editor.get(t, "/api/v1/requirements/"+hisKey+"-R1/revisions?limit=1"), http.StatusOK, "list revisions")
		revisions := list(t, page, "revisions")
		if page.Body["total"] != float64(2) || len(revisions) != 1 || revisions[0].(map[string]any)["comment"] != "Later." {
			t.Errorf("revision 2 first of two: %v", page.Body)
		}
		one := obj(t, want(t, editor.get(t, "/api/v1/requirements/"+hisKey+"-R1/revisions/1"), http.StatusOK, "read revision 1"), "revision")
		if one["attributes"].(map[string]any)[risk] != low {
			t.Errorf("revision 1 had Low: %v", one)
		}
		refuse(t, "a revision that never was", editor.get(t, "/api/v1/requirements/"+hisKey+"-R1/revisions/9"), http.StatusNotFound, "not_found")
		refuse(t, "a revision that is no number", editor.get(t, "/api/v1/requirements/"+hisKey+"-R1/revisions/one"), http.StatusBadRequest, "bad_request")
	})

	t.Run("two revisions compare block by block and value by value", func(t *testing.T) {
		c := obj(t, want(t, editor.get(t, "/api/v1/requirements/"+hisKey+"-R1/diff"), http.StatusOK, "compare the last two"), "comparison")
		if c["from"].(map[string]any)["number"] != float64(1) || c["to"].(map[string]any)["number"] != float64(2) {
			t.Errorf("the default is 1 against 2: %v %v", c["from"], c["to"])
		}
		block := c["statement"].([]any)[0].(map[string]any)
		if block["change"] != "modified" {
			t.Errorf("the statement's paragraph was modified: %v", block)
		}
		attr := c["attributes"].([]any)[0].(map[string]any)
		if attr["change"] != "modified" || attr["fromText"] != "Low" || attr["toText"] != "High" || attr["name"] != "Risk" {
			t.Errorf("Risk went from Low to High: %v", attr)
		}
		empty := obj(t, want(t, editor.get(t, "/api/v1/requirements/"+hisKey+"-R1/diff?from=0&to=1"), http.StatusOK, "compare with nothing"), "comparison")
		if empty["statement"].([]any)[0].(map[string]any)["change"] != "inserted" {
			t.Errorf("against the empty requirement everything is inserted: %v", empty["statement"])
		}
		refuse(t, "a revision past the last", editor.get(t, "/api/v1/requirements/"+hisKey+"-R1/diff?to=9"), http.StatusNotFound, "not_found")
		refuse(t, "a side that is no number", editor.get(t, "/api/v1/requirements/"+hisKey+"-R1/diff?from=x"), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("a restore writes the old content as the next revision", func(t *testing.T) {
		refuse(t, "a restore over an old base", editor.post(t, "/api/v1/requirements/"+hisKey+"-R1/revisions/1/restore", map[string]any{"baseRevision": 1}),
			http.StatusConflict, "revision_conflict")
		refuse(t, "restoring the latest", editor.post(t, "/api/v1/requirements/"+hisKey+"-R1/revisions/2/restore", map[string]any{"baseRevision": 2}),
			http.StatusConflict, "no_change")
		refuse(t, "restoring what never was", editor.post(t, "/api/v1/requirements/"+hisKey+"-R1/revisions/9/restore", map[string]any{"baseRevision": 2}),
			http.StatusNotFound, "not_found")
		refuse(t, "a guest restores", f.guest.post(t, "/api/v1/requirements/"+hisKey+"-R1/revisions/1/restore", map[string]any{"baseRevision": 2}),
			http.StatusNotFound, "not_found")
		// The option revision 1 chose is retired since, and comes back with it all the same.
		want(t, f.owner.patch(t, "/api/v1/projects/"+hisKey+"/attributes/"+risk, map[string]any{"options": []any{map[string]any{"id": high, "name": "High"}}}),
			http.StatusOK, "retire Low")
		got := want(t, editor.post(t, "/api/v1/requirements/"+hisKey+"-R1/revisions/1/restore", map[string]any{"baseRevision": 2}), http.StatusOK, "restore revision 1")
		restored, revision := obj(t, got, "requirement"), obj(t, got, "revision")
		if restored["revision"] != float64(3) || restored["attributes"].(map[string]any)[risk] != low {
			t.Errorf("revision 3 says what revision 1 said: %v", restored)
		}
		if revision["restoredFrom"] != float64(1) || revision["comment"] != "Restored revision 1" {
			t.Errorf("revision 3 records what it restored: %v", revision)
		}
	})

	t.Run("the activity merges revisions and changes, newest first, a window at a time", func(t *testing.T) {
		first := want(t, editor.get(t, "/api/v1/requirements/"+hisKey+"-R1/activity?limit=2"), http.StatusOK, "the newest two")
		entries := list(t, first, "entries")
		if len(entries) != 2 || entries[0].(map[string]any)["kind"] != "revision" || entries[1].(map[string]any)["kind"] != "change" {
			t.Fatalf("revision 3, then the priority change: %v", entries)
		}
		if change := entries[1].(map[string]any)["change"].(map[string]any); change["field"] != "priority" || change["to"] != "must" {
			t.Errorf("the change says what it set: %v", change)
		}
		rest := want(t, editor.get(t, "/api/v1/requirements/"+hisKey+"-R1/activity?limit=2&cursor="+first.Body["next"].(string)), http.StatusOK, "the next window")
		if more := list(t, rest, "entries"); len(more) != 2 || more[1].(map[string]any)["revision"].(map[string]any)["number"] != float64(1) {
			t.Errorf("revisions 2 and 1 come next: %v", more)
		}
		refuse(t, "a cursor that is none", editor.get(t, "/api/v1/requirements/"+hisKey+"-R1/activity?cursor=nope"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a guest reads the activity", f.guest.get(t, "/api/v1/requirements/"+hisKey+"-R1/activity"), http.StatusNotFound, "not_found")
	})
}
