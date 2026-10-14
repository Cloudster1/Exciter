//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Straight through SQL as exciter_app: a label is put on and taken off by who edits its requirement, in their own
// name, as Normalize writes it, never changed in place, and seen only with its requirement.
func TestRawSQLKeepsLabelsWithTheirRequirement(t *testing.T) {
	labKey := projectKey(t, "LAB")
	h := newHarness(t)
	a := h.owner(t, "label-wall-a")
	ctx := context.Background()
	var project uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO project (key, name, created_by) VALUES ('`+labKey+`', 'Labelled', $1) RETURNING id`,
		a.user).Scan(&project); err != nil {
		t.Fatal(err)
	}
	editorID := h.addPerson(t, "member")
	viewerID := h.addPerson(t, "guest")
	outsiderID := h.addPerson(t, "guest")
	h.grantAs(t, project, viewerID, "viewer")
	editor, viewer, outsider := h.appConn(t, editorID), h.appConn(t, viewerID), h.appConn(t, outsiderID)
	var id uuid.UUID
	if err := editor.QueryRow(ctx, `INSERT INTO requirement (project_id, title, statement, normative_hash, created_by)
		VALUES ($1, 'Labelled', '{"type":"doc"}', 'h', current_actor_id()) RETURNING id`, project).Scan(&id); err != nil {
		t.Fatal(err)
	}
	put := `INSERT INTO requirement_label (requirement_id, name, created_by) VALUES ($1, $2, current_actor_id())`

	t.Run("editors put labels on in their own name, as Normalize writes them", func(t *testing.T) {
		if _, err := editor.Exec(ctx, put, id, "safety-critical"); err != nil {
			t.Fatalf("an editor could not label a requirement: %v", err)
		}
		if _, err := viewer.Exec(ctx, put, id, "viewed"); !isPolicyViolation(err) {
			t.Errorf("a viewer labelled a requirement: %v", err)
		}
		if _, err := outsider.Exec(ctx, put, id, "foreign"); !isPolicyViolation(err) {
			t.Errorf("a guest without a grant labelled a requirement: %v", err)
		}
		if _, err := editor.Exec(ctx, `INSERT INTO requirement_label (requirement_id, name, created_by) VALUES ($1, 'forged', $2)`,
			id, viewerID); !isPolicyViolation(err) {
			t.Errorf("an editor labelled in somebody else's name: %v", err)
		}
		for _, name := range []string{"Upper", "two words", "-lead", "a/b", ""} {
			if _, err := editor.Exec(ctx, put, id, name); !refusedBy(err, "requirement_label_name_normal") {
				t.Errorf("the label %q was stored: %v", name, err)
			}
		}
		if _, err := editor.Exec(ctx, `UPDATE requirement_label SET name = 'renamed' WHERE requirement_id = $1`, id); !isPolicyViolation(err) {
			t.Errorf("a label was changed in place: %v", err)
		}
	})

	t.Run("a label is seen with its requirement and taken off by its editors", func(t *testing.T) {
		seen := map[string]int{"a viewer": 1, "a guest without a grant": 0}
		for who, conn := range map[string]*pgx.Conn{"a viewer": viewer, "a guest without a grant": outsider} {
			var n int
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM requirement_label WHERE requirement_id = $1`, id).Scan(&n); err != nil || n != seen[who] {
				t.Errorf("%s sees %d labels, want %d: %v", who, n, seen[who], err)
			}
		}
		if n := affects(t, viewer, `DELETE FROM requirement_label WHERE requirement_id = $1`, id); n != 0 {
			t.Error("a viewer took a label off")
		}
		if n := affects(t, editor, `UPDATE requirement SET trashed_at = now(), trash_id = id WHERE id = $1`, id); n != 1 {
			t.Fatal("an editor could not trash the requirement")
		}
		var n int
		if err := viewer.QueryRow(ctx, `SELECT count(*) FROM requirement_label WHERE requirement_id = $1`, id).Scan(&n); err != nil || n != 0 {
			t.Errorf("a viewer sees %d labels of a trashed requirement: %v", n, err)
		}
		if n := affects(t, editor, `DELETE FROM requirement_label WHERE requirement_id = $1`, id); n != 1 {
			t.Error("an editor could not take a label off")
		}
	})
}
