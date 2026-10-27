//go:build integration

package test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/label"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Straight through SQL as exciter_app: what the service refuses of a requirement in an archived project, in
// somebody else's name, past the label bound, of a hand lock and in the trash, the database refuses too (#309).

const nextRevision = `INSERT INTO requirement_revision (requirement_id, number, title, statement, attributes, hash, created_by)
	SELECT id, revision + 1, 'Changed', statement, attributes, 'h', $2 FROM requirement WHERE id = $1`

func TestRawSQLChangesNothingInAnArchivedProject(t *testing.T) {
	s := newLockScene(t, "RQA")
	want(t, s.bob.post(t, "/api/v1/requirements/"+s.req("1")+"/labels", map[string]any{"name": "brakes"}), http.StatusOK, "label R1")
	want(t, s.bob.delete(t, "/api/v1/requirements/"+s.req("3")), http.StatusNoContent, "trash R3")
	s.archive(t, s.key)
	bob := s.h.appConn(t, s.bobID)
	ctx := context.Background()

	if _, err := bob.Exec(ctx, `INSERT INTO requirement (project_id, title, statement, normative_hash, created_by)
		VALUES ($1, 'Written late', '{"type":"doc"}', 'h', current_actor_id())`, s.projectID); !isPolicyViolation(err) {
		t.Errorf("an editor wrote a requirement in the archived project: %v", err)
	}
	touchesNothing(t, bob, "a new title", `UPDATE requirement SET title = 'Changed' WHERE id = $1`, s.reqID)
	touchesNothing(t, bob, "a restore from the trash", `UPDATE requirement SET trashed_at = NULL, trashed_by = NULL, trash_id = NULL WHERE id = $1`, s.r3ID)
	if _, err := bob.Exec(ctx, nextRevision, s.reqID, s.bobID); !isPolicyViolation(err) {
		t.Errorf("an editor wrote a revision in the archived project: %v", err)
	}
	if _, err := bob.Exec(ctx, `INSERT INTO requirement_label (requirement_id, name, created_by) VALUES ($1, 'late', current_actor_id())`, s.reqID); !isPolicyViolation(err) {
		t.Errorf("an editor labelled a requirement in the archived project: %v", err)
	}
	touchesNothing(t, bob, "a label taken off", `DELETE FROM requirement_label WHERE requirement_id = $1`, s.reqID)
	if n := s.h.countRows(t, `SELECT count(*) FROM requirement WHERE id = $1 AND title = 'Brakes engage within 100 ms' AND revision = 1`, s.reqID); n != 1 {
		t.Error("R1 changed in the archived project")
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM requirement WHERE id = $1 AND trashed_at IS NOT NULL`, s.r3ID); n != 1 {
		t.Error("R3 left the trash of the archived project")
	}
}

func TestRawSQLWritesHistoryOnlyInItsWritersName(t *testing.T) {
	s := newLockScene(t, "RQN")
	bob := s.h.appConn(t, s.bobID)
	ctx := context.Background()
	if _, err := bob.Exec(ctx, nextRevision, s.reqID, s.carolID); !isPolicyViolation(err) {
		t.Errorf("bob wrote a revision in carol's name: %v", err)
	}
	if _, err := bob.Exec(ctx, `INSERT INTO requirement_change (requirement_id, field, from_value, to_value, actor_id) VALUES ($1, 'priority', '', 'must', $2)`,
		s.reqID, s.carolID); !isPolicyViolation(err) {
		t.Errorf("bob logged a change in carol's name: %v", err)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM requirement_revision WHERE requirement_id = $1 AND created_by = $2`, s.reqID, s.carolID); n != 0 {
		t.Errorf("%d revisions name carol", n)
	}
	if _, err := bob.Exec(ctx, nextRevision, s.reqID, s.bobID); err != nil {
		t.Errorf("bob could not write a revision in his own name: %v", err)
	}
	if _, err := bob.Exec(ctx, `INSERT INTO requirement_change (requirement_id, field, from_value, to_value, actor_id) VALUES ($1, 'priority', '', 'must', $2)`,
		s.reqID, s.bobID); err != nil {
		t.Errorf("bob could not log a change in his own name: %v", err)
	}
}

// The bound of label.MaxPerRequirement is the database's too, so a writer past the service meets it (#309).
func TestRawSQLHoldsALabelBound(t *testing.T) {
	s := newLockScene(t, "RQB")
	bob := s.h.appConn(t, s.bobID)
	ctx := context.Background()
	put := `INSERT INTO requirement_label (requirement_id, name, created_by) VALUES ($1, $2, current_actor_id())`
	for i := range label.MaxPerRequirement {
		if _, err := bob.Exec(ctx, put, s.reqID, "label-"+strconv.Itoa(i)); err != nil {
			t.Fatalf("label %d: %v", i+1, err)
		}
	}
	if _, err := bob.Exec(ctx, put, s.reqID, "one-more"); !refusedBy(err, "requirement_label_bound") {
		t.Errorf("a label past %d: %v", label.MaxPerRequirement, err)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM requirement_label WHERE requirement_id = $1`, s.reqID); n != label.MaxPerRequirement {
		t.Errorf("R1 carries %d labels", n)
	}
	refuse(t, "a label past the bound through the API", s.bob.post(t, "/api/v1/requirements/"+s.req("1")+"/labels", map[string]any{"name": "one-more"}),
		http.StatusUnprocessableEntity, "validation_failed")
	if _, err := bob.Exec(ctx, put, s.r3ID, "label-0"); err != nil {
		t.Errorf("another requirement takes a label of its own: %v", err)
	}
}

func TestRawSQLLocksByHandOnlyWhatMayBeLocked(t *testing.T) {
	s := newLockScene(t, "RQL")
	owner := s.h.appConn(t, s.ownerID)
	ctx := context.Background()
	if _, err := owner.Exec(ctx, `SELECT requirement_lock($1, $2)`, s.reqID, tooLong(requirement.MaxLockReasonLength)); !refusedBy(err, "requirement_lock_whole") {
		t.Errorf("a reason past %d characters: %v", requirement.MaxLockReasonLength, err)
	}
	want(t, s.bob.delete(t, "/api/v1/requirements/"+s.req("3")), http.StatusNoContent, "trash R3")
	if _, err := owner.Exec(ctx, `SELECT requirement_lock($1, 'Released')`, s.r3ID); !refusedBy(err, "requirement_lock_section") {
		t.Errorf("a lock in the trash: %v", err)
	}
	s.archive(t, s.key)
	if _, err := owner.Exec(ctx, `SELECT requirement_lock($1, 'Released')`, s.reqID); !refusedBy(err, "requirement_lock_forbidden") {
		t.Errorf("a lock in an archived project: %v", err)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM requirement WHERE project_id = $1 AND locked_at IS NOT NULL`, s.projectID); n != 0 {
		t.Errorf("%d requirements were locked", n)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM requirement_change r JOIN requirement q ON q.id = r.requirement_id WHERE q.project_id = $1 AND r.field = 'lock'`,
		s.projectID); n != 0 {
		t.Errorf("%d lock changes were logged", n)
	}
}

// A requirement in the trash takes no transition, through the API or straight through SQL; that one whose status
// locked while it was there still comes back is TestARequirementLockedByItsStatusComesBackFromTheTrash (#350).
func TestARequirementInTheTrashTakesNoTransition(t *testing.T) {
	s := newLockScene(t, "RQT")
	var submit string
	if err := s.h.super.QueryRow(context.Background(), `
		SELECT t.id FROM workflow_transition t JOIN workflow f ON f.id = t.workflow_id WHERE f.project_id = $1 AND t.name = 'Submit for review'`,
		s.projectID).Scan(&submit); err != nil {
		t.Fatal(err)
	}
	want(t, s.bob.delete(t, "/api/v1/requirements/"+s.req("1")), http.StatusNoContent, "trash R1")
	refuse(t, "a transition in the trash", s.bob.post(t, "/api/v1/requirements/"+s.req("1")+"/transitions", map[string]any{"transitionId": submit}),
		http.StatusConflict, "requirement_trashed")
	if _, err := s.h.appConn(t, s.bobID).Exec(context.Background(), `SELECT requirement_transition($1, $2)`, s.reqID, submit); !refusedBy(err, "requirement_transition_known") {
		t.Errorf("a transition in the trash through SQL: %v", err)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM requirement r JOIN req_status st ON st.id = r.status_id WHERE r.id = $1 AND st.name = 'Draft'`, s.reqID); n != 1 {
		t.Error("R1 left Draft in the trash")
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM requirement_change WHERE requirement_id = $1 AND field = 'status'`, s.reqID); n != 0 {
		t.Errorf("%d status changes were logged in the trash", n)
	}
}
