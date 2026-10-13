//go:build integration

package test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// statementSaying is a statement of one paragraph.
func statementSaying(text string) map[string]any {
	return map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}},
	}}
}

// grantAs gives person role in the project behind the policies' back.
func (h *harness) grantAs(t *testing.T, org, project, person uuid.UUID, role string) {
	t.Helper()
	if _, err := h.super.Exec(t.Context(), `INSERT INTO project_grant (org_id, project_id, subject_type, user_id, role) VALUES ($1, $2, 'user', $3, $4)`,
		org, project, person, role); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
}

// rowKeys reads a list of requirements as their keys.
func rowKeys(t *testing.T, r response) []string {
	t.Helper()
	var keys []string
	for _, each := range list(t, r, "rows") {
		keys = append(keys, each.(map[string]any)["key"].(string))
	}
	return keys
}

// Editors write requirements as revisions, a stale save is refused, what is not normative is logged, the
// trash keeps what was deleted until it is restored or purged, and old keys keep finding requirements.
func TestRequirementsOverTheAPI(t *testing.T) {
	f := newProjectFixture(t)
	project := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": "RQ", "name": "Requirements"}), http.StatusCreated, "make a project"), "project")
	projectID := uuid.MustParse(project["id"].(string))
	viewerID := f.h.addPerson(t, f.home.org, "member")
	// Everyone edits, so a viewer is somebody whom only a viewer grant reaches: a guest named by one.
	guestViewerID := f.h.addPerson(t, f.home.org, "guest")
	f.h.grantAs(t, f.home.org, projectID, guestViewerID, "viewer")
	viewer := f.api.as(t, guestViewerID, f.home.org)
	editor := f.plain

	made := obj(t, want(t, editor.post(t, "/api/v1/projects/RQ/requirements", map[string]any{
		"title": " Pay the basket ", "statement": statementSaying("The shopper pays for everything in the basket at once."),
		"priority": "must", "ownerId": viewerID, "comment": "First draft.",
	}), http.StatusCreated, "write a requirement"), "requirement")
	if made["key"] != "RQ-R1" || made["revision"] != float64(1) || made["title"] != "Pay the basket" || made["priority"] != "must" {
		t.Fatalf("the first requirement is RQ-R1 at revision 1: %v", made)
	}
	id := made["id"].(string)
	section := obj(t, want(t, editor.post(t, "/api/v1/projects/rq/requirements", map[string]any{"kind": "section", "title": "Checkout"}),
		http.StatusCreated, "write a section"), "requirement")
	if section["key"] != "RQ-R2" || section["kind"] != "section" {
		t.Fatalf("the section is RQ-R2: %v", section)
	}

	t.Run("what a requirement may not be is refused on its field", func(t *testing.T) {
		for what, body := range map[string]map[string]any{
			"a blank title":            {"title": "  "},
			"a statement of no editor": {"title": "X", "statement": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "panel"}}}},
			"a rationale on a section": {"kind": "section", "title": "X", "rationale": statementSaying("why")},
			"an owner without access":  {"title": "X", "ownerId": f.away.user},
			"a priority that is none":  {"title": "X", "priority": "maybe"},
			"a type before types":      {"title": "X", "typeId": uuid.New()},
			"a kind that is none":      {"title": "X", "kind": "chapter"},
		} {
			refuse(t, what, editor.post(t, "/api/v1/projects/RQ/requirements", body), http.StatusUnprocessableEntity, "validation_failed")
		}
		refuse(t, "a viewer writes", viewer.post(t, "/api/v1/projects/RQ/requirements", map[string]any{"title": "X"}), http.StatusForbidden, "forbidden")
		refuse(t, "another organization writes", f.outside.post(t, "/api/v1/projects/RQ/requirements", map[string]any{"title": "X"}), http.StatusNotFound, "not_found")
	})

	t.Run("a requirement is read by its key or its id by whoever views the project", func(t *testing.T) {
		got := obj(t, want(t, viewer.get(t, "/api/v1/requirements/rq-r1"), http.StatusOK, "read by key"), "requirement")
		if can := got["can"].(map[string]any); can["edit"] != false || got["owner"].(map[string]any)["id"] != viewerID.String() {
			t.Errorf("a viewer reads RQ-R1 and edits nothing: %v", got)
		}
		want(t, editor.get(t, "/api/v1/requirements/by-id/"+id), http.StatusOK, "read by id")
		refuse(t, "a key that is no requirement", editor.get(t, "/api/v1/requirements/RQ-R99"), http.StatusNotFound, "not_found")
		refuse(t, "a key that is none", editor.get(t, "/api/v1/requirements/RQ-42"), http.StatusNotFound, "not_found")
		refuse(t, "a guest without a grant", f.guest.get(t, "/api/v1/requirements/RQ-R1"), http.StatusNotFound, "not_found")
		refuse(t, "another organization", f.outside.get(t, "/api/v1/requirements/by-id/"+id), http.StatusNotFound, "not_found")
		refuse(t, "an id that is none", editor.get(t, "/api/v1/requirements/by-id/nope"), http.StatusBadRequest, "bad_request")
	})

	t.Run("a normative save writes the next revision over the one it was made from", func(t *testing.T) {
		got := obj(t, want(t, editor.patch(t, "/api/v1/requirements/RQ-R1", map[string]any{
			"title": "Pay the whole basket", "baseRevision": 1, "comment": "Clearer.",
		}), http.StatusOK, "save over revision 1"), "requirement")
		if got["revision"] != float64(2) {
			t.Fatalf("the save wrote revision %v, want 2", got["revision"])
		}
		got = obj(t, want(t, editor.patch(t, "/api/v1/requirements/RQ-R1", map[string]any{
			"title": "Pay  the whole basket ", "baseRevision": 2,
		}), http.StatusOK, "save the same words"), "requirement")
		if got["revision"] != float64(2) {
			t.Errorf("a save that says nothing new wrote revision %v", got["revision"])
		}
		refuse(t, "a save over an old revision", editor.patch(t, "/api/v1/requirements/RQ-R1", map[string]any{
			"title": "Late", "baseRevision": 1}), http.StatusConflict, "revision_conflict")
		refuse(t, "a save without its base", editor.patch(t, "/api/v1/requirements/RQ-R1", map[string]any{"title": "Late"}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a viewer saves", viewer.patch(t, "/api/v1/requirements/RQ-R1", map[string]any{"priority": "could"}), http.StatusForbidden, "forbidden")
		got = obj(t, want(t, editor.patch(t, "/api/v1/requirements/RQ-R1", map[string]any{"priority": nil, "ownerId": nil}),
			http.StatusOK, "clear the owner and the priority"), "requirement")
		if got["revision"] != float64(2) || got["owner"] != nil || got["priority"] != nil {
			t.Errorf("the owner and priority change without a revision: %v", got)
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM requirement_change WHERE requirement_id = $1 AND field IN ('owner', 'priority')`, id); n != 2 {
			t.Errorf("%d owner and priority changes were logged, want 2", n)
		}
		want(t, f.owner.patch(t, "/api/v1/projects/RQ", map[string]any{"settings": map[string]any{"requireRevisionComment": true}}), http.StatusOK, "ask for comments")
		refuse(t, "a revision without the comment asked for", editor.patch(t, "/api/v1/requirements/RQ-R1", map[string]any{
			"statement": statementSaying("Changed."), "baseRevision": 2}), http.StatusUnprocessableEntity, "validation_failed")
		want(t, editor.patch(t, "/api/v1/requirements/RQ-R1", map[string]any{
			"statement": statementSaying("Changed."), "baseRevision": 2, "comment": "Said why."}), http.StatusOK, "a revision with its comment")
		if n := f.h.countRows(t, `SELECT count(*) FROM requirement_revision WHERE requirement_id = $1`, id); n != 3 {
			t.Errorf("RQ-R1 has %d revisions, want 3", n)
		}
	})

	t.Run("a project's new key moves its requirements' keys, and the old ones still find them", func(t *testing.T) {
		want(t, f.owner.put(t, "/api/v1/projects/RQ/key", map[string]any{"key": "RQX"}), http.StatusOK, "rename the key")
		got := obj(t, want(t, editor.get(t, "/api/v1/requirements/RQ-R1"), http.StatusOK, "read by the old key"), "requirement")
		if got["key"] != "RQX-R1" || got["formerKeys"].([]any)[0] != "RQ-R1" {
			t.Errorf("RQ-R1 is now RQX-R1 and remembers it: %v", got)
		}
	})

	t.Run("the list holds a project's requirements in outline order, narrowed by kind", func(t *testing.T) {
		sameList(t, "every requirement", rowKeys(t, want(t, viewer.get(t, "/api/v1/projects/RQX/requirements"), http.StatusOK, "list")), "RQX-R1", "RQX-R2")
		sameList(t, "sections", rowKeys(t, want(t, viewer.get(t, "/api/v1/projects/RQX/requirements?kind=section"), http.StatusOK, "list sections")), "RQX-R2")
		refuse(t, "a kind that is none", viewer.get(t, "/api/v1/projects/RQX/requirements?kind=chapter"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "no rows at all", viewer.get(t, "/api/v1/projects/RQX/requirements?limit=0"), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("the trash keeps what was deleted until it comes back or goes for good", func(t *testing.T) {
		refuse(t, "a viewer deletes", viewer.delete(t, "/api/v1/requirements/RQX-R1"), http.StatusForbidden, "forbidden")
		want(t, editor.delete(t, "/api/v1/requirements/RQX-R1"), http.StatusNoContent, "trash RQX-R1")
		if got := obj(t, want(t, editor.get(t, "/api/v1/requirements/RQX-R1"), http.StatusOK, "an editor finds it in the trash"), "requirement"); got["trashedAt"] == nil {
			t.Error("a trashed requirement does not say so")
		}
		refuse(t, "a viewer reads a trashed requirement", viewer.get(t, "/api/v1/requirements/RQX-R1"), http.StatusNotFound, "not_found")
		refuse(t, "a change to a trashed requirement", editor.patch(t, "/api/v1/requirements/RQX-R1", map[string]any{"priority": "must"}), http.StatusConflict, "requirement_trashed")
		sameList(t, "the list without it", rowKeys(t, want(t, editor.get(t, "/api/v1/projects/RQX/requirements"), http.StatusOK, "list")), "RQX-R2")
		items := list(t, want(t, editor.get(t, "/api/v1/projects/RQX/trash"), http.StatusOK, "open the trash"), "items")
		if len(items) != 1 || items[0].(map[string]any)["key"] != "RQX-R1" {
			t.Fatalf("the trash holds RQX-R1: %v", items)
		}
		refuse(t, "a viewer opens the trash", viewer.get(t, "/api/v1/projects/RQX/trash"), http.StatusForbidden, "forbidden")
		want(t, editor.post(t, "/api/v1/projects/RQX/trash/"+id+"/restore", nil), http.StatusOK, "restore")
		refuse(t, "restoring what is not in the trash", editor.post(t, "/api/v1/projects/RQX/trash/"+id+"/restore", nil), http.StatusNotFound, "not_found")
		want(t, editor.delete(t, "/api/v1/requirements/RQX-R1"), http.StatusNoContent, "trash again")
		refuse(t, "an editor purges", editor.delete(t, "/api/v1/projects/RQX/trash/"+id), http.StatusForbidden, "forbidden")
		want(t, f.owner.delete(t, "/api/v1/projects/RQX/trash/"+id), http.StatusNoContent, "purge")
		refuse(t, "a purged requirement", f.owner.get(t, "/api/v1/requirements/RQX-R1"), http.StatusNotFound, "not_found")
		if n := f.h.countRows(t, `SELECT count(*) FROM requirement_revision WHERE requirement_id = $1`, id); n != 0 {
			t.Errorf("%d revisions outlived their requirement", n)
		}
		want(t, editor.delete(t, "/api/v1/requirements/RQX-R2"), http.StatusNoContent, "trash the section")
		refuse(t, "an editor empties the trash", editor.delete(t, "/api/v1/projects/RQX/trash"), http.StatusForbidden, "forbidden")
		want(t, f.owner.delete(t, "/api/v1/projects/RQX/trash"), http.StatusNoContent, "empty the trash")
		next := obj(t, want(t, editor.post(t, "/api/v1/projects/RQX/requirements", map[string]any{"title": "After"}), http.StatusCreated, "write after a purge"), "requirement")
		if next["key"] != "RQX-R3" {
			t.Errorf("a number was handed out again: %v", next["key"])
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action IN ('requirement.purged', 'trash.emptied')`, f.home.org); n != 2 {
			t.Errorf("the log holds %d purges, want 2", n)
		}
	})

	t.Run("an archived project takes no requirement", func(t *testing.T) {
		want(t, f.owner.put(t, "/api/v1/projects/RQX/archive", nil), http.StatusOK, "archive")
		refuse(t, "a requirement in an archived project", f.owner.post(t, "/api/v1/projects/RQX/requirements", map[string]any{"title": "Late"}), http.StatusConflict, "project_archived")
		refuse(t, "a change in an archived project", f.owner.patch(t, "/api/v1/requirements/RQX-R3", map[string]any{"priority": "must"}), http.StatusConflict, "project_archived")
		want(t, f.owner.delete(t, "/api/v1/projects/RQX/archive"), http.StatusOK, "unarchive")
	})
}
