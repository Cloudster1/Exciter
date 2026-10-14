//go:build integration

package test

import (
	"net/http"
	"strings"
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
	trKey := projectKey(t, "TR")
	mvKey := projectKey(t, "MV")
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": trKey, "name": "Tree"}), http.StatusCreated, "make a project")
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": mvKey, "name": "Moved"}), http.StatusCreated, "make another")
	editor := f.plain
	write := func(body map[string]any) map[string]any {
		t.Helper()
		return obj(t, want(t, editor.post(t, "/api/v1/projects/"+trKey+"/requirements", body), http.StatusCreated, "write"), "requirement")
	}
	scope := write(map[string]any{"kind": "section", "title": "Scope"})
	scopeID := scope["id"].(string)
	pay := write(map[string]any{"title": "Pay", "place": place(scopeID, nil)})
	first := write(map[string]any{"title": "First", "place": place(scopeID, nil)})
	last := write(map[string]any{"title": "Last"})
	payID, firstID, lastID := pay["id"].(string), first["id"].(string), last["id"].(string)

	t.Run("the tree is numbered in reading order, sections and requirements alike", func(t *testing.T) {
		refuse(t, "a guest reads the tree", f.guest.get(t, "/api/v1/projects/"+trKey+"/tree"), http.StatusNotFound, "not_found")
		sameList(t, "the tree", nodeOutlines(t, want(t, editor.get(t, "/api/v1/projects/"+trKey+"/tree"), http.StatusOK, "read the tree")),
			trKey+"-R1 1", trKey+"-R3 1.1", trKey+"-R2 1.2", trKey+"-R4 2")
		got := obj(t, want(t, editor.get(t, "/api/v1/requirements/"+trKey+"-R2"), http.StatusOK, "read one"), "requirement")
		if got["outline"] != "1.2" || got["parentId"] != scopeID {
			t.Errorf(trKey+"-R2 is 1.2 below the scope: %v %v", got["outline"], got["parentId"])
		}
		spec := want(t, editor.get(t, "/api/v1/projects/"+trKey+"/spec?limit=2&offset=1"), http.StatusOK, "read the specification")
		entries := list(t, spec, "entries")
		if spec.Body["total"] != float64(4) || len(entries) != 2 || entries[0].(map[string]any)["node"].(map[string]any)["key"] != trKey+"-R3" {
			t.Errorf("the specification's second and third entries: %v", spec.Body)
		}
		refuse(t, "too long a window", editor.get(t, "/api/v1/projects/"+trKey+"/spec?limit=201"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a guest reads the specification", f.guest.get(t, "/api/v1/projects/"+trKey+"/spec"), http.StatusNotFound, "not_found")
		refuse(t, "a place after a stranger", editor.post(t, "/api/v1/projects/"+trKey+"/requirements", map[string]any{"title": "X", "place": place(nil, payID)}),
			http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("a move within the project changes only the place, and never under itself", func(t *testing.T) {
		got := obj(t, want(t, editor.post(t, "/api/v1/requirements/"+trKey+"-R4/move", map[string]any{"place": place(payID, nil)}), http.StatusOK, "move under Pay"), "requirement")
		if got["outline"] != "1.2.1" || got["key"] != trKey+"-R4" {
			t.Errorf(trKey+"-R4 moved to 1.2.1 and kept its key: %v %v", got["outline"], got["key"])
		}
		refuse(t, "a move under itself", editor.post(t, "/api/v1/requirements/"+trKey+"-R1/move", map[string]any{"place": place(payID, nil)}),
			http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a status mapping within the project", editor.post(t, "/api/v1/requirements/"+trKey+"-R4/move", map[string]any{
			"place": place(nil, nil), "statusMapping": map[string]any{"x": scopeID}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a guest moves", f.guest.post(t, "/api/v1/requirements/"+trKey+"-R4/move", map[string]any{"place": place(nil, nil)}), http.StatusNotFound, "not_found")
		if n := f.h.countRows(t, `SELECT count(*) FROM requirement_change WHERE requirement_id = $1 AND field = 'place'`, lastID); n != 1 {
			t.Errorf("%d place changes were logged, want 1", n)
		}
	})

	t.Run("the trash takes everything below, and gives it back", func(t *testing.T) {
		want(t, editor.delete(t, "/api/v1/requirements/"+trKey+"-R1"), http.StatusNoContent, "trash the scope")
		if nodes := nodeOutlines(t, want(t, editor.get(t, "/api/v1/projects/"+trKey+"/tree"), http.StatusOK, "read the tree")); len(nodes) != 0 {
			t.Errorf("the tree keeps %v", nodes)
		}
		items := list(t, want(t, editor.get(t, "/api/v1/projects/"+trKey+"/trash"), http.StatusOK, "open the trash"), "items")
		if len(items) != 1 || items[0].(map[string]any)["requirements"] != float64(4) {
			t.Fatalf("one item of four: %v", items)
		}
		want(t, editor.post(t, "/api/v1/projects/"+trKey+"/trash/"+scopeID+"/restore", nil), http.StatusOK, "restore")
		sameList(t, "the tree back", nodeOutlines(t, want(t, editor.get(t, "/api/v1/projects/"+trKey+"/tree"), http.StatusOK, "read the tree")),
			trKey+"-R1 1", trKey+"-R3 1.1", trKey+"-R2 1.2", trKey+"-R4 1.2.1")
	})

	t.Run("a move to another project gives new keys, keeps the old ones, and carries values by name", func(t *testing.T) {
		notes := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+trKey+"/attributes", map[string]any{"name": "Notes", "kind": "text"}), http.StatusCreated, "notes here"), "attribute")
		only := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+trKey+"/attributes", map[string]any{"name": "Only here", "kind": "text"}), http.StatusCreated, "an attribute of here alone"), "attribute")
		there := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+mvKey+"/attributes", map[string]any{"name": "notes", "kind": "text"}), http.StatusCreated, "notes there"), "attribute")
		want(t, editor.patch(t, "/api/v1/requirements/"+trKey+"-R2", map[string]any{"attributes": map[string]any{notes["id"].(string): "kept", only["id"].(string): "lost"}, "baseRevision": 1}),
			http.StatusOK, "give values")
		refuse(t, "a guest moves to another project", f.guest.post(t, "/api/v1/requirements/"+trKey+"-R2/move", map[string]any{"projectKey": mvKey, "place": place(nil, nil)}),
			http.StatusNotFound, "not_found")
		moved := obj(t, want(t, editor.post(t, "/api/v1/requirements/"+trKey+"-R2/move", map[string]any{"projectKey": strings.ToLower(mvKey), "place": place(nil, nil)}),
			http.StatusOK, "move to "+mvKey), "requirement")
		if moved["key"] != mvKey+"-R1" || moved["attributes"].(map[string]any)[there["id"].(string)] != "kept" || moved["revision"] != float64(3) {
			t.Fatalf(trKey+"-R2 became "+mvKey+"-R1 with its notes, as revision 3: %v", moved)
		}
		if got := obj(t, want(t, editor.get(t, "/api/v1/requirements/"+trKey+"-R4"), http.StatusOK, "the child by its old key"), "requirement"); got["key"] != mvKey+"-R2" || got["outline"] != "1.1" {
			t.Errorf(trKey+"-R4 went along below it as "+mvKey+"-R2: %v %v", got["key"], got["outline"])
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM requirement_revision WHERE requirement_id = $1 AND comment = 'Moved from `+trKey+`-R2'`, payID); n != 1 {
			t.Error("the move that dropped a value wrote no revision saying so")
		}
		sameList(t, "what stayed", nodeOutlines(t, want(t, editor.get(t, "/api/v1/projects/"+trKey+"/tree"), http.StatusOK, "read the tree")), trKey+"-R1 1", trKey+"-R3 1.1")
	})

	t.Run("a copy makes new requirements, below and elsewhere too", func(t *testing.T) {
		made := want(t, editor.post(t, "/api/v1/requirements/"+trKey+"-R1/copy", map[string]any{"place": place(nil, scopeID), "children": true}), http.StatusCreated, "copy the scope")
		copy := obj(t, made, "requirement")
		if made.Body["copied"] != float64(2) || copy["revision"] != float64(1) || copy["copiedFrom"].(map[string]any)["key"] != trKey+"-R1" || copy["outline"] != "2" {
			t.Fatalf("two copied, the scope's copy second at the top: %v", made.Body)
		}
		sameList(t, "the tree with the copy", nodeOutlines(t, want(t, editor.get(t, "/api/v1/projects/"+trKey+"/tree"), http.StatusOK, "read the tree")),
			trKey+"-R1 1", trKey+"-R3 1.1", trKey+"-R5 2", trKey+"-R6 2.1")
		elsewhere := obj(t, want(t, editor.post(t, "/api/v1/requirements/"+trKey+"-R3/copy", map[string]any{"projectKey": mvKey, "place": place(nil, nil)}),
			http.StatusCreated, "copy to "+mvKey), "requirement")
		if elsewhere["key"] != mvKey+"-R3" {
			t.Errorf("the copy took "+mvKey+"'s next key: %v", elsewhere["key"])
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM requirement_revision WHERE requirement_id = $1 AND comment = 'Copied from `+trKey+`-R3'`, elsewhere["id"]); n != 1 {
			t.Error("the copy's revision 1 does not say where it came from")
		}
		refuse(t, "a guest copies", f.guest.post(t, "/api/v1/requirements/"+trKey+"-R3/copy", map[string]any{"place": place(nil, nil)}), http.StatusNotFound, "not_found")
		refuse(t, "a copy under a requirement of nowhere", editor.post(t, "/api/v1/requirements/"+trKey+"-R3/copy", map[string]any{"place": place(uuid.NewString(), nil)}),
			http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a copy after a requirement of another parent", editor.post(t, "/api/v1/requirements/"+trKey+"-R3/copy", map[string]any{"place": place(nil, firstID)}),
			http.StatusUnprocessableEntity, "validation_failed")
	})
}
