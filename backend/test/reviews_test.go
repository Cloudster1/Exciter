//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Review and approval of requirement revisions (#35): a review pins a revision and freezes its approvers, only they
// decide, and a new revision supersedes, through the API and straight through SQL.

// reviewScene is a comment world whose R1 the owner wrote, dave approves by name and carol and the owner through a
// group, two approvals needed.
type reviewScene struct {
	*commentWorld
	req, rules     string
	group          uuid.UUID
	reviewID       string
	ruleID         string
	nobody         *client
	submitID       string
	reviewRevision int
}

func newReviewScene(t *testing.T, prefix string) *reviewScene {
	t.Helper()
	w := newCommentWorld(t, prefix)
	s := &reviewScene{commentWorld: w, req: "/api/v1/requirements/" + w.reqKey, rules: "/api/v1/projects/" + w.key + "/approval-rules"}
	s.group = w.h.makeGroup(t, "Approvers", w.carolID, w.ownerID)
	s.nobody = newAPIServer(t, w.h).anonymous()
	return s
}

func (s *reviewScene) rule(required int) map[string]any {
	return map[string]any{"required": required, "approvers": []map[string]any{
		{"subjectType": "user", "userId": s.daveID}, {"subjectType": "group", "groupId": s.group},
	}}
}

func (s *reviewScene) revise(t *testing.T, base int) {
	t.Helper()
	want(t, s.bob.patch(t, s.req, map[string]any{"title": "Brakes engage within " + uuid.NewString()[:4], "baseRevision": base}),
		http.StatusOK, "bob revises R1")
}

func (s *reviewScene) state(t *testing.T, id string) string {
	t.Helper()
	return obj(t, want(t, s.bob.get(t, "/api/v1/reviews/"+id), http.StatusOK, "read the review"), "review")["state"].(string)
}

func TestARevisionIsReviewedAndApproved(t *testing.T) {
	s := newReviewScene(t, "REV")
	t.Run("a requirement no rule covers takes no review", s.noRule)
	t.Run("the project's administrators set who approves, and a rule must be able to be met", s.setRules)
	t.Run("an editor asks for the revision they saw, once at a time, and never for a section", s.request)
	t.Run("the approvers hear of it, and it waits in their inbox", s.inbox)
	t.Run("only a frozen approver decides, not the author, and the count closes it", s.decide)
	t.Run("a transition asking for approval goes only with the current revision approved", s.condition)
	t.Run("a new revision supersedes a pending review and leaves an approved one behind", s.supersede)
	t.Run("whoever asked, or an administrator, withdraws, and the administrator is audited", s.withdraw)
	t.Run("removing the rule refuses new reviews and keeps old ones", s.removeRule)
	t.Run("what nobody may do is refused", s.refusals)
}

func (s *reviewScene) noRule(t *testing.T) {
	refuse(t, "a review nobody approves", s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 1}), http.StatusConflict, "no_approval_rule")
	if rules := list(t, want(t, s.carol.get(t, s.rules), http.StatusOK, "list rules"), "rules"); len(rules) != 0 {
		t.Errorf("a new project has rules %v", rules)
	}
}

func (s *reviewScene) setRules(t *testing.T) {
	refuse(t, "a reader sets a rule", s.carol.put(t, s.rules, s.rule(2)), http.StatusForbidden, "forbidden")
	fieldError(t, s.owner.put(t, s.rules, s.rule(0)), "required")
	fieldError(t, s.owner.put(t, s.rules, map[string]any{"required": 1, "approvers": []any{}}), "approvers")
	fieldError(t, s.owner.put(t, s.rules, map[string]any{"required": 1, "approvers": []map[string]any{{"subjectType": "everyone"}}}), "approvers")
	fieldError(t, s.owner.put(t, s.rules, map[string]any{"required": 2, "approvers": []map[string]any{{"subjectType": "user", "userId": s.daveID}}}), "required")
	fieldError(t, s.owner.put(t, s.rules, map[string]any{"required": 1, "typeId": uuid.New(), "approvers": []map[string]any{{"subjectType": "user", "userId": s.daveID}}}), "typeId")
	rule := obj(t, want(t, s.owner.put(t, s.rules, s.rule(1)), http.StatusOK, "set the rule"), "rule")
	again := obj(t, want(t, s.owner.put(t, s.rules, s.rule(2)), http.StatusOK, "raise the count"), "rule")
	if again["id"] != rule["id"] || again["required"] != float64(2) || again["typeId"] != nil || len(again["approvers"].([]any)) != 2 {
		t.Errorf("the rule for every type reads %v", again)
	}
	s.ruleID = rule["id"].(string)
	if rules := list(t, want(t, s.carol.get(t, s.rules), http.StatusOK, "list rules"), "rules"); len(rules) != 1 {
		t.Errorf("a reader sees rules %v", rules)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'approval_rule.saved' AND target_id = $1`, s.ruleID); n != 2 {
		t.Errorf("%d audit entries for two saves of the rule", n)
	}
}

func (s *reviewScene) request(t *testing.T) {
	refuse(t, "asking for a revision that moved on", s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 2}), http.StatusConflict, "revision_moved_on")
	refuse(t, "a reader asks", s.carol.post(t, s.req+"/reviews", map[string]any{"revision": 1}), http.StatusForbidden, "forbidden")
	fieldError(t, s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 1, "comment": string(make([]rune, 501))}), "comment")
	made := obj(t, want(t, s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 1, "comment": "Ready to sign off."}), http.StatusCreated, "ask"), "review")
	s.reviewID = made["id"].(string)
	if made["state"] != "pending" || made["revision"] != float64(1) || made["required"] != float64(2) || len(made["approvers"].([]any)) != 2 ||
		made["requestedByName"] != "A member" || made["can"].(map[string]any)["withdraw"] != true || made["can"].(map[string]any)["decide"] != false {
		t.Errorf("the review reads %v", made)
	}
	refuse(t, "a second review", s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 1}), http.StatusConflict, "review_pending")
	section := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"kind": "section", "title": "Scope"}), http.StatusCreated, "write a section"), "requirement")
	refuse(t, "a section's review", s.bob.post(t, "/api/v1/requirements/"+section["key"].(string)+"/reviews", map[string]any{"revision": 1}),
		http.StatusConflict, "section_not_reviewed")
	approval := obj(t, want(t, s.carol.get(t, s.req+"/approval"), http.StatusOK, "read the approval"), "approval")
	if approval["pendingReviewId"] != s.reviewID || approval["approvedRevision"] != nil || approval["current"] != false {
		t.Errorf("the approval reads %v", approval)
	}
	if got := obj(t, want(t, s.carol.get(t, s.req), http.StatusOK, "read R1"), "requirement")["approval"].(map[string]any); got["pendingReviewId"] != s.reviewID {
		t.Errorf("the requirement's approval reads %v", got)
	}
}

func (s *reviewScene) inbox(t *testing.T) {
	waiting := func(c *client) []any {
		return list(t, want(t, c.get(t, "/api/v1/reviews/waiting"), http.StatusOK, "list the inbox"), "reviews")
	}
	for name, c := range map[string]*client{"dave": s.dave, "carol": s.carol} {
		if got := waiting(c); len(got) != 1 || got[0].(map[string]any)["id"] != s.reviewID {
			t.Errorf("%s's inbox is %v", name, got)
		}
	}
	if got := waiting(s.owner); len(got) != 0 {
		t.Errorf("the author, who may not approve, waits on %v", got)
	}
	s.h.drained(t)
	for name, id := range map[string]uuid.UUID{"dave": s.daveID, "carol": s.carolID} {
		if n := s.h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND review_id = $2 AND kind = 'review_requested'`, id, s.reviewID); n != 1 {
			t.Errorf("%s was told %d times of the review", name, n)
		}
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND review_id = $2`, s.ownerID, s.reviewID); n != 0 {
		t.Errorf("the author, who may not approve, was asked to")
	}
}

func (s *reviewScene) decide(t *testing.T) {
	decisions := "/api/v1/reviews/" + s.reviewID + "/decisions"
	refuse(t, "somebody not named decides", s.bob.post(t, decisions, map[string]any{"decision": "approve", "revision": 1}), http.StatusForbidden, "not_an_approver")
	refuse(t, "the author decides", s.owner.post(t, decisions, map[string]any{"decision": "approve", "revision": 1}), http.StatusForbidden, "own_revision")
	refuse(t, "a decision on another revision", s.dave.post(t, decisions, map[string]any{"decision": "approve", "revision": 2}), http.StatusConflict, "revision_moved_on")
	fieldError(t, s.dave.post(t, decisions, map[string]any{"decision": "maybe", "revision": 1}), "decision")
	got := obj(t, want(t, s.dave.post(t, decisions, map[string]any{"decision": "reject", "revision": 1, "comment": "Hm."}), http.StatusOK, "dave rejects"), "review")
	if got["state"] != "rejected" {
		t.Fatalf("one rejection leaves the review %v", got["state"])
	}
	refuse(t, "a decision on a closed review", s.dave.post(t, decisions, map[string]any{"decision": "approve", "revision": 1}), http.StatusConflict, "review_closed")

	s.reviewID = obj(t, want(t, s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 1}), http.StatusCreated, "ask again"), "review")["id"].(string)
	decisions = "/api/v1/reviews/" + s.reviewID + "/decisions"
	want(t, s.dave.post(t, decisions, map[string]any{"decision": "approve", "revision": 1}), http.StatusOK, "dave approves")
	got = obj(t, want(t, s.dave.post(t, decisions, map[string]any{"decision": "approve", "revision": 1, "comment": "Still yes."}), http.StatusOK, "dave approves again"), "review")
	if got["state"] != "pending" || got["approvals"] != float64(1) || len(got["decisions"].([]any)) != 1 {
		t.Fatalf("dave approving twice reads %v", got)
	}
	got = obj(t, want(t, s.carol.post(t, decisions, map[string]any{"decision": "approve", "revision": 1}), http.StatusOK, "carol approves"), "review")
	if got["state"] != "approved" || got["finishedAt"] == nil || got["approvals"] != float64(2) {
		t.Fatalf("two approvals of two leave the review %v", got)
	}
	approval := obj(t, want(t, s.carol.get(t, s.req+"/approval"), http.StatusOK, "read the approval"), "approval")
	if approval["pendingReviewId"] != nil || approval["approvedRevision"] != float64(1) || approval["current"] != true {
		t.Errorf("the approval reads %v", approval)
	}
	s.h.drained(t)
	for name, id := range map[string]uuid.UUID{"bob, who asked": s.bobID, "the author": s.ownerID} {
		if n := s.h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND review_id = $2 AND kind = 'reviewed'`, id, s.reviewID); n != 1 {
			t.Errorf("%s was told %d times of the approval", name, n)
		}
	}
	if reviews := list(t, want(t, s.carol.get(t, s.req+"/reviews"), http.StatusOK, "list reviews"), "reviews"); len(reviews) != 2 ||
		reviews[0].(map[string]any)["id"] != s.reviewID {
		t.Errorf("the reviews are %v", reviews)
	}
}

func (s *reviewScene) condition(t *testing.T) {
	submit, ok := transitionNamed(t, s.bob, s.reqKey, "Submit for review")
	if !ok {
		t.Fatal("bob is not offered Submit for review")
	}
	s.submitID = submit
	if _, err := s.h.super.Exec(t.Context(), `INSERT INTO transition_rule (transition_id, kind, type, position) VALUES ($1, 'condition', 'condition.approved', 9)`,
		submit); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.h.cleanupExec(t, s.h.super, `DELETE FROM transition_rule WHERE transition_id = $1 AND type = 'condition.approved'`, submit)
	})
	s.h.settle(t)
	if _, ok := transitionNamed(t, s.bob, s.reqKey, "Submit for review"); !ok {
		t.Error("an approved revision is not offered the transition that asks for approval")
	}
	s.revise(t, 1)
	s.reviewRevision = 2
	if _, ok := transitionNamed(t, s.bob, s.reqKey, "Submit for review"); ok {
		t.Error("a revision nobody approved is offered the transition that asks for approval")
	}
	refuse(t, "taking it anyway", s.bob.post(t, s.req+"/transitions", map[string]any{"transitionId": submit}), http.StatusForbidden, "forbidden")
	approval := obj(t, want(t, s.carol.get(t, s.req+"/approval"), http.StatusOK, "read the approval"), "approval")
	if approval["approvedRevision"] != float64(1) || approval["current"] != false {
		t.Errorf("after a new revision the approval reads %v", approval)
	}
}

func (s *reviewScene) supersede(t *testing.T) {
	id := obj(t, want(t, s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 2}), http.StatusCreated, "ask for revision 2"), "review")["id"].(string)
	approved := s.reviewID
	s.revise(t, 2)
	if got := s.state(t, id); got != "superseded" {
		t.Errorf("a review of an older revision is %s", got)
	}
	if got := s.state(t, approved); got != "approved" {
		t.Errorf("an approved review became %s", got)
	}
	if e := s.h.lastEvent(t, "review.closed", s.reqID); e["reviewId"] != id || e["state"] != "superseded" || e["actorId"] != s.bobID.String() {
		t.Errorf("the superseding event is %v", e)
	}
}

func (s *reviewScene) withdraw(t *testing.T) {
	id := obj(t, want(t, s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 3}), http.StatusCreated, "ask for revision 3"), "review")["id"].(string)
	withdraw := "/api/v1/reviews/" + id + "/withdraw"
	refuse(t, "an approver withdraws", s.dave.post(t, withdraw, nil), http.StatusForbidden, "forbidden")
	if got := obj(t, want(t, s.owner.post(t, withdraw, nil), http.StatusOK, "the administrator withdraws"), "review"); got["state"] != "withdrawn" {
		t.Errorf("a withdrawn review reads %v", got)
	}
	refuse(t, "withdrawing twice", s.bob.post(t, withdraw, nil), http.StatusConflict, "review_closed")
	if n := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'review.withdrawn' AND target_id = $1`, id); n != 1 {
		t.Errorf("%d audit entries for an administrator withdrawing", n)
	}
	mine := obj(t, want(t, s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 3}), http.StatusCreated, "ask again"), "review")["id"].(string)
	want(t, s.bob.post(t, "/api/v1/reviews/"+mine+"/withdraw", nil), http.StatusOK, "bob withdraws his own")
	if n := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'review.withdrawn' AND target_id = $1`, mine); n != 0 {
		t.Error("withdrawing one's own review was audited")
	}
}

func (s *reviewScene) removeRule(t *testing.T) {
	refuse(t, "a reader removes the rule", s.carol.delete(t, "/api/v1/approval-rules/"+s.ruleID), http.StatusForbidden, "forbidden")
	want(t, s.owner.delete(t, "/api/v1/approval-rules/"+s.ruleID), http.StatusNoContent, "remove the rule")
	refuse(t, "a review once the rule went", s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 3}), http.StatusConflict, "no_approval_rule")
	if got := obj(t, want(t, s.carol.get(t, "/api/v1/reviews/"+s.reviewID), http.StatusOK, "read the approved review"), "review"); len(got["approvers"].([]any)) != 2 {
		t.Errorf("a review lost its approvers with the rule: %v", got)
	}
}

func (s *reviewScene) refusals(t *testing.T) {
	refuse(t, "nobody reads the inbox", s.nobody.get(t, "/api/v1/reviews/waiting"), http.StatusUnauthorized, "unauthorized")
	refuse(t, "the rules of a project that is none", s.carol.get(t, "/api/v1/projects/NOPE"+s.key+"/approval-rules"), http.StatusNotFound, "not_found")
	refuse(t, "removing a rule that is none", s.owner.delete(t, "/api/v1/approval-rules/"+uuid.NewString()), http.StatusNotFound, "not_found")
	none := "/api/v1/requirements/" + s.key + "-R999"
	refuse(t, "the approval of a requirement that is none", s.carol.get(t, none+"/approval"), http.StatusNotFound, "not_found")
	refuse(t, "the reviews of a requirement that is none", s.carol.get(t, none+"/reviews"), http.StatusNotFound, "not_found")
	refuse(t, "a review that is none", s.carol.get(t, "/api/v1/reviews/"+uuid.NewString()), http.StatusNotFound, "not_found")
	refuse(t, "withdrawing a review that is none", s.bob.post(t, "/api/v1/reviews/"+uuid.NewString()+"/withdraw", nil), http.StatusNotFound, "not_found")
}

// The service refusing is not proof: straight through SQL as exciter_app, a review asks what its rule says of the
// current revision, only a frozen approver who did not write it decides, and a review only moves forward.
func TestRawSQLHoldsReviewsToTheirRules(t *testing.T) {
	s := newReviewScene(t, "REQ")
	h := s.h
	ctx := context.Background()
	project := s.projectID
	carol, bob, dave, owner := h.appConn(t, s.carolID), h.appConn(t, s.bobID), h.appConn(t, s.daveID), h.appConn(t, s.ownerID)

	if _, err := carol.Exec(ctx, `INSERT INTO approval_rule (project_id, required) VALUES ($1, 1)`, project); !isPolicyViolation(err) {
		t.Errorf("a reader set a rule: %v", err)
	}
	want(t, s.owner.put(t, s.rules, s.rule(1)), http.StatusOK, "set the rule")
	h.settle(t)
	if n := affects(t, carol, `UPDATE approval_rule SET required = 2 WHERE project_id = $1`, project); n != 0 {
		t.Error("a reader changed the rule")
	}
	insert := `INSERT INTO requirement_review (requirement_id, revision, requested_by, required, author_may_approve) VALUES ($1, $2, $3, $4, $5) RETURNING id`
	var id uuid.UUID
	if err := carol.QueryRow(ctx, insert, s.reqID, 1, s.carolID, 1, false).Scan(&id); !isPolicyViolation(err) {
		t.Errorf("a reader asked for a review: %v", err)
	}
	if err := bob.QueryRow(ctx, insert, s.reqID, 1, s.bobID, 2, false).Scan(&id); !isPolicyViolation(err) {
		t.Errorf("a review asked for more approvals than its rule: %v", err)
	}
	if err := bob.QueryRow(ctx, insert, s.reqID, 1, s.ownerID, 1, false).Scan(&id); !isPolicyViolation(err) {
		t.Errorf("bob asked in the owner's name: %v", err)
	}
	if err := bob.QueryRow(ctx, insert, s.reqID, 1, s.bobID, 1, true).Scan(&id); !isPolicyViolation(err) {
		t.Errorf("a review let the author approve against its rule: %v", err)
	}
	if err := bob.QueryRow(ctx, insert, s.reqID, 1, s.bobID, 1, false).Scan(&id); err != nil {
		t.Fatalf("bob could not ask as the rule says: %v", err)
	}
	if _, err := bob.Exec(ctx, `INSERT INTO review_subject (review_id, subject_type, user_id) VALUES ($1, 'user', $2)`, id, s.bobID); !isPolicyViolation(err) {
		t.Errorf("bob named himself an approver: %v", err)
	}
	for _, sub := range [][]any{{"user", s.daveID, nil}, {"group", nil, s.group}} {
		if _, err := bob.Exec(ctx, `INSERT INTO review_subject (review_id, subject_type, user_id, group_id) VALUES ($1, $2, $3, $4)`, id, sub[0], sub[1], sub[2]); err != nil {
			t.Fatalf("bob could not freeze the rule's approvers: %v", err)
		}
	}
	decide := `INSERT INTO review_decision (review_id, reviewer_id, decision) VALUES ($1, $2, $3)`
	if _, err := bob.Exec(ctx, decide, id, s.bobID, "approve"); !isPolicyViolation(err) {
		t.Errorf("somebody not named decided: %v", err)
	}
	if _, err := owner.Exec(ctx, decide, id, s.ownerID, "approve"); !isPolicyViolation(err) {
		t.Errorf("the author decided on their own revision: %v", err)
	}
	if _, err := dave.Exec(ctx, decide, id, s.carolID, "approve"); !isPolicyViolation(err) {
		t.Errorf("dave decided in carol's name: %v", err)
	}
	if _, err := bob.Exec(ctx, `UPDATE requirement_review SET state = 'approved', finished_at = now() WHERE id = $1`, id); !isPermissionDenied(err) {
		t.Errorf("a review was approved without approvals: %v", err)
	}
	if _, err := bob.Exec(ctx, `UPDATE requirement_review SET state = 'superseded', finished_at = now() WHERE id = $1`, id); !isPermissionDenied(err) {
		t.Errorf("a review was superseded without a new revision: %v", err)
	}
	if _, err := carol.Exec(ctx, `UPDATE requirement_review SET state = 'withdrawn', finished_at = now() WHERE id = $1`, id); !isPermissionDenied(err) {
		t.Errorf("an approver withdrew bob's review: %v", err)
	}
	if _, err := dave.Exec(ctx, decide, id, s.daveID, "approve"); err != nil {
		t.Fatalf("dave could not approve: %v", err)
	}
	if n := affects(t, dave, `UPDATE requirement_review SET state = 'approved', finished_at = now() WHERE id = $1`, id); n != 1 {
		t.Fatal("dave could not close the review his approval completed")
	}
	if _, err := bob.Exec(ctx, `UPDATE requirement_review SET state = 'pending', finished_at = NULL WHERE id = $1`, id); !isPermissionDenied(err) {
		t.Errorf("a closed review was opened again: %v", err)
	}
	if n := affects(t, dave, `UPDATE review_decision SET decision = 'reject' WHERE review_id = $1`, id); n != 0 {
		t.Error("a decision on a closed review changed")
	}
	if _, err := owner.Exec(ctx, `DELETE FROM requirement_review WHERE id = $1`, id); !isPermissionDenied(err) {
		t.Errorf("a review was deleted: %v", err)
	}

	submit, ok := transitionNamed(t, s.bob, s.reqKey, "Submit for review")
	if !ok {
		t.Fatal("bob is not offered Submit for review")
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO transition_rule (transition_id, kind, type, position) VALUES ($1, 'condition', 'condition.approved', 9)`, submit); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.cleanupExec(t, h.super, `DELETE FROM transition_rule WHERE transition_id = $1 AND type = 'condition.approved'`, submit)
	})
	s.revise(t, 1)
	h.settle(t)
	if _, err := bob.Exec(ctx, `SELECT requirement_transition($1, $2)`, s.reqID, submit); !refusedBy(err, "requirement_transition_approved") {
		t.Errorf("a transition asking for approval went without it: %v", err)
	}
}
