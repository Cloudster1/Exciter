//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/mail"
	"github.com/cloudster1/exciter/backend/internal/notify"
)

// Watching and notifications (#34): who follows a requirement, by itself or through its project, and what the
// stack's own worker tells them, in the app and by mail, through the API and straight through SQL.

// mailWait bounds waiting for Mailpit to list a mail the worker handed it.
const mailWait = 10 * time.Second

// mailbox keeps what a digester would have sent.
type mailbox struct {
	mu   sync.Mutex
	sent []mail.Mail
}

func (m *mailbox) Send(_ context.Context, msg mail.Mail) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mailbox) to(address string) []mail.Mail {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []mail.Mail
	for _, each := range m.sent {
		if each.To == address {
			out = append(out, each)
		}
	}
	return out
}

// told is what a person was told about one requirement, the latest first.
func told(t *testing.T, c *client, reqID string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, each := range list(t, want(t, c.get(t, "/api/v1/notifications?limit="+strconv.Itoa(notify.MaxLimit)), http.StatusOK, "list notifications"), "notifications") {
		n := each.(map[string]any)
		if n["requirement"].(map[string]any)["id"] == reqID {
			out = append(out, n)
		}
	}
	return out
}

func kindsOf(ns []map[string]any) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n["kind"].(string)
	}
	return out
}

func (h *harness) emailOf(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var email string
	if err := h.super.QueryRow(context.Background(), `SELECT email FROM app_user WHERE id = $1`, id).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return email
}

// caught are the subjects of the mails Mailpit holds for an address, waiting until at least least of them came.
func caught(t *testing.T, address string, least int) []string {
	t.Helper()
	base := os.Getenv("EXCITER_TEST_MAILPIT_URL")
	if base == "" {
		t.Fatal("EXCITER_TEST_MAILPIT_URL is not set; run the suite with make test-integration against the running stack")
	}
	deadline := time.Now().Add(mailWait)
	for {
		resp, err := http.Get(base + "/api/v1/search?query=" + url.QueryEscape("to:"+address))
		if err != nil {
			t.Fatal(err)
		}
		var found struct {
			Messages []struct{ Subject string } `json:"messages"`
		}
		err = json.NewDecoder(resp.Body).Decode(&found)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		subjects := make([]string, len(found.Messages))
		for i, m := range found.Messages {
			subjects[i] = m.Subject
		}
		if len(subjects) >= least || time.Now().After(deadline) {
			return subjects
		}
		time.Sleep(workerIdle)
	}
}

// watchScene is the requirement of a comment world as its people watch it, edit it and talk about it.
type watchScene struct {
	*commentWorld
	req               string
	threadID, replyID string
}

func (s *watchScene) watching(t *testing.T, c *client) map[string]any {
	t.Helper()
	return obj(t, want(t, c.get(t, s.req), http.StatusOK, "read R1"), "requirement")["watching"].(map[string]any)
}

func (s *watchScene) revise(t *testing.T, c *client, base int, title string) {
	t.Helper()
	want(t, c.patch(t, s.req, map[string]any{"title": title, "baseRevision": base, "comment": "Now " + title + "."}), http.StatusOK, "revise R1")
}

func TestWatchersHearWhatChanged(t *testing.T) {
	w := newCommentWorld(t, "WCH")
	s := &watchScene{commentWorld: w, req: "/api/v1/requirements/" + w.reqKey}
	t.Run("writing a requirement makes its author watch it, and whoever views it sees who does", s.authorWatches)
	t.Run("a project watch covers every requirement in it", s.projectWatch)
	t.Run("a new revision tells the watchers but not its author, who now watches it too", s.revisionTells)
	t.Run("a comment tells whom it mentions, the thread's writers and the watchers, each once", s.commentTells)
	t.Run("resolving tells the thread's writers", s.resolvingTells)
	t.Run("a transition tells the watchers which status the requirement is in now", s.transitionTells)
	t.Run("deleting a comment takes back what was told about it", s.deletingTakesBack)
	t.Run("unwatching sticks through the person's own edits, until they watch again", s.unwatchingSticks)
	t.Run("preferences switch a kind off in the app, and refuse what they do not know", s.preferences)
	t.Run("reading marks what was read, one or all", s.reading)
	t.Run("mail goes at once to whoever wants it, and to nobody for a kind switched off", s.mailAtOnce)
	t.Run("a digest gathers mail until its hour", s.digest)
	t.Run("what nobody may do is refused", s.refusals)
}

func (s *watchScene) authorWatches(t *testing.T) {
	if got := s.watching(t, s.owner); got["requirement"] != true || got["project"] != false {
		t.Errorf("the author watches %v", got)
	}
	if got := s.watching(t, s.dave); got["requirement"] != false {
		t.Errorf("a reviewer who wrote nothing watches %v", got)
	}
	body := want(t, s.carol.get(t, s.req+"/watchers"), http.StatusOK, "list watchers")
	watchers := list(t, body, "watchers")
	if len(watchers) != 1 || watchers[0].(map[string]any)["userId"] != s.ownerID.String() || watchers[0].(map[string]any)["via"] != "requirement" {
		t.Errorf("the watchers are %v", watchers)
	}
}

func (s *watchScene) projectWatch(t *testing.T) {
	made := obj(t, want(t, s.carol.put(t, "/api/v1/projects/"+s.key+"/watch", nil), http.StatusOK, "carol watches the project"), "watch")
	if made["kind"] != "project" || made["projectKey"] != s.key || made["requirement"] != nil {
		t.Errorf("the watch is %v", made)
	}
	want(t, s.carol.put(t, "/api/v1/projects/"+s.key+"/watch", nil), http.StatusOK, "carol watches the project again")
	if got := s.watching(t, s.carol); got["project"] != true || got["requirement"] != false {
		t.Errorf("carol watches %v", got)
	}
	if obj(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key), http.StatusOK, "read the project"), "project")["watching"] != true {
		t.Error("the project does not say carol watches it")
	}
	watches := list(t, want(t, s.carol.get(t, "/api/v1/watches"), http.StatusOK, "list carol's watches"), "watches")
	if len(watches) != 1 || watches[0].(map[string]any)["projectKey"] != s.key {
		t.Errorf("carol's watches are %v", watches)
	}
	if n := len(list(t, want(t, s.dave.get(t, s.req+"/watchers"), http.StatusOK, "list watchers"), "watchers")); n != 2 {
		t.Errorf("%d watchers, want the author and carol", n)
	}
}

func (s *watchScene) revisionTells(t *testing.T) {
	s.revise(t, s.bob, 1, "Brakes engage within 80 ms")
	s.h.drained(t)
	for name, c := range map[string]*client{"the author": s.owner, "carol": s.carol} {
		got := told(t, c, s.reqID)
		if len(got) != 1 || got[0]["kind"] != "revised" || got[0]["revision"] != float64(2) ||
			got[0]["excerpt"] != "Now Brakes engage within 80 ms." || got[0]["actorName"] != "A member" || got[0]["readAt"] != nil {
			t.Errorf("%s was told %v", name, got)
		}
	}
	if got := told(t, s.bob, s.reqID); len(got) != 0 {
		t.Errorf("bob was told of his own revision: %v", got)
	}
	if got := told(t, s.dave, s.reqID); len(got) != 0 {
		t.Errorf("dave, who watches nothing, was told %v", got)
	}
	if s.watching(t, s.bob)["requirement"] != true {
		t.Error("revising did not make bob watch R1")
	}
}

func (s *watchScene) commentTells(t *testing.T) {
	thread := obj(t, want(t, s.dave.post(t, s.req+"/comments", commentBody("Why 80 ms? ", s.carolID)), http.StatusCreated, "dave comments"), "thread")
	s.threadID = thread["id"].(string)
	s.h.drained(t)
	sameList(t, "carol, mentioned and watching", kindsOf(told(t, s.carol, s.reqID)), "mentioned", "revised")
	sameList(t, "the author", kindsOf(told(t, s.owner, s.reqID)), "commented", "revised")
	sameList(t, "bob", kindsOf(told(t, s.bob, s.reqID)), "commented")
	if got := told(t, s.dave, s.reqID); len(got) != 0 {
		t.Errorf("dave was told of his own comment: %v", got)
	}
	if got := told(t, s.carol, s.reqID)[0]; got["threadId"] != s.threadID || got["commentId"] != s.threadID || got["excerpt"] != "Why 80 ms? @Someone" {
		t.Errorf("carol's mention reads %v", got)
	}

	reply := obj(t, want(t, s.bob.post(t, "/api/v1/comments/"+s.threadID+"/replies", commentBody("The new sensor.")), http.StatusCreated, "bob replies"), "comment")
	s.replyID = reply["id"].(string)
	s.h.drained(t)
	sameList(t, "dave, whose thread it is", kindsOf(told(t, s.dave, s.reqID)), "replied")
	sameList(t, "carol, who only watches", kindsOf(told(t, s.carol, s.reqID)), "commented", "mentioned", "revised")
	if got := told(t, s.dave, s.reqID)[0]; got["commentId"] != s.replyID || got["threadId"] != s.threadID {
		t.Errorf("dave's reply reads %v", got)
	}
}

func (s *watchScene) resolvingTells(t *testing.T) {
	want(t, s.owner.put(t, "/api/v1/comment-threads/"+s.threadID+"/resolution", nil), http.StatusOK, "resolve")
	s.h.drained(t)
	sameList(t, "dave", kindsOf(told(t, s.dave, s.reqID)), "resolved", "replied")
	sameList(t, "bob", kindsOf(told(t, s.bob, s.reqID)), "resolved", "commented")
	if got := kindsOf(told(t, s.carol, s.reqID)); got[0] == "resolved" {
		t.Errorf("carol, who wrote nothing in the thread, heard it resolved: %v", got)
	}
}

func (s *watchScene) transitionTells(t *testing.T) {
	submit, ok := transitionNamed(t, s.bob, s.reqKey, "Submit for review")
	if !ok {
		t.Fatal("bob is not offered Submit for review")
	}
	want(t, s.bob.post(t, s.req+"/transitions", map[string]any{"transitionId": submit}), http.StatusOK, "submit")
	s.h.drained(t)
	status := obj(t, want(t, s.owner.get(t, s.req), http.StatusOK, "read R1"), "requirement")["status"].(map[string]any)["name"]
	got := told(t, s.owner, s.reqID)[0]
	if got["kind"] != "transitioned" || got["excerpt"] != status || got["requirement"].(map[string]any)["status"].(map[string]any)["name"] != status {
		t.Errorf("the author was told %v, with R1 in %v", got, status)
	}
}

func (s *watchScene) deletingTakesBack(t *testing.T) {
	if s.h.countRows(t, `SELECT count(*) FROM notification WHERE comment_id = $1`, s.replyID) == 0 {
		t.Fatal("nobody was told of the reply")
	}
	want(t, s.bob.delete(t, "/api/v1/comments/"+s.replyID), http.StatusNoContent, "bob deletes his reply")
	if n := s.h.countRows(t, `SELECT count(*) FROM notification WHERE comment_id = $1`, s.replyID); n != 0 {
		t.Errorf("%d notifications about a deleted reply remain", n)
	}
}

func (s *watchScene) unwatchingSticks(t *testing.T) {
	want(t, s.owner.delete(t, s.req+"/watch"), http.StatusNoContent, "the author unwatches")
	if got := s.watching(t, s.owner); got["requirement"] != false {
		t.Errorf("after unwatching the author watches %v", got)
	}
	before := len(told(t, s.owner, s.reqID))
	s.revise(t, s.owner, 2, "Brakes engage within 90 ms")
	s.revise(t, s.bob, 3, "Brakes engage within 85 ms")
	s.h.drained(t)
	if s.watching(t, s.owner)["requirement"] != false {
		t.Error("editing made the author watch again what they had unwatched")
	}
	if n := len(told(t, s.owner, s.reqID)); n != before {
		t.Errorf("the author, no longer watching, was told %d more", n-before)
	}
	if got := obj(t, want(t, s.owner.put(t, s.req+"/watch", nil), http.StatusOK, "the author watches again"), "watching"); got["requirement"] != true {
		t.Errorf("watching again reads %v", got)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM watch_optout WHERE user_id = $1`, s.ownerID); n != 0 {
		t.Errorf("watching again left %d opt-outs", n)
	}
}

func (s *watchScene) preferences(t *testing.T) {
	prefs := obj(t, want(t, s.carol.get(t, "/api/v1/notification-preferences"), http.StatusOK, "read preferences"), "preferences")
	inApp := prefs["inApp"].(map[string]any)
	if len(inApp) != len(notify.Kinds) || inApp["revised"] != true || prefs["digest"] != "off" || prefs["autoWatch"] != true {
		t.Errorf("the defaults are %v", prefs)
	}
	saved := obj(t, want(t, s.carol.put(t, "/api/v1/notification-preferences", map[string]any{
		"inApp": map[string]any{"revised": false}, "email": map[string]any{}, "digest": "off", "autoWatch": true,
	}), http.StatusOK, "switch revised off"), "preferences")
	if saved["inApp"].(map[string]any)["revised"] != false || saved["inApp"].(map[string]any)["mentioned"] != true {
		t.Errorf("the saved preferences are %v", saved)
	}
	fieldError(t, s.carol.put(t, "/api/v1/notification-preferences", map[string]any{
		"inApp": map[string]any{"published": false}, "email": map[string]any{}, "digest": "off", "autoWatch": true}), "inApp")
	fieldError(t, s.carol.put(t, "/api/v1/notification-preferences", map[string]any{
		"inApp": map[string]any{}, "email": map[string]any{}, "digest": "weekly", "autoWatch": true}), "digest")

	before := len(told(t, s.carol, s.reqID))
	s.revise(t, s.bob, 4, "Brakes engage within 75 ms")
	s.h.drained(t)
	if n := len(told(t, s.carol, s.reqID)); n != before {
		t.Errorf("carol switched revised off and was told %d more", n-before)
	}
	if got := told(t, s.owner, s.reqID)[0]; got["kind"] != "revised" || got["revision"] != float64(5) {
		t.Errorf("the author was told %v", got)
	}
}

func (s *watchScene) reading(t *testing.T) {
	mine := told(t, s.owner, s.reqID)
	count := func() float64 {
		return obj(t, want(t, s.owner.get(t, "/api/v1/notifications/unread-count"), http.StatusOK, "count unread"))["unread"].(float64)
	}
	if got := count(); got != float64(len(mine)) {
		t.Fatalf("%v unread, want the %d the author was told", got, len(mine))
	}
	want(t, s.owner.post(t, "/api/v1/notifications/read", map[string]any{"ids": []string{mine[0]["id"].(string), uuid.NewString()}}), http.StatusNoContent, "read one")
	if got := count(); got != float64(len(mine)-1) {
		t.Errorf("%v unread after reading one of %d", got, len(mine))
	}
	if told(t, s.owner, s.reqID)[0]["readAt"] == nil {
		t.Error("the one read has no time it was read")
	}
	want(t, s.carol.post(t, "/api/v1/notifications/read", map[string]any{"ids": []string{mine[1]["id"].(string)}}), http.StatusNoContent, "carol reads the author's")
	if got := count(); got != float64(len(mine)-1) {
		t.Errorf("carol marked the author's notification read: %v unread", got)
	}
	fieldError(t, s.owner.post(t, "/api/v1/notifications/read", map[string]any{}), "ids")
	want(t, s.owner.post(t, "/api/v1/notifications/read", map[string]any{"all": true}), http.StatusNoContent, "read all")
	if got := count(); got != 0 {
		t.Errorf("%v unread after reading all", got)
	}
	if n := len(list(t, want(t, s.owner.get(t, "/api/v1/notifications?unread=true"), http.StatusOK, "list unread"), "notifications")); n != 0 {
		t.Errorf("%d unread listed after reading all", n)
	}
	fieldError(t, s.owner.get(t, "/api/v1/notifications?unread=maybe"), "unread")
}

func (s *watchScene) mailAtOnce(t *testing.T) {
	subjects := caught(t, s.h.emailOf(t, s.ownerID), 1)
	if !slices.ContainsFunc(subjects, func(subject string) bool { return strings.Contains(subject, "saved a new revision of "+s.reqKey) }) {
		t.Errorf("the author's mail is %v", subjects)
	}
	if slices.ContainsFunc(caught(t, s.h.emailOf(t, s.carolID), 1), func(s string) bool {
		return strings.Contains(s, "Brakes engage within 75 ms")
	}) {
		t.Error("carol was mailed a kind she switched off")
	}
}

func (s *watchScene) digest(t *testing.T) {
	want(t, s.dave.put(t, "/api/v1/notification-preferences", map[string]any{
		"inApp": map[string]any{}, "email": map[string]any{}, "digest": "daily", "autoWatch": true,
	}), http.StatusOK, "dave asks for a daily digest")
	want(t, s.bob.patch(t, s.req, map[string]any{"ownerId": s.daveID}), http.StatusOK, "make dave the owner")
	if s.watching(t, s.dave)["requirement"] != true {
		t.Fatal("being made the owner did not make dave watch R1")
	}
	s.revise(t, s.bob, 5, "Brakes engage within 70 ms")
	s.h.drained(t)
	if n := s.h.countRows(t, `SELECT count(*) FROM notification_digest WHERE user_id = $1`, s.daveID); n != 1 {
		t.Fatalf("%d notifications wait for dave's digest, want the revision", n)
	}
	dave := s.h.emailOf(t, s.daveID)
	box := &mailbox{}
	now := time.Now().UTC()
	if _, err := notify.NewDigester(s.h.cluster, box, testAppURL(t), discard()).WithClock(func() time.Time { return now }).Once(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := box.to(dave); len(got) != 0 {
		t.Fatalf("a daily digest went before its hour: %v", got)
	}
	morning := time.Date(now.Year(), now.Month(), now.Day(), notify.DailyDigestHour, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	if _, err := notify.NewDigester(s.h.cluster, box, testAppURL(t), discard()).WithClock(func() time.Time { return morning }).Once(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := box.to(dave)
	if len(got) != 1 || got[0].Subject != "1 update in Exciter" || !strings.Contains(got[0].Body, "/r/"+s.reqID) {
		t.Fatalf("dave's digest is %v", got)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM notification_digest WHERE user_id = $1`, s.daveID); n != 0 {
		t.Errorf("%d notifications still wait after the digest went", n)
	}
}

func (s *watchScene) refusals(t *testing.T) {
	nobody := newAPIServer(t, s.h).anonymous()
	refuse(t, "nobody counts the unread", nobody.get(t, "/api/v1/notifications/unread-count"), http.StatusUnauthorized, "unauthorized")
	refuse(t, "nobody reads preferences", nobody.get(t, "/api/v1/notification-preferences"), http.StatusUnauthorized, "unauthorized")
	none := "/api/v1/requirements/" + s.key + "-R999"
	refuse(t, "watching a requirement that is none", s.carol.put(t, none+"/watch", nil), http.StatusNotFound, "not_found")
	refuse(t, "unwatching a requirement that is none", s.carol.delete(t, none+"/watch"), http.StatusNotFound, "not_found")
	refuse(t, "the watchers of a requirement that is none", s.carol.get(t, none+"/watchers"), http.StatusNotFound, "not_found")
	refuse(t, "watching a project that is none", s.carol.put(t, "/api/v1/projects/NOPE"+s.key+"/watch", nil), http.StatusNotFound, "not_found")
	refuse(t, "unwatching a project that is none", s.carol.delete(t, "/api/v1/projects/NOPE"+s.key+"/watch"), http.StatusNotFound, "not_found")
	fieldError(t, s.carol.get(t, "/api/v1/watches?limit="+strconv.Itoa(notify.MaxLimit+1)), "limit")

	want(t, s.carol.delete(t, "/api/v1/projects/"+s.key+"/watch"), http.StatusNoContent, "carol unwatches the project")
	if got := s.watching(t, s.carol); got["project"] != false {
		t.Errorf("after unwatching the project carol watches %v", got)
	}
}

// The service refusing is not proof: straight through SQL as exciter_app, a person's watches, opt-outs, preferences
// and notifications are theirs alone, and only about what they may view.
func TestRawSQLKeepsWatchesAndNotificationsToTheirPeople(t *testing.T) {
	w := newCommentWorld(t, "WCQ")
	h := w.h
	ctx := context.Background()
	hidden := projectKey(t, "WCX")
	want(t, w.owner.post(t, "/api/v1/projects", map[string]any{"key": hidden, "name": "Hidden"}), http.StatusCreated, "make a hidden project")
	if _, err := h.super.Exec(ctx, `DELETE FROM project_grant WHERE project_id = (SELECT id FROM project WHERE key = $1) AND subject_type = 'everyone'`, hidden); err != nil {
		t.Fatal(err)
	}
	secret := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+hidden+"/requirements", map[string]any{"title": "Secret"}), http.StatusCreated, "write a secret"), "requirement")["id"].(string)
	revise := obj(t, want(t, w.bob.patch(t, "/api/v1/requirements/"+w.reqKey, map[string]any{"title": "Changed", "baseRevision": 1}), http.StatusOK, "revise"), "requirement")
	if revise["revision"] != float64(2) {
		t.Fatalf("the revision is %v", revise["revision"])
	}
	h.drained(t)
	carol, bob, owner := h.appConn(t, w.carolID), h.appConn(t, w.bobID), h.appConn(t, w.ownerID)

	if _, err := carol.Exec(ctx, `INSERT INTO watch (user_id, kind, requirement_id) VALUES ($1, 'requirement', $2)`, w.bobID, w.reqID); !isPolicyViolation(err) {
		t.Errorf("carol made bob watch R1: %v", err)
	}
	if _, err := carol.Exec(ctx, `INSERT INTO watch (user_id, kind, requirement_id) VALUES ($1, 'requirement', $2)`, w.carolID, secret); !isPolicyViolation(err) {
		t.Errorf("carol watched a requirement she may not view: %v", err)
	}
	if n := affects(t, carol, `INSERT INTO watch (user_id, kind, requirement_id) VALUES ($1, 'requirement', $2)`, w.carolID, w.reqID); n != 1 {
		t.Error("carol could not watch R1 herself")
	}
	if n := affects(t, carol, `DELETE FROM watch WHERE user_id = $1`, w.ownerID); n != 0 {
		t.Errorf("carol removed %d of the author's watches", n)
	}
	if _, err := carol.Exec(ctx, `INSERT INTO watch_optout (user_id, requirement_id) VALUES ($1, $2)`, w.ownerID, w.reqID); !isPolicyViolation(err) {
		t.Errorf("carol opted the author out: %v", err)
	}
	if _, err := carol.Exec(ctx, `SELECT * FROM requirement_watch_coverage($1)`, w.reqID); !isPermissionDenied(err) {
		t.Errorf("carol read who watches across people: %v", err)
	}
	if _, err := carol.Exec(ctx, `SELECT watch_auto($1, $2)`, w.bobID, w.reqID); !isPermissionDenied(err) {
		t.Errorf("carol made bob watch through the trigger's function: %v", err)
	}

	var seen int
	if err := carol.QueryRow(ctx, `SELECT count(*) FROM notification WHERE user_id = $1`, w.ownerID).Scan(&seen); err != nil || seen != 0 {
		t.Errorf("carol read %d of the author's notifications: %v", seen, err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1`, w.ownerID); n == 0 {
		t.Fatal("the author was told nothing to hide")
	}
	if n := affects(t, carol, `UPDATE notification SET read_at = now() WHERE user_id = $1`, w.ownerID); n != 0 {
		t.Errorf("carol marked %d of the author's notifications read", n)
	}
	if _, err := owner.Exec(ctx, `UPDATE notification SET kind = 'mentioned' WHERE user_id = $1`, w.ownerID); !isPermissionDenied(err) {
		t.Errorf("the author rewrote what they were told: %v", err)
	}
	if _, err := owner.Exec(ctx, `DELETE FROM notification WHERE user_id = $1`, w.ownerID); !isPermissionDenied(err) {
		t.Errorf("the author deleted what they were told: %v", err)
	}
	insert := `INSERT INTO notification (user_id, event_id, kind, requirement_id, revision, read_at) VALUES ($1, $2, 'revised', $3, 1, $4)`
	if _, err := bob.Exec(ctx, insert, w.carolID, uuid.New(), w.reqID, nil); !isPolicyViolation(err) {
		t.Errorf("bob told carol something: %v", err)
	}
	if _, err := carol.Exec(ctx, insert, w.carolID, uuid.New(), secret, nil); !isPolicyViolation(err) {
		t.Errorf("carol was told about a requirement she may not view: %v", err)
	}
	if _, err := carol.Exec(ctx, insert, w.carolID, uuid.New(), w.reqID, time.Now()); !isPolicyViolation(err) {
		t.Errorf("a notification was written already read: %v", err)
	}
	if _, err := carol.Exec(ctx, `INSERT INTO notification_preference (user_id, digest) VALUES ($1, 'daily')`, w.bobID); !isPolicyViolation(err) {
		t.Errorf("carol chose bob's digest: %v", err)
	}
	var id uuid.UUID
	if err := h.super.QueryRow(ctx, `SELECT id FROM notification WHERE user_id = $1 LIMIT 1`, w.ownerID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := carol.Exec(ctx, `INSERT INTO notification_digest (user_id, notification_id) VALUES ($1, $2)`, w.ownerID, id); !isPolicyViolation(err) {
		t.Errorf("carol queued the author's mail: %v", err)
	}
}
