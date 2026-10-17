//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Review sessions (#204): opened over a section, decided requirement by requirement, moved on in rounds and closed
// with a report and a baseline of what was approved; the database holds a session's reviews to it.

// sessionScene is a baseline scene with a second requirement in the section, R4.
type sessionScene struct {
	*baselineScene
	r4ID string
}

func newSessionScene(t *testing.T, prefix string) *sessionScene {
	t.Helper()
	s := &sessionScene{baselineScene: newBaselineScene(t, prefix)}
	s.h.grantAs(t, s.projectID, s.carolID, "reviewer")
	r4 := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Brake lights dim at night",
		"place": place(s.sectionID, s.r3ID)}), http.StatusCreated, "write R4"), "requirement")
	s.r4ID = r4["id"].(string)
	return s
}

func (s *sessionScene) open(t *testing.T, c *client, body map[string]any) response {
	t.Helper()
	return c.post(t, "/api/v1/projects/"+s.key+"/review-sessions", body)
}

func (s *sessionScene) body(over map[string]any) map[string]any {
	body := map[string]any{
		"title": "Braking review",
		"scope": map[string]any{"kind": "section", "rootKey": s.req("2")},
		"participants": []map[string]any{
			{"subjectType": "user", "userId": s.daveID, "role": "approver"},
			{"subjectType": "user", "userId": s.carolID, "role": "reviewer"},
		},
		"deadline": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339),
	}
	for k, v := range over {
		body[k] = v
	}
	return body
}

func itemsOf(t *testing.T, session map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, each := range session["items"].([]any) {
		item := each.(map[string]any)
		out[item["requirement"].(map[string]any)["key"].(string)] = item
	}
	return out
}

func TestAReviewSessionGoesFromOpeningToItsReport(t *testing.T) {
	s := newSessionScene(t, "RSN")
	var id string
	var session map[string]any

	t.Run("an editor opens it over a section, and what is wrong is refused", func(t *testing.T) {
		refuse(t, "a reader opens one", s.open(t, s.carol, s.body(nil)), http.StatusForbidden, "forbidden")
		fieldError(t, s.open(t, s.bob, s.body(map[string]any{"participants": []map[string]any{{"subjectType": "user", "userId": s.daveID, "role": "reviewer"}}})), "participants")
		fieldError(t, s.open(t, s.bob, s.body(map[string]any{"participants": []map[string]any{{"subjectType": "user", "userId": s.bobID, "role": "approver"}}})), "participants")
		reader := s.h.addPerson(t, "member")
		fieldError(t, s.open(t, s.bob, s.body(map[string]any{"participants": []map[string]any{{"subjectType": "user", "userId": s.daveID, "role": "approver"},
			{"subjectType": "user", "userId": reader, "role": "reviewer"}}})), "participants")
		fieldError(t, s.open(t, s.bob, s.body(map[string]any{"scope": map[string]any{"kind": "section", "rootKey": s.req("1")}})), "scope")
		fieldError(t, s.open(t, s.bob, s.body(map[string]any{"deadline": time.Now().Add(time.Minute).UTC().Format(time.RFC3339)})), "deadline")
		session = obj(t, want(t, s.open(t, s.bob, s.body(nil)), http.StatusCreated, "bob opens it"), "session")
		id = session["id"].(string)
		items := itemsOf(t, session)
		if len(items) != 2 || items[s.req("3")] == nil || items[s.req("4")] == nil || session["round"] != float64(1) || session["state"] != "open" {
			t.Fatalf("the session reads %v", session)
		}
		review := items[s.req("3")]["review"].(map[string]any)
		if review["required"] != float64(1) || review["session"].(map[string]any)["id"] != id {
			t.Errorf("R3's review reads %v", review)
		}
		renamed := obj(t, want(t, s.bob.patch(t, "/api/v1/review-sessions/"+id, map[string]any{"title": "Braking review", "description": "Read it before Friday."}),
			http.StatusOK, "bob says what the session is for"), "session")
		if renamed["description"] != "Read it before Friday." {
			t.Errorf("the changed session reads %v", renamed)
		}
		refuse(t, "a reader changes it", s.carol.patch(t, "/api/v1/review-sessions/"+id, map[string]any{"title": "Mine"}), http.StatusForbidden, "forbidden")
		if n := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'review_session.opened' AND target_id = $1`, id); n != 1 {
			t.Errorf("%d audit entries for opening, want 1", n)
		}
	})

	t.Run("the participants are told and find it in their inbox", func(t *testing.T) {
		s.h.drained(t)
		for name, who := range map[string]uuid.UUID{"dave": s.daveID, "carol": s.carolID} {
			if n := s.h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND session_id = $2 AND kind = 'session_invited'`, who, id); n != 1 {
				t.Errorf("%s was told %d times of the session", name, n)
			}
		}
		mine := list(t, want(t, s.dave.get(t, "/api/v1/reviews/sessions"), http.StatusOK, "dave's sessions"), "sessions")
		if len(mine) != 1 || mine[0].(map[string]any)["waiting"] != float64(2) {
			t.Errorf("dave's sessions read %v", mine)
		}
		told := list(t, want(t, s.dave.get(t, "/api/v1/notifications"), http.StatusOK, "dave's notifications"), "notifications")
		if len(told) == 0 || told[0].(map[string]any)["session"].(map[string]any)["title"] != "Braking review" {
			t.Errorf("dave's notifications read %v", told)
		}
		entries := list(t, want(t, s.dave.get(t, "/api/v1/projects/"+s.key+"/spec?session="+id+"&waiting=true&changes=round"), http.StatusOK, "the document in review mode"), "entries")
		if len(entries) != 3 {
			t.Errorf("the document of the session holds %d entries, want the section and its two requirements", len(entries))
		}
	})

	t.Run("an approver decides, and a reviewer comments", func(t *testing.T) {
		items := itemsOf(t, session)
		for _, key := range []string{s.req("3"), s.req("4")} {
			review := items[key]["review"].(map[string]any)
			decision := "approve"
			if key == s.req("4") {
				decision = "reject"
			}
			want(t, s.dave.post(t, "/api/v1/reviews/"+review["id"].(string)+"/decisions", map[string]any{"decision": decision, "revision": 1}),
				http.StatusOK, "dave decides "+key)
		}
		want(t, s.carol.post(t, "/api/v1/requirements/"+s.req("4")+"/comments", commentBody("Dim how far?")), http.StatusCreated, "carol comments")
		session = obj(t, want(t, s.bob.get(t, "/api/v1/review-sessions/"+id), http.StatusOK, "read the session"), "session")
		counts := session["counts"].(map[string]any)
		if counts["approved"] != float64(1) || counts["rejected"] != float64(1) || counts["pending"] != float64(0) {
			t.Errorf("the counts read %v", counts)
		}
		for _, each := range session["progress"].([]any) {
			p := each.(map[string]any)
			name := p["person"].(map[string]any)["id"]
			if name == s.daveID.String() && p["decided"] != float64(2) {
				t.Errorf("dave's progress reads %v", p)
			}
			if name == s.carolID.String() && (p["commented"] != float64(1) || p["role"] != "reviewer") {
				t.Errorf("carol's progress reads %v", p)
			}
		}
	})

	t.Run("a new round pins anew only what was revised", func(t *testing.T) {
		refuse(t, "a round with nothing revised", s.bob.post(t, "/api/v1/review-sessions/"+id+"/rounds", map[string]any{}), http.StatusConflict, "nothing_revised")
		refuse(t, "a reader starts a round", s.carol.post(t, "/api/v1/review-sessions/"+id+"/rounds", map[string]any{}), http.StatusForbidden, "forbidden")
		want(t, s.bob.patch(t, "/api/v1/requirements/"+s.req("4"), map[string]any{"title": "Brake lights dim to half at night", "baseRevision": 1}),
			http.StatusOK, "bob revises R4")
		session = obj(t, want(t, s.bob.post(t, "/api/v1/review-sessions/"+id+"/rounds", map[string]any{"comment": "R4 says how far now."}),
			http.StatusOK, "bob starts round 2"), "session")
		items := itemsOf(t, session)
		if session["round"] != float64(2) || items[s.req("4")]["round"] != float64(2) || items[s.req("4")]["revision"] != float64(2) ||
			items[s.req("3")]["round"] != float64(1) || items[s.req("3")]["review"].(map[string]any)["state"] != "approved" {
			t.Fatalf("round 2 reads %v", items)
		}
		spec := want(t, s.dave.get(t, "/api/v1/projects/"+s.key+"/spec?session="+id+"&changes=round"), http.StatusOK, "the round's changes")
		for _, each := range list(t, spec, "entries") {
			e := each.(map[string]any)
			if e["node"].(map[string]any)["key"] == s.req("4") {
				if c := e["change"].(map[string]any); c["kind"] != "changed" || c["from"] != float64(1) || c["to"] != float64(2) {
					t.Errorf("R4 compared with its last round reads %v", c)
				}
			}
		}
	})

	t.Run("closing withdraws what is pending, takes a baseline of what was approved, and the report says it all", func(t *testing.T) {
		refuse(t, "a reader closes it", s.carol.post(t, "/api/v1/review-sessions/"+id+"/close", map[string]any{}), http.StatusForbidden, "forbidden")
		session = obj(t, want(t, s.bob.post(t, "/api/v1/review-sessions/"+id+"/close", map[string]any{"baseline": map[string]any{"name": "Braking agreed"}}),
			http.StatusOK, "bob, an editor, closes it with a baseline"), "session")
		items := itemsOf(t, session)
		if session["state"] != "closed" || session["baselineId"] == nil || items[s.req("4")]["review"].(map[string]any)["state"] != "withdrawn" {
			t.Fatalf("the closed session reads %v", session)
		}
		var keys []string
		for _, each := range list(t, want(t, s.bob.get(t, "/api/v1/baselines/"+session["baselineId"].(string)+"/spec"), http.StatusOK, "read the session's baseline"), "entries") {
			keys = append(keys, each.(map[string]any)["requirement"].(map[string]any)["key"].(string))
		}
		if strings.Join(keys, ",") != s.req("2")+","+s.req("3") {
			t.Errorf("the baseline holds %v, want the section and the approved R3", keys)
		}
		refuse(t, "a change of a closed session", s.bob.patch(t, "/api/v1/review-sessions/"+id, map[string]any{"title": "Again"}), http.StatusConflict, "session_closed")
		report := obj(t, want(t, s.bob.get(t, "/api/v1/review-sessions/"+id+"/report"), http.StatusOK, "read the report"), "report")
		if len(report["rounds"].([]any)) != 2 || report["baselineName"] != "Braking agreed" {
			t.Errorf("the report reads %v", report)
		}
		for _, each := range report["items"].([]any) {
			item := each.(map[string]any)
			if item["requirement"].(map[string]any)["key"] == s.req("4") && len(item["rounds"].([]any)) != 2 {
				t.Errorf("R4's report reads %v", item)
			}
		}
		word := want(t, s.bob.get(t, "/api/v1/review-sessions/"+id+"/report.docx"), http.StatusOK, "download the report as Word")
		if !strings.HasPrefix(string(word.Raw), "PK") {
			t.Errorf("the Word report is no zip: %.40q", word.Raw)
		}
		pdf := want(t, s.bob.get(t, "/api/v1/review-sessions/"+id+"/report.pdf"), http.StatusOK, "print the report")
		if !strings.HasPrefix(string(pdf.Raw), "%PDF-") {
			t.Errorf("the printed report is no PDF: %.40q", pdf.Raw)
		}
		unknown := "/api/v1/review-sessions/" + uuid.NewString()
		for _, path := range []string{unknown, unknown + "/report", unknown + "/report.docx", unknown + "/report.pdf"} {
			refuse(t, "a session that is not there", s.bob.get(t, path), http.StatusNotFound, "not_found")
		}
		refuse(t, "sessions in no state there is", s.carol.get(t, "/api/v1/projects/"+s.key+"/review-sessions?state=done"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a page too long of one's sessions", s.carol.get(t, "/api/v1/reviews/sessions?limit=100000"), http.StatusUnprocessableEntity, "validation_failed")
		listed := list(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/review-sessions?state=closed"), http.StatusOK, "list the closed sessions"), "sessions")
		if len(listed) != 1 || listed[0].(map[string]any)["state"] != "closed" {
			t.Errorf("the closed sessions read %v", listed)
		}
		for _, action := range []string{"review_session.round_started", "review_session.closed"} {
			if n := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = $1 AND target_id = $2`, action, id); n != 1 {
				t.Errorf("%d audit entries %s, want 1", n, action)
			}
		}
	})
}

func TestRawSQLHoldsReviewSessions(t *testing.T) {
	s := newSessionScene(t, "RSQ")
	ctx := context.Background()
	session := obj(t, want(t, s.open(t, s.bob, s.body(nil)), http.StatusCreated, "bob opens a session"), "session")
	id := uuid.MustParse(session["id"].(string))
	reviewID := uuid.MustParse(itemsOf(t, session)[s.req("3")]["review"].(map[string]any)["id"].(string))
	bob, dave, owner := s.h.appConn(t, s.bobID), s.h.appConn(t, s.daveID), s.h.appConn(t, s.ownerID)

	if _, err := owner.Exec(ctx, `INSERT INTO review_decision (review_id, reviewer_id, decision) VALUES ($1, $2, 'approve')`, reviewID, s.ownerID); !isPolicyViolation(err) {
		t.Errorf("somebody who takes no part decided a session's review: %v", err)
	}
	if _, err := bob.Exec(ctx, `INSERT INTO requirement_review (requirement_id, revision, requested_by, required, author_may_approve, session_id)
		VALUES ($1, 1, $2, 1, false, $3)`, s.r4ID, s.bobID, id); err == nil {
		t.Error("a session's review was asked straight through SQL")
	}
	if _, err := dave.Exec(ctx, `SELECT review_session_pin($1, $2)`, id, s.reqID); !refusedBy(err, "review_session_pin") {
		t.Errorf("an approver pinned a requirement: %v", err)
	}
	if _, err := bob.Exec(ctx, `UPDATE review_session SET quorum = 50 WHERE id = $1`, id); err == nil {
		t.Error("the moderator changed the quorum of an open session")
	}
	if _, err := bob.Exec(ctx, `UPDATE review_session SET state = 'closed', closed_at = now() WHERE id = $1`, id); err != nil {
		t.Fatalf("the moderator closes the session: %v", err)
	}
	if _, err := dave.Exec(ctx, `INSERT INTO review_decision (review_id, reviewer_id, decision) VALUES ($1, $2, 'approve')`, reviewID, s.daveID); !isPolicyViolation(err) {
		t.Errorf("an approver decided a review of a closed session: %v", err)
	}
	if _, err := bob.Exec(ctx, `UPDATE review_session SET title = 'Again' WHERE id = $1`, id); !refusedBy(err, "review_session_closed") {
		t.Errorf("a closed session was renamed: %v", err)
	}
}

// Each review needs the quorum's share of the people the approvers reach, groups by their members, and a requirement
// that waits on a review already is left out, saying why.
func TestASessionsQuorumIsAShareOfItsApprovers(t *testing.T) {
	s := newSessionScene(t, "RSU")
	want(t, s.open(t, s.bob, s.body(nil)), http.StatusCreated, "a first session pins R3 and R4")
	baselineID := obj(t, want(t, s.owner.post(t, s.baselines, map[string]any{"name": "Everything"}), http.StatusCreated, "take a baseline"), "baseline")["id"]
	// The owner wrote R1 and is not counted, so the group's other two and dave make three.
	group := s.h.makeGroup(t, "Approvers", s.ownerID, s.carolID, s.bobID)
	session := obj(t, want(t, s.open(t, s.bob, s.body(map[string]any{
		"scope":  map[string]any{"kind": "baseline", "baselineId": baselineID},
		"quorum": 50,
		"participants": []map[string]any{
			{"subjectType": "user", "userId": s.daveID, "role": "approver"},
			{"subjectType": "group", "groupId": group, "role": "approver"},
		},
	})), http.StatusCreated, "a second session over the baseline"), "session")
	items := itemsOf(t, session)
	if len(items) != 1 || items[s.req("1")] == nil {
		t.Fatalf("the second session holds %v, want R1 alone", items)
	}
	if required := items[s.req("1")]["review"].(map[string]any)["required"]; required != float64(2) {
		t.Errorf("half of three approvers asks %v approvals, want 2", required)
	}
	excluded := session["excluded"].([]any)
	if len(excluded) != 2 || excluded[0].(map[string]any)["reason"] != "review_pending" {
		t.Errorf("the second session left out %v", excluded)
	}
}
