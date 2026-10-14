//go:build integration

package test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Straight through SQL as exciter_app: no requirement goes below itself or into another project's tree, two
// moves that would close a loop together cannot both land, and a move to another project renumbers and keeps
// the old key whoever makes it.
func TestRawSQLKeepsTheTreeWhole(t *testing.T) {
	treeKey := projectKey(t, "TREE")
	elseKey := projectKey(t, "ELSE")
	h := newHarness(t)
	a := h.owner(t, "tree-wall")
	ctx := context.Background()
	projects := map[string]uuid.UUID{}
	for _, key := range []string{treeKey, elseKey} {
		var id uuid.UUID
		if err := h.super.QueryRow(ctx, `INSERT INTO project (key, name, created_by) VALUES ($1, $1, $2) RETURNING id`, key, a.user).Scan(&id); err != nil {
			t.Fatal(err)
		}
		projects[key] = id
	}
	editorID := h.addPerson(t, "member")
	editor := h.appConn(t, editorID)
	write := func(project string, parent *uuid.UUID) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := editor.QueryRow(ctx, `INSERT INTO requirement (project_id, title, statement, normative_hash, parent_id, created_by)
			VALUES ($1, 'Node', '{"type":"doc"}', 'h', $2, current_actor_id()) RETURNING id`, projects[project], parent).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	top := write(treeKey, nil)
	child := write(treeKey, &top)
	grandchild := write(treeKey, &child)
	elsewhere := write(elseKey, nil)

	t.Run("nothing goes below itself or into another project's tree", func(t *testing.T) {
		if _, err := editor.Exec(ctx, `UPDATE requirement SET parent_id = $2 WHERE id = $1`, top, grandchild); !refusedBy(err, "requirement_tree_no_cycle") {
			t.Errorf("a requirement went below its own grandchild: %v", err)
		}
		if _, err := editor.Exec(ctx, `UPDATE requirement SET parent_id = id WHERE id = $1`, top); !refusedBy(err, "requirement_tree_no_cycle") {
			t.Errorf("a requirement went below itself: %v", err)
		}
		if _, err := editor.Exec(ctx, `UPDATE requirement SET parent_id = $2 WHERE id = $1`, top, elsewhere); !refusedBy(err, "requirement_parent_same_project") {
			t.Errorf("a requirement went below one of another project: %v", err)
		}
		if _, err := editor.Exec(ctx, `UPDATE requirement SET number = 99 WHERE id = $1`, top); !refusedBy(err, "requirement_fixed") {
			t.Errorf("a number changed: %v", err)
		}
	})

	t.Run("two moves that would close a loop together cannot both land", func(t *testing.T) {
		left, right := write(treeKey, nil), write(treeKey, nil)
		other := h.appConn(t, editorID)
		first, err := editor.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := first.Exec(ctx, `UPDATE requirement SET parent_id = $2 WHERE id = $1`, left, right); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := other.Exec(ctx, `UPDATE requirement SET parent_id = $2 WHERE id = $1`, right, left)
			done <- err
		}()
		// The second move waits on the project's tree lock while the first holds it.
		select {
		case err := <-done:
			t.Fatalf("the second move did not wait for the first: %v", err)
		case <-time.After(300 * time.Millisecond):
		}
		if err := first.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-done; !refusedBy(err, "requirement_tree_no_cycle") {
			t.Errorf("the second move closed a loop: %v", err)
		}
	})

	t.Run("a move to another project renumbers and keeps the old key", func(t *testing.T) {
		var number int
		if err := editor.QueryRow(ctx, `UPDATE requirement SET project_id = $2, parent_id = NULL WHERE id = $1 RETURNING number`, grandchild, projects[elseKey]).Scan(&number); err != nil {
			t.Fatalf("an editor of both could not move a requirement: %v", err)
		}
		if number != 2 {
			t.Errorf("the moved requirement took number %d of the other project, want 2", number)
		}
		var old string
		if err := editor.QueryRow(ctx, `SELECT key FROM requirement_old_key WHERE requirement_id = $1`, grandchild).Scan(&old); err != nil || old != treeKey+"-R3" {
			t.Errorf("the old key stays behind: %q %v", old, err)
		}
		guest := h.addPerson(t, "guest")
		h.grantAs(t, projects[treeKey], guest, "editor")
		mover := h.appConn(t, guest)
		if _, err := mover.Exec(ctx, `UPDATE requirement SET project_id = $2, parent_id = NULL WHERE id = $1`, child, projects[elseKey]); !isPolicyViolation(err) {
			t.Errorf("a requirement moved into a project its mover may not edit: %v", err)
		}
	})
}
