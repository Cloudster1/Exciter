//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// Views of the matrix and the chains (#152): kept apart from the grid's, their settings held to their kind, and
// what they name that the project lost left out as they are read.
func TestMatrixAndChainViewsAreKeptApart(t *testing.T) {
	s := newTraceScene(t, "MXV")
	views := "/api/v1/projects/" + s.key + "/views"
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/trace-model", s.model(t)), http.StatusOK, "set the trace model")
	matrixView := map[string]any{"name": "Verified", "kind": "matrix", "shared": true,
		"settings": map[string]any{"linkTypes": []string{"verifies"}, "direction": "incoming", "gaps": "rows"}}
	made := obj(t, want(t, s.owner.post(t, views, matrixView), http.StatusCreated, "save a matrix view"), "view")
	names := func(c *client, kind string) []string {
		t.Helper()
		var out []string
		for _, each := range list(t, want(t, c.get(t, views+kind), http.StatusOK, "the views "+kind), "views") {
			out = append(out, each.(map[string]any)["name"].(string))
		}
		return out
	}
	t.Run("a view keeps its kind and its settings, and lists only with its kind", func(t *testing.T) {
		settings := made["settings"].(map[string]any)
		if made["kind"] != "matrix" || settings["direction"] != "incoming" || fmt.Sprint(settings["linkTypes"]) != "[verifies]" {
			t.Errorf("the matrix view reads %v", made)
		}
		want(t, s.owner.post(t, views, map[string]any{"name": "Verified", "filter": map[string]any{"q": "brake"}}), http.StatusCreated, "a grid view of the same name")
		if grid, matrix := names(s.owner, ""), names(s.carol, "?kind=matrix"); fmt.Sprint(grid) != "[Verified]" || fmt.Sprint(matrix) != "[Verified]" {
			t.Errorf("the grid lists %v and carol's matrix views %v", grid, matrix)
		}
		if chains := names(s.owner, "?kind=chain"); len(chains) != 0 {
			t.Errorf("the chain views list %v", chains)
		}
		fieldError(t, s.owner.get(t, views+"?kind=nonsense"), "kind")
	})
	t.Run("settings that do not fit the kind are refused", func(t *testing.T) {
		fieldError(t, s.owner.post(t, views, map[string]any{"name": "Cols", "kind": "matrix", "columns": []string{"key"}}), "columns")
		fieldError(t, s.owner.post(t, views, map[string]any{"name": "Grid", "settings": map[string]any{"gaps": "rows"}}), "settings")
		fieldError(t, s.owner.post(t, views, map[string]any{"name": "Mixed", "kind": "matrix", "settings": map[string]any{"levels": []int{1}}}), "settings")
		fieldError(t, s.owner.post(t, views, map[string]any{"name": "Odd", "kind": "chain", "settings": map[string]any{"levels": []int{9}}}), "settings")
		fieldError(t, s.owner.post(t, views, map[string]any{"name": "Odd", "kind": "map"}), "kind")
		fieldError(t, s.owner.patch(t, "/api/v1/views/"+made["id"].(string), map[string]any{"kind": "grid"}), "kind")
	})
	t.Run("what the project no longer has is left out and named", func(t *testing.T) {
		chain := obj(t, want(t, s.owner.post(t, views, map[string]any{"name": "Deep", "kind": "chain", "settings": map[string]any{"levels": []int{2, 3, 5}, "onlyBroken": true}}),
			http.StatusCreated, "save a chain view past the model's levels"), "view")
		if fmt.Sprint(chain["settings"].(map[string]any)["levels"]) != "[2 3]" || fmt.Sprint(chain["dropped"]) != "[level:5]" {
			t.Errorf("the chain view reads %v", chain)
		}
		lost := obj(t, want(t, s.owner.post(t, views, map[string]any{"name": "Elsewhere", "kind": "matrix", "settings": map[string]any{"columnProject": "NOPE" + s.key}}),
			http.StatusCreated, "save a matrix view of a project nobody can see"), "view")
		if lost["settings"].(map[string]any)["columnProject"] != nil || fmt.Sprint(lost["dropped"]) != "[columnProject:NOPE"+s.key+"]" {
			t.Errorf("the matrix view of a lost project reads %v", lost)
		}
	})
	t.Run("the app role never changes a view's kind", func(t *testing.T) {
		s.h.settle(t)
		owner := s.h.appConn(t, s.ownerID)
		if _, err := owner.Exec(context.Background(), `UPDATE saved_view SET kind = 'grid' WHERE id = $1`, made["id"]); !isPermissionDenied(err) {
			t.Errorf("a view's kind was changed: %v", err)
		}
	})
}
