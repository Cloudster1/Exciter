//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Straight through SQL as exciter_app: editors write requirements and numbers come from the project in order,
// revisions are append only and run in sequence, the trash hides from readers, and only administrators purge.
func TestRawSQLKeepsRequirementsAndTheirHistory(t *testing.T) {
	reqKey := projectKey(t, "REQ")
	oldKey := projectKey(t, "OLD")
	h := newHarness(t)
	a := h.owner(t, "req-wall-a")
	ctx := context.Background()
	var project uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO project (org_id, key, name, created_by) VALUES ($1, '`+reqKey+`', 'Walled', $2) RETURNING id`,
		a.org, a.user).Scan(&project); err != nil {
		t.Fatal(err)
	}
	editorID := h.addPerson(t, a.org, "member")
	viewerID := h.addPerson(t, a.org, "guest")
	h.grantAs(t, a.org, project, viewerID, "viewer")
	editor, viewer, owner := h.appConn(t, editorID), h.appConn(t, viewerID), h.appConn(t, a.user)
	strangerID := h.addPerson(t, a.org, "guest")
	stranger := h.appConn(t, strangerID)
	insert := `INSERT INTO requirement (org_id, project_id, number, title, statement, normative_hash, created_by)
		VALUES ($1, $2, 99, 'Walled in', '{"type":"doc"}', 'h', current_actor_id()) RETURNING id, number`

	var id uuid.UUID
	var number int
	t.Run("editors write, and the project hands out the number", func(t *testing.T) {
		if err := editor.QueryRow(ctx, insert, a.org, project).Scan(&id, &number); err != nil {
			t.Fatalf("an editor could not write a requirement: %v", err)
		}
		if number != 1 {
			t.Errorf("the requirement took number %d, want 1 whatever the insert said", number)
		}
		var second uuid.UUID
		if err := editor.QueryRow(ctx, insert, a.org, project).Scan(&second, &number); err != nil || number != 2 {
			t.Errorf("the next requirement took number %d: %v", number, err)
		}
		if _, err := viewer.Exec(ctx, insert, a.org, project); !isPolicyViolation(err) {
			t.Errorf("a viewer wrote a requirement: %v", err)
		}
		if _, err := stranger.Exec(ctx, insert, a.org, project); !isPolicyViolation(err) {
			t.Errorf("a guest no grant names wrote a requirement: %v", err)
		}
		if _, err := editor.Exec(ctx, `UPDATE requirement SET number = 7 WHERE id = $1`, id); !refusedBy(err, "requirement_fixed") {
			t.Errorf("a requirement's number changed: %v", err)
		}
		if n := affects(t, viewer, `UPDATE requirement SET title = 'Viewed' WHERE id = $1`, id); n != 0 {
			t.Error("a viewer changed a requirement")
		}
	})

	t.Run("revisions are written in sequence and never changed", func(t *testing.T) {
		revision := `INSERT INTO requirement_revision (org_id, requirement_id, number, title, statement, hash, created_by)
			VALUES ($1, $2, $3, 'Walled in', '{"type":"doc"}', 'h', current_actor_id())`
		if _, err := editor.Exec(ctx, revision, a.org, id, 1); err != nil {
			t.Fatalf("an editor could not write revision 1: %v", err)
		}
		if _, err := editor.Exec(ctx, revision, a.org, id, 3); !refusedBy(err, "requirement_revision_in_sequence") {
			t.Errorf("revision 3 followed revision 1: %v", err)
		}
		if _, err := viewer.Exec(ctx, revision, a.org, id, 2); !isPolicyViolation(err) {
			t.Errorf("a viewer wrote a revision: %v", err)
		}
		var current int
		if err := editor.QueryRow(ctx, `SELECT revision FROM requirement WHERE id = $1`, id).Scan(&current); err != nil || current != 1 {
			t.Errorf("the requirement says revision %d after revision 1: %v", current, err)
		}
		if _, err := owner.Exec(ctx, `UPDATE requirement_revision SET title = 'Rewritten' WHERE requirement_id = $1`, id); !isPolicyViolation(err) {
			t.Errorf("even the owner rewrote history: %v", err)
		}
		if _, err := owner.Exec(ctx, `DELETE FROM requirement_revision WHERE requirement_id = $1`, id); !isPolicyViolation(err) {
			t.Errorf("even the owner deleted history: %v", err)
		}
		if _, err := editor.Exec(ctx, `INSERT INTO requirement_change (org_id, requirement_id, field, actor_id) VALUES ($1, $2, 'owner', current_actor_id())`, a.org, id); err != nil {
			t.Errorf("an editor could not log a change: %v", err)
		}
		if _, err := owner.Exec(ctx, `DELETE FROM requirement_change WHERE requirement_id = $1`, id); !isPolicyViolation(err) {
			t.Errorf("a logged change was deleted: %v", err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO requirement_old_key (org_id, key, requirement_id) VALUES ($1, '`+oldKey+`-R1', $2)`, a.org, id); !isPolicyViolation(err) {
			t.Errorf("the app wrote an old key by hand: %v", err)
		}
	})

	t.Run("readers see what they may, and the trash only its editors", func(t *testing.T) {
		seen := func(who string, n int, want int, err error) {
			t.Helper()
			if err != nil || n != want {
				t.Errorf("%s sees %d requirements, want %d: %v", who, n, want, err)
			}
		}
		var n int
		err := viewer.QueryRow(ctx, `SELECT count(*) FROM requirement WHERE project_id = $1`, project).Scan(&n)
		seen("a viewer", n, 2, err)
		err = stranger.QueryRow(ctx, `SELECT count(*) FROM requirement WHERE project_id = $1`, project).Scan(&n)
		seen("a guest no grant names", n, 0, err)
		err = stranger.QueryRow(ctx, `SELECT count(*) FROM requirement_revision WHERE requirement_id = $1`, id).Scan(&n)
		seen("a guest no grant names, the history", n, 0, err)
		if n := affects(t, editor, `UPDATE requirement SET trashed_at = now(), trash_id = id WHERE id = $1`, id); n != 1 {
			t.Fatal("an editor could not trash a requirement")
		}
		err = viewer.QueryRow(ctx, `SELECT count(*) FROM requirement WHERE project_id = $1`, project).Scan(&n)
		seen("a viewer, with one in the trash", n, 1, err)
		err = viewer.QueryRow(ctx, `SELECT count(*) FROM requirement_revision WHERE requirement_id = $1`, id).Scan(&n)
		seen("a viewer, in a trashed requirement's history", n, 0, err)
		err = editor.QueryRow(ctx, `SELECT count(*) FROM requirement WHERE project_id = $1`, project).Scan(&n)
		seen("an editor, with one in the trash", n, 2, err)
	})

	t.Run("only administrators purge, and only from the trash", func(t *testing.T) {
		if n := affects(t, editor, `DELETE FROM requirement WHERE id = $1`, id); n != 0 {
			t.Error("an editor purged a requirement")
		}
		var live uuid.UUID
		if err := owner.QueryRow(ctx, `SELECT id FROM requirement WHERE project_id = $1 AND trashed_at IS NULL`, project).Scan(&live); err != nil {
			t.Fatal(err)
		}
		if n := affects(t, owner, `DELETE FROM requirement WHERE id = $1`, live); n != 0 {
			t.Error("a requirement out of the trash was purged")
		}
		if n := affects(t, owner, `DELETE FROM requirement WHERE id = $1`, id); n != 1 {
			t.Error("the organization's owner could not purge from the trash")
		}
		if n := h.countRows(t, `SELECT count(*) FROM requirement_revision WHERE requirement_id = $1`, id); n != 0 {
			t.Errorf("%d revisions outlived their purged requirement", n)
		}
	})
}
