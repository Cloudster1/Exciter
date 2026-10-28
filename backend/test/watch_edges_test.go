//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/notify"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// The edges of watching and notifications (#312): they follow what a person may still view and page through what
// is listed, each preference has its effect, a read-only token changes nothing, and the worker mails on schedule.

// digestHourMargin keeps an hourly digest test clear of the full hour, when the stack's own digester would send the
// bundle the test is about to look for.
const digestHourMargin = time.Minute

// seen is what a person's lists show of the requirements they follow.
type seen struct {
	watched, told []string
	unread        float64
	total         float64
}

// sees reads the person's watches and notifications, by the requirement each is about.
func sees(t *testing.T, c *client) seen {
	t.Helper()
	var out seen
	for _, each := range list(t, want(t, c.get(t, "/api/v1/watches?limit="+strconv.Itoa(notify.MaxLimit)), http.StatusOK, "list watches"), "watches") {
		if r, ok := each.(map[string]any)["requirement"].(map[string]any); ok {
			out.watched = append(out.watched, r["key"].(string))
		} else {
			out.watched = append(out.watched, "project "+each.(map[string]any)["projectKey"].(string))
		}
	}
	got := want(t, c.get(t, "/api/v1/notifications?limit="+strconv.Itoa(notify.MaxLimit)), http.StatusOK, "list notifications")
	for _, each := range list(t, got, "notifications") {
		out.told = append(out.told, each.(map[string]any)["requirement"].(map[string]any)["key"].(string))
	}
	out.total = got.Body["total"].(float64)
	out.unread = obj(t, want(t, c.get(t, "/api/v1/notifications/unread-count"), http.StatusOK, "count unread"))["unread"].(float64)
	return out
}

func countOf(keys []string, key string) int {
	n := 0
	for _, k := range keys {
		if k == key {
			n++
		}
	}
	return n
}

// watcherIDs lists who watches a requirement, as c reads it.
func watcherIDs(t *testing.T, c *client, reqKey string) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, want(t, c.get(t, "/api/v1/requirements/"+reqKey+"/watchers"), http.StatusOK, "list watchers"), "watchers") {
		out = append(out, each.(map[string]any)["userId"].(string))
	}
	return out
}

func idsIn(t *testing.T, r response, key string) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, r, key) {
		out = append(out, each.(map[string]any)["id"].(string))
	}
	return out
}

func TestWatchingFollowsWhatOneMayView(t *testing.T) {
	w := newCommentWorld(t, "WFV")
	h := w.h
	w.closeToEveryone(t)
	r1, r2 := w.reqKey, w.key+"-R2"
	want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/requirements", map[string]any{"title": "Brakes report wear"}), http.StatusCreated, "write R2")
	r2ID := obj(t, want(t, w.owner.get(t, "/api/v1/requirements/"+r2), http.StatusOK, "read R2"), "requirement")["id"].(string)

	t.Run("somebody who may not view the project watches nothing in it", func(t *testing.T) {
		outsider := h.addPerson(t, "member")
		out := w.owner.api.as(t, outsider)
		req := "/api/v1/requirements/" + r1
		refuse(t, "watching R1", out.put(t, req+"/watch", nil), http.StatusNotFound, "not_found")
		refuse(t, "unwatching R1", out.delete(t, req+"/watch"), http.StatusNotFound, "not_found")
		refuse(t, "R1's watchers", out.get(t, req+"/watchers"), http.StatusNotFound, "not_found")
		refuse(t, "watching the project", out.put(t, "/api/v1/projects/"+w.key+"/watch", nil), http.StatusNotFound, "not_found")
		refuse(t, "unwatching the project", out.delete(t, "/api/v1/projects/"+w.key+"/watch"), http.StatusNotFound, "not_found")
		if n := h.countRows(t, `SELECT count(*) FROM watch WHERE user_id = $1`, outsider) +
			h.countRows(t, `SELECT count(*) FROM watch_optout WHERE user_id = $1`, outsider); n != 0 {
			t.Errorf("the refusals left %d rows", n)
		}
	})

	want(t, w.carol.put(t, "/api/v1/requirements/"+r1+"/watch", nil), http.StatusOK, "carol watches R1")
	want(t, w.carol.put(t, "/api/v1/projects/"+w.key+"/watch", nil), http.StatusOK, "carol watches the project")
	for base := 1; base <= 2; base++ {
		for _, key := range []string{r1, r2} {
			want(t, w.bob.patch(t, "/api/v1/requirements/"+key, map[string]any{"title": key + " at " + strconv.Itoa(base), "baseRevision": base}),
				http.StatusOK, "revise "+key)
		}
	}
	h.drained(t)
	everything := sees(t, w.carol)
	if countOf(everything.told, r1) != 2 || countOf(everything.told, r2) != 2 || everything.unread != 4 || len(everything.watched) != 2 {
		t.Fatalf("carol sees %+v", everything)
	}

	t.Run("a window of notifications and watches is the next one, with the total listed", func(t *testing.T) {
		all := idsIn(t, want(t, w.carol.get(t, "/api/v1/notifications?limit=100"), http.StatusOK, "all"), "notifications")
		window := want(t, w.carol.get(t, "/api/v1/notifications?limit=2&offset=2"), http.StatusOK, "the second window")
		sameList(t, "the second window", idsIn(t, window, "notifications"), all[2:4]...)
		if window.Body["total"] != float64(4) || window.Body["offset"] != float64(2) {
			t.Errorf("the second window reads total %v offset %v", window.Body["total"], window.Body["offset"])
		}
		want(t, w.carol.post(t, "/api/v1/notifications/read", map[string]any{"ids": []string{all[0]}}), http.StatusNoContent, "carol reads the latest")
		unread := want(t, w.carol.get(t, "/api/v1/notifications?unread=true&limit=2&offset=2"), http.StatusOK, "the unread window")
		sameList(t, "the unread window", idsIn(t, unread, "notifications"), all[3])
		if unread.Body["total"] != float64(3) {
			t.Errorf("the unread total is %v, want 3", unread.Body["total"])
		}
		watches := idsIn(t, want(t, w.carol.get(t, "/api/v1/watches"), http.StatusOK, "all watches"), "watches")
		second := want(t, w.carol.get(t, "/api/v1/watches?limit=1&offset=1"), http.StatusOK, "the second watch")
		sameList(t, "the second watch", idsIn(t, second, "watches"), watches[1])
		if second.Body["total"] != float64(2) {
			t.Errorf("the watches total %v, want 2", second.Body["total"])
		}
	})

	t.Run("without her grant carol hears of nothing in the project, and with it again of everything", func(t *testing.T) {
		before := sees(t, w.carol)
		if _, err := h.super.Exec(t.Context(), `DELETE FROM project_grant WHERE project_id = $1 AND user_id = $2`, w.projectID, w.carolID); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		if got := sees(t, w.carol); len(got.watched) != 0 || len(got.told) != 0 || got.unread != 0 || got.total != 0 {
			t.Errorf("without a grant carol sees %+v", got)
		}
		if ids := watcherIDs(t, w.owner, r1); strings.Contains(strings.Join(ids, " "), w.carolID.String()) {
			t.Errorf("R1's watchers name carol, who may not read it: %v", ids)
		}
		h.grantAs(t, w.projectID, w.carolID, "viewer")
		if got := sees(t, w.carol); strings.Join(got.watched, "|") != strings.Join(before.watched, "|") ||
			strings.Join(got.told, "|") != strings.Join(before.told, "|") || got.unread != before.unread {
			t.Errorf("with her grant back carol sees %+v, before %+v", got, before)
		}
		if ids := watcherIDs(t, w.owner, r1); !strings.Contains(strings.Join(ids, " "), w.carolID.String()) {
			t.Errorf("R1's watchers leave carol out after her grant came back: %v", ids)
		}
	})

	t.Run("a requirement in the trash leaves the lists until it comes back", func(t *testing.T) {
		before := sees(t, w.carol)
		unreadOfR1 := h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND requirement_id = $2 AND read_at IS NULL`, w.carolID, w.reqID)
		want(t, w.bob.delete(t, "/api/v1/requirements/"+r1), http.StatusNoContent, "trash R1")
		got := sees(t, w.carol)
		if countOf(got.watched, r1) != 0 || countOf(got.told, r1) != 0 || countOf(got.told, r2) != 2 || got.total != 2 ||
			got.unread != before.unread-float64(unreadOfR1) {
			t.Errorf("with R1 in the trash carol sees %+v, before %+v", got, before)
		}
		if ids := watcherIDs(t, w.bob, r1); strings.Contains(strings.Join(ids, " "), w.carolID.String()) {
			t.Errorf("the watchers of R1 in the trash name carol, who may not read it there: %v", ids)
		}
		want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/trash/"+w.reqID+"/restore", nil), http.StatusOK, "restore R1")
		if got := sees(t, w.carol); countOf(got.watched, r1) != 1 || countOf(got.told, r1) != 2 || got.unread != before.unread {
			t.Errorf("with R1 back carol sees %+v, before %+v", got, before)
		}
		if ids := watcherIDs(t, w.bob, r1); !slices.Contains(ids, w.carolID.String()) {
			t.Errorf("the watchers of R1 back from the trash leave carol out: %v", ids)
		}
		if n := h.countRows(t, `SELECT count(*) FROM notification WHERE requirement_id = $1`, r2ID); n == 0 {
			t.Error("nothing was told about R2")
		}
	})
}

// preferences is a whole set of preferences as the settings page saves it.
func preferences(inApp, email map[string]any, digest string, autoWatch bool) map[string]any {
	return map[string]any{"inApp": inApp, "email": email, "digest": digest, "autoWatch": autoWatch}
}

func TestPreferencesEachHaveTheirEffect(t *testing.T) {
	w := newCommentWorld(t, "WPE")
	h := w.h
	req := "/api/v1/requirements/" + w.reqKey
	watches := `SELECT count(*) FROM watch WHERE user_id = $1 AND requirement_id = $2`

	t.Run("with auto watch off, revising and being made the owner watch nothing", func(t *testing.T) {
		want(t, w.bob.put(t, "/api/v1/notification-preferences", preferences(map[string]any{}, map[string]any{}, "off", false)), http.StatusOK,
			"bob switches auto watch off")
		want(t, w.bob.patch(t, req, map[string]any{"title": "Brakes engage within 90 ms", "baseRevision": 1}), http.StatusOK, "bob revises R1")
		if n := h.countRows(t, watches, w.bobID, w.reqID); n != 0 {
			t.Errorf("revising made bob watch R1: %d", n)
		}
		want(t, w.owner.patch(t, req, map[string]any{"ownerId": w.bobID}), http.StatusOK, "make bob the owner")
		if n := h.countRows(t, watches, w.bobID, w.reqID); n != 0 {
			t.Errorf("being made the owner made bob watch R1: %d", n)
		}
		// Carol watches next, and must not hear of this revision should the worker come to it late.
		h.drained(t)
	})

	t.Run("a kind switched off by email alone is told in the app and not mailed", func(t *testing.T) {
		want(t, w.carol.put(t, req+"/watch", nil), http.StatusOK, "carol watches R1")
		want(t, w.carol.put(t, "/api/v1/notification-preferences", preferences(map[string]any{"revised": true}, map[string]any{"revised": false}, "off", true)),
			http.StatusOK, "carol takes revisions in the app alone")
		want(t, w.bob.patch(t, req, map[string]any{"title": "Brakes engage within 85 ms", "baseRevision": 2, "comment": "Tighter."}), http.StatusOK,
			"bob revises R1")
		h.drained(t)
		if got := told(t, w.carol, w.reqID); len(got) != 1 || got[0]["kind"] != "revised" || got[0]["revision"] != float64(3) {
			t.Errorf("carol was told %v in the app", got)
		}
		if subjects := caught(t, h.emailOf(t, w.ownerID), 1); len(subjects) == 0 {
			t.Fatal("the author, mailed at once, got no mail of the revisions")
		}
		if subjects := caught(t, h.emailOf(t, w.carolID), 0); len(subjects) != 0 {
			t.Errorf("carol, who switched revisions off by email, was mailed %v", subjects)
		}
	})
}

func TestAReadOnlyTokenChangesNoWatchOrNotification(t *testing.T) {
	w := newCommentWorld(t, "WRO")
	h := w.h
	req := "/api/v1/requirements/" + w.reqKey
	project := "/api/v1/projects/" + w.key + "/watch"
	want(t, w.carol.put(t, req+"/watch", nil), http.StatusOK, "carol watches R1")
	want(t, w.carol.put(t, project, nil), http.StatusOK, "carol watches the project")
	want(t, w.bob.patch(t, req, map[string]any{"title": "Brakes engage within 90 ms", "baseRevision": 1}), http.StatusOK, "bob revises R1")
	h.drained(t)
	_, secret := makeToken(t, w.carol, map[string]any{"name": "reader", "scopes": []string{"read"}})
	h.settle(t)
	reader := w.owner.api.withToken(w.carolID, secret)
	state := func() string {
		return strconv.Itoa(h.countRows(t, `SELECT count(*) FROM watch WHERE user_id = $1`, w.carolID)) + " watches, " +
			strconv.Itoa(h.countRows(t, `SELECT count(*) FROM watch_optout WHERE user_id = $1`, w.carolID)) + " opt-outs, " +
			strconv.Itoa(h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND read_at IS NULL`, w.carolID)) + " unread, " +
			strconv.Itoa(h.countRows(t, `SELECT count(*) FROM notification_preference WHERE user_id = $1`, w.carolID)) + " preferences"
	}
	before := state()
	if before != "2 watches, 0 opt-outs, 1 unread, 0 preferences" {
		t.Fatalf("carol starts with %s", before)
	}
	want(t, reader.get(t, "/api/v1/notifications/unread-count"), http.StatusOK, "the token reads")
	refuse(t, "saving preferences", reader.put(t, "/api/v1/notification-preferences", preferences(map[string]any{}, map[string]any{}, "daily", true)),
		http.StatusForbidden, "read_only_token")
	refuse(t, "marking everything read", reader.post(t, "/api/v1/notifications/read", map[string]any{"all": true}), http.StatusForbidden, "read_only_token")
	refuse(t, "unwatching R1", reader.delete(t, req+"/watch"), http.StatusForbidden, "read_only_token")
	refuse(t, "watching R1", reader.put(t, req+"/watch", nil), http.StatusForbidden, "read_only_token")
	refuse(t, "unwatching the project", reader.delete(t, project), http.StatusForbidden, "read_only_token")
	refuse(t, "watching the project", reader.put(t, project, nil), http.StatusForbidden, "read_only_token")
	if after := state(); after != before {
		t.Errorf("the refusals left %s, before %s", after, before)
	}
}

// revisedEvent is the event a revision writes, for a fan-out the test runs itself.
func revisedEvent(t *testing.T, actor uuid.UUID, requirementID string, revision int) events.Event {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"actorId": actor, "requirementId": requirementID, "revision": revision})
	if err != nil {
		t.Fatal(err)
	}
	return events.Event{ID: uuid.New(), Topic: requirement.TopicRevised, Payload: payload, CreatedAt: time.Now()}
}

func TestTheWorkerMailsOnEachPersonsSchedule(t *testing.T) {
	w := newCommentWorld(t, "WMS")
	h := w.h
	req := "/api/v1/requirements/" + w.reqKey
	if wait := time.Until(time.Now().Truncate(time.Hour).Add(time.Hour)); wait < digestHourMargin {
		time.Sleep(wait + time.Second)
	}
	want(t, w.dave.put(t, "/api/v1/notification-preferences", preferences(map[string]any{}, map[string]any{}, "hourly", true)), http.StatusOK,
		"dave asks for an hourly digest")
	want(t, w.dave.put(t, req+"/watch", nil), http.StatusOK, "dave watches R1")
	dave := h.emailOf(t, w.daveID)
	queued := func() int {
		return h.countRows(t, `SELECT count(*) FROM notification_digest WHERE user_id = $1`, w.daveID)
	}
	revision := 1
	revise := func(comment string) {
		t.Helper()
		want(t, w.bob.patch(t, req, map[string]any{"title": "Brakes " + comment, "baseRevision": revision, "comment": comment}), http.StatusOK, "revise R1")
		revision++
		h.drained(t)
	}
	digestAt := func(at time.Time) *mailbox {
		t.Helper()
		box := &mailbox{}
		if _, err := notify.NewDigester(h.cluster, box, testAppURL(t), discard()).WithClock(func() time.Time { return at }).Once(t.Context()); err != nil {
			t.Fatal(err)
		}
		return box
	}

	t.Run("an hourly digest waits for the full hour and leaves out what was read", func(t *testing.T) {
		revise("First look.")
		revise("Second look.")
		if n := queued(); n != 2 {
			t.Fatalf("%d notifications wait for dave's digest, want 2", n)
		}
		rows := told(t, w.dave, w.reqID)
		want(t, w.dave.post(t, "/api/v1/notifications/read", map[string]any{"ids": []string{rows[len(rows)-1]["id"].(string)}}), http.StatusNoContent,
			"dave reads the first")
		if got := digestAt(time.Now()).to(dave); len(got) != 0 {
			t.Fatalf("an hourly digest went before the hour: %v", got)
		}
		if n := queued(); n != 2 {
			t.Errorf("%d wait after a look before the hour, want 2", n)
		}
		got := digestAt(time.Now().UTC().Truncate(time.Hour).Add(time.Hour)).to(dave)
		if len(got) != 1 || got[0].Subject != "1 update in Exciter" || !strings.Contains(got[0].Body, "Second look.") || strings.Contains(got[0].Body, "First look.") {
			t.Fatalf("dave's digest is %v", got)
		}
		if n := queued(); n != 0 {
			t.Errorf("%d still wait after the digest went", n)
		}
	})

	t.Run("a queue whose rows were all read is emptied and sends nothing", func(t *testing.T) {
		revise("Third look.")
		if n := queued(); n != 1 {
			t.Fatalf("%d wait, want the third revision", n)
		}
		want(t, w.dave.post(t, "/api/v1/notifications/read", map[string]any{"all": true}), http.StatusNoContent, "dave reads everything")
		if got := digestAt(time.Now().UTC().Truncate(time.Hour).Add(2 * time.Hour)).to(dave); len(got) != 0 {
			t.Errorf("a digest of read notifications went: %v", got)
		}
		if n := queued(); n != 0 {
			t.Errorf("%d still wait", n)
		}
	})

	t.Run("an event handled twice writes one row per person and one mail", func(t *testing.T) {
		box := &mailbox{}
		fan := notify.NewFanOut(h.cluster, box, testAppURL(t), discard())
		event := revisedEvent(t, w.bobID, w.reqID, revision)
		for range 2 {
			if err := fan.Handle(t.Context(), event); err != nil {
				t.Fatal(err)
			}
		}
		for name, id := range map[string]uuid.UUID{"the author": w.ownerID, "dave": w.daveID} {
			if n := h.countRows(t, `SELECT count(*) FROM notification WHERE event_id = $1 AND user_id = $2`, event.ID, id); n != 1 {
				t.Errorf("%s has %d rows of the event, want 1", name, n)
			}
		}
		if got := box.to(h.emailOf(t, w.ownerID)); len(got) != 1 {
			t.Errorf("the author, mailed at once, got %d mails of the event", len(got))
		}
		if n := h.countRows(t, `SELECT count(*) FROM notification_digest q JOIN notification n ON n.id = q.notification_id WHERE n.event_id = $1`,
			event.ID); n != 1 {
			t.Errorf("%d rows of the event wait for a digest, want dave's", n)
		}
	})
}
