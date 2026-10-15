//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

const addReqIFFile = `INSERT INTO reqif_import_object (import_id, path, content_type, data) VALUES ($1, $2, 'image/png', '\x89')`

const insertReqIF = `INSERT INTO reqif_import (project_id, created_by, file_name, format, content, total)
	VALUES ($1, $2, 'file.reqif', 'reqif', '{"objects":[]}', 0) RETURNING id`

// Straight through SQL as exciter_app: a ReqIF import and its files are its project administrators', fixed once
// uploaded and moving only forward, and only they tie an identifier, and only to something of their project.
func TestRawSQLGuardsReqIFImports(t *testing.T) {
	w := newTraceWorld(t, "reqif")
	ctx := context.Background()
	admin := w.h.appConn(t, w.a.user)
	var id uuid.UUID
	if err := admin.QueryRow(ctx, insertReqIF, w.here, w.a.user).Scan(&id); err != nil {
		t.Fatalf("an administrator uploads a file: %v", err)
	}

	t.Run("made by the project's administrators, in their own name, and seen by them", func(t *testing.T) {
		var other uuid.UUID
		if err := w.editor.QueryRow(ctx, insertReqIF, w.here, w.editorID).Scan(&other); err == nil {
			t.Error("an editor who does not administer the project made an import")
		}
		if err := admin.QueryRow(ctx, insertReqIF, w.here, w.editorID).Scan(&other); err == nil {
			t.Error("an import was made in somebody else's name")
		}
		if seesRows(t, w.editor, `SELECT count(*) FROM reqif_import WHERE id = $1`, id) != 0 || seesRows(t, w.stranger, `SELECT count(*) FROM reqif_import`) != 0 {
			t.Error("somebody who neither made the import nor runs the project sees it")
		}
		if tag, err := w.editor.Exec(ctx, `UPDATE reqif_import SET state = 'queued' WHERE id = $1`, id); err != nil || tag.RowsAffected() != 0 {
			t.Errorf("an editor started somebody else's import: %v", err)
		}
	})

	t.Run("its files are added by its maker while it is uploaded, and seen with it", func(t *testing.T) {
		if _, err := admin.Exec(ctx, addReqIFFile, id, "files/a.png"); err != nil {
			t.Fatalf("the maker adds a file: %v", err)
		}
		if _, err := w.editor.Exec(ctx, addReqIFFile, id, "files/b.png"); err == nil {
			t.Error("somebody else added a file to the import")
		}
		if seesRows(t, w.editor, `SELECT count(*) FROM reqif_import_object WHERE import_id = $1`, id) != 0 {
			t.Error("somebody who does not see the import sees its files")
		}
		if _, err := admin.Exec(ctx, `UPDATE reqif_import_object SET data = '\x00' WHERE import_id = $1`, id); err == nil {
			t.Error("a file of an import was changed")
		}
	})

	t.Run("its file never changes, its choices only until it starts, and it moves only forward", func(t *testing.T) {
		if _, err := admin.Exec(ctx, `UPDATE reqif_import SET content = '{}' WHERE id = $1`, id); !isCheckViolation(err) {
			t.Errorf("an import's file was changed: %v", err)
		}
		if _, err := admin.Exec(ctx, `UPDATE reqif_import SET choices = '{"_t":"create"}' WHERE id = $1`, id); err != nil {
			t.Fatalf("the choices of an uploaded import: %v", err)
		}
		if _, err := admin.Exec(ctx, `UPDATE reqif_import SET state = 'done' WHERE id = $1`, id); !isCheckViolation(err) {
			t.Errorf("an import skipped the worker: %v", err)
		}
		if _, err := admin.Exec(ctx, `UPDATE reqif_import SET state = 'queued' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `UPDATE reqif_import SET choices = '{}' WHERE id = $1`, id); !isCheckViolation(err) {
			t.Errorf("the choices of a started import changed: %v", err)
		}
		if _, err := admin.Exec(ctx, `UPDATE reqif_import SET state = 'uploaded' WHERE id = $1`, id); !isCheckViolation(err) {
			t.Errorf("an import went back: %v", err)
		}
		if _, err := admin.Exec(ctx, addReqIFFile, id, "files/c.png"); err == nil {
			t.Error("a file was added to a started import")
		}
	})

	t.Run("an identifier is tied by who administers the project, to something of it", func(t *testing.T) {
		tie := `INSERT INTO reqif_identity (project_id, kind, identifier, target_id, scope_id) VALUES ($1, $2, $3, $4, $5)`
		if _, err := admin.Exec(ctx, tie, w.here, "object", "_req_1", w.r1, nil); err != nil {
			t.Fatalf("an administrator ties an identifier: %v", err)
		}
		if _, err := w.editor.Exec(ctx, tie, w.here, "object", "_req_2", w.r2, nil); err == nil {
			t.Error("an editor who does not administer the project tied an identifier")
		}
		if seesRows(t, w.viewer, `SELECT count(*) FROM reqif_identity WHERE project_id = $1`, w.here) != 1 {
			t.Error("a viewer of the project, whose export writes its identifiers, does not see them")
		}
		if seesRows(t, w.outsider, `SELECT count(*) FROM reqif_identity WHERE project_id = $1`, w.here) != 0 {
			t.Error("somebody who does not view the project sees its identifiers")
		}
		if _, err := admin.Exec(ctx, tie, w.other, "object", "_req_3", w.r1, nil); !isCheckViolation(err) {
			t.Errorf("an identifier of one project was tied to a requirement of another: %v", err)
		}
		if _, err := admin.Exec(ctx, tie, w.here, "type", "_type_1", uuid.New(), nil); !isCheckViolation(err) {
			t.Errorf("an identifier was tied to a type that does not exist: %v", err)
		}
		if _, err := admin.Exec(ctx, tie, w.here, "specification", "_spec_1", w.r1, nil); !isCheckViolation(err) {
			t.Errorf("a specification was tied to a requirement that is no section: %v", err)
		}
		if _, err := admin.Exec(ctx, tie, w.here, "specification", "_spec_2", w.section, nil); err != nil {
			t.Errorf("a specification tied to a section of the project: %v", err)
		}
	})

	t.Run("its files go with it", func(t *testing.T) {
		var fresh uuid.UUID
		if err := admin.QueryRow(ctx, insertReqIF, w.here, w.a.user).Scan(&fresh); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, addReqIFFile, fresh, "files/d.png"); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `DELETE FROM reqif_import WHERE id = $1`, fresh); err != nil {
			t.Fatal(err)
		}
		if w.h.countRows(t, `SELECT count(*) FROM reqif_import_object WHERE import_id = $1`, fresh) != 0 {
			t.Error("the files of a discarded import stayed")
		}
	})
}
