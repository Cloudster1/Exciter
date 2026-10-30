//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// The service changing groups only for administrators is not proof (#335): straight through SQL as exciter_app,
// a plain member makes, renames and removes no group and joins or leaves none, so a group's admin grant never
// reaches them, and an administrator still makes every one of those writes.
func TestRawSQLKeepsGroupsToTheAdministrators(t *testing.T) {
	grpKey := projectKey(t, "GRP")
	h := newHarness(t)
	a := h.owner(t, "groups-a")
	ctx := context.Background()
	adminID := h.addPerson(t, "admin")
	plainID := h.addPerson(t, "member")
	otherID := h.addPerson(t, "member")
	wardens := h.makeGroup(t, "Wardens", otherID)

	var project uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO project (key, name, created_by) VALUES ('`+grpKey+`', 'Guarded', $1) RETURNING id`,
		a.user).Scan(&project); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO role_assignment (project_id, group_id, role) VALUES ($1, $2, 'admin')`,
		project, wardens); err != nil {
		t.Fatal(err)
	}
	h.settle(t)

	t.Run("a plain member is refused", func(t *testing.T) {
		plain := h.appConn(t, plainID)
		refusals := []struct {
			what string
			sql  string
			args []any
		}{
			{"making a group", `INSERT INTO groups (name) VALUES ($1)`, []any{"Planted " + h.suffix()}},
			{"making a group the provider fills", `INSERT INTO groups (name, source, external_ref) VALUES ($1, 'oidc', $1)`, []any{"planted-" + h.suffix()}},
			{"joining a group that runs a project", `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, []any{wardens, plainID}},
		}
		for _, try := range refusals {
			if _, err := plain.Exec(ctx, try.sql, try.args...); !isPolicyViolation(err) {
				t.Errorf("a plain member %s = %v, want a policy violation", try.what, err)
			}
		}
		touchesNothing(t, plain, "a plain member renaming a group", `UPDATE groups SET name = 'Taken' WHERE id = $1`, wardens)
		touchesNothing(t, plain, "a plain member handing a group to the provider",
			`UPDATE groups SET source = 'oidc', external_ref = 'taken' WHERE id = $1`, wardens)
		touchesNothing(t, plain, "a plain member deleting a group", `DELETE FROM groups WHERE id = $1`, wardens)
		touchesNothing(t, plain, "a plain member taking somebody out of a group", `DELETE FROM group_member WHERE group_id = $1`, wardens)
		touchesNothing(t, plain, "a plain member taking over somebody's membership",
			`UPDATE group_member SET user_id = $2 WHERE group_id = $1`, wardens, plainID)

		if n := h.countRows(t, `SELECT count(*) FROM group_member WHERE group_id = $1 AND user_id = $2`, wardens, otherID); n != 1 {
			t.Errorf("the group holds %d of its one member", n)
		}
		if n := h.countRows(t, `SELECT count(*) FROM groups WHERE id = $1 AND name LIKE 'Wardens %' AND source = 'local'`, wardens); n != 1 {
			t.Error("a plain member changed the group")
		}
		var holds bool
		if err := plain.QueryRow(ctx, `SELECT perm_project_holds($1, $2, 'administer')`, project, plainID).Scan(&holds); err != nil || holds {
			t.Errorf("the group's admin grant reaches the plain member: %v (%v)", holds, err)
		}
	})

	t.Run("an administrator still writes them", func(t *testing.T) {
		tx, err := h.appConn(t, adminID).Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		var made uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO groups (name) VALUES ($1) RETURNING id`, "Keepers "+h.suffix()).Scan(&made); err != nil {
			t.Fatalf("an administrator making a group: %v", err)
		}
		for _, try := range []struct {
			what string
			sql  string
			args []any
		}{
			{"renaming the group", `UPDATE groups SET name = $2 WHERE id = $1`, []any{wardens, "Stewards " + h.suffix()}},
			{"putting the member in it", `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, []any{wardens, plainID}},
			{"moving the other to the new group", `UPDATE group_member SET group_id = $2 WHERE group_id = $1 AND user_id = $3`, []any{wardens, made, otherID}},
			{"taking the other out of it", `DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`, []any{made, otherID}},
			{"deleting the new group", `DELETE FROM groups WHERE id = $1`, []any{made}},
		} {
			tag, err := tx.Exec(ctx, try.sql, try.args...)
			if err != nil || tag.RowsAffected() != 1 {
				t.Errorf("an administrator %s reached %d rows (%v)", try.what, tag.RowsAffected(), err)
			}
		}
		var holds bool
		if err := tx.QueryRow(ctx, `SELECT perm_project_holds($1, $2, 'administer')`, project, plainID).Scan(&holds); err != nil || !holds {
			t.Errorf("the group's admin grant does not reach the member an administrator put in it: %v (%v)", holds, err)
		}
	})
}
