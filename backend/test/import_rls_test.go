//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

const insertImport = `INSERT INTO import_job (org_id, project_id, created_by, file_name, headers, rows, total)
	VALUES ($1, $2, $3, 'file.csv', '["title"]', '[{"line":2,"cells":["Pay"]}]', 1) RETURNING id`

// Straight through SQL as exciter_app: an import is made by who administers its project and seen by them and
// its maker, its file never changes, it only moves forward, and only it sets the status of what it just made.
func TestRawSQLGuardsImports(t *testing.T) {
	w := newTraceWorld(t, "imports")
	ctx := context.Background()
	admin := w.h.appConn(t, w.a.user)
	var id uuid.UUID
	if err := admin.QueryRow(ctx, insertImport, w.a.org, w.here, w.a.user).Scan(&id); err != nil {
		t.Fatalf("an administrator uploads a file: %v", err)
	}

	t.Run("made by the project's administrators, in their own name, and seen by them", func(t *testing.T) {
		var other uuid.UUID
		if err := w.editor.QueryRow(ctx, insertImport, w.a.org, w.here, w.editorID).Scan(&other); err == nil {
			t.Error("an editor who does not administer the project made an import")
		}
		if err := admin.QueryRow(ctx, insertImport, w.a.org, w.here, w.editorID).Scan(&other); err == nil {
			t.Error("an import was made in somebody else's name")
		}
		if seesRows(t, w.editor, `SELECT count(*) FROM import_job WHERE id = $1`, id) != 0 || seesRows(t, w.stranger, `SELECT count(*) FROM import_job`) != 0 {
			t.Error("somebody who neither made the import nor runs the project sees it")
		}
		if tag, err := w.editor.Exec(ctx, `UPDATE import_job SET state = 'queued' WHERE id = $1`, id); err != nil || tag.RowsAffected() != 0 {
			t.Errorf("an editor started somebody else's import: %v", err)
		}
	})

	t.Run("its file never changes, its mapping only until it starts, and it moves only forward", func(t *testing.T) {
		if _, err := admin.Exec(ctx, `UPDATE import_job SET rows = '[]', total = 0 WHERE id = $1`, id); !isCheckViolation(err) {
			t.Errorf("an import's file was changed: %v", err)
		}
		if _, err := admin.Exec(ctx, `UPDATE import_job SET mapping = '{"title":[0]}' WHERE id = $1`, id); err != nil {
			t.Fatalf("the mapping of an uploaded import: %v", err)
		}
		if _, err := admin.Exec(ctx, `UPDATE import_job SET state = 'done' WHERE id = $1`, id); !isCheckViolation(err) {
			t.Errorf("an import skipped the worker: %v", err)
		}
		if _, err := admin.Exec(ctx, `UPDATE import_job SET state = 'queued' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `UPDATE import_job SET mapping = '{}' WHERE id = $1`, id); !isCheckViolation(err) {
			t.Errorf("the mapping of a started import changed: %v", err)
		}
		if _, err := admin.Exec(ctx, `UPDATE import_job SET state = 'uploaded' WHERE id = $1`, id); !isCheckViolation(err) {
			t.Errorf("an import went back: %v", err)
		}
	})

	t.Run("only an import sets the status of what it just made, to one of its workflow", func(t *testing.T) {
		var approved, elsewhere uuid.UUID
		if err := w.h.super.QueryRow(ctx, `SELECT id FROM req_status WHERE project_id = $1 AND category = 'approved' LIMIT 1`, w.here).Scan(&approved); err != nil {
			t.Fatal(err)
		}
		if err := w.h.super.QueryRow(ctx, `SELECT id FROM req_status WHERE project_id = $1 LIMIT 1`, w.other).Scan(&elsewhere); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `SELECT requirement_import_status($1, $2)`, w.r1, approved); err == nil {
			t.Error("the status of a requirement made earlier was set")
		}
		write := `INSERT INTO requirement (org_id, project_id, kind, title, statement, normative_hash, created_by)
			VALUES ($1, $2, 'requirement', 'Imported', '{"type":"doc"}', 'h', current_actor_id()) RETURNING id`
		for name, tc := range map[string]struct {
			admin  bool
			status uuid.UUID
			ok     bool
		}{
			"an administrator, to a status of its workflow": {true, approved, true},
			"to another project's status":                   {true, elsewhere, false},
			"an editor who does not run the project":        {false, approved, false},
		} {
			conn := w.editor
			if tc.admin {
				conn = admin
			}
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var made uuid.UUID
			if err := tx.QueryRow(ctx, write, w.a.org, w.here).Scan(&made); err != nil {
				t.Fatalf("%s: write: %v", name, err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO requirement_revision (org_id, requirement_id, number, title, statement, hash, created_by)
				VALUES ($1, $2, 1, 'Imported', '{"type":"doc"}', 'h', current_actor_id())`, w.a.org, made); err != nil {
				t.Fatalf("%s: revision 1: %v", name, err)
			}
			_, err = tx.Exec(ctx, `SELECT requirement_import_status($1, $2)`, made, tc.status)
			_ = tx.Rollback(ctx)
			if (err == nil) != tc.ok {
				t.Errorf("%s: %v", name, err)
			}
		}
	})
}
