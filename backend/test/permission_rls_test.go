//go:build integration

package test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// makeGroup makes a group with members, behind the policies' back.
func (h *harness) makeGroup(t *testing.T, name string, members ...uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO groups (name) VALUES ($1) RETURNING id`, name+" "+h.suffix()).Scan(&id); err != nil {
		t.Fatalf("make the group %s: %v", name, err)
	}
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM groups WHERE id = $1`, id) })
	for _, m := range members {
		if _, err := h.super.Exec(ctx, `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, id, m); err != nil {
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
// project and is reached only by grants in it naming them or their groups, and only administrators touch grants.
func TestRawSQLHoldsProjectsToTheirGrants(t *testing.T) {
	graKey := projectKey(t, "GRA")
	h := newHarness(t)
	h.keepRoles(t)
	a := h.owner(t, "grants-a")
	ctx := context.Background()
	var project uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO project (key, name, created_by) VALUES ('`+graKey+`', 'Granted', $1) RETURNING id`,
		a.user).Scan(&project); err != nil {
		t.Fatal(err)
	}
	plainID := h.addPerson(t, "member")
	guestID := h.addPerson(t, "guest")
	plain, guest, owner := h.appConn(t, plainID), h.appConn(t, guestID), h.appConn(t, a.user)
	holds := func(person uuid.UUID, right string) bool {
		t.Helper()
		var ok bool
		if err := owner.QueryRow(ctx, `SELECT perm_project_holds($1, $2, $3)`, project, person, right).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	// setGrants makes the project's grants the rows given, each a role and a person or a group, behind the policies'
	// back.
	setGrants := func(rows ...[3]any) {
		t.Helper()
		if _, err := h.super.Exec(ctx, `DELETE FROM role_assignment WHERE project_id = $1`, project); err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if _, err := h.super.Exec(ctx, `INSERT INTO role_assignment (project_id, role, user_id, group_id) VALUES ($1, $2, $3, $4)`,
				project, row[0], row[1], row[2]); err != nil {
				t.Fatalf("%v: %v", row, err)
			}
		}
		h.settle(t)
	}
	everyone := h.everyoneGroup(t)

	t.Run("a new project is its maker's to run and every member's to work in", func(t *testing.T) {
		if n := h.countRows(t, `SELECT count(*) FROM role_assignment WHERE project_id = $1 AND user_id = $2 AND role = 'admin'`, project, a.user); n != 1 {
			t.Error("the maker holds no project administrator grant")
		}
		if n := h.countRows(t, `SELECT count(*) FROM role_assignment WHERE project_id = $1 AND group_id = $2 AND role = 'editor'`, project, everyone); n != 1 {
			t.Error("Everyone holds no editor grant")
		}
		if !holds(plainID, "edit") || holds(plainID, "approve") || holds(guestID, "view") {
			t.Error("Everyone's editor grant reaches members and not guests")
		}
	})

	t.Run("roles add up, and a guest a role to run the project reaches works in it as an editor", func(t *testing.T) {
		reviewers := h.makeGroup(t, "Reviewers", plainID, guestID)
		setGrants([3]any{"viewer", nil, everyone}, [3]any{"reviewer", nil, reviewers}, [3]any{"editor", plainID, nil})
		for _, right := range []string{"view", "comment", "edit", "approve"} {
			if !holds(plainID, right) {
				t.Errorf("a reviewer through a group and an editor on their own lacks %s", right)
			}
		}
		if holds(plainID, "administer") {
			t.Error("no grant gives the member administer")
		}
		if !holds(guestID, "approve") || holds(guestID, "edit") {
			t.Error("the guest is reached by their group's grant, and not by Everyone's")
		}
		admins := h.makeGroup(t, "Admins", guestID)
		setGrants([3]any{"admin", nil, admins})
		if !holds(guestID, "edit") || holds(guestID, "administer") || holds(guestID, "approve") {
			t.Error("a project administrator grant reaching a guest through a group counts as editor")
		}
		if n := affects(t, guest, `UPDATE project SET name = 'Guest run' WHERE id = $1`, project); n != 0 {
			t.Error("a guest changed the project through a group's grant")
		}
	})

	t.Run("a grant across the installation reaches every project, and never a guest", func(t *testing.T) {
		setGrants([3]any{"admin", a.user, nil})
		if holds(plainID, "view") {
			t.Fatal("the member views a project nothing grants them")
		}
		for _, who := range []uuid.UUID{plainID, guestID} {
			if _, err := h.super.Exec(ctx, `INSERT INTO role_assignment (role, group_id) VALUES ('reviewer', $1)`, h.makeGroup(t, "Across", who)); err != nil {
				t.Fatal(err)
			}
		}
		h.settle(t)
		if !holds(plainID, "approve") || holds(plainID, "edit") {
			t.Error("a reviewer across the installation does not approve in the project")
		}
		if holds(guestID, "view") {
			t.Error("a grant across the installation reached a guest")
		}
	})

	t.Run("a grant of the project administrator makes a member the project's administrator", func(t *testing.T) {
		setGrants([3]any{"admin", plainID, nil})
		if n := affects(t, plain, `UPDATE project SET description = 'Run by a member' WHERE id = $1`, project); n != 1 {
			t.Error("a member holding project administrator could not change the project")
		}
		if _, err := plain.Exec(ctx, `INSERT INTO role_assignment (project_id, role, group_id) VALUES ($1, 'viewer', $2)`, project, everyone); err != nil {
			t.Errorf("the project's administrator could not grant: %v", err)
		}
		if _, err := plain.Exec(ctx, `INSERT INTO role_assignment (role, user_id) VALUES ('viewer', $1)`, plainID); !isPolicyViolation(err) {
			t.Errorf("a project's administrator granted across the installation: %v", err)
		}
	})

	t.Run("only a project's administrators read and change its grants", func(t *testing.T) {
		setGrants([3]any{"editor", nil, everyone})
		var n int
		if err := plain.QueryRow(ctx, `SELECT count(*) FROM role_assignment WHERE project_id = $1`, project).Scan(&n); err != nil || n != 0 {
			t.Errorf("an editor reads %d grants: %v", n, err)
		}
		if _, err := plain.Exec(ctx, `INSERT INTO role_assignment (project_id, role, user_id) VALUES ($1, 'admin', $2)`,
			project, plainID); !isPolicyViolation(err) {
			t.Errorf("an editor made themselves the project's administrator: %v", err)
		}
		if n := affects(t, plain, `UPDATE role_assignment SET role = 'viewer' WHERE project_id = $1`, project); n != 0 {
			t.Error("an editor changed a grant")
		}
		if n := affects(t, plain, `DELETE FROM role_assignment WHERE project_id = $1`, project); n != 0 {
			t.Error("an editor removed a grant")
		}
		outsider := h.appConn(t, h.addPerson(t, "guest"))
		if _, err := outsider.Exec(ctx, `INSERT INTO role_assignment (project_id, role, group_id) VALUES ($1, 'viewer', $2)`, project, everyone); !isPolicyViolation(err) {
			t.Errorf("a guest no grant names granted in the project: %v", err)
		}
	})

	t.Run("the database refuses grants nobody may hold", func(t *testing.T) {
		if _, err := owner.Exec(ctx, `INSERT INTO role_assignment (project_id, role, user_id) VALUES ($1, 'admin', $2)`,
			project, guestID); !refusedBy(err, "role_assignment_guest_not_administrator") {
			t.Errorf("a guest was made a project's administrator: %v", err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO role_assignment (role, user_id) VALUES ('viewer', $1)`, guestID); !refusedBy(err, "role_assignment_guest_in_a_project") {
			t.Errorf("a guest was granted a role across the installation: %v", err)
		}
		if _, err := owner.Exec(ctx, `UPDATE role_assignment SET role = 'admin' WHERE project_id = $1 AND group_id = $2`, project, everyone); !refusedBy(err, "role_assignment_everyone_administers") {
			t.Errorf("Everyone was made the project's administrator: %v", err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO role_assignment (role, group_id) VALUES ('administrator', $1)`, everyone); !refusedBy(err, "role_assignment_everyone_administers") {
			t.Errorf("Everyone was made an administrator of the installation: %v", err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO role_assignment (project_id, role, user_id) VALUES ($1, 'administrator', $2)`, project, plainID); !refusedBy(err, "role_assignment_installation_wide") {
			t.Errorf("the administrator of the installation was granted in one project: %v", err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO role_assignment (project_id, role, group_id) VALUES ($1, 'editor', $2)`, project, everyone); err == nil {
			t.Error("Everyone was granted the same role twice in one project")
		}
	})

	t.Run("an archived project's grants stay as they are", func(t *testing.T) {
		if n := affects(t, owner, `UPDATE project SET archived_at = now() WHERE id = $1`, project); n != 1 {
			t.Fatal("the owner could not archive the project")
		}
		if n := affects(t, owner, `UPDATE role_assignment SET role = 'viewer' WHERE project_id = $1`, project); n != 0 {
			t.Error("a grant of an archived project changed")
		}
		if holds(plainID, "edit") || !holds(plainID, "view") {
			t.Error("an archived project is read only to its editors")
		}
	})
}

// Straight through SQL as exciter_app: every member reads the roles, only an administrator changes them or grants
// across the installation, a built-in role and Everyone stay as they are, and a role or a renamed one reaches the
// rules at once.
func TestRawSQLKeepsRolesAndGrantsToTheAdministrators(t *testing.T) {
	roleKey := "probe_" + strings.ReplaceAll(h0suffix(), "-", "")
	h := newHarness(t)
	h.keepRoles(t)
	a := h.owner(t, "roles-a")
	ctx := context.Background()
	plainID, guestID := h.addPerson(t, "member"), h.addPerson(t, "guest")
	plain, owner, unscoped := h.appConn(t, plainID), h.appConn(t, a.user), h.appConnUnscoped(t)
	everyone := h.everyoneGroup(t)

	t.Run("a plain member reads the roles and changes none", func(t *testing.T) {
		var n int
		if err := plain.QueryRow(ctx, `SELECT count(*) FROM role WHERE builtin`).Scan(&n); err != nil || n != 5 {
			t.Errorf("a member reads %d built-in roles (%v), want 5", n, err)
		}
		if err := plain.QueryRow(ctx, `SELECT count(*) FROM role_assignment WHERE project_id IS NULL`).Scan(&n); err != nil || n != 0 {
			t.Errorf("a member reads %d grants across the installation (%v)", n, err)
		}
		if err := unscoped.QueryRow(ctx, `SELECT count(*) FROM role_assignment`).Scan(&n); err != nil || n != 0 {
			t.Errorf("an unscoped connection reads %d grants (%v)", n, err)
		}
		if _, err := plain.Exec(ctx, `INSERT INTO role (key, name, permissions) VALUES ($1, 'Mine', '{view}')`, roleKey); !isPolicyViolation(err) {
			t.Errorf("a member added a role: %v", err)
		}
		touchesNothing(t, plain, "a member giving the viewer edit", `UPDATE role SET permissions = '{view,edit}' WHERE key = 'viewer'`)
		touchesNothing(t, plain, "a member deleting a role", `DELETE FROM role WHERE key = 'reviewer'`)
		if _, err := plain.Exec(ctx, `INSERT INTO role_assignment (role, user_id) VALUES ('administrator', $1)`, plainID); !isPolicyViolation(err) {
			t.Errorf("a member made themselves an administrator: %v", err)
		}
	})

	t.Run("an administrator adds a role, and a grant of it reaches the rules at once", func(t *testing.T) {
		if _, err := owner.Exec(ctx, `INSERT INTO role (key, name, permissions) VALUES ($1, 'Probe', '{view,approve}')`, roleKey); err != nil {
			t.Fatalf("an administrator could not add a role: %v", err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO role_assignment (role, user_id) VALUES ($1, $2)`, roleKey, plainID); err != nil {
			t.Fatalf("an administrator could not grant it across the installation: %v", err)
		}
		var approves, edits bool
		var roles []string
		project := h.anyProject(t)
		if err := owner.QueryRow(ctx, `SELECT perm_project_holds($1, $2, 'approve'), perm_project_holds($1, $2, 'edit'), perm_project_roles($1, $2)`,
			project, plainID).Scan(&approves, &edits, &roles); err != nil {
			t.Fatal(err)
		}
		if !approves || !slices.Contains(roles, roleKey) {
			t.Errorf("a grant of the new role does not reach the project: approves %v, roles %v", approves, roles)
		}
		if _, err := owner.Exec(ctx, `UPDATE role SET name = 'Signer', permissions = '{view}' WHERE key = $1`, roleKey); err != nil {
			t.Fatal(err)
		}
		if err := owner.QueryRow(ctx, `SELECT perm_project_holds($1, $2, 'approve')`, project, plainID).Scan(&approves); err != nil || approves {
			t.Errorf("the renamed role still approves after approve was taken from it (%v)", err)
		}
	})

	t.Run("built-in roles and Everyone stay as they are", func(t *testing.T) {
		for what, try := range map[string]struct {
			sql        string
			constraint string
		}{
			"deleting a built-in role":                    {`DELETE FROM role WHERE key = 'viewer'`, "role_builtin_stays"},
			"changing a built-in role's key":              {`UPDATE role SET key = 'watcher' WHERE key = 'viewer'`, "role_key_fixed"},
			"taking administering from the administrator": {`UPDATE role SET permissions = '{view}' WHERE key = 'administrator'`, "role_administrator_administers"},
			"renaming Everyone":                           {`UPDATE groups SET name = 'All of us' WHERE builtin = 'everyone'`, "groups_builtin_fixed"},
			"deleting Everyone":                           {`DELETE FROM groups WHERE builtin = 'everyone'`, "groups_builtin_fixed"},
		} {
			if _, err := owner.Exec(ctx, try.sql); !refusedBy(err, try.constraint) {
				t.Errorf("%s = %v, want refused by %s", what, err, try.constraint)
			}
		}
		if _, err := owner.Exec(ctx, `INSERT INTO role_assignment (role, group_id) VALUES ($1, $2)`, roleKey, everyone); err != nil {
			t.Fatalf("an administrator could not grant Everyone the probe role: %v", err)
		}
		if _, err := owner.Exec(ctx, `UPDATE role SET permissions = '{view,administer}' WHERE key = $1`, roleKey); !refusedBy(err, "role_assignment_everyone_administers") {
			t.Errorf("the role Everyone holds came to administer: %v", err)
		}
		if _, err := owner.Exec(ctx, `DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`, everyone, plainID); !refusedBy(err, "group_member_keeps_everyone") {
			t.Errorf("a member was taken out of Everyone: %v", err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, everyone, guestID); !refusedBy(err, "group_member_everyone_members") {
			t.Errorf("a guest was put in Everyone: %v", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM group_member WHERE group_id = $1 AND user_id = ANY($2)`, everyone, []uuid.UUID{plainID, a.user, guestID}); n != 2 {
			t.Errorf("Everyone holds %d of the two members and the guest, want the two members", n)
		}
	})
}

// h0suffix is a short unique word for names made before the harness is.
func h0suffix() string { return uuid.NewString()[:8] }

// anyProject is a project of the installation's, made for the test.
func (h *harness) anyProject(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `INSERT INTO project (key, name) VALUES ($1, 'Probed') RETURNING id`,
		projectKey(t, "PRB")).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
