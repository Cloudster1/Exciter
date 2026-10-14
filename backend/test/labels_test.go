//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// labelsOf reads a requirement's labels from an answer that carries them.
func labelsOf(t *testing.T, r response) []string {
	t.Helper()
	var names []string
	for _, each := range list(t, r, "labels") {
		names = append(names, each.(string))
	}
	return names
}

// Labels are everyone's: coined by being used, put on and taken off by editors and logged without a
// revision, offered and counted only where the reader may look, and carried along by copies and the trash.
func TestLabelsOverTheAPI(t *testing.T) {
	lblKey := projectKey(t, "LBL")
	shutKey := projectKey(t, "SHUT")
	f := newProjectFixture(t)
	project := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": lblKey, "name": "Labelled"}), http.StatusCreated, "make a project"), "project")
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": shutKey, "name": "Shut"}), http.StatusCreated, "make a second project")
	guestViewerID := f.h.addPerson(t, "guest")
	f.h.grantAs(t, uuid.MustParse(project["id"].(string)), guestViewerID, "viewer")
	viewer := f.api.as(t, guestViewerID)
	editor := f.plain

	made := obj(t, want(t, editor.post(t, "/api/v1/projects/"+lblKey+"/requirements", map[string]any{
		"title": "Pay", "labels": []string{"Safety Critical", "checkout", "safety-critical"},
	}), http.StatusCreated, "write a labelled requirement"), "requirement")
	sameList(t, "labels as written once each", toStrings(made["labels"]), "checkout", "safety-critical")
	want(t, editor.post(t, "/api/v1/projects/"+lblKey+"/requirements", map[string]any{"title": "Refund", "labels": []string{"checkout"}}),
		http.StatusCreated, "write a second")
	want(t, editor.post(t, "/api/v1/projects/"+shutKey+"/requirements", map[string]any{"title": "Hidden", "labels": []string{"checkout", "secret"}}),
		http.StatusCreated, "write one the viewer may not see")
	refuse(t, "a label that is none", editor.post(t, "/api/v1/projects/"+lblKey+"/requirements", map[string]any{"title": "Bad", "labels": []string{"#1"}}),
		http.StatusUnprocessableEntity, "validation_failed")

	t.Run("editors put labels on and take them off, logged without a revision", func(t *testing.T) {
		got := want(t, editor.post(t, "/api/v1/requirements/"+lblKey+"-R1/labels", map[string]any{"name": " Release 2 "}), http.StatusOK, "add a label")
		sameList(t, "the labels after the add", labelsOf(t, got), "checkout", "release-2", "safety-critical")
		again := want(t, editor.post(t, "/api/v1/requirements/"+lblKey+"-R1/labels", map[string]any{"name": "release-2"}), http.StatusOK, "add it again")
		sameList(t, "no change", labelsOf(t, again), "checkout", "release-2", "safety-critical")
		refuse(t, "a viewer adds", viewer.post(t, "/api/v1/requirements/"+lblKey+"-R1/labels", map[string]any{"name": "mine"}), http.StatusForbidden, "forbidden")
		refuse(t, "a guest adds", f.guest.post(t, "/api/v1/requirements/"+lblKey+"-R1/labels", map[string]any{"name": "mine"}), http.StatusNotFound, "not_found")
		bad := editor.post(t, "/api/v1/requirements/"+lblKey+"-R1/labels", map[string]any{"name": ".hidden"})
		refuse(t, "a label that starts with a dot", bad, http.StatusUnprocessableEntity, "validation_failed")
		if msg, _ := obj(t, bad, "error", "fields")["name"].(string); !strings.HasPrefix(msg, "Use only letters") || !strings.HasSuffix(msg, ".") {
			t.Errorf("the refusal is a sentence on name: %q", msg)
		}
		refuse(t, "a label too long", editor.post(t, "/api/v1/requirements/"+lblKey+"-R1/labels", map[string]any{"name": strings.Repeat("a", 41)}),
			http.StatusUnprocessableEntity, "validation_failed")

		want(t, editor.delete(t, "/api/v1/requirements/"+lblKey+"-R1/labels/Safety%20Critical"), http.StatusNoContent, "remove a label by how it was typed")
		want(t, editor.delete(t, "/api/v1/requirements/"+lblKey+"-R1/labels/never-there"), http.StatusNoContent, "remove one it never carried")
		refuse(t, "a viewer removes", viewer.delete(t, "/api/v1/requirements/"+lblKey+"-R1/labels/checkout"), http.StatusForbidden, "forbidden")
		sameList(t, "the labels a viewer reads", labelsOf(t, want(t, viewer.get(t, "/api/v1/requirements/"+lblKey+"-R1/labels"), http.StatusOK, "read them")),
			"checkout", "release-2")
		refuse(t, "a guest reads them", f.guest.get(t, "/api/v1/requirements/"+lblKey+"-R1/labels"), http.StatusNotFound, "not_found")

		r := obj(t, want(t, editor.get(t, "/api/v1/requirements/"+lblKey+"-R1"), http.StatusOK, "read the requirement"), "requirement")
		if r["revision"] != float64(1) {
			t.Errorf("labels wrote a revision: %v", r["revision"])
		}
		entries := list(t, want(t, editor.get(t, "/api/v1/requirements/"+lblKey+"-R1/activity"), http.StatusOK, "read the activity"), "entries")
		change := entries[0].(map[string]any)["change"].(map[string]any)
		if change["field"] != "labels" || change["from"] != "checkout, release-2, safety-critical" || change["to"] != "checkout, release-2" {
			t.Errorf("the removal is logged with before and after: %v", change)
		}
		if n := len(entries); n != 3 {
			t.Errorf("revision 1 and two changes, not the repeated add or the removal of nothing: %d entries", n)
		}
	})

	t.Run("a requirement carries a bounded number of labels", func(t *testing.T) {
		names := make([]string, 50)
		for i := range names {
			names[i] = "many-" + strings.Repeat("x", i%10) + string(rune('a'+i/10))
		}
		full := obj(t, want(t, editor.post(t, "/api/v1/projects/"+lblKey+"/requirements", map[string]any{"title": "Full", "labels": names}),
			http.StatusCreated, "write one with fifty"), "requirement")
		refuse(t, "the fifty-first", editor.post(t, "/api/v1/requirements/"+full["key"].(string)+"/labels", map[string]any{"name": "one-more"}),
			http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "fifty-one at once", editor.post(t, "/api/v1/projects/"+lblKey+"/requirements", map[string]any{"title": "Over", "labels": append(names, "one-more")}),
			http.StatusUnprocessableEntity, "validation_failed")
		want(t, editor.delete(t, "/api/v1/requirements/"+full["key"].(string)), http.StatusNoContent, "trash it out of the way")
	})

	t.Run("suggestions count only what the reader may view", func(t *testing.T) {
		got := list(t, want(t, editor.get(t, "/api/v1/labels?q=ch"), http.StatusOK, "suggest"), "labels")
		if len(got) != 1 || got[0].(map[string]any)["name"] != "checkout" || got[0].(map[string]any)["requirements"] != float64(3) {
			t.Errorf("checkout on three requirements the editor sees: %v", got)
		}
		seen := list(t, want(t, viewer.get(t, "/api/v1/labels"), http.StatusOK, "suggest to the viewer"), "labels")
		for _, each := range seen {
			if name := each.(map[string]any)["name"]; name == "secret" {
				t.Errorf("the viewer learned a label from a project they may not view: %v", seen)
			}
		}
		if first := seen[0].(map[string]any); first["name"] != "checkout" || first["requirements"] != float64(2) {
			t.Errorf("the most used first, counted where the viewer looks: %v", seen)
		}
		within := list(t, want(t, editor.get(t, "/api/v1/labels?q=s&project="+strings.ToLower(shutKey)), http.StatusOK, "suggest within a project"), "labels")
		if len(within) != 1 || within[0].(map[string]any)["name"] != "secret" {
			t.Errorf("within "+shutKey+" only secret starts with s: %v", within)
		}
		if got := list(t, want(t, editor.get(t, "/api/v1/labels?q=100%25"), http.StatusOK, "a percent taken literally"), "labels"); len(got) != 0 {
			t.Errorf("a typed percent matched: %v", got)
		}
		refuse(t, "a limit past the most", editor.get(t, "/api/v1/labels?limit=51"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a prefix longer than a label", editor.get(t, "/api/v1/labels?q="+strings.Repeat("a", 41)), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a project the viewer may not see", viewer.get(t, "/api/v1/labels?project="+shutKey), http.StatusNotFound, "not_found")
	})

	t.Run("a label lists what carries it, by key, where the reader may look", func(t *testing.T) {
		page := want(t, editor.get(t, "/api/v1/labels/checkout/requirements?limit=2"), http.StatusOK, "list by label")
		got := list(t, page, "requirements")
		if page.Body["total"] != float64(3) || len(got) != 2 || got[0].(map[string]any)["key"] != lblKey+"-R1" || got[1].(map[string]any)["key"] != lblKey+"-R2" {
			t.Errorf(lblKey+"-R1 and "+lblKey+"-R2 first of three: %v", page.Body)
		}
		sameList(t, "the viewer's", keysIn(t, want(t, viewer.get(t, "/api/v1/labels/Checkout/requirements"), http.StatusOK, "the viewer lists")),
			lblKey+"-R1", lblKey+"-R2")
		sameList(t, "within "+shutKey, keysIn(t, want(t, editor.get(t, "/api/v1/labels/checkout/requirements?project="+shutKey), http.StatusOK, "within a project")),
			shutKey+"-R1")
		refuse(t, "a name that is no label", editor.get(t, "/api/v1/labels/%23x/requirements"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a guest's project", f.guest.get(t, "/api/v1/labels/checkout/requirements?project="+lblKey), http.StatusNotFound, "not_found")
		sameList(t, "the grid by label", rowKeys(t, want(t, viewer.get(t, "/api/v1/projects/"+lblKey+"/requirements?label=release-2&label=nothing"), http.StatusOK,
			"narrow the grid")), lblKey+"-R1")
		refuse(t, "a grid label that is none", viewer.get(t, "/api/v1/projects/"+lblKey+"/requirements?label=%2F"), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("a copy takes its labels, and the trash keeps them", func(t *testing.T) {
		copied := obj(t, want(t, editor.post(t, "/api/v1/requirements/"+lblKey+"-R1/copy", map[string]any{"place": map[string]any{}}), http.StatusCreated, "copy"),
			"requirement")
		sameList(t, "the copy's labels", toStrings(copied["labels"]), "checkout", "release-2")
		key := copied["key"].(string)
		want(t, editor.delete(t, "/api/v1/requirements/"+key), http.StatusNoContent, "trash the copy")
		refuse(t, "a label on a trashed requirement", editor.post(t, "/api/v1/requirements/"+key+"/labels", map[string]any{"name": "late"}),
			http.StatusConflict, "requirement_trashed")
		if total := want(t, editor.get(t, "/api/v1/labels/release-2/requirements"), http.StatusOK, "list without the trash").Body["total"]; total != float64(1) {
			t.Errorf("the trashed copy is not listed: %v", total)
		}
		restored := obj(t, want(t, editor.post(t, "/api/v1/projects/"+lblKey+"/trash/"+copied["id"].(string)+"/restore", nil), http.StatusOK, "restore"), "requirement")
		sameList(t, "labels back with it", toStrings(restored["labels"]), "checkout", "release-2")
	})
}

func toStrings(v any) []string {
	var out []string
	for _, each := range v.([]any) {
		out = append(out, each.(string))
	}
	return out
}

func keysIn(t *testing.T, r response) []string {
	t.Helper()
	var keys []string
	for _, each := range list(t, r, "requirements") {
		keys = append(keys, each.(map[string]any)["key"].(string))
	}
	return keys
}
