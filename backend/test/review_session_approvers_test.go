//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// A session's approvers must reach somebody who may decide each requirement it pins (#463): when it opens, when a
// round pins anew, and straight through SQL, and the quorum counts only them.

// deactivate switches somebody's account off behind the policies' back, keeping their group memberships.
func (h *harness) deactivate(t *testing.T, id uuid.UUID) {
	t.Helper()
	if _, err := h.super.Exec(t.Context(), `UPDATE app_user SET is_active = false WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
}

// Approvers who reach nobody who may decide would pin reviews that can never be decided, so the session is refused,
// and the database refuses the pin too, and a round that finds them so.
func TestASessionsApproversMustReachSomebody(t *testing.T) {
	s := newSessionScene(t, "RSN")
	approvers := func(group uuid.UUID) map[string]any {
		return s.body(map[string]any{"participants": []map[string]any{{"subjectType": "group", "groupId": group, "role": "approver"}}})
	}
	empty := s.h.makeGroup(t, "Nobody")
	s.h.grantGroupAs(t, s.projectID, empty, "reviewer")
	goneID := s.h.addPerson(t, "member")
	s.h.deactivate(t, goneID)
	gone := s.h.makeGroup(t, "Only the deactivated", goneID)
	s.h.grantGroupAs(t, s.projectID, gone, "reviewer")
	alone := s.h.makeGroup(t, "Only the author", s.ownerID)
	s.h.grantGroupAs(t, s.projectID, alone, "reviewer")

	for name, group := range map[string]uuid.UUID{"an empty group": empty, "a group of deactivated members": gone} {
		if msg := fieldError(t, s.open(t, s.bob, approvers(group)), "participants"); !strings.Contains(msg, "reach nobody") || strings.Contains(msg, "author") {
			t.Errorf("%s: the refusal says %q", name, msg)
		}
	}
	if msg := fieldError(t, s.open(t, s.bob, approvers(alone)), "participants"); !strings.Contains(msg, "but the author") || !strings.Contains(msg, s.req("3")) {
		t.Errorf("a group of the requirements' author: the refusal says %q", msg)
	}
	if n := s.sessionCount(t); n != 0 {
		t.Errorf("the refusals opened %d sessions", n)
	}

	erinID := s.h.addPerson(t, "member")
	if _, err := s.h.super.Exec(t.Context(), `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, empty, erinID); err != nil {
		t.Fatal(err)
	}
	session := obj(t, want(t, s.open(t, s.bob, approvers(empty)), http.StatusCreated, "a member makes the group reach somebody"), "session")
	id := session["id"].(string)
	pinR4 := func(what string) {
		t.Helper()
		if _, err := s.h.appConn(t, s.bobID).Exec(t.Context(), `SELECT review_session_pin($1, $2)`, id, s.r4ID); !refusedBy(err, "review_session_approvers") {
			t.Errorf("%s: a requirement was pinned for approvers who reach nobody: %v", what, err)
		}
	}

	// The group is emptied after the session opened, then its member comes back deactivated, as a round would find it.
	if _, err := s.h.super.Exec(t.Context(), `DELETE FROM group_member WHERE group_id = $1`, empty); err != nil {
		t.Fatal(err)
	}
	pinR4("an emptied group")
	if _, err := s.h.super.Exec(t.Context(), `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, empty, erinID); err != nil {
		t.Fatal(err)
	}
	s.h.deactivate(t, erinID)
	pinR4("a group of deactivated members")

	before := s.sessionAsItIs(t, id)
	s.revise(t, s.req("3"), int(itemsOf(t, session)[s.req("3")]["revision"].(float64)))
	round := s.bob.post(t, sessionPath(id)+"/rounds", map[string]any{})
	refuse(t, "a round its approvers reach nobody for", round, http.StatusConflict, "approvers_reach_nobody")
	if msg, _ := obj(t, round, "error")["message"].(string); !strings.Contains(msg, s.req("3")) || strings.Contains(msg, "author") {
		t.Errorf("the round's refusal says %q", msg)
	}
	if after := s.sessionAsItIs(t, id); after != before {
		t.Errorf("the refused round left the session %s, was %s", after, before)
	}
}

// Nobody approves their own revision, so a round that finds the only approver the author of what it pins is refused
// with a sentence that says so.
func TestARoundWhoseOnlyApproverRevisedIsRefused(t *testing.T) {
	s := newSessionScene(t, "RSA")
	eveID := s.h.addPerson(t, "member")
	s.h.grantAs(t, s.projectID, eveID, "editor")
	s.h.grantAs(t, s.projectID, eveID, "reviewer")
	eve := s.owner.api.as(t, eveID)
	session := obj(t, want(t, s.open(t, s.bob, s.body(map[string]any{"participants": []map[string]any{{"subjectType": "user", "userId": eveID, "role": "approver"}}})),
		http.StatusCreated, "bob opens it with eve approving"), "session")
	id := session["id"].(string)
	before := s.sessionAsItIs(t, id)

	want(t, eve.patch(t, "/api/v1/requirements/"+s.req("3"), map[string]any{"title": "Brake lights follow the pedal closely",
		"baseRevision": itemsOf(t, session)[s.req("3")]["revision"]}), http.StatusOK, "eve revises R3")
	round := s.bob.post(t, sessionPath(id)+"/rounds", map[string]any{})
	refuse(t, "a round only the author approves", round, http.StatusConflict, "approvers_reach_nobody")
	if msg, _ := obj(t, round, "error")["message"].(string); !strings.Contains(msg, s.req("3")) || !strings.Contains(msg, "author") {
		t.Errorf("the round's refusal says %q", msg)
	}
	if after := s.sessionAsItIs(t, id); after != before {
		t.Errorf("the refused round left the session %s, was %s", after, before)
	}
}

// The quorum is a share of the people who may decide, so a deactivated member of an approver group does not raise it.
func TestASessionsQuorumCountsOnlyWhoMayDecide(t *testing.T) {
	s := newSessionScene(t, "RSQ")
	aliceID, goneID := s.h.addPerson(t, "member"), s.h.addPerson(t, "member")
	s.h.deactivate(t, goneID)
	group := s.h.makeGroup(t, "Half gone", aliceID, goneID)
	s.h.grantGroupAs(t, s.projectID, group, "reviewer")
	session := obj(t, want(t, s.open(t, s.bob, s.body(map[string]any{"quorum": 100,
		"participants": []map[string]any{{"subjectType": "group", "groupId": group, "role": "approver"}}})), http.StatusCreated, "bob opens it"), "session")
	for key, item := range itemsOf(t, session) {
		if required := item["review"].(map[string]any)["required"]; required != float64(1) {
			t.Errorf("%s requires %v approvals of the one person who may decide", key, required)
		}
	}
}
