//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Straight through SQL as exciter_app: views keep to their owners, editors share them and administrators manage
// shared ones; and the search function returns only what the caller may view.
func TestRawSQLKeepsViewsAndSearchHitsToTheirReaders(t *testing.T) {
	viwKey := projectKey(t, "VIW")
	h := newHarness(t)
	a := h.owner(t, "view-wall-a")
	ctx := context.Background()
	var project uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO project (key, name, created_by) VALUES ('`+viwKey+`', 'Viewed', $1) RETURNING id`,
		a.user).Scan(&project); err != nil {
		t.Fatal(err)
	}
	editorID, viewerID, outsiderID := h.addPerson(t, "member"), h.addPerson(t, "guest"), h.addPerson(t, "guest")
	h.grantAs(t, project, editorID, "editor")
	h.grantAs(t, project, viewerID, "viewer")
	editor, viewer, outsider, owner := h.appConn(t, editorID), h.appConn(t, viewerID), h.appConn(t, outsiderID), h.appConn(t, a.user)
	save := `INSERT INTO saved_view (project_id, owner_id, name, shared) VALUES ($1, current_actor_id(), $2, $3) RETURNING id`

	var private, shared uuid.UUID
	t.Run("views are saved in one's own name, and shared by editors", func(t *testing.T) {
		if err := editor.QueryRow(ctx, save, project, "Mine", false).Scan(&private); err != nil {
			t.Fatalf("an editor could not save a view: %v", err)
		}
		if err := editor.QueryRow(ctx, save, project, "Ours", true).Scan(&shared); err != nil {
			t.Fatalf("an editor could not share a view: %v", err)
		}
		if _, err := viewer.Exec(ctx, save, project, "Shout", true); !isPolicyViolation(err) {
			t.Errorf("a viewer shared a view: %v", err)
		}
		if _, err := outsider.Exec(ctx, save, project, "Peek", false); !isPolicyViolation(err) {
			t.Errorf("somebody who may not view the project saved a view of it: %v", err)
		}
		if _, err := viewer.Exec(ctx, `INSERT INTO saved_view (project_id, owner_id, name) VALUES ($1, $2, 'Forged')`,
			project, editorID); !isPolicyViolation(err) {
			t.Errorf("a view was saved in somebody else's name: %v", err)
		}
		if _, err := editor.Exec(ctx, save, project, "mine", false); !refusedBy(err, "saved_view_name_key") {
			t.Errorf("a second view took a name without regard to case: %v", err)
		}
	})

	t.Run("a view is read by its owner, or by the project's readers once shared", func(t *testing.T) {
		count := map[string]int{}
		for who, conn := range map[string]*pgx.Conn{"editor": editor, "viewer": viewer, "outsider": outsider} {
			var n int
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM saved_view WHERE project_id = $1`, project).Scan(&n); err != nil {
				t.Fatal(err)
			}
			count[who] = n
		}
		if count["editor"] != 2 || count["viewer"] != 1 || count["outsider"] != 0 {
			t.Errorf("views seen: %v", count)
		}
	})

	t.Run("a view is changed by its owner, or by an administrator once shared, and keeps its owner", func(t *testing.T) {
		if n := affects(t, viewer, `UPDATE saved_view SET name = 'Taken' WHERE id = $1`, shared); n != 0 {
			t.Error("a viewer renamed a shared view")
		}
		if n := affects(t, owner, `UPDATE saved_view SET name = 'Peeked' WHERE id = $1`, private); n != 0 {
			t.Error("an administrator changed somebody's private view")
		}
		if n := affects(t, owner, `UPDATE saved_view SET name = 'Everyone' WHERE id = $1`, shared); n != 1 {
			t.Error("an administrator could not rename a shared view")
		}
		if _, err := owner.Exec(ctx, `UPDATE saved_view SET shared = false WHERE id = $1`, shared); !isPolicyViolation(err) {
			t.Errorf("an administrator took somebody's view out of sight: %v", err)
		}
		if _, err := editor.Exec(ctx, `UPDATE saved_view SET owner_id = $2 WHERE id = $1`, private, viewerID); !isPolicyViolation(err) {
			t.Errorf("a view changed hands: %v", err)
		}
		if n := affects(t, viewer, `DELETE FROM saved_view WHERE id = $1`, shared); n != 0 {
			t.Error("a viewer deleted a shared view")
		}
		if n := affects(t, editor, `DELETE FROM saved_view WHERE id = $1`, private); n != 1 {
			t.Error("the owner could not delete their view")
		}
	})

	t.Run("the search function returns only what the caller may view", func(t *testing.T) {
		if _, err := h.super.Exec(ctx, `INSERT INTO requirement (project_id, title, statement, normative_hash, created_by)
			VALUES ($1, 'Walled basket', '{"type":"doc"}', 'h', $2)`, project, a.user); err != nil {
			t.Fatal(err)
		}
		query := `SELECT count(*) FROM requirement_search_rows(websearch_to_tsquery('exciter_search', 'basket'), '', 0)`
		for who, want := range map[*pgx.Conn]int{viewer: 1, outsider: 0} {
			var n int
			if err := who.QueryRow(ctx, query).Scan(&n); err != nil || n != want {
				t.Errorf("found %d, want %d: %v", n, want, err)
			}
		}
		var n int
		if err := outsider.QueryRow(ctx, `SELECT count(*) FROM requirement_search_rows(to_tsquery('exciter_search', 'nothing'), '`+viwKey+`', 1)`).Scan(&n); err != nil || n != 0 {
			t.Errorf("a key reached past the caller's rights: %d %v", n, err)
		}
	})
}
