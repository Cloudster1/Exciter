//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Straight through SQL as exciter_app: a link is read by its requirement's viewers and made in one's own name on a
// live requirement one edits; the stored status, sync records and sync events are the server's alone to write.
func TestRawSQLGuardsArmatureLinks(t *testing.T) {
	walKey := projectKey(t, "WAL")
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-link-wall")
	owner := api.as(t, home.user)
	editorID := h.addPerson(t, "member")
	guestID := h.addPerson(t, "guest")
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	project := obj(t, want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": walKey, "name": "Walled"}), http.StatusCreated, "make a project"), "project")
	live := obj(t, want(t, owner.post(t, "/api/v1/projects/"+walKey+"/requirements", map[string]any{"title": "Live"}), http.StatusCreated, "write a requirement"), "requirement")
	section := obj(t, want(t, owner.post(t, "/api/v1/projects/"+walKey+"/requirements", map[string]any{"title": "Part", "kind": "section"}), http.StatusCreated, "write a section"), "requirement")
	trashed := obj(t, want(t, owner.post(t, "/api/v1/projects/"+walKey+"/requirements", map[string]any{"title": "Gone"}), http.StatusCreated, "write another"), "requirement")
	want(t, owner.delete(t, "/api/v1/requirements/"+walKey+"-R3"), http.StatusNoContent, "trash it")
	h.settle(t)
	ctx := context.Background()
	insert := `INSERT INTO armature_issue_link (requirement_id, relation, issue_key, created_by) VALUES ($1, 'implements', $2, $3) RETURNING id`

	editor := h.appConn(t, editorID)
	var linkID uuid.UUID
	if err := editor.QueryRow(ctx, insert, live["id"], "CP-1", editorID).Scan(&linkID); err != nil {
		t.Fatalf("an editor links in their own name: %v", err)
	}

	t.Run("an editor links only live requirements, in their own name, and writes nothing of the issue", func(t *testing.T) {
		refusedOutright(t, editor, "a link in the owner's name", insert, live["id"], "CP-2", home.user)
		refusedOutright(t, editor, "a link on a section", insert, section["id"], "CP-2", editorID)
		refusedOutright(t, editor, "a link on a trashed requirement", insert, trashed["id"], "CP-2", editorID)
		refusedOutright(t, editor, "a key of the wrong shape", insert, live["id"], "cp-2", editorID)
		refusedOutright(t, editor, "a stored status", `INSERT INTO armature_issue_link (requirement_id, relation, issue_key, created_by, status_category)
			VALUES ($1, 'verifies', 'CP-2', $2, 'done')`, live["id"], editorID)
		refusedOutright(t, editor, "marking the status", `UPDATE armature_issue_link SET status_category = 'done' WHERE id = $1`, linkID)
		refusedOutright(t, editor, "detaching the link", `UPDATE armature_issue_link SET detached_at = now() WHERE id = $1`, linkID)
		refusedOutright(t, editor, "a sync record", `INSERT INTO armature_remote_link (requirement_id, issue_key, state) VALUES ($1, 'CP-1', 'synced')`,
			live["id"])
		refusedOutright(t, editor, "an event with more than a requirement", `INSERT INTO outbox_event (topic, payload) VALUES ('armature.links', $1)`,
			map[string]any{"requirementId": live["id"], "actorId": editorID, "issueKey": "CP-9"})
		refusedOutright(t, editor, "an event without a requirement", `INSERT INTO outbox_event (topic, payload) VALUES ('armature.links', $1)`,
			map[string]any{"actorId": editorID})
		if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE topic = 'armature.links' AND payload ->> 'requirementId' = $1`, live["id"]); n != 1 {
			t.Errorf("the link wrote %d sync events, want 1", n)
		}
	})

	t.Run("a guest without the project reads and removes nothing, and a viewer reads but removes nothing", func(t *testing.T) {
		guest := h.appConn(t, guestID)
		if n := seesRows(t, guest, `SELECT count(*) FROM armature_issue_link`); n != 0 {
			t.Errorf("a guest reads %d links", n)
		}
		touchesNothing(t, guest, "a guest removing a link", `DELETE FROM armature_issue_link WHERE id = $1`, linkID)
		h.grantAs(t, uuid.MustParse(project["id"].(string)), guestID, "viewer")
		if n := seesRows(t, guest, `SELECT count(*) FROM armature_issue_link`); n != 1 {
			t.Errorf("a viewer reads %d links, want 1", n)
		}
		touchesNothing(t, guest, "a viewer removing a link", `DELETE FROM armature_issue_link WHERE id = $1`, linkID)
		refusedOutright(t, guest, "a viewer linking", insert, live["id"], "CP-3", guestID)
	})

	t.Run("without a connection nothing is linked, and the links stay", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/armature/connection"), http.StatusNoContent, "disconnect Armature")
		h.settle(t)
		refusedOutright(t, h.appConn(t, editorID), "a link without a connection", insert, live["id"], "CP-5", editorID)
		if n := h.countRows(t, `SELECT count(*) FROM armature_issue_link`); n != 1 {
			t.Errorf("disconnecting left %d links, want the 1 made", n)
		}
	})
}
