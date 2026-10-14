//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Straight through SQL as exciter_app: a status changes only through its workflow's transitions, which the
// database holds to their start and to who may take them, and only administrators shape statuses.
func TestRawSQLMovesStatusesOnlyByTransitions(t *testing.T) {
	flowKey := projectKey(t, "FLOW")
	h := newHarness(t)
	a := h.owner(t, "flow-wall")
	ctx := context.Background()
	var project uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO project (org_id, key, name, created_by) VALUES ($1, '`+flowKey+`', 'Flow', $2) RETURNING id`, a.org, a.user).Scan(&project); err != nil {
		t.Fatal(err)
	}
	status := func(name string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := h.super.QueryRow(ctx, `SELECT id FROM req_status WHERE project_id = $1 AND name = $2`, project, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	transition := func(name string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := h.super.QueryRow(ctx, `SELECT t.id FROM workflow_transition t JOIN workflow w ON w.id = t.workflow_id
			WHERE w.project_id = $1 AND t.name = $2 LIMIT 1`, project, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	editorID := h.addPerson(t, a.org, "member")
	viewerID := h.addPerson(t, a.org, "guest")
	h.grantAs(t, a.org, project, viewerID, "viewer")
	editor, viewer := h.appConn(t, a.org, editorID), h.appConn(t, a.org, viewerID)
	var id, current uuid.UUID
	if err := editor.QueryRow(ctx, `INSERT INTO requirement (org_id, project_id, title, statement, normative_hash, status_id, created_by)
		VALUES ($1, $2, 'Flowing', '{"type":"doc"}', 'h', $3, current_actor_id()) RETURNING id, status_id`, a.org, project, status("Approved")).Scan(&id, &current); err != nil {
		t.Fatal(err)
	}
	if current != status("Draft") {
		t.Errorf("a new requirement started where its insert said, not in Draft")
	}

	t.Run("the column takes no plain update", func(t *testing.T) {
		if _, err := editor.Exec(ctx, `UPDATE requirement SET status_id = $2 WHERE id = $1`, id, status("Verified")); !isPolicyViolation(err) {
			t.Errorf("an editor set a status by hand: %v", err)
		}
	})

	t.Run("a transition moves it only from its start, and only for whoever may take it", func(t *testing.T) {
		if _, err := editor.Exec(ctx, `SELECT requirement_transition($1, $2)`, id, transition("Mark verified")); !refusedBy(err, "requirement_transition_leaves") {
			t.Errorf("a transition from another status was taken: %v", err)
		}
		if _, err := viewer.Exec(ctx, `SELECT requirement_transition($1, $2)`, id, transition("Submit for review")); !refusedBy(err, "requirement_transition_allowed") {
			t.Errorf("a viewer took a transition: %v", err)
		}
		if _, err := editor.Exec(ctx, `SELECT requirement_transition($1, $2)`, id, transition("Submit for review")); err != nil {
			t.Fatalf("an editor could not take a transition from Draft: %v", err)
		}
		if _, err := editor.Exec(ctx, `SELECT requirement_remap_status($1, $2)`, id, status("Approved")); !refusedBy(err, "requirement_remap_lacking") {
			t.Errorf("a status the workflow has was remapped: %v", err)
		}
	})

	t.Run("an approver who may not edit takes the transition, and the database logs it and runs its post-functions", func(t *testing.T) {
		reviewerID := h.addPerson(t, a.org, "guest")
		h.grantAs(t, a.org, project, reviewerID, "reviewer")
		if _, err := h.super.Exec(ctx, `INSERT INTO transition_rule (org_id, transition_id, kind, type, position) VALUES ($1, $2, 'postfunction', 'postfunction.assign_to_actor', 9)`,
			a.org, transition("Approve")); err != nil {
			t.Fatal(err)
		}
		reviewer := h.appConn(t, a.org, reviewerID)
		if n := affects(t, reviewer, `UPDATE requirement SET owner_id = current_actor_id() WHERE id = $1`, id); n != 0 {
			t.Fatal("a reviewer made themselves the owner by hand")
		}
		if _, err := reviewer.Exec(ctx, `SELECT requirement_transition($1, $2, 'Reads well.')`, id, transition("Approve")); err != nil {
			t.Fatalf("a reviewer could not approve: %v", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM requirement WHERE id = $1 AND status_id = $2 AND owner_id = $3`, id, status("Approved"), reviewerID); n != 1 {
			t.Error("the approval left the status or the owner where they were")
		}
		if n := h.countRows(t, `SELECT count(*) FROM requirement_change WHERE requirement_id = $1 AND actor_id = $2
			AND ((field = 'status' AND to_value = 'Approved' AND reason = 'Reads well.') OR (field = 'owner' AND from_value = ''))`, id, reviewerID); n != 2 {
			t.Errorf("%d changes were logged for the approval, want the status and the owner", n)
		}
	})

	t.Run("whoever views reads the statuses, and one a workflow names stays", func(t *testing.T) {
		if n := affects(t, viewer, `SELECT 1 FROM req_status WHERE project_id = $1`, project); n != 7 {
			t.Errorf("a viewer reads %d statuses, want 7", n)
		}
		admin := h.appConn(t, a.org, a.user)
		if _, err := admin.Exec(ctx, `DELETE FROM req_status WHERE id = $1`, status("Obsolete")); !refusedBy(err, "workflow_step_status_id_fkey") {
			t.Errorf("an administrator deleted a status a workflow names: %v", err)
		}
	})

	t.Run("only administrators shape statuses and workflows", func(t *testing.T) {
		if _, err := editor.Exec(ctx, `INSERT INTO req_status (org_id, project_id, name, category) VALUES ($1, $2, 'Mine', 'draft')`, a.org, project); !isPolicyViolation(err) {
			t.Errorf("an editor made a status: %v", err)
		}
		if n := affects(t, editor, `UPDATE workflow_transition SET name = 'Hijacked' WHERE id = $1`, transition("Approve")); n != 0 {
			t.Error("an editor renamed a transition")
		}
		if n := affects(t, editor, `DELETE FROM transition_rule WHERE transition_id = $1`, transition("Approve")); n != 0 {
			t.Error("an editor took the rules off a transition")
		}
	})
}
