//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// A session's approver approves in the project, in person or through a group: a group with a member who may not is
// refused when the session opens, and whoever decides is asked for the right again then, by the database too.

// grantGroupAs gives a group a role in a project.
func (h *harness) grantGroupAs(t *testing.T, project, group uuid.UUID, role string) {
	t.Helper()
	if _, err := h.super.Exec(t.Context(), `INSERT INTO role_assignment (project_id, group_id, role) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		project, group, role); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
}

// reviewState reads the state of a review, and how many decisions it holds.
func (s *sessionScene) reviewState(t *testing.T, id string) (string, int) {
	t.Helper()
	var state string
	if err := s.h.super.QueryRow(t.Context(), `SELECT state FROM requirement_review WHERE id = $1`, id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state, s.h.countRows(t, `SELECT count(*) FROM review_decision WHERE review_id = $1`, id)
}

func TestASessionGroupTakesPartOnlyWhenEachMemberMay(t *testing.T) {
	s := newSessionScene(t, "RAG")
	participants := func(group uuid.UUID, role string) map[string]any {
		return s.body(map[string]any{"quorum": 1, "participants": []map[string]any{
			{"subjectType": "user", "userId": s.daveID, "role": "approver"},
			{"subjectType": "group", "groupId": group, "role": role},
		}})
	}

	t.Run("an editor naming their own group to approve is refused, and nothing is opened", func(t *testing.T) {
		mixed := s.h.makeGroup(t, "Mixed", s.bobID, s.carolID)
		msg := fieldError(t, s.open(t, s.bob, participants(mixed, "approver")), "participants")
		if !strings.Contains(msg, "group") || !strings.HasSuffix(msg, ".") {
			t.Errorf("the refusal says %q", msg)
		}
		if n := s.sessionCount(t); n != 0 {
			t.Errorf("the refusal opened %d sessions", n)
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM requirement_review v JOIN requirement r ON r.id = v.requirement_id WHERE r.project_id = $1`, s.projectID); n != 0 {
			t.Errorf("the refusal asked %d reviews", n)
		}
	})

	t.Run("a group to review with a member who may only read is refused", func(t *testing.T) {
		reader := s.h.addPerson(t, "member")
		fieldError(t, s.open(t, s.bob, participants(s.h.makeGroup(t, "Readers", s.carolID, reader), "reviewer")), "participants")
	})

	t.Run("a group whose every member approves takes part, and its member approves", func(t *testing.T) {
		reviewers := s.h.makeGroup(t, "Reviewers", s.carolID, s.daveID)
		session := obj(t, want(t, s.open(t, s.bob, participants(reviewers, "approver")), http.StatusCreated, "bob opens it"), "session")
		review := itemsOf(t, session)[s.req("3")]["review"].(map[string]any)["id"].(string)
		got := obj(t, want(t, s.carol.post(t, "/api/v1/reviews/"+review+"/decisions", map[string]any{"decision": "approve", "revision": 1}),
			http.StatusOK, "carol approves R3 through the group"), "review")
		if got["state"] != "approved" {
			t.Errorf("carol's approval left the review %v", got["state"])
		}
	})
}

// Who is in a group changes after a session opens: somebody who joins it without the approve right decides nothing,
// through the API or straight through SQL, and decides once they are given the right.
func TestASessionDecisionAsksForTheApproveRight(t *testing.T) {
	s := newSessionScene(t, "RAD")
	ctx := context.Background()
	reviewers := s.h.makeGroup(t, "Reviewers", s.carolID, s.daveID)
	session := obj(t, want(t, s.open(t, s.bob, s.body(map[string]any{"quorum": 1, "participants": []map[string]any{
		{"subjectType": "group", "groupId": reviewers, "role": "approver"},
	}})), http.StatusCreated, "bob opens a session the reviewers approve"), "session")
	items := itemsOf(t, session)
	r3 := items[s.req("3")]["review"].(map[string]any)["id"].(string)
	r4 := items[s.req("4")]["review"].(map[string]any)["id"].(string)
	if _, err := s.h.super.Exec(ctx, `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, reviewers, s.bobID); err != nil {
		t.Fatal(err)
	}
	s.h.settle(t)

	refuse(t, "bob approves R3 without the right", s.bob.post(t, "/api/v1/reviews/"+r3+"/decisions", map[string]any{"decision": "approve", "revision": 1}),
		http.StatusForbidden, "not_an_approver")
	refuse(t, "bob rejects R3 without the right", s.bob.post(t, "/api/v1/reviews/"+r3+"/decisions", map[string]any{"decision": "reject", "revision": 1}),
		http.StatusForbidden, "not_an_approver")
	bob := s.h.appConn(t, s.bobID)
	if _, err := bob.Exec(ctx, `INSERT INTO review_decision (review_id, reviewer_id, decision) VALUES ($1, $2, 'approve')`, r3, s.bobID); !isPolicyViolation(err) {
		t.Errorf("bob approved R3 straight through SQL without the right: %v", err)
	}
	if state, decisions := s.reviewState(t, r3); state != "pending" || decisions != 0 {
		t.Errorf("after bob's refusals R3's review is %s with %d decisions", state, decisions)
	}
	for _, each := range list(t, want(t, s.bob.get(t, "/api/v1/requirements/"+s.req("3")+"/reviews"), http.StatusOK, "bob reads R3's reviews"), "reviews") {
		if v := each.(map[string]any); v["id"] == r3 && v["can"].(map[string]any)["decide"] != false {
			t.Errorf("R3's review offers bob a decision: %v", v["can"])
		}
	}
	for _, each := range list(t, want(t, s.bob.get(t, "/api/v1/reviews/waiting"), http.StatusOK, "bob's inbox"), "reviews") {
		if id := each.(map[string]any)["id"]; id == r3 || id == r4 {
			t.Errorf("bob's inbox holds the session's review %v", id)
		}
	}

	want(t, s.dave.post(t, "/api/v1/reviews/"+r4+"/decisions", map[string]any{"decision": "approve", "revision": 1}), http.StatusOK, "dave approves R4")
	if state, _ := s.reviewState(t, r4); state != "approved" {
		t.Errorf("dave's approval left R4's review %s", state)
	}

	s.h.grantAs(t, s.projectID, s.bobID, "reviewer")
	want(t, s.bob.post(t, "/api/v1/reviews/"+r3+"/decisions", map[string]any{"decision": "approve", "revision": 1}), http.StatusOK, "bob approves R3 with the right")
	if state, _ := s.reviewState(t, r3); state != "approved" {
		t.Errorf("bob's approval with the right left R3's review %s", state)
	}
}

// The service's refusal is not the guard: an editor opening a session straight through SQL, naming a group of their
// own to approve with a quorum of one, still may not decide its reviews.
func TestRawSQLRefusesASessionDecisionWithoutTheApproveRight(t *testing.T) {
	s := newSessionScene(t, "RAQ")
	ctx := context.Background()
	own := s.h.makeGroup(t, "Own", s.bobID)
	bob := s.h.appConn(t, s.bobID)
	tx, err := bob.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var session, review uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO review_session (project_id, title, scope_kind, scope_root, moderator_id, quorum)
		VALUES ($1, 'Mine', 'section', $2, $3, 1) RETURNING id`, s.projectID, s.sectionID, s.bobID).Scan(&session); err != nil {
		t.Fatalf("bob opens a session through SQL: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO review_session_participant (session_id, subject_type, group_id, role) VALUES ($1, 'group', $2, 'approver')`,
		session, own); err != nil {
		t.Fatalf("bob names his group to approve: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO review_session_round (session_id, number, started_by) VALUES ($1, 1, $2)`, session, s.bobID); err != nil {
		t.Fatalf("bob starts the first round: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT review_session_pin($1, $2)`, session, s.r3ID).Scan(&review); err != nil {
		t.Fatalf("bob pins R3: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var decidable bool
	if err := bob.QueryRow(ctx, `SELECT review_decidable($1, $2)`, review, s.bobID).Scan(&decidable); err != nil || decidable {
		t.Errorf("R3's review reads as bob's to decide: %v %v", decidable, err)
	}
	for _, decision := range []string{"approve", "reject"} {
		if _, err := bob.Exec(ctx, `INSERT INTO review_decision (review_id, reviewer_id, decision) VALUES ($1, $2, $3)`, review, s.bobID, decision); !isPolicyViolation(err) {
			t.Errorf("bob decided %s on R3 straight through SQL without the right: %v", decision, err)
		}
	}
	if _, err := bob.Exec(ctx, `UPDATE requirement_review SET state = 'approved', finished_at = now() WHERE id = $1`, review); err == nil {
		t.Error("bob closed R3's review as approved without an approval")
	}
	if state, decisions := s.reviewState(t, review.String()); state != "pending" || decisions != 0 {
		t.Errorf("after bob's refusals R3's review is %s with %d decisions", state, decisions)
	}
	var approved bool
	if err := s.h.super.QueryRow(ctx, `SELECT requirement_approved($1)`, s.r3ID).Scan(&approved); err != nil || approved {
		t.Errorf("R3 reads as approved: %v %v", approved, err)
	}
}
