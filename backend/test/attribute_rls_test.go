//go:build integration

package test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Straight through SQL as exciter_app: only a project's administrators shape its types and attributes, a
// requirement names only its own project's type and attributes, a kind never changes, and a used type stays.
func TestRawSQLKeepsTypesAndAttributesToTheirProject(t *testing.T) {
	homeKey := projectKey(t, "HOME")
	awayKey := projectKey(t, "AWAY")
	h := newHarness(t)
	a := h.owner(t, "attr-wall")
	ctx := context.Background()
	var home, other, homeType, otherAttr uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO project (org_id, key, name, created_by) VALUES ($1, '`+homeKey+`', 'Home', $2) RETURNING id`, a.org, a.user).Scan(&home); err != nil {
		t.Fatal(err)
	}
	if err := h.super.QueryRow(ctx, `INSERT INTO project (org_id, key, name, created_by) VALUES ($1, '`+awayKey+`', 'Away', $2) RETURNING id`, a.org, a.user).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if err := h.super.QueryRow(ctx, `SELECT id FROM requirement_type WHERE project_id = $1`, home).Scan(&homeType); err != nil {
		t.Fatalf("a new project has no type: %v", err)
	}
	if err := h.super.QueryRow(ctx, `INSERT INTO attribute_def (org_id, project_id, name, kind, position) VALUES ($1, $2, 'Elsewhere', 'text', 0) RETURNING id`,
		a.org, other).Scan(&otherAttr); err != nil {
		t.Fatal(err)
	}
	editor, owner := h.appConn(t, h.addPerson(t, a.org, "member")), h.appConn(t, a.user)
	insert := `INSERT INTO requirement (org_id, project_id, title, statement, normative_hash, type_id, attributes, created_by)
		VALUES ($1, $2, 'Walled', '{"type":"doc"}', 'h', $3, $4, current_actor_id())`

	t.Run("only administrators shape a project's types and attributes", func(t *testing.T) {
		if _, err := editor.Exec(ctx, `INSERT INTO requirement_type (org_id, project_id, name, icon, colour, position) VALUES ($1, $2, 'Mine', 'x', '#000000', 1)`,
			a.org, home); !isPolicyViolation(err) {
			t.Errorf("an editor made a type: %v", err)
		}
		if n := affects(t, editor, `UPDATE requirement_type SET name = 'Renamed' WHERE id = $1`, homeType); n != 0 {
			t.Error("an editor renamed a type")
		}
		var n int
		if err := editor.QueryRow(ctx, `SELECT count(*) FROM requirement_type WHERE project_id = $1`, home).Scan(&n); err != nil || n != 1 {
			t.Errorf("an editor reads %d types: %v", n, err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO attribute_def (org_id, project_id, name, kind, position) VALUES ($1, $2, 'Notes', 'text', 0)`, a.org, home); err != nil {
			t.Errorf("the owner could not define an attribute: %v", err)
		}
	})

	t.Run("a requirement names only its own project's type and attributes", func(t *testing.T) {
		var awayType uuid.UUID
		if err := h.super.QueryRow(ctx, `SELECT id FROM requirement_type WHERE project_id = $1`, other).Scan(&awayType); err != nil {
			t.Fatal(err)
		}
		if _, err := editor.Exec(ctx, insert, a.org, home, awayType, `{}`); !refusedBy(err, "requirement_type_same_project") {
			t.Errorf("a requirement took another project's type: %v", err)
		}
		if _, err := editor.Exec(ctx, insert, a.org, home, homeType, `{"`+otherAttr.String()+`":"x"}`); !refusedBy(err, "requirement_attributes_same_project") {
			t.Errorf("a requirement took another project's attribute: %v", err)
		}
		if _, err := editor.Exec(ctx, insert, a.org, home, homeType, `{}`); err != nil {
			t.Fatalf("an editor could not write a typed requirement: %v", err)
		}
	})

	t.Run("a kind never changes, and a type a requirement has cannot go", func(t *testing.T) {
		if _, err := owner.Exec(ctx, `UPDATE attribute_def SET kind = 'number' WHERE project_id = $1`, home); !refusedBy(err, "attribute_def_kind_fixed") {
			t.Errorf("an attribute changed its kind: %v", err)
		}
		_, err := owner.Exec(ctx, `DELETE FROM requirement_type WHERE id = $1`, homeType)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
			t.Errorf("a type a requirement has was deleted: %v", err)
		}
	})

	t.Run("an archived project's types and attributes stay as they are", func(t *testing.T) {
		if n := affects(t, owner, `UPDATE project SET archived_at = now() WHERE id = $1`, home); n != 1 {
			t.Fatal("the owner could not archive the project")
		}
		if n := affects(t, owner, `UPDATE requirement_type SET name = 'Archived' WHERE id = $1`, homeType); n != 0 {
			t.Error("a type of an archived project changed")
		}
	})
}
