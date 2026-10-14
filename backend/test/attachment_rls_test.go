//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const insertFile = `INSERT INTO attachment (requirement_id, uploaded_by, file_name, content_type, size_bytes)
	VALUES ($1, $2, 'spec.pdf', 'application/pdf', 10) RETURNING id`

// showFile writes the requirement's next revision, its statement showing the file, as the editor saves one.
func (w *traceWorld) showFile(t *testing.T, requirement, file uuid.UUID) {
	t.Helper()
	statement := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"attachment","attrs":{"attachmentId":"` + file.String() + `","fileName":"spec.pdf"}}]}]}`
	if _, err := w.editor.Exec(t.Context(), `
		INSERT INTO requirement_revision (requirement_id, number, title, statement, hash, created_by)
		SELECT id, revision + 1, title, $2, 'h', current_actor_id() FROM requirement WHERE id = $1`, requirement, statement); err != nil {
		t.Fatalf("show the file: %v", err)
	}
}

func (w *traceWorld) attach(t *testing.T, requirement uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := w.editor.QueryRow(t.Context(), insertFile, requirement, w.editorID).Scan(&id); err != nil {
		t.Fatalf("attach: %v", err)
	}
	return id
}

func seesFile(t *testing.T, conn *pgx.Conn, id uuid.UUID) bool {
	t.Helper()
	var n int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM attachment WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// A file is its requirement's, straight through SQL as exciter_app: seen with it, attached and deleted by who
// edits it, never changed but to be taken off the list, and never deleted while a revision shows it.
func TestRawSQLGuardsAttachments(t *testing.T) {
	w := newTraceWorld(t, "files")
	ctx := context.Background()
	file := w.attach(t, w.r1)

	t.Run("seen by who views the requirement, and nobody else", func(t *testing.T) {
		if !seesFile(t, w.viewer, file) {
			t.Error("a viewer does not see the file")
		}
		if seesFile(t, w.outsider, file) || seesFile(t, w.stranger, file) {
			t.Error("somebody who may not view the requirement sees its file")
		}
	})

	t.Run("attached only by an editor, in their own name", func(t *testing.T) {
		var id uuid.UUID
		if err := w.viewer.QueryRow(ctx, insertFile, w.r1, w.viewerID).Scan(&id); err == nil {
			t.Error("a viewer attached a file")
		}
		if err := w.editor.QueryRow(ctx, insertFile, w.r1, w.viewerID).Scan(&id); err == nil {
			t.Error("an editor attached a file in somebody else's name")
		}
		if err := w.stranger.QueryRow(ctx, insertFile, w.r1, w.strangerID).Scan(&id); err == nil {
			t.Error("a guest no grant names attached a file")
		}
		if err := w.editor.QueryRow(ctx, `INSERT INTO attachment (requirement_id, uploaded_by, file_name, size_bytes, deleted_at)
			VALUES ($1, current_actor_id(), 'x', 1, now()) RETURNING id`, w.r1).Scan(&id); err == nil {
			t.Error("a file was attached already taken off the list")
		}
	})

	t.Run("never changed, but taken off the list once by an editor", func(t *testing.T) {
		if _, err := w.editor.Exec(ctx, `UPDATE attachment SET file_name = 'other.pdf' WHERE id = $1`, file); err == nil {
			t.Error("the app renamed a file, which would let it point at other bytes")
		}
		if _, err := w.editor.Exec(ctx, `UPDATE attachment SET requirement_id = $2 WHERE id = $1`, file, w.r2); err == nil {
			t.Error("the app moved a file, and its key with it")
		}
		if tag, err := w.viewer.Exec(ctx, `UPDATE attachment SET deleted_at = now(), deleted_by = $2 WHERE id = $1`, file, w.viewerID); err == nil && tag.RowsAffected() > 0 {
			t.Error("a viewer took a file off the list")
		}
		gone := w.attach(t, w.r1)
		if _, err := w.editor.Exec(ctx, `UPDATE attachment SET deleted_at = now(), deleted_by = $2 WHERE id = $1`, gone, w.editorID); err != nil {
			t.Fatalf("an editor takes a file off the list: %v", err)
		}
		if _, err := w.editor.Exec(ctx, `UPDATE attachment SET deleted_at = NULL, deleted_by = NULL WHERE id = $1`, gone); !isCheckViolation(err) {
			t.Errorf("a file taken off the list was put back: %v", err)
		}
		if _, err := w.editor.Exec(ctx, `UPDATE attachment SET deleted_at = now() - interval '1 day' WHERE id = $1`, gone); !isCheckViolation(err) {
			t.Errorf("when a file was taken off the list was changed: %v", err)
		}
	})

	t.Run("deleted by an editor while nothing shows it, leaving a tombstone", func(t *testing.T) {
		loose := w.attach(t, w.r2)
		if tag, err := w.viewer.Exec(ctx, `DELETE FROM attachment WHERE id = $1`, loose); err != nil || tag.RowsAffected() != 0 {
			t.Errorf("a viewer deleted a file: %v", err)
		}
		if tag, err := w.editor.Exec(ctx, `DELETE FROM attachment WHERE id = $1`, loose); err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("an editor deletes a file nothing shows: %v", err)
		}
		if n := w.h.countRows(t, `SELECT count(*) FROM attachment_tombstone WHERE object_key = $1`,
			fileKey(w.h.installation(t).ID, w.r2.String(), loose.String())); n != 1 {
			t.Error("the deleted file left no tombstone")
		}
		var key string
		if err := w.stranger.QueryRow(ctx, `INSERT INTO attachment_tombstone (object_key) VALUES ($1) RETURNING object_key`,
			fileKey(w.h.installation(t).ID, w.r2.String(), loose.String())).Scan(&key); err == nil {
			t.Error("a guest no grant names named somebody's object for the reaper")
		}
		if _, err := w.editor.Exec(ctx, `UPDATE attachment_tombstone SET created_at = now()`); err == nil {
			t.Error("the app changed a tombstone")
		}
	})

	t.Run("never deleted while a revision shows it, but purged with its requirement", func(t *testing.T) {
		w.showFile(t, w.r1, file)
		if _, err := w.editor.Exec(ctx, `DELETE FROM attachment WHERE id = $1`, file); !isCheckViolation(err) {
			t.Fatalf("a file a revision shows was deleted: %v", err)
		}
		if _, err := w.h.super.Exec(ctx, `DELETE FROM requirement WHERE id = $1`, w.r1); err != nil {
			t.Fatalf("a purge was refused for the file its revision shows: %v", err)
		}
		if n := w.h.countRows(t, `SELECT count(*) FROM attachment WHERE requirement_id = $1`, w.r1); n != 0 {
			t.Errorf("%d files outlived their requirement", n)
		}
		if n := w.h.countRows(t, `SELECT count(*) FROM attachment_tombstone WHERE object_key = $1`,
			fileKey(w.h.installation(t).ID, w.r1.String(), file.String())); n != 1 {
			t.Error("the purged file left no tombstone for the reaper")
		}
	})
}
