//go:build integration

package test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// isKeyTaken says the database refused a key another project has or had.
func isKeyTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Straight through SQL as exciter_app: guests no grant names see no project, only administrators make, change
// and, once archived, delete one, and no key, current or former, names two projects.
func TestRawSQLKeepsProjectsToTheirPeople(t *testing.T) {
	sysKey := projectKey(t, "SYS")
	sysxKey := projectKey(t, "SYSX")
	plantKey := projectKey(t, "PLNT")
	mineKey := projectKey(t, "MINE")
	forgeKey := projectKey(t, "FRGE")
	otherKey := projectKey(t, "OTHR")
	h := newHarness(t)
	a := h.owner(t, "project-wall-a")
	ctx := context.Background()
	var projectID uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO project (org_id, key, name, created_by) VALUES ($1, '`+sysKey+`', 'System', $2) RETURNING id`,
		a.org, a.user).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	strangerID := h.addPerson(t, a.org, "guest")
	stranger := h.appConn(t, strangerID)
	plain := h.appConn(t, h.addPerson(t, a.org, "member"))
	guest := h.appConn(t, h.addPerson(t, a.org, "guest"))
	owner := h.appConn(t, a.user)

	t.Run("a guest, another guest or nobody sees nothing", func(t *testing.T) {
		unscoped := h.appConnUnscoped(t)
		for name, conn := range map[string]*pgx.Conn{"unscoped": unscoped, "another guest": stranger, "a guest": guest} {
			var n int
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM project WHERE id = $1`, projectID).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Errorf("%s sees the project", name)
			}
		}
		if n := affects(t, stranger, `UPDATE project SET name = 'Taken' WHERE id = $1`, projectID); n != 0 {
			t.Error("a guest renamed the project")
		}
		if _, err := stranger.Exec(ctx, `INSERT INTO project (org_id, key, name, created_by) VALUES ($1, '`+plantKey+`', 'Planted', $2)`, a.org, strangerID); !isPolicyViolation(err) {
			t.Errorf("a guest planted a project: %v", err)
		}
	})

	t.Run("members see it, only administrators make and change it", func(t *testing.T) {
		var n int
		if err := plain.QueryRow(ctx, `SELECT count(*) FROM project WHERE id = $1`, projectID).Scan(&n); err != nil || n != 1 {
			t.Errorf("a member sees %d projects: %v", n, err)
		}
		if n := affects(t, plain, `UPDATE project SET name = 'Mine' WHERE id = $1`, projectID); n != 0 {
			t.Error("a member renamed the project")
		}
		if _, err := plain.Exec(ctx, `INSERT INTO project (org_id, key, name, created_by) VALUES ($1, '`+mineKey+`', 'Mine', current_actor_id())`, a.org); !isPolicyViolation(err) {
			t.Errorf("a member made a project: %v", err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO project (org_id, key, name, created_by) VALUES ($1, '`+forgeKey+`', 'Forged', $2)`, a.org, strangerID); !isPolicyViolation(err) {
			t.Errorf("a project was made in somebody else's name: %v", err)
		}
		if n := affects(t, owner, `UPDATE project SET description = 'Tidied' WHERE id = $1`, projectID); n != 1 {
			t.Error("the organization's owner could not change the project")
		}
	})

	t.Run("a project goes only once archived", func(t *testing.T) {
		if n := affects(t, owner, `DELETE FROM project WHERE id = $1`, projectID); n != 0 {
			t.Fatal("a project that was not archived was deleted")
		}
		if n := affects(t, owner, `UPDATE project SET archived_at = now() WHERE id = $1`, projectID); n != 1 {
			t.Fatal("the owner could not archive the project")
		}
		if n := affects(t, plain, `DELETE FROM project WHERE id = $1`, projectID); n != 0 {
			t.Error("a member deleted an archived project")
		}
		if n := affects(t, owner, `UPDATE project SET archived_at = NULL WHERE id = $1`, projectID); n != 1 {
			t.Error("the owner could not unarchive the project")
		}
	})

	t.Run("a key, current or former, names one project", func(t *testing.T) {
		if n := affects(t, owner, `UPDATE project SET key = '`+sysxKey+`' WHERE id = $1`, projectID); n != 1 {
			t.Fatal("the owner could not change the key")
		}
		var alias string
		if err := owner.QueryRow(ctx, `SELECT key FROM project_key_alias WHERE project_id = $1`, projectID).Scan(&alias); err != nil || alias != sysKey {
			t.Fatalf("the old key did not stay behind: %q %v", alias, err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO project (org_id, key, name, created_by) VALUES ($1, '`+sysKey+`', 'Squatter', current_actor_id())`, a.org); !isKeyTaken(err) {
			t.Errorf("a new project took a former key: %v", err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO project_key_alias (org_id, key, project_id) VALUES ($1, 'ANY', $2)`, a.org, projectID); err == nil {
			t.Error("the app role wrote an alias by hand")
		}
		if _, err := h.super.Exec(ctx, `INSERT INTO project_key_alias (org_id, key, project_id) VALUES ($1, '`+sysxKey+`', $2)`, a.org, projectID); !isKeyTaken(err) {
			t.Errorf("an alias took a current key: %v", err)
		}
		var otherID uuid.UUID
		if err := h.super.QueryRow(ctx, `INSERT INTO project (org_id, key, name) VALUES ($1, '`+otherKey+`', 'Other') RETURNING id`, a.org).Scan(&otherID); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.Exec(ctx, `UPDATE project SET key = '`+sysKey+`' WHERE id = $1`, otherID); !isKeyTaken(err) {
			t.Errorf("another project took a former key: %v", err)
		}
		if n := affects(t, owner, `UPDATE project SET key = '`+sysKey+`' WHERE id = $1`, projectID); n != 1 {
			t.Fatal("a project could not take its own former key back")
		}
		var aliases int
		if err := owner.QueryRow(ctx, `SELECT count(*) FROM project_key_alias WHERE project_id = $1 AND key = '`+sysKey+`'`, projectID).Scan(&aliases); err != nil || aliases != 0 {
			t.Errorf("a key taken back stays an alias: %d %v", aliases, err)
		}
	})
}
