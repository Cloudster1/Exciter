//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Straight through SQL as exciter_app: a page link is read by its requirement's viewers and made in one's own name on
// a live requirement one edits; the page's versions are the server's alone, and clearing is its function's.
func TestRawSQLGuardsStatorLinks(t *testing.T) {
	swlKey := projectKey(t, "SWL")
	h := newHarness(t)
	h.keepStator(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "stator-link-wall")
	owner := api.as(t, home.user)
	editorID := h.addPerson(t, "member")
	guestID := h.addPerson(t, "guest")
	want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t)}), http.StatusOK, "connect Stator")
	project := obj(t, want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": swlKey, "name": "Walled pages"}), http.StatusCreated, "make a project"), "project")
	live := obj(t, want(t, owner.post(t, "/api/v1/projects/"+swlKey+"/requirements", map[string]any{"title": "Live"}), http.StatusCreated, "write a requirement"), "requirement")
	section := obj(t, want(t, owner.post(t, "/api/v1/projects/"+swlKey+"/requirements", map[string]any{"title": "Part", "kind": "section"}), http.StatusCreated, "write a section"), "requirement")
	trashed := obj(t, want(t, owner.post(t, "/api/v1/projects/"+swlKey+"/requirements", map[string]any{"title": "Gone"}), http.StatusCreated, "write another"), "requirement")
	want(t, owner.delete(t, "/api/v1/requirements/"+swlKey+"-R3"), http.StatusNoContent, "trash it")
	h.settle(t)
	ctx := context.Background()
	page := uuid.New()
	insert := `INSERT INTO stator_page_link (requirement_id, relation, page_id, space_key, url, created_by)
		VALUES ($1, 'specifies', $2, 'ENG', 'https://stator.example/s/ENG/p/x', $3) RETURNING id`
	mine := func(sql string) string { return sql + ` AND requirement_id = '` + live["id"].(string) + `'` }

	editor := h.appConn(t, editorID)
	var linkID uuid.UUID
	if err := editor.QueryRow(ctx, insert, live["id"], page, editorID).Scan(&linkID); err != nil {
		t.Fatalf("an editor links in their own name: %v", err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM stator_page_link WHERE id = $1 AND reviewed_rev = 1 AND page_version IS NULL`, linkID); n != 1 {
		t.Error("a new link does not take its requirement's revision, or takes a version nobody read")
	}
	if _, err := h.super.Exec(ctx, `UPDATE stator_page_link SET page_version = 5, page_version_seen = 4 WHERE id = $1`, linkID); err != nil {
		t.Fatal(err)
	}
	h.settle(t)

	t.Run("an editor links only live requirements, in their own name, and writes nothing of the page", func(t *testing.T) {
		refusedOutright(t, editor, "a link in the owner's name", insert, live["id"], uuid.New(), home.user)
		refusedOutright(t, editor, "a link on a section", insert, section["id"], uuid.New(), editorID)
		refusedOutright(t, editor, "a link on a trashed requirement", insert, trashed["id"], uuid.New(), editorID)
		refusedOutright(t, editor, "a version of their own", `INSERT INTO stator_page_link (requirement_id, relation, page_id, space_key, url, created_by, page_version, page_version_seen)
			VALUES ($1, 'relates', $2, 'ENG', 'https://stator.example/s/ENG/p/y', $3, 9, 9)`, live["id"], uuid.New(), editorID)
		refusedOutright(t, editor, "a revision of their own", `INSERT INTO stator_page_link (requirement_id, relation, page_id, space_key, url, created_by, reviewed_rev)
			VALUES ($1, 'relates', $2, 'ENG', 'https://stator.example/s/ENG/p/y', $3, 99)`, live["id"], uuid.New(), editorID)
		refusedOutright(t, editor, "confirming the version seen", `UPDATE stator_page_link SET page_version_seen = 5 WHERE id = $1`, linkID)
		refusedOutright(t, editor, "breaking the link", `UPDATE stator_page_link SET broken_at = now() WHERE id = $1`, linkID)
		refusedOutright(t, editor, "a clearance of their own", `INSERT INTO trace_link_clearance (stator_link_id, source_rev, page_version, comment, cleared_by)
			VALUES ($1, 1, 5, 'Read it.', $2)`, linkID, editorID)
		refusedOutright(t, editor, "clearing against a version Stator never named", `SELECT stator_page_link_clear($1, 4, 'Read it.')`, linkID)
	})

	t.Run("a guest without the project reads and clears nothing, and a viewer reads but changes nothing", func(t *testing.T) {
		guest := h.appConn(t, guestID)
		if n := seesRows(t, guest, mine(`SELECT count(*) FROM stator_page_link WHERE true`)); n != 0 {
			t.Errorf("a guest reads %d links", n)
		}
		touchesNothing(t, guest, "a guest removing a link", `DELETE FROM stator_page_link WHERE id = $1`, linkID)
		refusedOutright(t, guest, "a guest clearing", `SELECT stator_page_link_clear($1, 5, 'Read it.')`, linkID)
		h.grantAs(t, uuid.MustParse(project["id"].(string)), guestID, "viewer")
		if n := seesRows(t, guest, mine(`SELECT count(*) FROM stator_page_link WHERE true`)); n != 1 {
			t.Errorf("a viewer reads %d links, want 1", n)
		}
		touchesNothing(t, guest, "a viewer removing a link", `DELETE FROM stator_page_link WHERE id = $1`, linkID)
		refusedOutright(t, guest, "a viewer linking", insert, live["id"], uuid.New(), guestID)
		refusedOutright(t, guest, "a viewer clearing", `SELECT stator_page_link_clear($1, 5, 'Read it.')`, linkID)
	})

	t.Run("an editor clears through the function alone, and the clearance shows to the requirement's viewers", func(t *testing.T) {
		if _, err := editor.Exec(ctx, `SELECT stator_page_link_clear($1, 5, 'Read it.')`, linkID); err != nil {
			t.Fatalf("the editor clears the link: %v", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM stator_page_link WHERE id = $1 AND page_version_seen = 5`, linkID); n != 1 {
			t.Error("clearing did not confirm the version")
		}
		if n := seesRows(t, h.appConn(t, guestID), `SELECT count(*) FROM trace_link_clearance WHERE stator_link_id = '`+linkID.String()+`'`); n != 1 {
			t.Errorf("a viewer reads %d clearances of the link, want 1", n)
		}
		refusedOutright(t, editor, "rewriting the clearance", `UPDATE trace_link_clearance SET comment = 'Changed.' WHERE stator_link_id = $1`, linkID)
	})

	t.Run("without a connection nothing is linked, and the links stay", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/stator/connection"), http.StatusNoContent, "disconnect Stator")
		h.settle(t)
		refusedOutright(t, h.appConn(t, editorID), "a link without a connection", insert, live["id"], uuid.New(), editorID)
		if n := h.countRows(t, `SELECT count(*) FROM stator_page_link WHERE requirement_id = $1`, live["id"]); n != 1 {
			t.Errorf("disconnecting left %d links, want the 1 made", n)
		}
	})
}
