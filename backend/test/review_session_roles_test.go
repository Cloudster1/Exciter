//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/review"
)

// Review sessions through every role, scope and limit (#317): only the moderator changes one, each scope and since
// pins what it says, each limit is refused with nothing written, and the reminder tells only who still decides.

// sessionPath is where a session is read and changed.
func sessionPath(id string) string { return "/api/v1/review-sessions/" + id }

// sessionAsItIs reads what a refusal must leave of a session: its title, description, round, state and baseline.
func (s *sessionScene) sessionAsItIs(t *testing.T, id string) string {
	t.Helper()
	got := obj(t, want(t, s.bob.get(t, sessionPath(id)), http.StatusOK, "read the session"), "session")
	return fmt.Sprintf("%v|%v|%v|%v|%v", got["title"], got["description"], got["round"], got["state"], got["baselineId"])
}

// itemKeys are the keys a session pins now, in outline order.
func itemKeys(session map[string]any) []string {
	var out []string
	for _, each := range session["items"].([]any) {
		out = append(out, each.(map[string]any)["requirement"].(map[string]any)["key"].(string))
	}
	return out
}

// sessionCount counts the sessions of the scene's project.
func (s *sessionScene) sessionCount(t *testing.T) int {
	t.Helper()
	return s.h.countRows(t, `SELECT count(*) FROM review_session WHERE project_id = $1`, s.projectID)
}

// revise gives a requirement a new title as its next revision, answering the revision it reached.
func (s *sessionScene) revise(t *testing.T, key string, base int) int {
	t.Helper()
	got := obj(t, want(t, s.bob.patch(t, "/api/v1/requirements/"+key, map[string]any{"title": "Brake lights " + uuid.NewString()[:6], "baseRevision": base}),
		http.StatusOK, "bob revises "+key), "requirement")
	return int(got["revision"].(float64))
}

func TestOnlyTheModeratorChangesASession(t *testing.T) {
	s := newSessionScene(t, "RSM")
	eveID := s.h.addPerson(t, "member")
	s.h.grantAs(t, s.projectID, eveID, "editor")
	eve := s.owner.api.as(t, eveID)
	id := obj(t, want(t, s.open(t, s.bob, s.body(nil)), http.StatusCreated, "bob opens a session"), "session")["id"].(string)
	before := s.sessionAsItIs(t, id)
	want(t, s.bob.patch(t, "/api/v1/requirements/"+s.req("3"), map[string]any{"title": "Brake lights follow the pedal at once", "baseRevision": 1}),
		http.StatusOK, "bob revises R3, for a round")

	t.Run("another editor changes, moves on and closes nothing", func(t *testing.T) {
		refuse(t, "eve renames it", eve.patch(t, sessionPath(id), map[string]any{"title": "Eve's review"}), http.StatusForbidden, "forbidden")
		refuse(t, "eve starts a round", eve.post(t, sessionPath(id)+"/rounds", map[string]any{}), http.StatusForbidden, "forbidden")
		refuse(t, "eve closes it", eve.post(t, sessionPath(id)+"/close", map[string]any{"baseline": map[string]any{"name": "Eve's"}}), http.StatusForbidden, "forbidden")
		if got := s.sessionAsItIs(t, id); got != before {
			t.Errorf("after eve's refusals the session reads %s, was %s", got, before)
		}
		if can := obj(t, want(t, eve.get(t, sessionPath(id)), http.StatusOK, "eve reads it"), "session")["can"].(map[string]any); can["edit"] != false || can["close"] != false {
			t.Errorf("eve is offered %v", can)
		}
	})

	t.Run("what the moderator gets wrong is refused on its field, and nothing changes", func(t *testing.T) {
		long := strings.Repeat("x", review.MaxCommentLength+1)
		fieldError(t, s.bob.patch(t, sessionPath(id), map[string]any{"title": " "}), "title")
		fieldError(t, s.bob.patch(t, sessionPath(id), map[string]any{"description": long}), "description")
		fieldError(t, s.bob.patch(t, sessionPath(id), map[string]any{"deadline": time.Now().Add(time.Minute).UTC().Format(time.RFC3339)}), "deadline")
		fieldError(t, s.bob.post(t, sessionPath(id)+"/rounds", map[string]any{"comment": long}), "comment")
		fieldError(t, s.bob.post(t, sessionPath(id)+"/close", map[string]any{"baseline": map[string]any{"name": " "}}), "name")
		refuse(t, "a session by no id", s.bob.patch(t, sessionPath("not-a-session"), map[string]any{"title": "Again"}), http.StatusBadRequest, "bad_request")
		if got := s.sessionAsItIs(t, id); got != before {
			t.Errorf("after the refusals the session reads %s, was %s", got, before)
		}
	})

	t.Run("closing with a baseline name the project has is refused, and the session stays open", func(t *testing.T) {
		want(t, s.owner.post(t, s.baselines, map[string]any{"name": "Release 1"}), http.StatusCreated, "take Release 1")
		counts := obj(t, want(t, s.bob.get(t, sessionPath(id)), http.StatusOK, "read the session"), "session")["counts"].(map[string]any)
		if counts["pending"] != float64(1) {
			t.Fatalf("before closing, R4 alone should wait, R3's review being superseded by its revision: %v", counts)
		}
		refuse(t, "a baseline name taken", s.bob.post(t, sessionPath(id)+"/close", map[string]any{"baseline": map[string]any{"name": "Release 1"}}),
			http.StatusConflict, "baseline_name_taken")
		got := obj(t, want(t, s.bob.get(t, sessionPath(id)), http.StatusOK, "read the session"), "session")
		if got["state"] != "open" || got["baselineId"] != nil || got["counts"].(map[string]any)["pending"] != counts["pending"] {
			t.Errorf("after the refusal the session reads state %v, baseline %v, counts %v", got["state"], got["baselineId"], got["counts"])
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM baseline WHERE session_id = $1`, id); n != 0 {
			t.Errorf("the refused closing left %d baselines", n)
		}
	})

	t.Run("an administrator who did not open it closes it without a baseline", func(t *testing.T) {
		closed := obj(t, want(t, s.owner.post(t, sessionPath(id)+"/close", map[string]any{}), http.StatusOK, "the owner closes it"), "session")
		if closed["state"] != "closed" || closed["baselineId"] != nil || closed["counts"].(map[string]any)["pending"] != float64(0) {
			t.Errorf("the closed session reads state %v, baseline %v, counts %v", closed["state"], closed["baselineId"], closed["counts"])
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM baseline WHERE session_id = $1`, id); n != 0 {
			t.Errorf("closing without a baseline took %d", n)
		}
	})
}

// queryKeys are the requirements a query keeps in the scene's project, as the grid lists them.
func (s *sessionScene) queryKeys(t *testing.T, query string) []string {
	t.Helper()
	return rowKeys(t, want(t, s.bob.get(t, "/api/v1/projects/"+s.key+"/requirements?query="+url.QueryEscape(query)), http.StatusOK, "the grid by "+query))
}

func TestASessionPinsWhatItsQueryAndSinceKeep(t *testing.T) {
	s := newSessionScene(t, "RSS")
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/approval-rules", map[string]any{"required": 1,
		"approvers": []map[string]any{{"subjectType": "user", "userId": s.daveID}}}), http.StatusOK, "dave approves")
	for _, key := range []string{s.req("1"), s.req("3")} {
		asked := obj(t, want(t, s.bob.post(t, "/api/v1/requirements/"+key+"/reviews", map[string]any{"revision": 1}), http.StatusCreated, "ask for "+key), "review")
		want(t, s.dave.post(t, "/api/v1/reviews/"+asked["id"].(string)+"/decisions", map[string]any{"decision": "approve", "revision": 1}), http.StatusOK, "approve "+key)
	}
	s.revise(t, s.req("3"), 1)
	query := func(q string, since map[string]any) map[string]any {
		body := s.body(map[string]any{"scope": map[string]any{"kind": "query", "query": q}})
		if since != nil {
			body["since"] = since
		}
		return body
	}
	openAndClose := func(t *testing.T, body map[string]any, what string) map[string]any {
		t.Helper()
		session := obj(t, want(t, s.open(t, s.bob, body), http.StatusCreated, what), "session")
		want(t, s.bob.post(t, sessionPath(session["id"].(string))+"/close", map[string]any{}), http.StatusOK, "close "+what)
		return session
	}

	t.Run("a query pins exactly what it keeps", func(t *testing.T) {
		kept := s.queryKeys(t, "title ~ lights")
		sameList(t, "what the query keeps", kept, s.req("3"), s.req("4"))
		session := openAndClose(t, query("title ~ lights", nil), "a session over a query")
		sameList(t, "what the session pins", itemKeys(session), kept...)
		if sc := session["scope"].(map[string]any); sc["kind"] != "query" || sc["query"] != "title ~ lights" {
			t.Errorf("the session's scope reads %v", sc)
		}
	})

	t.Run("since the last approval pins what changed, and leaves out what did not", func(t *testing.T) {
		session := openAndClose(t, query("title ~ brake", map[string]any{"base": "approved"}), "a session since the last approval")
		sameList(t, "what changed since its approval", itemKeys(session), s.req("3"), s.req("4"))
		// What did not change is neither pinned nor listed: excluded names what waits on a review of its own.
		if excluded := session["excluded"].([]any); len(excluded) != 0 {
			t.Errorf("the session left out %v", excluded)
		}
		if since := session["since"].(map[string]any); since["base"] != "approved" {
			t.Errorf("the session's since reads %v", since)
		}
	})

	t.Run("since a baseline pins what changed after it", func(t *testing.T) {
		baselineID := obj(t, want(t, s.owner.post(t, s.baselines, map[string]any{"name": "Before"}), http.StatusCreated, "take a baseline"), "baseline")["id"].(string)
		s.revise(t, s.req("4"), 1)
		session := openAndClose(t, query("title ~ brake", map[string]any{"base": "baseline", "baselineId": baselineID}), "a session since the baseline")
		sameList(t, "what changed since the baseline", itemKeys(session), s.req("4"))
	})

	t.Run("since a baseline names one", func(t *testing.T) {
		before := s.sessionCount(t)
		fieldError(t, s.open(t, s.bob, query("title ~ brake", map[string]any{"base": "baseline"})), "since")
		fieldError(t, s.open(t, s.bob, query("title ~ brake", map[string]any{"base": "yesterday"})), "since")
		if n := s.sessionCount(t); n != before {
			t.Errorf("the refusals opened %d sessions", n-before)
		}
	})
}

// importTitles imports one requirement of each title into a project through a CSV file, as a person would.
func importTitles(t *testing.T, c *client, projectKey string, titles []string) {
	t.Helper()
	file := "title\n" + strings.Join(titles, "\n") + "\n"
	made := obj(t, want(t, uploadCSV(t, c, projectKey, "many.csv", []byte(file)), http.StatusCreated, "upload the requirements"), "import")
	want(t, c.post(t, "/api/v1/imports/"+made["id"].(string)+"/start", nil), http.StatusAccepted, "start the import")
	if done := finished(t, c, made["id"].(string)); done["state"] != "done" {
		t.Fatalf("the import ended %v", done)
	}
}

// crowd makes n people in one group, gone with the test.
func (s *sessionScene) crowd(t *testing.T, n int) uuid.UUID {
	t.Helper()
	rows, err := s.h.super.Query(t.Context(), `INSERT INTO app_user (email, name, role)
		SELECT 'crowd-' || $2 || '-' || i || '@example.test', 'Crowd ' || i, 'member' FROM generate_series(1, $1) i RETURNING id`, n, s.h.suffix())
	if err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		s.h.later(t, id)
		ids = append(ids, id)
	}
	rows.Close()
	return s.h.makeGroup(t, "Crowd", ids...)
}

func TestASessionIsRefusedPastItsLimits(t *testing.T) {
	s := newSessionScene(t, "RSX")
	scopeError := func(t *testing.T, body map[string]any, says string) {
		t.Helper()
		if msg := fieldError(t, s.open(t, s.bob, body), "scope"); !strings.Contains(msg, says) {
			t.Errorf("the refusal on scope says %q, want it to say %q", msg, says)
		}
	}

	t.Run("a scope with nothing to review", func(t *testing.T) {
		scopeError(t, s.body(map[string]any{"scope": map[string]any{"kind": "query", "query": "title ~ nowhere"}}), "Nothing in this scope needs a review.")
	})

	t.Run("a quorum out of range", func(t *testing.T) {
		for _, quorum := range []int{101, -1} {
			fieldError(t, s.open(t, s.bob, s.body(map[string]any{"quorum": quorum})), "quorum")
		}
	})

	t.Run("approvers who reach more people than a session takes", func(t *testing.T) {
		group := s.crowd(t, review.MaxSessionApprovers+1)
		msg := fieldError(t, s.open(t, s.bob, s.body(map[string]any{"participants": []map[string]any{{"subjectType": "group", "groupId": group, "role": "approver"}}})),
			"participants")
		if !strings.Contains(msg, fmt.Sprintf("reach %d people", review.MaxSessionApprovers+1)) {
			t.Errorf("the refusal says %q", msg)
		}
	})

	t.Run("more requirements than a session reviews", func(t *testing.T) {
		titles := make([]string, review.MaxSessionItems+1)
		for i := range titles {
			titles[i] = fmt.Sprintf("Lamp %d", i+1)
		}
		importTitles(t, s.owner, s.key, titles)
		scopeError(t, s.body(map[string]any{"scope": map[string]any{"kind": "query", "query": "title ~ lamp"}}),
			fmt.Sprintf("The scope holds %d requirements", review.MaxSessionItems+1))
	})

	if n := s.sessionCount(t); n != 0 {
		t.Errorf("the refusals opened %d sessions", n)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM requirement_review v JOIN requirement r ON r.id = v.requirement_id WHERE r.project_id = $1`, s.projectID); n != 0 {
		t.Errorf("the refusals asked %d reviews", n)
	}
}

func TestASessionTakesItsLastRoundAndNoMore(t *testing.T) {
	s := newSessionScene(t, "RSR")
	id := obj(t, want(t, s.open(t, s.bob, s.body(nil)), http.StatusCreated, "bob opens a session"), "session")["id"].(string)
	revision := 1
	for round := 2; round <= review.MaxSessionRounds; round++ {
		revision = s.revise(t, s.req("3"), revision)
		got := obj(t, want(t, s.bob.post(t, sessionPath(id)+"/rounds", map[string]any{}), http.StatusOK, fmt.Sprintf("start round %d", round)), "session")
		if got["round"] != float64(round) {
			t.Fatalf("round %d reads %v", round, got["round"])
		}
	}
	s.revise(t, s.req("3"), revision)
	refuse(t, "a round past the last", s.bob.post(t, sessionPath(id)+"/rounds", map[string]any{}), http.StatusConflict, "too_many_rounds")
	got := obj(t, want(t, s.bob.get(t, sessionPath(id)), http.StatusOK, "read the session"), "session")
	if got["round"] != float64(review.MaxSessionRounds) || got["can"].(map[string]any)["round"] != false || got["can"].(map[string]any)["close"] != true {
		t.Errorf("after its last round the session reads round %v, can %v", got["round"], got["can"])
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM review_session_round WHERE session_id = $1`, id); n != review.MaxSessionRounds {
		t.Errorf("the session holds %d rounds", n)
	}
}

// remindedOf counts the reminders a session's events and notifications hold.
func (s *sessionScene) remindedOf(t *testing.T, id string) int {
	t.Helper()
	return s.h.countRows(t, `SELECT count(*) FROM outbox_event WHERE topic = $1 AND payload ->> 'sessionId' = $2`, review.TopicSessionReminder, id)
}

func TestASessionsDeadlineIsRemindedOnceToWhoStillDecides(t *testing.T) {
	s := newSessionScene(t, "RSD")
	eveID := s.h.addPerson(t, "member")
	s.h.grantAs(t, s.projectID, eveID, "reviewer")
	eve := s.owner.api.as(t, eveID)
	session := obj(t, want(t, s.open(t, s.bob, s.body(map[string]any{"participants": []map[string]any{
		{"subjectType": "user", "userId": s.daveID, "role": "approver"},
		{"subjectType": "user", "userId": eveID, "role": "approver"},
		{"subjectType": "user", "userId": s.carolID, "role": "reviewer"},
	}})), http.StatusCreated, "bob opens a session"), "session")
	id := session["id"].(string)
	for key, item := range itemsOf(t, session) {
		want(t, eve.post(t, "/api/v1/reviews/"+item["review"].(map[string]any)["id"].(string)+"/decisions", map[string]any{"decision": "approve", "revision": 1}),
			http.StatusOK, "eve approves "+key)
	}
	closedID := obj(t, want(t, s.open(t, s.bob, s.body(map[string]any{"title": "Closed review", "scope": map[string]any{"kind": "query", "query": "title ~ engage"}})),
		http.StatusCreated, "bob opens a second session"), "session")["id"].(string)
	// Closed and due in one statement, so the stack's own reminder never finds it open and due.
	if _, err := s.h.super.Exec(t.Context(), `UPDATE review_session SET state = 'closed', closed_at = now(), deadline = now() + interval '2 hours' WHERE id = $1`,
		closedID); err != nil {
		t.Fatal(err)
	}
	want(t, s.bob.patch(t, sessionPath(id), map[string]any{"deadline": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)}), http.StatusOK,
		"the deadline comes within a day")
	reminder := review.NewReminder(s.h.cluster, discard())

	t.Run("an open session due within a day is reminded once, and a closed one never", func(t *testing.T) {
		// The stack's worker runs the same reminder every minute, so either may mark the session first.
		for range 2 {
			if _, err := reminder.Once(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM review_session WHERE id = $1 AND reminded_at IS NOT NULL`, id); n != 1 {
			t.Error("the due session is not marked reminded")
		}
		if n := s.remindedOf(t, id); n != 1 {
			t.Errorf("%d reminders of the due session, want 1", n)
		}
		if n := s.remindedOf(t, closedID); n != 0 {
			t.Errorf("%d reminders of the closed session", n)
		}
	})

	t.Run("only an approver who still has something to decide is told", func(t *testing.T) {
		s.h.drained(t)
		for name, who := range map[string]uuid.UUID{"dave": s.daveID, "eve": eveID, "carol": s.carolID, "bob": s.bobID} {
			told := s.h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND session_id = $2 AND kind = 'session_reminder'`, who, id)
			wanted := 0
			if who == s.daveID {
				wanted = 1
			}
			if told != wanted {
				t.Errorf("%s was reminded %d times, want %d", name, told, wanted)
			}
		}
	})
}

func TestSessionsAreListedToThoseWhoMayReadThem(t *testing.T) {
	s := newSessionScene(t, "RSL")
	// Only those the scene names read the project from here on.
	if _, err := s.h.super.Exec(t.Context(), `DELETE FROM project_grant WHERE project_id = $1 AND subject_type = 'everyone'`, s.projectID); err != nil {
		t.Fatal(err)
	}
	s.h.settle(t)
	outsiderID := s.h.addPerson(t, "member")
	outsider := s.owner.api.as(t, outsiderID)
	opened := func(title string, scope map[string]any, close bool) string {
		id := obj(t, want(t, s.open(t, s.bob, s.body(map[string]any{"title": title, "scope": scope})), http.StatusCreated, "open "+title), "session")["id"].(string)
		if close {
			want(t, s.bob.post(t, sessionPath(id)+"/close", map[string]any{}), http.StatusOK, "close "+title)
		}
		return id
	}
	section := map[string]any{"kind": "section", "rootKey": s.req("2")}
	first := opened("First review", section, true)
	second := opened("Second review", map[string]any{"kind": "query", "query": "title ~ engage"}, true)
	third := opened("Third review", section, false)
	sessions := "/api/v1/projects/" + s.key + "/review-sessions"
	ids := func(t *testing.T, c *client, path string) ([]string, float64) {
		t.Helper()
		got := want(t, c.get(t, path), http.StatusOK, "list "+path)
		var out []string
		for _, each := range list(t, got, "sessions") {
			out = append(out, each.(map[string]any)["id"].(string))
		}
		return out, got.Body["total"].(float64)
	}

	t.Run("a project's sessions, open first, by state and a page at a time", func(t *testing.T) {
		all, _ := ids(t, s.carol, sessions)
		sameList(t, "every session", all, third, second, first)
		closed, total := ids(t, s.carol, sessions+"?state=closed")
		sameList(t, "the closed sessions", closed, second, first)
		paged, pagedTotal := ids(t, s.carol, sessions+"?state=closed&limit=1&offset=1")
		sameList(t, "the second closed session", paged, first)
		if total != 2 || pagedTotal != 2 {
			t.Errorf("the closed sessions total %v and %v a page, want 2", total, pagedTotal)
		}
		open, _ := ids(t, s.carol, sessions+"?state=open")
		sameList(t, "the open sessions", open, third)
	})

	t.Run("one's own sessions are the open ones one takes part in", func(t *testing.T) {
		mine, _ := ids(t, s.dave, "/api/v1/reviews/sessions")
		sameList(t, "dave's sessions", mine, third)
		moderated, _ := ids(t, s.bob, "/api/v1/reviews/sessions")
		sameList(t, "bob's, who moderates and takes no part", moderated)
	})

	t.Run("somebody outside the project finds none of them", func(t *testing.T) {
		refuse(t, "an outsider lists the sessions", outsider.get(t, sessions), http.StatusNotFound, "not_found")
		refuse(t, "an outsider reads one", outsider.get(t, sessionPath(third)), http.StatusNotFound, "not_found")
		theirs, _ := ids(t, outsider, "/api/v1/reviews/sessions")
		sameList(t, "the outsider's sessions", theirs)
	})

	t.Run("an assistant reads the sessions as its person does", func(t *testing.T) {
		_, secret := makeToken(t, s.carol, map[string]any{"name": "session reader"})
		_, outsiderSecret := makeToken(t, outsider, map[string]any{"name": "outsider's assistant"})
		s.h.settle(t)
		a := connectAssistant(t, s.owner.api, secret)
		listed := list(t, response{Body: a.call(t, "list_review_sessions", map[string]any{"projectKey": s.key, "state": "closed"})}, "sessions")
		var listedIDs []string
		for _, each := range listed {
			listedIDs = append(listedIDs, each.(map[string]any)["id"].(string))
		}
		sameList(t, "list_review_sessions", listedIDs, second, first)
		got := a.call(t, "get_review_session", map[string]any{"sessionID": third})["session"].(map[string]any)
		rest := obj(t, want(t, s.carol.get(t, sessionPath(third)), http.StatusOK, "read the session"), "session")
		if got["title"] != rest["title"] || !slices.Equal(itemKeys(got), itemKeys(rest)) || got["round"] != rest["round"] {
			t.Errorf("get_review_session answers %v, the API %v", got, rest)
		}
		stranger := connectAssistant(t, s.owner.api, outsiderSecret)
		stranger.refused(t, "list_review_sessions", map[string]any{"projectKey": s.key})
		stranger.refused(t, "get_review_session", map[string]any{"sessionID": third})
	})
}

// The service refusing is not proof: straight through SQL as exciter_app, a session keeps what it was opened over,
// moves on one round at a time, takes participants and rounds only from its moderator, and hides from outsiders.
func TestRawSQLKeepsASessionToItsOpening(t *testing.T) {
	s := newSessionScene(t, "RSG")
	if _, err := s.h.super.Exec(t.Context(), `DELETE FROM project_grant WHERE project_id = $1 AND subject_type = 'everyone'`, s.projectID); err != nil {
		t.Fatal(err)
	}
	outsiderID := s.h.addPerson(t, "member")
	session := obj(t, want(t, s.open(t, s.bob, s.body(nil)), http.StatusCreated, "bob opens a session"), "session")
	id := uuid.MustParse(session["id"].(string))
	ctx := context.Background()
	bob, dave, outsider := s.h.appConn(t, s.bobID), s.h.appConn(t, s.daveID), s.h.appConn(t, outsiderID)

	t.Run("what it was opened over never changes", func(t *testing.T) {
		for _, c := range []struct{ what, sql string }{
			{"its root", `UPDATE review_session SET scope_root = NULL WHERE id = $1`},
			{"its since", `UPDATE review_session SET since_base = 'approved' WHERE id = $1`},
			{"its project", `UPDATE review_session SET project_id = (SELECT id FROM project WHERE id <> project_id LIMIT 1) WHERE id = $1`},
		} {
			// exciter_app may not update those columns at all; the trigger holds even a role that may.
			if _, err := bob.Exec(ctx, c.sql, id); !isPermissionDenied(err) {
				t.Errorf("the moderator changes %s as exciter_app: %v", c.what, err)
			}
			if _, err := s.h.super.Exec(ctx, c.sql, id); !refusedBy(err, "review_session_fixed") {
				t.Errorf("%s changed as the superuser: %v", c.what, err)
			}
		}
	})

	t.Run("it moves on one round at a time", func(t *testing.T) {
		if _, err := bob.Exec(ctx, `UPDATE review_session SET round = round + 2 WHERE id = $1`, id); !refusedBy(err, "review_session_round") {
			t.Errorf("the moderator skipped a round: %v", err)
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM review_session WHERE id = $1 AND round = 1`, id); n != 1 {
			t.Error("the session left round 1")
		}
	})

	t.Run("a participant who does not moderate names nobody and starts no round", func(t *testing.T) {
		if _, err := dave.Exec(ctx, `INSERT INTO review_session_participant (session_id, subject_type, user_id, role) VALUES ($1, 'user', $2, 'approver')`,
			id, outsiderID); !isPolicyViolation(err) {
			t.Errorf("dave named a participant: %v", err)
		}
		if _, err := dave.Exec(ctx, `INSERT INTO review_session_round (session_id, number, started_by) VALUES ($1, 2, $2)`, id, s.daveID); !isPolicyViolation(err) {
			t.Errorf("dave started a round: %v", err)
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM review_session_participant WHERE session_id = $1`, id); n != 2 {
			t.Errorf("the session names %d participants, want its 2", n)
		}
	})

	t.Run("somebody who cannot view the project sees nothing of it", func(t *testing.T) {
		for _, table := range []string{"review_session_item", "review_session_participant", "review_session_round"} {
			sql := `SELECT count(*) FROM ` + table + ` WHERE session_id = $1`
			var theirs, mine int
			if err := outsider.QueryRow(ctx, sql, id).Scan(&theirs); err != nil {
				t.Fatal(err)
			}
			if err := bob.QueryRow(ctx, sql, id).Scan(&mine); err != nil {
				t.Fatal(err)
			}
			if theirs != 0 || mine == 0 {
				t.Errorf("%s: the outsider sees %d rows, the moderator %d", table, theirs, mine)
			}
		}
	})
}
