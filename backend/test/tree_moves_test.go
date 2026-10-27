//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// moveScene is a project with a section of two requirements and one at the top, written by an editor of every
// project the scene makes; the readers of a project made with only are everyone else.
type moveScene struct {
	f      *projectFixture
	key    string
	id     uuid.UUID
	editor *client
	ids    map[string]string
}

func newMoveScene(t *testing.T, prefix string) *moveScene {
	t.Helper()
	f := newProjectFixture(t)
	s := &moveScene{f: f, key: projectKey(t, prefix), editor: f.plain, ids: map[string]string{}}
	s.id = s.project(t, s.key, "Braking", "editor", true)
	section := s.write(t, s.key, map[string]any{"kind": "section", "title": "Braking"})
	first := s.write(t, s.key, map[string]any{"title": "Brakes engage within 100 ms", "place": place(section, nil)})
	s.write(t, s.key, map[string]any{"title": "Brakes hold on a slope", "place": place(section, first)})
	s.write(t, s.key, map[string]any{"title": "Lights dim at night"})
	return s
}

// project makes a project where everyone holds role, and the scene's editor edits when edits says so; it answers the
// project's id.
func (s *moveScene) project(t *testing.T, key, name, role string, edits bool) uuid.UUID {
	t.Helper()
	made := obj(t, want(t, s.f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": name}), http.StatusCreated, "make "+key), "project")
	id := uuid.MustParse(made["id"].(string))
	if _, err := s.f.h.super.Exec(t.Context(), `UPDATE project_grant SET role = $2 WHERE project_id = $1 AND subject_type = 'everyone'`, id, role); err != nil {
		t.Fatal(err)
	}
	if edits {
		s.f.h.grantAs(t, id, s.editor.user, "editor")
	}
	s.f.h.settle(t)
	return id
}

// write puts a requirement in a project as the editor and answers its id, kept under its key too.
func (s *moveScene) write(t *testing.T, key string, body map[string]any) string {
	t.Helper()
	made := obj(t, want(t, s.editor.post(t, "/api/v1/projects/"+key+"/requirements", body), http.StatusCreated, "write "+body["title"].(string)), "requirement")
	s.ids[made["key"].(string)] = made["id"].(string)
	return made["id"].(string)
}

func (s *moveScene) tree(t *testing.T, key string) []string {
	t.Helper()
	return nodeOutlines(t, want(t, s.editor.get(t, "/api/v1/projects/"+key+"/tree"), http.StatusOK, "read the tree of "+key))
}

func (s *moveScene) move(c *client, t *testing.T, key string, body map[string]any) response {
	t.Helper()
	return c.post(t, "/api/v1/requirements/"+key+"/move", body)
}

// stays fails unless the scene's project reads as it was written and no requirement of it left a key behind.
func (s *moveScene) stays(t *testing.T, what string) {
	t.Helper()
	sameList(t, what, s.tree(t, s.key), s.key+"-R1 1", s.key+"-R2 1.1", s.key+"-R3 1.2", s.key+"-R4 2")
	if n := s.f.h.countRows(t, `SELECT count(*) FROM requirement_old_key WHERE key LIKE $1 || '-R%'`, s.key); n != 0 {
		t.Errorf("%s: %d keys were left behind", what, n)
	}
}

// inProject counts the requirements of a project, trashed or not.
func (s *moveScene) inProject(t *testing.T, key string) int {
	t.Helper()
	return s.f.h.countRows(t, `SELECT count(*) FROM requirement r JOIN project p ON p.id = r.project_id WHERE p.key = $1`, key)
}

// A move and a copy are refused to whoever may not write where the requirement is or where it would go, and to an
// archived project on either end; nothing moves (#310).
func TestAMoveIsRefusedWhereItsPersonMayNotWrite(t *testing.T) {
	s := newMoveScene(t, "MR")
	readOnly, archived := projectKey(t, "MRO"), projectKey(t, "MRA")
	s.project(t, readOnly, "Read only", "viewer", false)
	s.project(t, archived, "Archived", "editor", true)
	want(t, s.f.owner.put(t, "/api/v1/projects/"+archived+"/archive", nil), http.StatusOK, "archive a project")
	// Only the editor writes in the scene's project; the reviewer reads, comments and approves.
	if _, err := s.f.h.super.Exec(t.Context(), `UPDATE project_grant SET role = 'viewer' WHERE subject_type = 'everyone' AND project_id = $1`, s.id); err != nil {
		t.Fatal(err)
	}
	reviewerID := s.f.h.addPerson(t, "member")
	s.f.h.grantAs(t, s.id, reviewerID, "reviewer")
	reviewer := s.f.api.as(t, reviewerID)
	key := s.key
	top := key + "-R4"

	t.Run("a reviewer moves and copies nothing, and reads the same tree", func(t *testing.T) {
		before := nodeOutlines(t, want(t, reviewer.get(t, "/api/v1/projects/"+key+"/tree"), http.StatusOK, "the reviewer reads the tree"))
		refuse(t, "a reviewer moves", s.move(reviewer, t, top, map[string]any{"place": place(s.ids[key+"-R1"], nil)}), http.StatusForbidden, "forbidden")
		refuse(t, "a reviewer copies", reviewer.post(t, "/api/v1/requirements/"+top+"/copy", map[string]any{"place": place(nil, nil)}), http.StatusForbidden, "forbidden")
		sameList(t, "the reviewer's tree after the refusals",
			nodeOutlines(t, want(t, reviewer.get(t, "/api/v1/projects/"+key+"/tree"), http.StatusOK, "the reviewer reads the tree again")), before...)
		s.stays(t, "after a reviewer's move")
	})

	t.Run("a project the mover only reads, or that is archived, takes nothing", func(t *testing.T) {
		refuse(t, "a move into a project the editor reads", s.move(s.editor, t, top, map[string]any{"projectKey": readOnly, "place": place(nil, nil)}),
			http.StatusForbidden, "forbidden")
		refuse(t, "a copy into a project the editor reads", s.editor.post(t, "/api/v1/requirements/"+top+"/copy", map[string]any{"projectKey": readOnly, "place": place(nil, nil)}),
			http.StatusForbidden, "forbidden")
		refuse(t, "a move into an archived project", s.move(s.editor, t, top, map[string]any{"projectKey": archived, "place": place(nil, nil)}),
			http.StatusConflict, "project_archived")
		if n, m := s.inProject(t, readOnly), s.inProject(t, archived); n != 0 || m != 0 {
			t.Errorf("the refusals left %d requirements in the project read only and %d in the archived one", n, m)
		}
		s.stays(t, "after the moves elsewhere")
	})

	t.Run("in an archived project nothing moves", func(t *testing.T) {
		want(t, s.f.owner.put(t, "/api/v1/projects/"+key+"/archive", nil), http.StatusOK, "archive the project")
		refuse(t, "a move within an archived project", s.move(s.editor, t, top, map[string]any{"place": place(s.ids[key+"-R1"], nil)}),
			http.StatusConflict, "project_archived")
		refuse(t, "a move out of an archived project", s.move(s.editor, t, top, map[string]any{"projectKey": readOnly, "place": place(nil, nil)}),
			http.StatusConflict, "project_archived")
		s.stays(t, "after the moves in an archived project")
	})
}

// A move names a place under a live parent of the project after one of that parent's other children; any other
// place is refused on the field that names it, and nothing moves (#310).
func TestAMoveToAPlaceThatIsNoneIsRefused(t *testing.T) {
	s := newMoveScene(t, "MP")
	gone := s.write(t, s.key, map[string]any{"title": "Horn sounds twice"})
	want(t, s.editor.delete(t, "/api/v1/requirements/"+s.key+"-R5"), http.StatusNoContent, "trash R5")
	top, moving := s.key+"-R4", s.ids[s.key+"-R4"]
	for _, c := range []struct {
		what, field string
		place       map[string]any
	}{
		{"after a sibling under another parent", "place.afterId", place(nil, s.ids[s.key+"-R2"])},
		{"after itself", "place.afterId", place(nil, moving)},
		{"under a requirement in the trash", "place.parentId", place(gone, nil)},
	} {
		if msg := fieldError(t, s.move(s.editor, t, top, map[string]any{"place": c.place}), c.field); msg == "" {
			t.Errorf("%s: no sentence", c.what)
		}
	}
	s.stays(t, "after the moves to no place")
	if n := s.f.h.countRows(t, `SELECT count(*) FROM requirement_change WHERE requirement_id = $1 AND field = 'place'`, moving); n != 0 {
		t.Errorf("%d place changes were logged for refused moves", n)
	}
}

// statusIDs reads a project's statuses by name.
func statusIDs(t *testing.T, c *client, key string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, each := range list(t, want(t, c.get(t, "/api/v1/projects/"+key+"/statuses"), http.StatusOK, "list the statuses of "+key), "statuses") {
		st := each.(map[string]any)
		out[st["name"].(string)] = st["id"].(string)
	}
	return out
}

// A move to a project whose workflow lacks a status of the subtree is refused whole until each such status is mapped
// to one of that workflow; mapped, every requirement lands in the status it was given (#310).
func TestAMoveMapsTheStatusesTheOtherWorkflowLacks(t *testing.T) {
	s := newMoveScene(t, "MS")
	to := projectKey(t, "MST")
	s.project(t, to, "Supplier", "editor", true)
	submit, ok := transitionNamed(t, s.editor, s.key+"-R2", "Submit for review")
	if !ok {
		t.Fatal("the editor is not offered Submit for review")
	}
	want(t, s.editor.post(t, "/api/v1/requirements/"+s.key+"-R2/transitions", map[string]any{"transitionId": submit}), http.StatusOK, "submit R2")
	pending := obj(t, want(t, s.f.owner.post(t, "/api/v1/projects/"+to+"/statuses", map[string]any{"name": "Pending"}), http.StatusCreated, "make Pending"), "status")["id"].(string)
	there := statusIDs(t, s.f.owner, to)
	simple := obj(t, want(t, s.f.owner.post(t, "/api/v1/projects/"+to+"/workflows", map[string]any{"name": "Simple", "steps": []any{
		map[string]any{"statusId": there["Draft"], "isInitial": true}, map[string]any{"statusId": pending},
	}, "transitions": []any{map[string]any{"name": "Hold", "fromStatusId": there["Draft"], "toStatusId": pending, "rules": []any{}}}}),
		http.StatusCreated, "make a workflow without In review"), "workflow")
	want(t, s.f.owner.put(t, "/api/v1/projects/"+to+"/workflow", map[string]any{"workflowId": simple["id"]}), http.StatusOK, "follow it")
	inReview := statusIDs(t, s.editor, s.key)["In review"]
	section := s.key + "-R1"
	moveSection := func(mapping map[string]any) response {
		body := map[string]any{"projectKey": to, "place": place(nil, nil)}
		if mapping != nil {
			body["statusMapping"] = mapping
		}
		return s.move(s.editor, t, section, body)
	}

	t.Run("unmapped, or mapped to a status the workflow does not hold, nothing of the section moves", func(t *testing.T) {
		if msg := fieldError(t, moveSection(nil), "statusMapping"); !strings.Contains(msg, "In review") {
			t.Errorf("the refusal says %q, not naming In review", msg)
		}
		// Approved is a status of the project, but not of the workflow it follows.
		if msg := fieldError(t, moveSection(map[string]any{inReview: there["Approved"]}), "statusMapping"); !strings.Contains(msg, "In review") {
			t.Errorf("the refusal of a status of another workflow says %q", msg)
		}
		s.stays(t, "after the unmapped moves")
		if n := s.inProject(t, to); n != 0 {
			t.Errorf("%d requirements arrived", n)
		}
	})

	t.Run("mapped, each lands in the status it was given or the one of its name", func(t *testing.T) {
		want(t, moveSection(map[string]any{inReview: pending}), http.StatusOK, "move with In review mapped to Pending")
		sameList(t, "the tree there", s.tree(t, to), to+"-R1 1", to+"-R2 1.1", to+"-R3 1.2")
		if got := statusOf(t, s.editor, to+"-R2"); got != "Pending" {
			t.Errorf("the submitted requirement is %s there, want Pending", got)
		}
		if got := statusOf(t, s.editor, to+"-R3"); got != "Draft" {
			t.Errorf("the draft is %s there, want Draft", got)
		}
	})
}

// A section whose child is locked does not move to another project: the whole section stays, keys and numbers as
// they were (#310).
func TestASectionWithALockedChildStaysWhereItIs(t *testing.T) {
	s := newMoveScene(t, "ML")
	to := projectKey(t, "MLT")
	s.project(t, to, "Supplier", "editor", true)
	want(t, s.f.owner.post(t, "/api/v1/requirements/"+s.key+"-R2/lock", map[string]any{"reason": "Released in 2.0"}), http.StatusOK, "lock R2")
	r := s.move(s.editor, t, s.key+"-R1", map[string]any{"projectKey": to, "place": place(nil, nil)})
	refuse(t, "a section with a locked child moves", r, http.StatusConflict, "requirement_locked")
	if msg := refusalMessage(t, r); !strings.Contains(msg, s.key+"-R2") || !strings.Contains(msg, "Released in 2.0") {
		t.Errorf("the refusal says %q", msg)
	}
	s.stays(t, "after the locked move")
	for _, key := range []string{s.key + "-R1", s.key + "-R2", s.key + "-R3"} {
		got := obj(t, want(t, s.editor.get(t, "/api/v1/requirements/"+key), http.StatusOK, "read "+key), "requirement")
		if got["key"] != key || got["projectKey"] != s.key || got["id"] != s.ids[key] {
			t.Errorf("%s reads as %v of %v", key, got["key"], got["projectKey"])
		}
	}
	if n := s.inProject(t, to); n != 0 {
		t.Errorf("%d requirements arrived", n)
	}
}

// Somebody who views only the project a requirement left finds nothing under its old key, not even where it went
// (#310).
func TestAnOldKeyTellsNothingToWhoCannotFollowIt(t *testing.T) {
	s := newMoveScene(t, "MK")
	to := projectKey(t, "MKT")
	toID := s.project(t, to, "Secret", "viewer", true)
	if _, err := s.f.h.super.Exec(t.Context(), `DELETE FROM project_grant WHERE project_id = $1 AND subject_type = 'everyone'`, toID); err != nil {
		t.Fatal(err)
	}
	onlyHere := s.f.api.as(t, s.f.h.addPerson(t, "member"))
	s.f.h.settle(t)
	moved := obj(t, want(t, s.move(s.editor, t, s.key+"-R4", map[string]any{"projectKey": to, "place": place(nil, nil)}), http.StatusOK, "move R4"), "requirement")
	if moved["key"] != to+"-R1" {
		t.Fatalf("R4 became %v", moved["key"])
	}
	if got := obj(t, want(t, s.editor.get(t, "/api/v1/requirements/"+s.key+"-R4"), http.StatusOK, "the editor follows the old key"), "requirement"); got["key"] != to+"-R1" {
		t.Errorf("the editor's old key leads to %v", got["key"])
	}
	r := onlyHere.get(t, "/api/v1/requirements/"+s.key+"-R4")
	refuse(t, "somebody who views only the old project asks for the old key", r, http.StatusNotFound, "not_found")
	for _, secret := range []string{to, "Lights dim at night"} {
		if strings.Contains(string(r.Raw), secret) {
			t.Errorf("the refusal names %q: %s", secret, r.Raw)
		}
	}
}

// An export template is deleted only by its project's administrators while the project is open. A deletion has no
// input to refuse with a 422, and an id that is none names no template (#310).
func TestATemplateIsDeletedOnlyWhereItMayBe(t *testing.T) {
	s := newDocumentScene(t)
	owner := s.f.owner
	id := obj(t, want(t, owner.uploadTemplate(t, s.key, "Supplier", "supplier.docx", aWordTemplate(t, "", nil)), http.StatusCreated, "upload a template"), "template")["id"].(string)
	refuse(t, "a member who does not administer deletes it", s.f.plain.delete(t, "/api/v1/export-templates/"+id), http.StatusForbidden, "forbidden")
	want(t, owner.put(t, "/api/v1/projects/"+s.key+"/archive", nil), http.StatusOK, "archive the project")
	refuse(t, "a template of an archived project is deleted", owner.delete(t, "/api/v1/export-templates/"+id), http.StatusConflict, "project_archived")
	refuse(t, "an id that is none", owner.delete(t, "/api/v1/export-templates/not-an-id"), http.StatusNotFound, "not_found")
	if n := s.f.h.countRows(t, `SELECT count(*) FROM export_template WHERE id = $1`, id); n != 1 {
		t.Errorf("the refusals left %d templates, want 1", n)
	}
}
