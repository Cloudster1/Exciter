//go:build integration

package test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The grid narrows a project's requirements by words, fields and attribute conditions, and orders them by a
// column or by the outline; views save it per person, shared when an editor says so.
func TestGridAndViewsOverTheAPI(t *testing.T) {
	grdKey := projectKey(t, "GRD")
	f := newSearchFixture(t, grdKey)
	grid := func(query string) []string {
		t.Helper()
		return rowKeys(t, want(t, f.plain.get(t, "/api/v1/projects/"+grdKey+"/requirements?"+query), http.StatusOK, "the grid "+query))
	}
	attr := func(id, op, value string) string { return "attr=" + url.QueryEscape(id+":"+op+":"+value) }

	t.Run("the grid narrows by words and fields, in outline order", func(t *testing.T) {
		rows := list(t, want(t, f.plain.get(t, "/api/v1/projects/"+grdKey+"/requirements"), http.StatusOK, "the whole grid"), "rows")
		if first := rows[0].(map[string]any); first["outline"] != "1" || first["key"] != grdKey+"-R1" || len(first["labels"].([]any)) != 1 {
			t.Errorf("the grid starts with 1, "+grdKey+"-R1 and its label: %v", first)
		}
		sameList(t, "in outline order", grid(""), grdKey+"-R1", grdKey+"-R2", grdKey+"-R3", grdKey+"-R4", grdKey+"-R5")
		sameList(t, "by words", grid("q=receipt"), grdKey+"-R4", grdKey+"-R5")
		sameList(t, "by key", grid("q="+strings.ToLower(grdKey)+"-r2"), grdKey+"-R2")
		sameList(t, "by kind", grid("kind=section"), grdKey+"-R3")
		sameList(t, "by category", grid("category=draft&kind=requirement&priority=must"), grdKey+"-R1")
		status := rows[0].(map[string]any)["status"].(map[string]any)["id"].(string)
		sameList(t, "by status", grid("status="+status+"&label=paper"), grdKey+"-R4")
		typeID := rows[0].(map[string]any)["type"].(map[string]any)["id"].(string)
		sameList(t, "by type and priority", grid("type="+typeID+"&priority=must&priority=could"), grdKey+"-R1", grdKey+"-R2")
		sameList(t, "nobody owns any", grid("owner="+uuid.NewString()))
	})

	t.Run("attribute conditions compare as each kind does", func(t *testing.T) {
		sameList(t, "is an option", grid(attr(f.risk, "is", f.high)), grdKey+"-R1")
		sameList(t, "not, with what has none", grid(attr(f.risk, "not", f.high)), grdKey+"-R2", grdKey+"-R3", grdKey+"-R4", grdKey+"-R5")
		sameList(t, "at least", grid(attr(f.cost, "gte", "6")), grdKey+"-R2")
		sameList(t, "at most, and a day", grid(attr(f.cost, "lte", "5")+"&"+attr(f.due, "gte", "2026-01-01")), grdKey+"-R1")
		sameList(t, "text whole, without regard to case", grid(attr(f.note, "is", "CARD or cash")), grdKey+"-R1")
		sameList(t, "words in rich text", grid(attr(f.detail, "contains", "creme")), grdKey+"-R2")
		sameList(t, "set", grid(attr(f.due, "set", "")), grdKey+"-R1")
		sameList(t, "unset", grid(attr(f.cost, "unset", "")), grdKey+"-R3", grdKey+"-R4", grdKey+"-R5")
		for what, query := range map[string]string{
			"an attribute that is none": attr(uuid.NewString(), "is", "x"),
			"an op the kind lacks":      attr(f.risk, "gte", f.high),
			"a value of the wrong kind": attr(f.cost, "is", "many"),
			"a condition misspelled":    "attr=nonsense",
			"an op with no value":       attr(f.note, "contains", ""),
		} {
			refuse(t, what, f.plain.get(t, "/api/v1/projects/"+grdKey+"/requirements?"+query), http.StatusUnprocessableEntity, "validation_failed")
		}
	})

	t.Run("the grid orders by a column, empty values last", func(t *testing.T) {
		sameList(t, "by title", grid("sort=title"), grdKey+"-R5", grdKey+"-R1", grdKey+"-R4", grdKey+"-R3", grdKey+"-R2")
		sameList(t, "by a number, backwards", grid("sort="+f.cost+"&desc=true"), grdKey+"-R2", grdKey+"-R1", grdKey+"-R3", grdKey+"-R4", grdKey+"-R5")
		sameList(t, "by an option's place", grid("sort="+f.risk), grdKey+"-R2", grdKey+"-R1", grdKey+"-R3", grdKey+"-R4", grdKey+"-R5")
		sameList(t, "by priority", grid("sort=priority"), grdKey+"-R1", grdKey+"-R2", grdKey+"-R3", grdKey+"-R4", grdKey+"-R5")
		refuse(t, "a column that is none", f.plain.get(t, "/api/v1/projects/"+grdKey+"/requirements?sort=colour"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "backwards misspelled", f.plain.get(t, "/api/v1/projects/"+grdKey+"/requirements?desc=yes"), http.StatusUnprocessableEntity, "validation_failed")
	})

	guestViewerID := f.h.addPerson(t, "guest")
	project := obj(t, want(t, f.owner.get(t, "/api/v1/projects/"+grdKey), http.StatusOK, "read the project"), "project")
	f.h.grantAs(t, uuid.MustParse(project["id"].(string)), guestViewerID, "viewer")
	viewer := f.api.as(t, guestViewerID)
	views := func(c *client) []string {
		t.Helper()
		var names []string
		for _, each := range list(t, want(t, c.get(t, "/api/v1/projects/"+grdKey+"/views"), http.StatusOK, "list views"), "views") {
			names = append(names, each.(map[string]any)["name"].(string))
		}
		return names
	}
	save := func(c *client, body map[string]any) response {
		t.Helper()
		return c.post(t, "/api/v1/projects/"+grdKey+"/views", body)
	}

	t.Run("a view is its owner's, and shared by editors", func(t *testing.T) {
		mine := obj(t, want(t, save(f.plain, map[string]any{"name": " My checkout ", "filter": map[string]any{"labels": []string{"Checkout"}},
			"columns": []string{"key", "title", "labels", f.cost}, "sort": map[string]any{"column": "title"}}), http.StatusCreated, "save a view"), "view")
		if mine["name"] != "My checkout" || mine["canEdit"] != true || mine["filter"].(map[string]any)["labels"].([]any)[0] != "checkout" {
			t.Errorf("the view is saved as the grid reads it: %v", mine)
		}
		team := obj(t, want(t, save(f.plain, map[string]any{"name": "Team", "shared": true}), http.StatusCreated, "share a view"), "view")
		want(t, save(viewer, map[string]any{"name": "Mine"}), http.StatusCreated, "a viewer saves one of their own")
		refuse(t, "a viewer shares", save(viewer, map[string]any{"name": "Ours", "shared": true}), http.StatusForbidden, "forbidden")
		sameList(t, "the member's views, own first", views(f.plain), "My checkout", "Team")
		sameList(t, "the viewer's", views(viewer), "Mine", "Team")
		refuse(t, "a guest lists", f.guest.get(t, "/api/v1/projects/"+grdKey+"/views"), http.StatusNotFound, "not_found")

		for what, body := range map[string]map[string]any{
			"the same name":         {"name": "my CHECKOUT"},
			"no name":               {"name": " "},
			"a filter it refuses":   {"name": "Bad", "filter": map[string]any{"attributes": []any{map[string]any{"attributeId": uuid.NewString(), "op": "is", "value": "x"}}}},
			"a column that is none": {"name": "Bad", "columns": []string{"colour"}},
			"an order that is none": {"name": "Bad", "sort": map[string]any{"column": "colour"}},
		} {
			refuse(t, what, save(f.plain, body), http.StatusUnprocessableEntity, "validation_failed")
		}

		refuse(t, "a viewer changes a shared view", viewer.patch(t, "/api/v1/views/"+team["id"].(string), map[string]any{"name": "Theirs"}),
			http.StatusForbidden, "forbidden")
		refuse(t, "a viewer changes a private one", viewer.patch(t, "/api/v1/views/"+mine["id"].(string), map[string]any{"name": "Theirs"}),
			http.StatusNotFound, "not_found")
		renamed := obj(t, want(t, f.owner.patch(t, "/api/v1/views/"+team["id"].(string), map[string]any{"name": "Everyone's"}), http.StatusOK,
			"an administrator renames a shared view"), "view")
		if renamed["name"] != "Everyone's" || renamed["ownerName"] == "" {
			t.Errorf("the view keeps its owner under its new name: %v", renamed)
		}
		refuse(t, "an administrator takes the sharing back", f.owner.patch(t, "/api/v1/views/"+team["id"].(string), map[string]any{"shared": false}),
			http.StatusForbidden, "forbidden")
		want(t, f.plain.patch(t, "/api/v1/views/"+team["id"].(string), map[string]any{"shared": false}), http.StatusOK, "the owner takes it back")
		sameList(t, "the viewer's, without it", views(viewer), "Mine")
		refuse(t, "a view that is no id", f.plain.delete(t, "/api/v1/views/nope"), http.StatusBadRequest, "bad_request")
		want(t, f.plain.delete(t, "/api/v1/views/"+team["id"].(string)), http.StatusNoContent, "the owner deletes it")
		refuse(t, "a view deleted already", f.plain.delete(t, "/api/v1/views/"+team["id"].(string)), http.StatusNotFound, "not_found")
		sameList(t, "the member's after", views(f.plain), "My checkout")
	})
}

// A shared view is only kept shared by who edits its project, so an owner who no longer does is told so (#355).
func TestAnOwnerWhoNoLongerEditsIsToldWhyTheirSharedViewStays(t *testing.T) {
	w := newCommentWorld(t, "VWL")
	view := obj(t, want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/views", map[string]any{"name": "Team", "shared": true}),
		http.StatusCreated, "bob shares a view"), "view")
	path := "/api/v1/views/" + view["id"].(string)
	if _, err := w.h.super.Exec(t.Context(), `UPDATE role_assignment SET role = 'viewer' WHERE project_id = $1 AND user_id = $2`, w.projectID, w.bobID); err != nil {
		t.Fatal(err)
	}
	w.h.settle(t)

	refuse(t, "the owner, now a viewer, renames the shared view", w.bob.patch(t, path, map[string]any{"name": "Mine now"}), http.StatusForbidden, "forbidden")
	for _, each := range list(t, want(t, w.bob.get(t, "/api/v1/projects/"+w.key+"/views"), http.StatusOK, "list the views"), "views") {
		if got := each.(map[string]any); got["id"] == view["id"] && (got["name"] != "Team" || got["shared"] != true) {
			t.Errorf("the refused view reads %v", got)
		}
	}
	want(t, w.bob.patch(t, path, map[string]any{"shared": false}), http.StatusOK, "the owner, now a viewer, takes it back")
	want(t, w.bob.patch(t, path, map[string]any{"name": "Mine now"}), http.StatusOK, "and renames it, now their own")
}
