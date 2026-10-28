//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/testexec"
)

// told counts the notifications of a kind a person has about an execution, once the outbox has drained.
func (s *executionScene) told(t *testing.T, person uuid.UUID, execution, kind string) int {
	t.Helper()
	s.f.h.drained(t)
	return s.f.h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND execution_id = $2 AND kind = $3`, person, execution, kind)
}

// reminders counts the reminders the outbox holds of an execution.
func (s *executionScene) reminders(t *testing.T, execution string) int {
	t.Helper()
	return s.f.h.countRows(t, `SELECT count(*) FROM outbox_event WHERE topic = $1 AND payload ->> 'executionId' = $2`, testexec.TopicReminder, execution)
}

// startCurrent starts an execution of the scene's plan at the cases' current revisions, plain testing.
func (s *executionScene) startCurrent(t *testing.T, title string) string {
	t.Helper()
	return obj(t, want(t, s.f.owner.post(t, "/api/v1/projects/"+s.key+"/test-executions", map[string]any{"title": title, "planId": s.plan,
		"testers": []any{tester(s.f.plain.user)}}), http.StatusCreated, "start "+title), "execution")["id"].(string)
}

// executionIDs are the ids of a list of executions, in its order.
func executionIDs(t *testing.T, r response) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, r, "executions") {
		out = append(out, each.(map[string]any)["id"].(string))
	}
	return out
}

// An aborted execution never changes again and counts for nothing: the case it passed is no longer verified by it,
// and its testers are told once that it ended (#324).
func TestAnAbortedExecutionCountsForNothing(t *testing.T) {
	s := newExecutionScene(t, "TXB")
	owner, plain, reader := s.f.owner, s.f.plain, s.readerOf(t)
	r1, r2 := s.key+"-R1", s.key+"-R2"
	want(t, owner.put(t, "/api/v1/projects/"+s.key+"/coverage-rules", map[string]any{"verified": true, "verifiedFrom": []string{"runs"}}),
		http.StatusOK, "count test executions")
	verified := func(t *testing.T, key string) string {
		t.Helper()
		for _, each := range list(t, want(t, reader.get(t, "/api/v1/projects/"+s.key+"/coverage"), http.StatusOK, "read the coverage"), "rows") {
			if r := each.(map[string]any); r["requirement"].(map[string]any)["key"] == key {
				return r["verified"].(string)
			}
		}
		t.Fatalf("the coverage has no row of %s", key)
		return ""
	}
	query := func(t *testing.T, q string) []string {
		t.Helper()
		return rowKeys(t, want(t, reader.get(t, "/api/v1/projects/"+s.key+"/requirements?query="+url.QueryEscape(q)), http.StatusOK, "query "+q))
	}
	current := s.startCurrent(t, "Current")
	base := "/api/v1/test-executions/" + current
	passCase(t, plain, current, r2, 1)
	passCase(t, plain, current, r2, 2)
	if got := verified(t, r1); got != "full" {
		t.Fatalf("a case passed at its current revision verifies %s", got)
	}
	sameList(t, "casesResult = passed", query(t, "casesResult = passed"), r1)

	t.Run("a close comment over the limit is refused and the execution stays open", func(t *testing.T) {
		refuse422(t, "a long comment", owner.post(t, base+"/close", map[string]any{"abort": true, "comment": strings.Repeat("c", testexec.MaxDescriptionLength+1)}),
			"comment")
		if e := obj(t, want(t, owner.get(t, base), http.StatusOK, "read it"), "execution"); e["state"] != "open" {
			t.Errorf("the refused close left it %v", e["state"])
		}
	})

	t.Run("aborted, it takes no result and is not closed again", func(t *testing.T) {
		e := obj(t, want(t, owner.post(t, base+"/close", map[string]any{"abort": true, "comment": "Wrong build."}), http.StatusOK, "abort it"), "execution")
		if e["state"] != "aborted" || e["closeComment"] != "Wrong build." || e["closedAt"] == nil {
			t.Errorf("the aborted execution reads %v", e)
		}
		refuse(t, "a result after aborting", plain.put(t, base+"/cases/"+r2+"/result", map[string]any{"iteration": 1, "outcome": "failed"}),
			http.StatusConflict, "execution_closed")
		refuse(t, "closing it again", owner.post(t, base+"/close", map[string]any{}), http.StatusConflict, "execution_closed")
		refuse(t, "aborting it again", owner.post(t, base+"/close", map[string]any{"abort": true}), http.StatusConflict, "execution_closed")
		if outcome, _, _ := runOf(t, plain, current, r2, 1); outcome != "passed" {
			t.Errorf("the aborted execution's result reads %v", outcome)
		}
	})

	t.Run("its passed case no longer verifies", func(t *testing.T) {
		sameList(t, "casesResult = passed", query(t, "casesResult = passed"))
		sameList(t, "runResult = passed", query(t, "runResult = passed"))
		if got := verified(t, r1); got != "partial" {
			t.Errorf("after aborting %s verifies %s, want partial", r1, got)
		}
	})

	t.Run("the case still lists the aborted result, marked aborted", func(t *testing.T) {
		states := map[string]any{}
		for _, each := range list(t, want(t, reader.get(t, "/api/v1/requirements/"+r2+"/test-executions"), http.StatusOK, "the case's results"), "results") {
			r := each.(map[string]any)
			if r["executionId"] == current {
				states[r["state"].(string)] = r["outcome"]
			}
		}
		if len(states) != 1 || states["aborted"] != "passed" {
			t.Errorf("the case lists the aborted execution as %v", states)
		}
	})

	t.Run("each tester is told once that it ended", func(t *testing.T) {
		if n := s.told(t, plain.user, current, "execution_closed"); n != 1 {
			t.Errorf("the tester was told %d times", n)
		}
	})
}

// A project's executions narrow to a state, and a person's own list holds only those they test or manage (#324).
func TestExecutionsAreListedByStateAndToThoseInvolved(t *testing.T) {
	s := newExecutionScene(t, "TXL")
	owner, plain := s.f.owner, s.f.plain
	bystander := s.f.api.as(t, s.f.h.addPerson(t, "member"))
	aborted, open := s.startCurrent(t, "To abort"), s.startCurrent(t, "Still open")
	want(t, owner.post(t, "/api/v1/test-executions/"+s.execution+"/close", map[string]any{}), http.StatusOK, "close the first")
	want(t, owner.post(t, "/api/v1/test-executions/"+aborted+"/close", map[string]any{"abort": true}), http.StatusOK, "abort the second")
	for state, ids := range map[string][]string{"open": {open}, "closed": {s.execution}, "aborted": {aborted}} {
		sameList(t, state, executionIDs(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/test-executions?state="+state), http.StatusOK, "list "+state)), ids...)
	}
	sameList(t, "every state, open first", executionIDs(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/test-executions"), http.StatusOK, "list all"))[:1], open)
	mine := executionIDs(t, want(t, plain.get(t, "/api/v1/test-executions/mine"), http.StatusOK, "the tester's own"))
	for _, id := range []string{open, aborted, s.execution} {
		if !slices.Contains(mine, id) {
			t.Errorf("the tester's own list lacks %s: %v", id, mine)
		}
	}
	for _, id := range executionIDs(t, want(t, bystander.get(t, "/api/v1/test-executions/mine"), http.StatusOK, "a bystander's own")) {
		if id == open || id == aborted || id == s.execution {
			t.Errorf("a person who neither tests nor manages %s lists it", id)
		}
	}
}

// An open execution due within ReminderLead is reminded once, again once its due date moves, and a closed one
// never; its testers are told each time (#324).
func TestADueExecutionIsRemindedOnce(t *testing.T) {
	s := newExecutionScene(t, "TXD")
	owner, plain := s.f.owner, s.f.plain
	reminder := testexec.NewReminder(s.f.h.cluster, discard())
	remind := func(t *testing.T) {
		t.Helper()
		// The stack's worker runs the same reminder every minute, so either may mark the execution first.
		for range 2 {
			if _, err := reminder.Once(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
	closed := s.startCurrent(t, "Done early")
	// Closed and due in one statement, so the stack's own reminder never finds it open and due.
	if _, err := s.f.h.super.Exec(t.Context(), `UPDATE test_execution SET state = 'closed', closed_at = now(), due_at = now() + interval '2 hours' WHERE id = $1`,
		closed); err != nil {
		t.Fatal(err)
	}

	t.Run("due in two days it is not reminded yet", func(t *testing.T) {
		remind(t)
		if n := s.reminders(t, s.execution); n != 0 {
			t.Errorf("%d reminders of an execution due in two days", n)
		}
	})

	t.Run("due within a day it is reminded once, and a closed one never", func(t *testing.T) {
		want(t, owner.patch(t, "/api/v1/test-executions/"+s.execution, map[string]any{"dueAt": due(2 * time.Hour)}), http.StatusOK, "due in two hours")
		remind(t)
		if n := s.reminders(t, s.execution); n != 1 {
			t.Errorf("%d reminders, want 1", n)
		}
		if n := s.reminders(t, closed); n != 0 {
			t.Errorf("%d reminders of the closed execution", n)
		}
		if n := s.told(t, plain.user, s.execution, "execution_reminder"); n != 1 {
			t.Errorf("the tester was reminded %d times", n)
		}
	})

	t.Run("a new due date reminds once more", func(t *testing.T) {
		want(t, owner.patch(t, "/api/v1/test-executions/"+s.execution, map[string]any{"dueAt": due(3 * time.Hour)}), http.StatusOK, "due in three hours")
		remind(t)
		if n := s.reminders(t, s.execution); n != 2 {
			t.Errorf("%d reminders, want 2", n)
		}
		if n := s.told(t, plain.user, s.execution, "execution_reminder"); n != 2 {
			t.Errorf("the tester was reminded %d times", n)
		}
		if n := s.told(t, plain.user, closed, "execution_reminder"); n != 0 {
			t.Errorf("the tester was reminded %d times of the closed execution", n)
		}
	})
}

// A group tests an execution through its members: each may record, lists it as theirs and is told, while a person
// outside the group may do none of it (#324).
func TestAGroupTestsThroughItsMembers(t *testing.T) {
	s := newExecutionScene(t, "TXG")
	memberID, outsiderID := s.f.h.addPerson(t, "member"), s.f.h.addPerson(t, "member")
	member, outsider := s.f.api.as(t, memberID), s.f.api.as(t, outsiderID)
	group := s.f.h.makeGroup(t, "Brake testers", memberID)
	e := obj(t, want(t, s.f.owner.post(t, "/api/v1/projects/"+s.key+"/test-executions", map[string]any{"title": "By the group", "planId": s.plan,
		"testers": []any{map[string]any{"subjectType": "group", "groupId": group}}}), http.StatusCreated, "start with a group testing"), "execution")
	id := e["id"].(string)
	if testers := e["testers"].([]any); len(testers) != 1 || testers[0].(map[string]any)["groupId"] != group.String() {
		t.Errorf("the execution's testers: %v", testers)
	}
	result := "/api/v1/test-executions/" + id + "/cases/" + s.key + "-R2/result"

	if c := obj(t, want(t, member.get(t, "/api/v1/test-executions/"+id), http.StatusOK, "a member reads it"), "execution")["can"].(map[string]any); c["record"] != true {
		t.Errorf("a member of the group may not record: %v", c)
	}
	recorded := obj(t, want(t, member.put(t, result, map[string]any{"iteration": 1, "outcome": "passed"}), http.StatusOK, "a member records"), "case")
	if by := recorded["run"].(map[string]any)["recordedBy"].(map[string]any); by["id"] != memberID.String() {
		t.Errorf("the result is recorded by %v", by)
	}
	refuse(t, "somebody outside the group records", outsider.put(t, result, map[string]any{"iteration": 2, "outcome": "passed"}), http.StatusForbidden, "forbidden")
	if outcome, _, _ := runOf(t, member, id, s.key+"-R2", 2); outcome != nil {
		t.Errorf("the refused result reads %v", outcome)
	}
	if !slices.Contains(executionIDs(t, want(t, member.get(t, "/api/v1/test-executions/mine"), http.StatusOK, "a member's own")), id) {
		t.Error("a member of the group does not list the execution as theirs")
	}
	if slices.Contains(executionIDs(t, want(t, outsider.get(t, "/api/v1/test-executions/mine"), http.StatusOK, "an outsider's own")), id) {
		t.Error("somebody outside the group lists the execution as theirs")
	}
	if n := s.told(t, memberID, id, "execution_assigned"); n != 1 {
		t.Errorf("the member was told %d times that they test", n)
	}
	if n := s.told(t, outsiderID, id, "execution_assigned"); n != 0 {
		t.Errorf("somebody outside the group was told %d times", n)
	}
}
