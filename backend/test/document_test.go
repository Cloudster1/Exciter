//go:build integration

package test

import (
	"net/http"
	"testing"
)

// specEntry finds the entry a key names in a specification answer.
func specEntry(t *testing.T, r response, key string) map[string]any {
	t.Helper()
	for _, each := range list(t, r, "entries") {
		e := each.(map[string]any)
		if e["node"].(map[string]any)["key"] == key {
			return e
		}
	}
	t.Fatalf("%s is not in the specification: %v", key, r.Body)
	return nil
}

// The document edits a requirement from what its entry holds: the save names the entry's revision, a stale one is
// refused, and a requirement added after an entry stands right below it (#168).
func TestTheDocumentEditsInPlace(t *testing.T) {
	key := projectKey(t, "DOC")
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Document"}), http.StatusCreated, "make a project")
	ownerID := f.h.addPerson(t, "member")
	editor := f.plain
	note := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+key+"/types", map[string]any{"name": "Note", "icon": "test", "colour": "#22aa55"}),
		http.StatusCreated, "make a type"), "type")
	write := func(body map[string]any) map[string]any {
		t.Helper()
		return obj(t, want(t, editor.post(t, "/api/v1/projects/"+key+"/requirements", body), http.StatusCreated, "write"), "requirement")
	}
	scope := write(map[string]any{"kind": "section", "title": "Scope"})
	pay := write(map[string]any{"title": "Pay", "typeId": note["id"], "place": place(scope["id"], nil),
		"ownerId": ownerID, "priority": "must"})
	payKey := pay["key"].(string)
	spec := func() response {
		t.Helper()
		return want(t, editor.get(t, "/api/v1/projects/"+key+"/spec"), http.StatusOK, "read the specification")
	}

	t.Run("an entry holds what its editor needs: type, owner, priority and approval", func(t *testing.T) {
		e := specEntry(t, spec(), payKey)
		typ, _ := e["type"].(map[string]any)
		owner, _ := e["owner"].(map[string]any)
		approval, _ := e["approval"].(map[string]any)
		if typ == nil || typ["id"] != note["id"] || typ["name"] != "Note" || owner == nil || owner["id"] != ownerID.String() || e["priority"] != "must" {
			t.Errorf("the entry's type, owner and priority: %v %v %v", e["type"], e["owner"], e["priority"])
		}
		if approval == nil || approval["pendingReviewId"] != nil || approval["approvedRevision"] != nil || approval["current"] != false {
			t.Errorf("an entry never reviewed has no approval: %v", e["approval"])
		}
		section := specEntry(t, spec(), scope["key"].(string))
		if section["type"] != nil || section["owner"] != nil || section["priority"] != nil {
			t.Errorf("a section has no type, owner or priority: %v", section)
		}
	})

	t.Run("a save names the entry's revision, and a stale one is refused", func(t *testing.T) {
		base := specEntry(t, spec(), payKey)["node"].(map[string]any)["revision"]
		saved := obj(t, want(t, editor.patch(t, "/api/v1/requirements/"+payKey, map[string]any{"title": "Pay by card", "baseRevision": base}),
			http.StatusOK, "save from the document"), "requirement")
		if saved["revision"] != float64(2) {
			t.Errorf("the save wrote revision %v, want 2", saved["revision"])
		}
		if got := specEntry(t, spec(), payKey)["node"].(map[string]any); got["title"] != "Pay by card" || got["revision"] != float64(2) {
			t.Errorf("the document reads the saved entry: %v", got)
		}
		refuse(t, "a save over the revision the document read before", editor.patch(t, "/api/v1/requirements/"+payKey,
			map[string]any{"title": "Pay in cash", "baseRevision": base}), http.StatusConflict, "revision_conflict")
	})

	t.Run("a requirement added after an entry, or as a section's first, stands where it was asked", func(t *testing.T) {
		after := write(map[string]any{"title": "Refund", "place": place(pay["parentId"], pay["id"])})
		first := write(map[string]any{"title": "Currency", "place": place(scope["id"], nil)})
		sameList(t, "the document", nodeOutlines(t, want(t, editor.get(t, "/api/v1/projects/"+key+"/tree"), http.StatusOK, "read the tree")),
			scope["key"].(string)+" 1", first["key"].(string)+" 1.1", payKey+" 1.2", after["key"].(string)+" 1.3")
	})
}
