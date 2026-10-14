//go:build integration

package test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// makeGroup makes a group of org with members, behind the policies' back.
func (h *harness) makeGroup(t *testing.T, org uuid.UUID, name string, members ...uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO groups (org_id, name) VALUES ($1, $2) RETURNING id`, org, name+" "+h.suffix()).Scan(&id); err != nil {
		t.Fatalf("make the group %s: %v", name, err)
	}
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM groups WHERE id = $1`, id) })
	for _, m := range members {
		if _, err := h.super.Exec(ctx, `INSERT INTO group_member (org_id, group_id, user_id) VALUES ($1, $2, $3)`, org, id, m); err != nil {
			t.Fatalf("put somebody in %s: %v", name, err)
		}
	}
	h.settle(t)
	return id
}

// refusedBy says the database refused a write naming the constraint.
func refusedBy(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == constraint
}

// Straight through SQL as exciter_app: grants decide what a person may do, roles add up, a guest never runs a
// project and is reached only by grants naming them or their groups, and only administrators touch grants.
func TestRawSQLHoldsProjectsToTheirGrants(t *testing.T) {
	graKey := projectKey(t, "GRA")
	h := newHarness(t)
	a := h.owner(t, "grants-a")
	ctx := context.Background()
	var project uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO project (org_id, key, name, created_by) VALUES ($1, '`+graKey+`', 'Granted', $2) RETURNING id`,
		a.org, a.user).Scan(&project); err != nil {
		t.Fatal(err)
	}
	plainID := h.addPerson(t, a.org, "member")
	guestID := h.addPerson(t, a.org, "guest")
	plain, guest, owner := h.appConn(t, a.org, plainID), h.appConn(t, a.org, guestID), h.appConn(t, a.org, a.user)
	holds := func(person uuid.UUID, right string) bool {
		t.Helper()
		var ok bool
		if err := owner.QueryRow(ctx, `SELECT perm_project_holds($1, $2, $3)`, project, person, right).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	setGrants := func(rows ...string) {
		t.Helper()
		if _, err := h.super.Exec(ctx, `DELETE FROM project_grant WHERE project_id = $1`, project); err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if _, err := h.super.Exec(ctx, `INSERT INTO project_grant (org_id, project_id, subject_type, user_id, group_id, role) `+row, a.org, project); err != nil {
				t.Fatalf("%s: %v", row, err)
			}
		}
	}

	t.Run("a new project is its maker's to run and everybody's to work in", func(t *testing.T) {
		if n := h.countRows(t, `SELECT count(*) FROM project_grant WHERE project_id = $1 AND subject_type = 'user' AND user_id = $2 AND role = 'admin'`, project, a.user); n != 1 {
			t.Error("the maker holds no admin grant")
		}
		if n := h.countRows(t, `SELECT count(*) FROM project_grant WHERE project_id = $1 AND subject_type = 'everyone' AND role = 'editor'`, project); n != 1 {
			t.Error("everyone holds no editor grant")
		}
		if !holds(plainID, "edit") || holds(plainID, "approve") || holds(guestID, "view") {
			t.Error("everyone's editor grant reaches members and not guests")
		}
	})

	t.Run("roles add up, and a guest holds at most editor", func(t *testing.T) {
		reviewers := h.makeGroup(t, a.org, "Reviewers", plainID, guestID)
		setGrants(
			`SELECT $1, $2, 'everyone', NULL, NULL, 'viewer'`,
			`SELECT $1, $2, 'group', NULL, '`+reviewers.String()+`', 'reviewer'`,
			`SELECT $1, $2, 'user', '`+plainID.String()+`', NULL, 'editor'`,
		)
		for _, right := range []string{"view", "comment", "edit", "approve"} {
			if !holds(plainID, right) {
				t.Errorf("a reviewer through a group and an editor on their own lacks %s", right)
			}
		}
		if holds(plainID, "administer") {
			t.Error("no grant gives the member administer")
		}
		if !holds(guestID, "approve") || holds(guestID, "edit") {
			t.Error("the guest is reached by their group's grant, and not by everyone's")
		}
		admins := h.makeGroup(t, a.org, "Admins", guestID)
		setGrants(`SELECT $1, $2, 'group', NULL, '` + admins.String() + `', 'admin'`)
		if !holds(guestID, "edit") || holds(guestID, "administer") || holds(guestID, "approve") {
			t.Error("an admin grant reaching a guest through a group counts as editor")
		}
		if n := affects(t, guest, `UPDATE project SET name = 'Guest run' WHERE id = $1`, project); n != 0 {
			t.Error("a guest changed the project through a group's admin grant")
		}
	})

	t.Run("a grant of admin makes a member the project's administrator", func(t *testing.T) {
		setGrants(`SELECT $1, $2, 'user', '` + plainID.String() + `', NULL, 'admin'`)
		if n := affects(t, plain, `UPDATE project SET description = 'Run by a member' WHERE id = $1`, project); n != 1 {
			t.Error("a member holding admin could not change the project")
		}
		if _, err := plain.Exec(ctx, `INSERT INTO project_grant (org_id, project_id, subject_type, role) VALUES ($1, $2, 'everyone', 'viewer')`, a.org, project); err != nil {
			t.Errorf("the project's administrator could not grant: %v", err)
		}
	})

	t.Run("only a project's administrators read and change its grants", func(t *testing.T) {
		setGrants(`SELECT $1, $2, 'everyone', NULL, NULL, 'editor'`)
		var n int
		if err := plain.QueryRow(ctx, `SELECT count(*) FROM project_grant WHERE project_id = $1`, project).Scan(&n); err != nil || n != 0 {
			t.Errorf("an editor reads %d grants: %v", n, err)
		}
		if _, err := plain.Exec(ctx, `INSERT INTO project_grant (org_id, project_id, subject_type, user_id, role) VALUES ($1, $2, 'user', $3, 'admin')`,
			a.org, project, plainID); !isPolicyViolation(err) {
			t.Errorf("an editor made themselves the project's administrator: %v", err)
		}
		if n := affects(t, plain, `UPDATE project_grant SET role = 'viewer' WHERE project_id = $1`, project); n != 0 {
			t.Error("an editor changed a grant")
		}
		if n := affects(t, plain, `DELETE FROM project_grant WHERE project_id = $1`, project); n != 0 {
			t.Error("an editor removed a grant")
		}
		outsider := h.appConn(t, a.org, h.addPerson(t, a.org, "guest"))
		if _, err := outsider.Exec(ctx, `INSERT INTO project_grant (org_id, project_id, subject_type, role) VALUES ($1, $2, 'everyone', 'viewer')`, a.org, project); !isPolicyViolation(err) {
			t.Errorf("a guest no grant names granted in the project: %v", err)
		}
	})

	t.Run("the database refuses grants nobody may hold", func(t *testing.T) {
		if _, err := owner.Exec(ctx, `INSERT INTO project_grant (org_id, project_id, subject_type, user_id, role) VALUES ($1, $2, 'user', $3, 'admin')`,
			a.org, project, guestID); !refusedBy(err, "project_grant_guest_not_admin") {
			t.Errorf("a guest was made a project's administrator: %v", err)
		}
		if _, err := owner.Exec(ctx, `UPDATE project_grant SET role = 'admin' WHERE project_id = $1 AND subject_type = 'everyone'`, project); !refusedBy(err, "project_grant_everyone_not_admin") {
			t.Errorf("everyone was made the project's administrator: %v", err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO project_grant (org_id, project_id, subject_type, role) VALUES ($1, $2, 'everyone', 'viewer')`, a.org, project); err == nil {
			t.Error("everyone was granted twice in one project")
		}
	})

	t.Run("an archived project's grants stay as they are", func(t *testing.T) {
		if n := affects(t, owner, `UPDATE project SET archived_at = now() WHERE id = $1`, project); n != 1 {
			t.Fatal("the owner could not archive the project")
		}
		if n := affects(t, owner, `UPDATE project_grant SET role = 'viewer' WHERE project_id = $1`, project); n != 0 {
			t.Error("a grant of an archived project changed")
		}
		if holds(plainID, "edit") || !holds(plainID, "view") {
			t.Error("an archived project is read only to its editors")
		}
	})
}
