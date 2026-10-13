//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// traceWorld is an organization with two projects, four requirements and a section in the first, and people
// who edit, view, or do not see it, each on their own exciter_app connection.
type traceWorld struct {
	h                                  *harness
	a, b                               member
	here, other                        uuid.UUID
	editorID, viewerID                 uuid.UUID
	editor, viewer, outsider, stranger *pgx.Conn
	r1, r2, r3, r4, section            uuid.UUID
	derives, relates                   uuid.UUID
}

func newTraceWorld(t *testing.T, slug string) *traceWorld {
	h := newHarness(t)
	w := &traceWorld{h: h, a: h.makeMember(t, slug+"-a"), b: h.makeMember(t, slug+"-b")}
	w.here, w.other = w.project(t, "TRC"), w.project(t, "OTH")
	w.editorID = h.addPerson(t, w.a.org, "member")
	w.viewerID = h.addPerson(t, w.a.org, "guest")
	outsiderID := h.addPerson(t, w.a.org, "guest")
	h.grantAs(t, w.a.org, w.here, w.viewerID, "viewer")
	w.editor, w.viewer, w.outsider = h.appConn(t, w.a.org, w.editorID), h.appConn(t, w.a.org, w.viewerID), h.appConn(t, w.a.org, outsiderID)
	w.stranger = h.appConn(t, w.b.org, w.b.user)
	w.r1, w.r2, w.r3, w.r4, w.section = w.write(t, "requirement"), w.write(t, "requirement"), w.write(t, "requirement"), w.write(t, "requirement"), w.write(t, "section")
	w.derives, w.relates = w.typeOf(t, "derives"), w.typeOf(t, "relates")
	return w
}

func (w *traceWorld) project(t *testing.T, key string) uuid.UUID {
	var id uuid.UUID
	if err := w.h.super.QueryRow(t.Context(), `INSERT INTO project (org_id, key, name, created_by) VALUES ($1, $2, $2, $3) RETURNING id`,
		w.a.org, key, w.a.user).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (w *traceWorld) write(t *testing.T, kind string) uuid.UUID {
	var id uuid.UUID
	if err := w.editor.QueryRow(t.Context(), `INSERT INTO requirement (org_id, project_id, kind, title, statement, normative_hash, created_by)
		VALUES ($1, $2, $3, 'Linked', '{"type":"doc"}', 'h', current_actor_id()) RETURNING id`, w.a.org, w.here, kind).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (w *traceWorld) typeOf(t *testing.T, key string) uuid.UUID {
	var id uuid.UUID
	if err := w.h.super.QueryRow(t.Context(), `SELECT id FROM link_type WHERE org_id = $1 AND project_id IS NULL AND key = $2`, w.a.org, key).Scan(&id); err != nil {
		t.Fatalf("the organization was not given the %s type: %v", key, err)
	}
	return id
}

const insertLink = `INSERT INTO trace_link (org_id, type_id, source_id, target_id, created_by) VALUES ($1, $2, $3, $4, current_actor_id()) RETURNING id`

func (w *traceWorld) tryLink(conn *pgx.Conn, typ, source, target uuid.UUID) error {
	var id uuid.UUID
	return conn.QueryRow(context.Background(), insertLink, w.a.org, typ, source, target).Scan(&id)
}

func (w *traceWorld) mustLink(t *testing.T, conn *pgx.Conn, typ, source, target uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := conn.QueryRow(t.Context(), insertLink, w.a.org, typ, source, target).Scan(&id); err != nil {
		t.Fatalf("link: %v", err)
	}
	return id
}

// Straight through SQL as exciter_app: a link joins two requirements once, never round an acyclic circle,
// records its ends' revisions, is seen with both ends, made by who edits one, and only ever tombstoned.
func TestRawSQLGuardsTraceLinks(t *testing.T) {
	w := newTraceWorld(t, "trace-wall")
	h, a, b := w.h, w.a, w.b
	ctx := context.Background()
	other := w.other
	viewerID := w.viewerID
	editor, viewer, outsider, stranger := w.editor, w.viewer, w.outsider, w.stranger
	r1, r2, r3, r4, section := w.r1, w.r2, w.r3, w.r4, w.section
	derives, relates := w.derives, w.relates
	project := func(key string) uuid.UUID { return w.project(t, key) }
	mustLink := func(conn *pgx.Conn, typ, source, target uuid.UUID) uuid.UUID {
		return w.mustLink(t, conn, typ, source, target)
	}
	tryLink := w.tryLink

	t.Run("every organization starts with the six link types", func(t *testing.T) {
		var n int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM link_type WHERE org_id = $1 AND project_id IS NULL`, b.org).Scan(&n); err != nil || n != 6 {
			t.Errorf("a new organization has %d link types: %v", n, err)
		}
	})

	t.Run("the database refuses a self-link, a section, a repeat and a cycle", func(t *testing.T) {
		if err := tryLink(editor, derives, r1, r1); !refusedBy(err, "trace_link_not_self") {
			t.Errorf("a requirement was linked to itself: %v", err)
		}
		if err := tryLink(editor, derives, r1, section); !refusedBy(err, "trace_link_ends") {
			t.Errorf("a section was linked: %v", err)
		}
		mustLink(editor, derives, r1, r2)
		mustLink(editor, derives, r2, r3)
		if err := tryLink(editor, derives, r1, r2); !refusedBy(err, "trace_link_repeat") {
			t.Errorf("a link was made twice: %v", err)
		}
		if err := tryLink(editor, derives, r3, r1); !refusedBy(err, "trace_link_cycle") {
			t.Errorf("an acyclic type went round in a circle: %v", err)
		}
		mustLink(editor, relates, r3, r1)
		if err := tryLink(editor, relates, r1, r3); !refusedBy(err, "trace_link_repeat") {
			t.Errorf("a symmetric link was made again the other way round: %v", err)
		}
	})

	t.Run("a cycle is refused over links the writer cannot see", func(t *testing.T) {
		hidden := project("HID")
		var secret uuid.UUID
		if err := h.super.QueryRow(ctx, `INSERT INTO requirement (org_id, project_id, title, statement, normative_hash, created_by)
			VALUES ($1, $2, 'Hidden', '{"type":"doc"}', 'h', $3) RETURNING id`, a.org, hidden, a.user).Scan(&secret); err != nil {
			t.Fatal(err)
		}
		owner := h.appConn(t, a.org, a.user)
		mustLink(owner, derives, r3, secret)
		mustLink(owner, derives, secret, r4)
		if _, err := h.super.Exec(ctx, `DELETE FROM project_grant WHERE project_id = $1 AND subject_type = 'everyone'`, hidden); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		if err := tryLink(editor, derives, r4, r1); !refusedBy(err, "trace_link_cycle") {
			t.Errorf("a cycle through a requirement the writer cannot see was made: %v", err)
		}
	})

	t.Run("a link records the revisions of its ends, whatever the writer says", func(t *testing.T) {
		if _, err := h.super.Exec(ctx, `UPDATE requirement SET revision = 4 WHERE id = $1`, r4); err != nil {
			t.Fatal(err)
		}
		var id uuid.UUID
		if err := editor.QueryRow(ctx, `INSERT INTO trace_link (org_id, type_id, source_id, target_id, created_by, source_reviewed_rev, target_reviewed_rev)
			VALUES ($1, $2, $3, $4, current_actor_id(), 99, 99) RETURNING id`, a.org, relates, r4, r2).Scan(&id); err != nil {
			t.Fatal(err)
		}
		var source, target int
		if err := h.super.QueryRow(ctx, `SELECT source_reviewed_rev, target_reviewed_rev FROM trace_link WHERE id = $1`, id).Scan(&source, &target); err != nil ||
			source != 4 || target != 0 {
			t.Errorf("the link recorded revisions %d and %d: %v", source, target, err)
		}
	})

	t.Run("a link is logged on both ends", func(t *testing.T) {
		var to string
		if err := h.super.QueryRow(ctx, `SELECT to_value FROM requirement_change WHERE requirement_id = $1 AND field = 'link' ORDER BY created_at, id LIMIT 1`,
			r2).Scan(&to); err != nil || to != "is derived into TRC-R1" {
			t.Errorf("the target logged %q: %v", to, err)
		}
	})

	t.Run("links are seen with both ends and made by who edits one", func(t *testing.T) {
		if err := tryLink(viewer, relates, r1, r4); !isPolicyViolation(err) {
			t.Errorf("a viewer made a link: %v", err)
		}
		if err := tryLink(stranger, relates, r1, r4); !isPolicyViolation(err) {
			t.Errorf("another organization made a link: %v", err)
		}
		if _, err := editor.Exec(ctx, `INSERT INTO trace_link (org_id, type_id, source_id, target_id, created_by) VALUES ($1, $2, $3, $4, $5)`,
			a.org, relates, r1, r4, viewerID); !isPolicyViolation(err) {
			t.Errorf("an editor linked in somebody else's name: %v", err)
		}
		count := func(conn *pgx.Conn) int {
			var n int
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM trace_link WHERE source_id = $1 OR target_id = $1`, r1).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		if n := count(viewer); n != 2 {
			t.Errorf("a viewer sees %d links of TRC-R1, want 2", n)
		}
		if n := count(outsider) + count(stranger); n != 0 {
			t.Errorf("somebody without the project sees %d links", n)
		}
		var hiddenEnd int
		if err := editor.QueryRow(ctx, `SELECT count(*) FROM trace_link WHERE source_id = $1`, r3).Scan(&hiddenEnd); err != nil || hiddenEnd != 1 {
			t.Errorf("an editor sees %d links from TRC-R3, the one to a hidden requirement left out: %v", hiddenEnd, err)
		}
	})

	t.Run("a link is tombstoned, never deleted or changed, and stays removed", func(t *testing.T) {
		id := mustLink(editor, relates, r1, r4)
		if _, err := editor.Exec(ctx, `DELETE FROM trace_link WHERE id = $1`, id); !isPolicyViolation(err) {
			t.Errorf("a link was deleted: %v", err)
		}
		if _, err := editor.Exec(ctx, `UPDATE trace_link SET target_id = $2 WHERE id = $1`, id, r2); !isPolicyViolation(err) {
			t.Errorf("a link changed its end: %v", err)
		}
		if n := affects(t, viewer, `UPDATE trace_link SET deleted_at = now(), deleted_by = current_actor_id() WHERE id = $1`, id); n != 0 {
			t.Error("a viewer removed a link")
		}
		if n := affects(t, editor, `UPDATE trace_link SET deleted_at = now(), deleted_by = current_actor_id() WHERE id = $1`, id); n != 1 {
			t.Fatal("an editor could not remove a link")
		}
		if n := affects(t, editor, `UPDATE trace_link SET deleted_at = NULL, deleted_by = NULL WHERE id = $1`, id); n != 0 {
			t.Error("a removed link came back")
		}
		var removals int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM requirement_change WHERE requirement_id = $1 AND field = 'link' AND from_value = 'relates to TRC-R1'`,
			r4).Scan(&removals); err != nil || removals != 1 {
			t.Errorf("the far end logged %d removals: %v", removals, err)
		}
		mustLink(editor, relates, r1, r4)
	})

	t.Run("a link takes a live type its source's project may use", func(t *testing.T) {
		var foreign uuid.UUID
		if err := h.super.QueryRow(ctx, `INSERT INTO link_type (org_id, project_id, key, name, forward, backward) VALUES ($1, $2, 'tests', 'Tests', 'tests', 'is tested by')
			RETURNING id`, a.org, other).Scan(&foreign); err != nil {
			t.Fatal(err)
		}
		if err := tryLink(editor, foreign, r1, r4); !refusedBy(err, "trace_link_type") {
			t.Errorf("a link took another project's type: %v", err)
		}
		if _, err := h.super.Exec(ctx, `UPDATE link_type SET retired = true WHERE id = $1`, relates); err != nil {
			t.Fatal(err)
		}
		if err := tryLink(editor, relates, r2, r4); !refusedBy(err, "trace_link_type") {
			t.Errorf("a retired type took a link: %v", err)
		}
	})
}

// Straight through SQL as exciter_app: the organization's administrators shape its link types and a project's
// administrators the project's, keys stay taken across both, and a type with links cannot be deleted.
func TestRawSQLKeepsLinkTypesToTheirAdministrators(t *testing.T) {
	w := newTraceWorld(t, "link-type-wall")
	ctx := context.Background()
	h, a, here, other := w.h, w.a, w.here, w.other
	editor, viewer, derives := w.editor, w.viewer, w.derives
	w.mustLink(t, editor, derives, w.r1, w.r2)

	add := `INSERT INTO link_type (org_id, project_id, key, name, forward, backward, created_by) VALUES ($1, $2, $3, $3, 'a', 'b', current_actor_id())`
	if _, err := editor.Exec(ctx, add, a.org, nil, "mine"); !isPolicyViolation(err) {
		t.Errorf("a member made a link type for the organization: %v", err)
	}
	if _, err := editor.Exec(ctx, add, a.org, here, "ours"); !isPolicyViolation(err) {
		t.Errorf("an editor made a link type for the project: %v", err)
	}
	owner := h.appConn(t, a.org, a.user)
	if _, err := owner.Exec(ctx, add, a.org, here, "derives"); !refusedBy(err, "link_type_key_taken") {
		t.Errorf("a project's type took the key of the organization's: %v", err)
	}
	if _, err := owner.Exec(ctx, add, a.org, here, "ours"); err != nil {
		t.Fatalf("an administrator could not make a project's type: %v", err)
	}
	if _, err := owner.Exec(ctx, `UPDATE link_type SET key = 'renamed' WHERE org_id = $1 AND key = 'ours'`, a.org); !refusedBy(err, "link_type_key_fixed") {
		t.Errorf("a link type changed its key: %v", err)
	}
	if n := affects(t, owner, `DELETE FROM link_type WHERE id = $1`, derives); n != 0 {
		t.Error("a link type with links was deleted")
	}
	if _, err := editor.Exec(ctx, `INSERT INTO link_type_project (org_id, project_id, link_type_id, enabled) VALUES ($1, $2, $3, false)`,
		a.org, here, derives); !isPolicyViolation(err) {
		t.Errorf("an editor disabled a link type in the project: %v", err)
	}
	var foreignType uuid.UUID
	if err := h.super.QueryRow(ctx, `SELECT id FROM requirement_type WHERE project_id = $1`, other).Scan(&foreignType); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO link_type_rule (org_id, project_id, link_type_id, source_type_id) VALUES ($1, $2, $3, $4)`,
		a.org, here, derives, foreignType); !refusedBy(err, "link_type_project_reach") {
		t.Errorf("a rule named another project's requirement type: %v", err)
	}
	var seen int
	if err := viewer.QueryRow(ctx, `SELECT count(*) FROM link_type WHERE org_id = $1`, a.org).Scan(&seen); err != nil || seen != 7 {
		t.Errorf("a viewer of the project sees %d link types, want the six and the project's own: %v", seen, err)
	}
}
