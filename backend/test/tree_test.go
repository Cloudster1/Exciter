//go:build integration

package test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// nodeOutlines reads a tree answer as "key outline" rows, in reading order.
func nodeOutlines(t *testing.T, r response) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, r, "nodes") {
		n := each.(map[string]any)
		out = append(out, n["key"].(string)+" "+n["outline"].(string))
	}
	return out
}

func place(parent, after any) map[string]any {
	return map[string]any{"parentId": parent, "afterId": after}
}

// Requirements stand in a tree numbered on read; they move within a project and to another, which gives them
// new keys and keeps the old ones, and they copy with what is below them.
func TestTheTreeOverTheAPI(t *testing.T) {
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": "TR", "name": "Tree"}), http.StatusCreated, "make a project")
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": "MV", "name": "Moved"}), http.StatusCreated, "make another")
	editor := f.plain
	write := func(body map[string]any) map[string]any {
		t.Helper()
		return obj(t, want(t, editor.post(t, "/api/v1/projects/TR/requirements", body), http.StatusCreated, "write"), "requirement")
	}
	scope := write(map[string]any{"kind": "section", "title": "Scope"})
	scopeID := scope["id"].(string)
	pay := write(map[string]any{"title": "Pay", "place": place(scopeID, nil)})
	first := write(map[string]any{"title": "First", "place": place(scopeID, nil)})
	last := write(map[string]any{"title": "Last"})
	payID, firstID, lastID := pay["id"].(string), first["id"].(string), last["id"].(string)

	t.Run("the tree is numbered in reading order, sections and requirements alike", func(t *testing.T) {
		refuse(t, "a guest reads the tree", f.guest.get(t, "/api/v1/projects/TR/tree"), http.StatusNotFound, "not_found")
		sameList(t, "the tree", nodeOutlines(t, want(t, editor.get(t, "/api/v1/projects/TR/tree"), http.StatusOK, "read the tree")),
			"TR-R1 1", "TR-R3 1.1", "TR-R2 1.2", "TR-R4 2")
		got := obj(t, want(t, editor.get(t, "/api/v1/requirements/TR-R2"), http.StatusOK, "read one"), "requirement")
		if got["outline"] != "1.2" || got["parentId"] != scopeID {
			t.Errorf("TR-R2 is 1.2 below the scope: %v %v", got["outline"], got["parentId"])
		}
		spec := want(t, editor.get(t, "/api/v1/projects/TR/spec?limit=2&offset=1"), http.StatusOK, "read the specification")
		entries := list(t, spec, "entries")
		if spec.Body["total"] != float64(4) || len(entries) != 2 || entries[0].(map[string]any)["node"].(map[string]any)["key"] != "TR-R3" {
			t.Errorf("the specification's second and third entries: %v", spec.Body)
		}
		refuse(t, "too long a window", editor.get(t, "/api/v1/projects/TR/spec?limit=201"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a guest reads the specification", f.guest.get(t, "/api/v1/projects/TR/spec"), http.StatusNotFound, "not_found")
		refuse(t, "a place after a stranger", editor.post(t, "/api/v1/projects/TR/requirements", map[string]any{"title": "X", "place": place(nil, payID)}),
			http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("a move within the project changes only the place, and never under itself", func(t *testing.T) {
		got := obj(t, want(t, editor.post(t, "/api/v1/requirements/TR-R4/move", map[string]any{"place": place(payID, nil)}), http.StatusOK, "move under Pay"), "requirement")
		if got["outline"] != "1.2.1" || got["key"] != "TR-R4" {
			t.Errorf("TR-R4 moved to 1.2.1 and kept its key: %v %v", got["outline"], got["key"])
		}
		refuse(t, "a move under itself", editor.post(t, "/api/v1/requirements/TR-R1/move", map[string]any{"place": place(payID, nil)}),
			http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a status mapping within the project", editor.post(t, "/api/v1/requirements/TR-R4/move", map[string]any{
			"place": place(nil, nil), "statusMapping": map[string]any{"x": scopeID}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a guest moves", f.guest.post(t, "/api/v1/requirements/TR-R4/move", map[string]any{"place": place(nil, nil)}), http.StatusNotFound, "not_found")
		if n := f.h.countRows(t, `SELECT count(*) FROM requirement_change WHERE requirement_id = $1 AND field = 'place'`, lastID); n != 1 {
			t.Errorf("%d place changes were logged, want 1", n)
		}
	})

	t.Run("the trash takes everything below, and gives it back", func(t *testing.T) {
		want(t, editor.delete(t, "/api/v1/requirements/TR-R1"), http.StatusNoContent, "trash the scope")
		if nodes := nodeOutlines(t, want(t, editor.get(t, "/api/v1/projects/TR/tree"), http.StatusOK, "read the tree")); len(nodes) != 0 {
			t.Errorf("the tree keeps %v", nodes)
		}
		items := list(t, want(t, editor.get(t, "/api/v1/projects/TR/trash"), http.StatusOK, "open the trash"), "items")
		if len(items) != 1 || items[0].(map[string]any)["requirements"] != float64(4) {
			t.Fatalf("one item of four: %v", items)
		}
		want(t, editor.post(t, "/api/v1/projects/TR/trash/"+scopeID+"/restore", nil), http.StatusOK, "restore")
		sameList(t, "the tree back", nodeOutlines(t, want(t, editor.get(t, "/api/v1/projects/TR/tree"), http.StatusOK, "read the tree")),
			"TR-R1 1", "TR-R3 1.1", "TR-R2 1.2", "TR-R4 1.2.1")
	})

	t.Run("a move to another project gives new keys, keeps the old ones, and carries values by name", func(t *testing.T) {
		notes := obj(t, want(t, f.owner.post(t, "/api/v1/projects/TR/attributes", map[string]any{"name": "Notes", "kind": "text"}), http.StatusCreated, "notes here"), "attribute")
		only := obj(t, want(t, f.owner.post(t, "/api/v1/projects/TR/attributes", map[string]any{"name": "Only here", "kind": "text"}), http.StatusCreated, "an attribute of here alone"), "attribute")
		there := obj(t, want(t, f.owner.post(t, "/api/v1/projects/MV/attributes", map[string]any{"name": "notes", "kind": "text"}), http.StatusCreated, "notes there"), "attribute")
		want(t, editor.patch(t, "/api/v1/requirements/TR-R2", map[string]any{"attributes": map[string]any{notes["id"].(string): "kept", only["id"].(string): "lost"}, "baseRevision": 1}),
			http.StatusOK, "give values")
		refuse(t, "a guest moves to another project", f.guest.post(t, "/api/v1/requirements/TR-R2/move", map[string]any{"projectKey": "MV", "place": place(nil, nil)}),
			http.StatusNotFound, "not_found")
		moved := obj(t, want(t, editor.post(t, "/api/v1/requirements/TR-R2/move", map[string]any{"projectKey": "mv", "place": place(nil, nil)}),
			http.StatusOK, "move to MV"), "requirement")
		if moved["key"] != "MV-R1" || moved["attributes"].(map[string]any)[there["id"].(string)] != "kept" || moved["revision"] != float64(3) {
			t.Fatalf("TR-R2 became MV-R1 with its notes, as revision 3: %v", moved)
		}
		if got := obj(t, want(t, editor.get(t, "/api/v1/requirements/TR-R4"), http.StatusOK, "the child by its old key"), "requirement"); got["key"] != "MV-R2" || got["outline"] != "1.1" {
			t.Errorf("TR-R4 went along below it as MV-R2: %v %v", got["key"], got["outline"])
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM requirement_revision WHERE requirement_id = $1 AND comment = 'Moved from TR-R2'`, payID); n != 1 {
			t.Error("the move that dropped a value wrote no revision saying so")
		}
		sameList(t, "what stayed", nodeOutlines(t, want(t, editor.get(t, "/api/v1/projects/TR/tree"), http.StatusOK, "read the tree")), "TR-R1 1", "TR-R3 1.1")
	})

	t.Run("a copy makes new requirements, below and elsewhere too", func(t *testing.T) {
		made := want(t, editor.post(t, "/api/v1/requirements/TR-R1/copy", map[string]any{"place": place(nil, scopeID), "children": true}), http.StatusCreated, "copy the scope")
		copy := obj(t, made, "requirement")
		if made.Body["copied"] != float64(2) || copy["revision"] != float64(1) || copy["copiedFrom"].(map[string]any)["key"] != "TR-R1" || copy["outline"] != "2" {
			t.Fatalf("two copied, the scope's copy second at the top: %v", made.Body)
		}
		sameList(t, "the tree with the copy", nodeOutlines(t, want(t, editor.get(t, "/api/v1/projects/TR/tree"), http.StatusOK, "read the tree")),
			"TR-R1 1", "TR-R3 1.1", "TR-R5 2", "TR-R6 2.1")
		elsewhere := obj(t, want(t, editor.post(t, "/api/v1/requirements/TR-R3/copy", map[string]any{"projectKey": "MV", "place": place(nil, nil)}),
			http.StatusCreated, "copy to MV"), "requirement")
		if elsewhere["key"] != "MV-R3" {
			t.Errorf("the copy took MV's next key: %v", elsewhere["key"])
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM requirement_revision WHERE requirement_id = $1 AND comment = 'Copied from TR-R3'`, elsewhere["id"]); n != 1 {
			t.Error("the copy's revision 1 does not say where it came from")
		}
		refuse(t, "a guest copies", f.guest.post(t, "/api/v1/requirements/TR-R3/copy", map[string]any{"place": place(nil, nil)}), http.StatusNotFound, "not_found")
		refuse(t, "a copy under a requirement of nowhere", editor.post(t, "/api/v1/requirements/TR-R3/copy", map[string]any{"place": place(uuid.NewString(), nil)}),
			http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a copy after a requirement of another parent", editor.post(t, "/api/v1/requirements/TR-R3/copy", map[string]any{"place": place(nil, firstID)}),
			http.StatusUnprocessableEntity, "validation_failed")
	})
}
