//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Straight through SQL as exciter_app: an editor renames no link type, whichever scope, and a project's
// administrator neither renames a type for all projects nor moves the project's own to another project (#313).
func TestRawSQLKeepsLinkTypeNamesAndScopesToTheirAdministrators(t *testing.T) {
	w := newTraceWorld(t, "link-type-names")
	h := w.h
	// A failure here would rename a type for all projects.
	h.keepLinkTypes(t)
	ctx := context.Background()
	adminID := h.addPerson(t, "member")
	h.grantAs(t, w.here, adminID, "admin")
	admin := h.appConn(t, adminID)
	var ours uuid.UUID
	if err := admin.QueryRow(ctx, `INSERT INTO link_type (project_id, key, name, forward, backward, created_by)
		VALUES ($1, 'ours', 'Ours', 'a', 'b', current_actor_id()) RETURNING id`, w.here).Scan(&ours); err != nil {
		t.Fatalf("the project's administrator could not make its own type: %v", err)
	}
	rename := `UPDATE link_type SET name = 'Renamed' WHERE id = $1`

	touchesNothing(t, w.editor, "an editor renames a type for all projects", rename, w.derives)
	touchesNothing(t, w.editor, "an editor renames the project's own type", rename, ours)
	touchesNothing(t, admin, "a project's administrator renames a type for all projects", rename, w.derives)
	if _, err := admin.Exec(ctx, `UPDATE link_type SET project_id = $2 WHERE id = $1`, ours, w.other); !refusedBy(err, "link_type_key_fixed") {
		t.Errorf("a project's administrator moved the project's type to another project: %v", err)
	}
	if n := affects(t, admin, rename, ours); n != 1 {
		t.Fatal("the project's administrator could not rename the project's own type")
	}
	var derives, own string
	var project uuid.UUID
	if err := h.super.QueryRow(ctx, `SELECT (SELECT name FROM link_type WHERE id = $1), name, project_id FROM link_type WHERE id = $2`,
		w.derives, ours).Scan(&derives, &own, &project); err != nil {
		t.Fatal(err)
	}
	if derives != "Derives" || own != "Renamed" || project != w.here {
		t.Errorf("the types read %q and %q in %s", derives, own, project)
	}
}

// Decided: the database leaves a project's choice of types to the service, which offers only what the project
// enables and its pairs allow; switching a type off or narrowing its pairs keeps the links it already has (#313).
func TestRawSQLLeavesAProjectsChoiceOfLinkTypesToTheService(t *testing.T) {
	w := newTraceWorld(t, "link-type-choice")
	h := w.h
	ctx := context.Background()
	owner := h.appConn(t, w.a.user)
	if _, err := owner.Exec(ctx, `INSERT INTO link_type_project (project_id, link_type_id, enabled) VALUES ($1, $2, false)`, w.here, w.relates); err != nil {
		t.Fatalf("turn relates off: %v", err)
	}
	var test uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO requirement_type (project_id, name, icon, colour, position) VALUES ($1, 'Test', 'test', '#22aa55', 99)
		RETURNING id`, w.here).Scan(&test); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO link_type_rule (project_id, link_type_id, source_type_id, target_type_id) VALUES ($1, $2, $3, $3)`,
		w.here, w.derives, test); err != nil {
		t.Fatalf("limit derives to tests: %v", err)
	}
	if err := w.tryLink(w.editor, w.relates, w.r1, w.r2); err != nil {
		t.Errorf("a link of a type the project turned off was refused: %v", err)
	}
	if err := w.tryLink(w.editor, w.derives, w.r3, w.r4); err != nil {
		t.Errorf("a link between requirement types no pair allows was refused: %v", err)
	}
}

// A link keeps its type even against the superuser, and a link with an end in the trash is not confirmed, whoever
// calls the clearing function (#313).
func TestRawSQLKeepsALinksTypeAndClearsNoneInTheTrash(t *testing.T) {
	w := newTraceWorld(t, "link-fixed")
	h := w.h
	ctx := context.Background()
	link := w.mustLink(t, w.editor, w.derives, w.r1, w.r2)
	if _, err := h.super.Exec(ctx, `UPDATE trace_link SET type_id = $2 WHERE id = $1`, link, w.relates); !refusedBy(err, "trace_link_fixed") {
		t.Errorf("the superuser changed a link's type: %v", err)
	}

	if n := affects(t, w.editor, `UPDATE requirement SET trashed_at = now(), trash_id = id WHERE id = $1`, w.r2); n != 1 {
		t.Fatal("the editor could not trash R2")
	}
	clear := `SELECT trace_link_clear($1, 0, 0, 'Fine.')`
	if _, err := w.editor.Exec(ctx, clear, link); !refusedBy(err, "trace_link_clear_trashed") {
		t.Errorf("an editor confirmed a link to the trash: %v", err)
	}
	tx, err := h.super.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, db.UserVar, w.editorID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, clear, link); !refusedBy(err, "trace_link_clear_trashed") {
		t.Errorf("the superuser acting for the editor confirmed a link to the trash: %v", err)
	}
	var typ uuid.UUID
	var cleared int
	if err := h.super.QueryRow(ctx, `SELECT type_id, (SELECT count(*) FROM trace_link_clearance WHERE link_id = $1) FROM trace_link WHERE id = $1`,
		link).Scan(&typ, &cleared); err != nil || typ != w.derives || cleared != 0 {
		t.Errorf("the link reads type %s with %d clearances: %v", typ, cleared, err)
	}
}
