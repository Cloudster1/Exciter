//go:build integration

package test

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The migration that moves access to roles and grants, and the one before it.
const (
	beforeRolesAndGrants = 1520
	rolesAndGrants       = 1540
)

// accessBefore is an installation of the release before roles and grants, with every kind of access it gave:
// owners and administrators by hand and by a provider group, members by hand and by a provider group, guests by
// name and through a group, every project role by name, through a group and to everyone, and an archived project.
const accessBefore = `
INSERT INTO app_user (email, name, role, role_source) VALUES
    ('owner@upgrade.test', 'Owner', 'owner', 'manual'),
    ('admin@upgrade.test', 'Admin', 'admin', 'manual'),
    ('padmin@upgrade.test', 'Provider admin', 'admin', 'oidc'),
    ('lone@upgrade.test', 'Lone provider admin', 'admin', 'oidc'),
    ('member@upgrade.test', 'Member', 'member', 'manual'),
    ('pmember@upgrade.test', 'Provider member', 'member', 'oidc'),
    ('guest@upgrade.test', 'Guest', 'guest', 'manual'),
    ('gguest@upgrade.test', 'Group guest', 'guest', 'manual'),
    ('team@upgrade.test', 'Teammate', 'member', 'manual');
INSERT INTO app_user (email, name) VALUES ('waiting@upgrade.test', 'Waiting');
INSERT INTO groups (name, source, external_ref) VALUES ('admins', 'oidc', 'admins');
INSERT INTO groups (name) VALUES ('Team'), ('Everyone');
INSERT INTO group_member (group_id, user_id)
SELECT g.id, u.id FROM groups g, app_user u WHERE g.name = 'admins' AND u.email = 'padmin@upgrade.test';
INSERT INTO group_member (group_id, user_id)
SELECT g.id, u.id FROM groups g, app_user u WHERE g.name = 'Team' AND u.email IN ('team@upgrade.test', 'gguest@upgrade.test');
INSERT INTO group_member (group_id, user_id)
SELECT g.id, u.id FROM groups g, app_user u WHERE g.name = 'Everyone' AND u.email = 'member@upgrade.test';
INSERT INTO oidc_provider (issuer, client_id) VALUES ('https://id.upgrade.test', 'exciter');
INSERT INTO oidc_group_role (group_ref, role) VALUES ('admins', 'admin'), ('staff', 'member'), ('lone-admins', 'admin');
INSERT INTO project (key, name, created_by) SELECT 'ALPHA', 'Alpha', id FROM app_user WHERE email = 'member@upgrade.test';
INSERT INTO project (key, name, created_by) SELECT 'BETA', 'Beta', id FROM app_user WHERE email = 'owner@upgrade.test';
INSERT INTO project (key, name) VALUES ('GAMMA', 'Gamma');
UPDATE project SET archived_at = now() WHERE key = 'GAMMA';
DELETE FROM project_grant WHERE project_id = (SELECT id FROM project WHERE key = 'BETA');
INSERT INTO project_grant (project_id, subject_type, user_id, role)
SELECT p.id, 'user', u.id, 'admin' FROM project p, app_user u WHERE p.key = 'BETA' AND u.email = 'admin@upgrade.test';
INSERT INTO project_grant (project_id, subject_type, role) SELECT id, 'everyone', 'viewer' FROM project WHERE key = 'BETA';
INSERT INTO project_grant (project_id, subject_type, group_id, role)
SELECT p.id, 'group', g.id, 'admin' FROM project p, groups g WHERE p.key = 'BETA' AND g.name = 'Team';
INSERT INTO project_grant (project_id, subject_type, group_id, role)
SELECT p.id, 'group', g.id, 'reviewer' FROM project p, groups g WHERE p.key = 'ALPHA' AND g.name = 'Everyone';
INSERT INTO project_grant (project_id, subject_type, user_id, role)
SELECT p.id, 'user', u.id, 'reviewer' FROM project p, app_user u WHERE p.key = 'BETA' AND u.email = 'pmember@upgrade.test';
INSERT INTO project_grant (project_id, subject_type, user_id, role)
SELECT p.id, 'user', u.id, 'editor' FROM project p, app_user u WHERE p.key = 'ALPHA' AND u.email = 'guest@upgrade.test';
INSERT INTO project_grant (project_id, subject_type, user_id, role)
SELECT p.id, 'user', u.id, 'viewer' FROM project p, app_user u WHERE p.key = 'GAMMA' AND u.email = 'guest@upgrade.test';
`

// accessMatrix is everybody's every right in every project as perm_project_holds answers it, the project roles
// the workflow conditions read, and who administers the installation, one line each.
func accessMatrix(t *testing.T, super *pgxpool.Pool, administers string) []string {
	t.Helper()
	rows, err := super.Query(context.Background(), `
		SELECT u.email || ' ' || p.key || ' ' || r.right_name || ' ' || perm_project_holds(p.id, u.id, r.right_name)
		FROM app_user u, project p, unnest(ARRAY['view', 'comment', 'edit', 'approve', 'administer']) AS r (right_name)
		UNION ALL
		SELECT u.email || ' ' || p.key || ' roles ' || coalesce(array_to_string(ARRAY(
		           SELECT x FROM unnest(perm_project_roles(p.id, u.id)) x ORDER BY x), ','), '')
		FROM app_user u, project p
		UNION ALL
		SELECT u.email || ' standing ' || coalesce(u.role, 'none') || ' administers ' || (`+administers+`)
		FROM app_user u
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// An installation upgraded to roles and grants lets everybody do exactly what they could before, in every project,
// archived or not, and administers through the same people; stepping back gives the grants of before again.
func TestTheUpgradeToRolesAndGrantsKeepsEverybodysAccess(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	provider, super, _ := scratchDatabase(t, h, "exciter_roles_and_grants_test")
	if _, err := provider.UpTo(ctx, beforeRolesAndGrants); err != nil {
		t.Fatalf("migrate to %d: %v", beforeRolesAndGrants, err)
	}
	if _, err := super.Exec(ctx, accessBefore); err != nil {
		t.Fatalf("fill the installation of before: %v", err)
	}
	// Standing and administering were one before; owners and admins administered.
	before := accessMatrix(t, super, `CASE WHEN u.role IN ('owner', 'admin') THEN 'yes' ELSE 'no' END`)
	for i, line := range before {
		before[i] = replaceStanding(line)
	}
	var grantsBefore []string
	if err := pgx.BeginFunc(ctx, super, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT p.key || ' ' || g.subject_type || ' ' || coalesce(u.email, gr.name, '') || ' ' || g.role
			FROM project_grant g JOIN project p ON p.id = g.project_id
			LEFT JOIN app_user u ON u.id = g.user_id LEFT JOIN groups gr ON gr.id = g.group_id ORDER BY 1`)
		if err != nil {
			return err
		}
		grantsBefore, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := provider.UpTo(ctx, rolesAndGrants); err != nil {
		t.Fatalf("upgrade to roles and grants: %v", err)
	}
	after := accessMatrix(t, super, `CASE WHEN perm_is_administrator(u.id) THEN 'yes' ELSE 'no' END`)
	if !slices.Equal(before, after) {
		t.Errorf("the upgrade changed what somebody may do:\n%s", lineDifference(before, after))
	}

	t.Run("a provider group mapped to a role holds its grant, and still lets people in", func(t *testing.T) {
		got := map[string]string{}
		rows, err := super.Query(ctx, `SELECT g.external_ref, a.role FROM role_assignment a JOIN groups g ON g.id = a.group_id
			WHERE a.project_id IS NULL AND g.external_ref IS NOT NULL`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var ref, role string
			if err := rows.Scan(&ref, &role); err != nil {
				t.Fatal(err)
			}
			got[ref] = role
		}
		rows.Close()
		if want := map[string]string{"admins": "administrator", "lone-admins": "administrator", "staff": "member"}; !maps.Equal(got, want) {
			t.Errorf("the provider groups hold %v, want %v", got, want)
		}
	})

	t.Run("Everyone is the members, and the group made by hand under the name keeps its own", func(t *testing.T) {
		var everyone, handMade []string
		if err := super.QueryRow(ctx, `
			SELECT ARRAY(SELECT u.email FROM group_member m JOIN app_user u ON u.id = m.user_id JOIN groups g ON g.id = m.group_id
			             WHERE g.builtin = 'everyone' ORDER BY 1),
			       ARRAY(SELECT u.email FROM group_member m JOIN app_user u ON u.id = m.user_id JOIN groups g ON g.id = m.group_id
			             WHERE g.name = 'Everyone (made by hand)' ORDER BY 1)`).Scan(&everyone, &handMade); err != nil {
			t.Fatal(err)
		}
		members := []string{"admin@upgrade.test", "lone@upgrade.test", "member@upgrade.test", "owner@upgrade.test",
			"padmin@upgrade.test", "pmember@upgrade.test", "team@upgrade.test"}
		if !slices.Equal(everyone, members) || !slices.Equal(handMade, []string{"member@upgrade.test"}) {
			t.Errorf("Everyone holds %v and the hand-made group %v", everyone, handMade)
		}
	})

	t.Run("stepping back gives the grants of before, and the upgrade runs again", func(t *testing.T) {
		if _, err := provider.DownTo(ctx, beforeRolesAndGrants); err != nil {
			t.Fatalf("step back: %v", err)
		}
		rows, err := super.Query(ctx, `SELECT p.key || ' ' || g.subject_type || ' ' || coalesce(u.email, gr.name, '') || ' ' || g.role
			FROM project_grant g JOIN project p ON p.id = g.project_id
			LEFT JOIN app_user u ON u.id = g.user_id LEFT JOIN groups gr ON gr.id = g.group_id ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		grantsBack, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(grantsBefore, grantsBack) {
			t.Errorf("stepping back changed the grants:\n%s", lineDifference(grantsBefore, grantsBack))
		}
		back := accessMatrix(t, super, `CASE WHEN u.role IN ('owner', 'admin') THEN 'yes' ELSE 'no' END`)
		for i, line := range back {
			back[i] = replaceStanding(line)
		}
		if !slices.Equal(before, back) {
			t.Errorf("stepping back changed what somebody may do:\n%s", lineDifference(before, back))
		}
		if _, err := provider.UpTo(ctx, rolesAndGrants); err != nil {
			t.Fatalf("upgrade again: %v", err)
		}
	})
}

// replaceStanding reads an owner's and an administrator's standing of before as the member they are now.
func replaceStanding(line string) string {
	for _, was := range []string{" standing owner ", " standing admin "} {
		if i := strings.Index(line, was); i >= 0 {
			return line[:i] + " standing member " + line[i+len(was):]
		}
	}
	return line
}

// lineDifference names the lines one listing has and the other lacks.
func lineDifference(a, b []string) string {
	out := ""
	for _, l := range a {
		if !slices.Contains(b, l) {
			out += "  gone: " + l + "\n"
		}
	}
	for _, l := range b {
		if !slices.Contains(a, l) {
			out += "  new:  " + l + "\n"
		}
	}
	return out
}
