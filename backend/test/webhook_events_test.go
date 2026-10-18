//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/webhook"
)

// Every event a webhook may take reaches the receiver as its owner may read it, about the projects it listens to only.
func TestRequirementEventsReachTheReceiverAsTheOwnerMayReadThem(t *testing.T) {
	w := newCommentWorld(t, "HKE")
	w.h.keepWebhooks(t)
	h := w.h
	elsewhere := projectKey(t, "HKO")
	want(t, w.owner.post(t, "/api/v1/projects", map[string]any{"key": elsewhere, "name": "Elsewhere"}), http.StatusCreated, "make another project")
	bin, address := hookBin(t)
	id, plain := makeHook(t, w.owner, "Everything here", address, []string{w.key}, webhook.TopicAny)
	otherBin, otherAddress := hookBin(t)
	makeHook(t, w.owner, "Elsewhere only", otherAddress, []string{elsewhere}, webhook.TopicAny)
	r1, r2 := w.key+"-R1", w.key+"-R2"

	want(t, w.bob.patch(t, "/api/v1/requirements/"+r1, map[string]any{"title": "Brakes engage within 90 ms", "baseRevision": 1}), http.StatusOK, "bob revises R1")
	r2ID := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/requirements", map[string]any{"title": "Pedal sensor"}), http.StatusCreated, "write R2"), "requirement")["id"].(string)
	link := obj(t, want(t, w.bob.post(t, "/api/v1/requirements/"+r2+"/links", map[string]any{"typeKey": "derives", "otherKey": r1}), http.StatusCreated, "link R2 to R1"), "link")["id"].(string)
	want(t, w.bob.patch(t, "/api/v1/requirements/"+r1, map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 2}), http.StatusOK, "bob revises R1 again")
	want(t, w.bob.post(t, "/api/v1/links/"+link+"/clear", map[string]any{"sourceRevision": 1, "targetRevision": 3, "comment": "Still derived."}), http.StatusOK, "bob clears the link")
	want(t, w.bob.patch(t, "/api/v1/requirements/"+r2, map[string]any{"priority": "must"}), http.StatusOK, "bob sets R2's priority")
	thread := obj(t, want(t, w.dave.post(t, "/api/v1/requirements/"+r1+"/comments", commentBody("Why 80 ms?")), http.StatusCreated, "dave comments"), "thread")["id"].(string)
	want(t, w.bob.put(t, "/api/v1/comment-threads/"+thread+"/resolution", nil), http.StatusOK, "bob resolves")
	want(t, w.owner.put(t, "/api/v1/projects/"+w.key+"/approval-rules", map[string]any{"required": 1, "authorMayApprove": false,
		"approvers": []any{map[string]any{"subjectType": "user", "userId": w.daveID}}}), http.StatusOK, "dave approves")
	review := obj(t, want(t, w.bob.post(t, "/api/v1/requirements/"+r1+"/reviews", map[string]any{"revision": 3}), http.StatusCreated, "bob asks for R1"), "review")["id"].(string)
	want(t, w.dave.post(t, "/api/v1/reviews/"+review+"/decisions", map[string]any{"decision": "approve", "revision": 3}), http.StatusOK, "dave approves R1")
	withdrawn := obj(t, want(t, w.bob.post(t, "/api/v1/requirements/"+r2+"/reviews", map[string]any{"revision": 1}), http.StatusCreated, "bob asks for R2"), "review")["id"].(string)
	want(t, w.bob.post(t, "/api/v1/reviews/"+withdrawn+"/withdraw", nil), http.StatusOK, "bob withdraws")
	want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/baselines", map[string]any{"name": "Release 1"}), http.StatusCreated, "take a baseline")
	want(t, w.bob.delete(t, "/api/v1/links/"+link), http.StatusNoContent, "bob removes the link")
	want(t, w.bob.delete(t, "/api/v1/requirements/"+r2), http.StatusNoContent, "bob trashes R2")
	want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/trash/"+r2ID+"/restore", nil), http.StatusOK, "bob restores R2")

	for _, topic := range webhook.Topics {
		if topic == webhook.TopicRequirementMoved {
			continue
		}
		asDocumented(t, awaitTopic(t, bin, topic, w.key, 1)[0], plain)
	}

	revised := awaitTopic(t, bin, webhook.TopicRequirementRevised, r1, 2)
	p := revised[0].payload()
	req := p["requirement"].(map[string]any)
	if req["key"] != r1 || req["project"].(map[string]any)["key"] != w.key || !strings.HasSuffix(req["url"].(string), "/p/"+w.key+"/r/"+r1) ||
		p["actor"].(map[string]any)["id"] != w.bobID.String() || asNumber(p["revision"]) < 2 {
		t.Errorf("a revision reads %s", revised[0].Body)
	}
	created := awaitTopic(t, bin, webhook.TopicLinkCreated, r2, 1)[0].payload()["link"].(map[string]any)
	if created["source"].(map[string]any)["key"] != r2 || created["target"].(map[string]any)["key"] != r1 || created["type"].(map[string]any)["key"] != "derives" {
		t.Errorf("a link reads %v", created)
	}
	if c := awaitTopic(t, bin, webhook.TopicCommentCreated, r1, 1)[0].payload()["comment"].(map[string]any); c["text"] != "Why 80 ms?" || c["threadId"] != thread {
		t.Errorf("a comment reads %v", c)
	}
	if d := awaitTopic(t, bin, webhook.TopicReviewDecided, r1, 1)[0].payload(); d["decision"] != "approve" || d["signed"] != false ||
		d["review"].(map[string]any)["state"] != "approved" {
		t.Errorf("a decision reads %v", d)
	}
	awaitTopic(t, bin, webhook.TopicRequirementChanged, `"field":"priority"`, 1)
	if l := awaitTopic(t, bin, webhook.TopicLinkCleared, r2, 1)[0].payload()["link"].(map[string]any); l["kind"] != webhook.ClearedTrace || l["id"] != link {
		t.Errorf("a cleared link reads %v", l)
	}
	if b := awaitTopic(t, bin, webhook.TopicBaselineTaken, w.key, 1)[0].payload()["baseline"].(map[string]any); b["name"] != "Release 1" {
		t.Errorf("a baseline reads %v", b)
	}
	h.drained(t)
	for _, d := range binDeliveries(t, otherBin) {
		if strings.Contains(d.Body, w.key) {
			t.Fatalf("a webhook listening to another project heard this one: %s", d.Body)
		}
	}

	t.Run("a redelivery is the same event again, as the next attempt", func(t *testing.T) {
		log := h.awaitState(t, id, webhook.TopicRequirementCreated, r2ID, "delivered", 1)
		again := obj(t, want(t, w.owner.post(t, "/api/v1/webhooks/"+id+"/deliveries/"+log[0].ID.String()+"/redeliver", nil), http.StatusOK, "redeliver"), "delivery")
		if again["state"] != "delivered" || asNumber(again["attempt"]) != 2 || again["manual"] != true {
			t.Fatalf("the redelivery reads %v", again)
		}
		both := awaitTopic(t, bin, webhook.TopicRequirementCreated, r2ID, 2)
		if both[0].env["id"] != both[1].env["id"] || both[0].Headers[webhook.DeliveryHeader] == both[1].Headers[webhook.DeliveryHeader] {
			t.Error("a redelivery does not repeat the event under a delivery of its own")
		}
	})
}

func TestAFailingReceiverIsRetriedThenTurnedOff(t *testing.T) {
	w := newCommentWorld(t, "HKR")
	w.h.keepWebhooks(t)
	h := w.h
	h.started(t)
	bin, address := hookBin(t)
	binAnswers(t, bin, http.StatusServiceUnavailable)
	id, _ := makeHook(t, w.owner, "Flaky", address, []string{w.key}, webhook.TopicRequirementRevised)
	revise := func(title string, base int) {
		want(t, w.bob.patch(t, "/api/v1/requirements/"+w.reqKey, map[string]any{"title": title, "baseRevision": base}), http.StatusOK, "revise to "+title)
	}

	revise("Brakes engage within 90 ms", 1)
	log := h.awaitState(t, id, webhook.TopicRequirementRevised, `"revision": 2`, "pending", 2)
	if len(log) != 2 || log[0].State != "failed" || !strings.Contains(log[0].Error, "503") || log[1].Attempt != 2 {
		t.Fatalf("after one failure the log reads %+v", log)
	}
	var seconds float64
	if err := h.super.QueryRow(context.Background(), `
		SELECT extract(epoch FROM next_attempt_at - (SELECT attempted_at FROM webhook_delivery WHERE id = $2))::float8
		FROM webhook_delivery WHERE id = $1`, log[1].ID, log[0].ID).Scan(&seconds); err != nil {
		t.Fatal(err)
	}
	if wait := time.Duration(seconds * float64(time.Second)); wait < webhook.Backoff[0]-5*time.Second || wait > webhook.Backoff[0]+5*time.Second {
		t.Errorf("the second attempt is due %s after the first, want %s", wait, webhook.Backoff[0])
	}

	binAnswers(t, bin, http.StatusOK)
	h.cleanupExec(t, h.super, `UPDATE webhook_delivery SET next_attempt_at = now() WHERE id = $1`, log[1].ID)
	log = h.awaitState(t, id, webhook.TopicRequirementRevised, `"revision": 2`, "delivered", 2)
	got := awaitTopic(t, bin, webhook.TopicRequirementRevised, w.reqKey, 2)
	if got[0].env["id"] != got[1].env["id"] {
		t.Error("a retry is not the same event")
	}
	if n := h.countRows(t, `SELECT failures FROM webhook_endpoint WHERE id = $1`, id); n != 0 {
		t.Errorf("a delivery left %d failures counted", n)
	}

	t.Run("a webhook failing for a day is turned off, and turning it on starts again", func(t *testing.T) {
		binAnswers(t, bin, http.StatusInternalServerError)
		h.cleanupExec(t, h.super, `UPDATE webhook_endpoint SET failures = $2, failing_since = now() - interval '25 hours' WHERE id = $1`,
			id, webhook.DisableAfterFailures-1)
		revise("Brakes engage within 85 ms", 2)
		if log := h.awaitState(t, id, webhook.TopicRequirementRevised, `"revision": 3`, "failed", 1); len(log) != 1 {
			t.Errorf("a webhook turned off still tries again: %+v", log)
		}
		h.settle(t)
		hook := list(t, want(t, w.owner.get(t, "/api/v1/webhooks"), http.StatusOK, "the list"), "webhooks")[0].(map[string]any)
		if hook["enabled"] != false || hook["disabledReason"] != webhook.ReasonFailing || asNumber(hook["failures"]) != webhook.DisableAfterFailures {
			t.Fatalf("the failing webhook reads %v", hook)
		}
		if rows := h.auditRows(t, audit.ActionWebhookDisabled, uuid.MustParse(id)); len(rows) != 1 || rows[0]["byProvider"] != true {
			t.Errorf("the worker's turning it off is recorded as %v", rows)
		}
		revise("Brakes engage within 82 ms", 3)
		h.drained(t)
		if n := h.countRows(t, `SELECT count(*) FROM webhook_delivery WHERE endpoint_id = $1 AND event ->> 'revision' = '4'`, id); n != 0 {
			t.Errorf("a webhook that is off was queued %d attempts", n)
		}
		binAnswers(t, bin, http.StatusNoContent)
		on := obj(t, want(t, w.owner.patch(t, "/api/v1/webhooks/"+id, map[string]any{"name": "Flaky", "url": address,
			"topics": []string{webhook.TopicRequirementRevised}, "projects": []string{w.key}, "enabled": true}), http.StatusOK, "turn it on again"), "webhook")
		if on["enabled"] != true || on["disabledReason"] != nil || asNumber(on["failures"]) != 0 {
			t.Errorf("turned on again it reads %v", on)
		}
	})
}

func TestAPayloadIsWithheldWhatItsOwnerMayNotViewWhenItIsSent(t *testing.T) {
	w := newCommentWorld(t, "HKW")
	w.h.keepWebhooks(t)
	h := w.h
	annID := h.addPerson(t, "admin")
	ann := newAPIServer(t, h).as(t, annID)
	if _, err := h.super.Exec(context.Background(), `DELETE FROM project_grant WHERE project_id = $1 AND subject_type = 'everyone'`, w.projectID); err != nil {
		t.Fatal(err)
	}
	bin, address := hookBin(t)
	id, _ := makeHook(t, ann, "Ann's", address, nil, webhook.TopicRequirementRevised)

	want(t, w.bob.patch(t, "/api/v1/requirements/"+w.reqKey, map[string]any{"title": "Brakes engage within 90 ms", "baseRevision": 1}), http.StatusOK, "bob revises")
	awaitTopic(t, bin, webhook.TopicRequirementRevised, w.reqKey, 1)

	// Ann made it as an administrator; as a member who is granted nothing, she may not see the project.
	h.cleanupExec(t, h.super, `UPDATE app_user SET role = 'member' WHERE id = $1`, annID)
	h.settle(t)
	want(t, w.bob.patch(t, "/api/v1/requirements/"+w.reqKey, map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 2}), http.StatusOK, "bob revises again")
	log := h.awaitState(t, id, webhook.TopicRequirementRevised, `"revision": 3`, "withheld", 1)
	if !strings.Contains(log[0].Error, "may not view") {
		t.Errorf("a withheld delivery says %q", log[0].Error)
	}
	for _, d := range binDeliveries(t, bin) {
		if strings.Contains(d.Body, "80 ms") {
			t.Fatalf("the receiver got what its owner may not view: %s", d.Body)
		}
	}

	t.Run("a redelivery reads the requirement afresh", func(t *testing.T) {
		delivered := h.awaitState(t, id, webhook.TopicRequirementRevised, `"revision": 2`, "delivered", 1)
		again := obj(t, want(t, w.owner.post(t, "/api/v1/webhooks/"+id+"/deliveries/"+delivered[0].ID.String()+"/redeliver", nil), http.StatusOK, "redeliver"), "delivery")
		if again["state"] != "withheld" {
			t.Errorf("a redelivery of what the owner may no longer view reads %v", again)
		}
	})

	t.Run("an owner switched off withholds everything but a ping", func(t *testing.T) {
		h.cleanupExec(t, h.super, `UPDATE app_user SET is_active = false WHERE id = $1`, annID)
		h.settle(t)
		want(t, w.bob.patch(t, "/api/v1/requirements/"+w.reqKey, map[string]any{"title": "Brakes engage within 70 ms", "baseRevision": 3}), http.StatusOK, "bob revises once more")
		log := h.awaitState(t, id, webhook.TopicRequirementRevised, `"revision": 4`, "withheld", 1)
		if !strings.Contains(log[0].Error, "switched off") {
			t.Errorf("a webhook whose owner left says %q", log[0].Error)
		}
		if sent := obj(t, want(t, w.owner.post(t, "/api/v1/webhooks/"+id+"/test", nil), http.StatusOK, "ping"), "delivery"); sent["state"] != "delivered" {
			t.Errorf("a ping without an owner reads %v", sent)
		}
	})
}

func TestTheServerNeverPostsWebhooksIntoItsOwnNetwork(t *testing.T) {
	w := newCommentWorld(t, "HKN")
	w.h.keepWebhooks(t)
	for _, inside := range []string{"http://10.0.0.1/hook", "http://169.254.169.254/latest/meta-data", "http://localhost:8080/api/v1/auth/me"} {
		fieldError(t, w.owner.post(t, "/api/v1/webhooks", map[string]any{"name": "In", "url": inside, "topics": []string{"*"}}), "url")
	}
	// A name says nothing until it resolves; the guard refuses where it leads.
	id, _ := makeHook(t, w.owner, "Valkey", "http://valkey:6379/", []string{w.key}, webhook.TopicAny)
	sent := obj(t, want(t, w.owner.post(t, "/api/v1/webhooks/"+id+"/test", nil), http.StatusOK, "ping valkey"), "delivery")
	if sent["state"] != "failed" || sent["status"] != nil || !strings.Contains(sent["error"].(string), "EXCITER_OUTBOUND_ALLOW") || strings.Contains(sent["error"].(string), "6379") {
		t.Errorf("a ping into the network reads %v", sent)
	}
}

// Straight through SQL as exciter_app, an administrator never reads a secret, names an owner or writes the log, and a
// member touches none of it.
func TestRawSQLKeepsWebhooksToAdministrators(t *testing.T) {
	w := newCommentWorld(t, "HKS")
	w.h.keepWebhooks(t)
	h := w.h
	// The world's own events would otherwise reach the webhook made below and add deliveries to the log it counts.
	h.drained(t)
	_, address := hookBin(t)
	id, _ := makeHook(t, w.owner, "Walled", address, []string{w.key}, webhook.TopicAny)
	want(t, w.owner.post(t, "/api/v1/webhooks/"+id+"/test", nil), http.StatusOK, "a ping for the log")
	h.settle(t)
	ctx := context.Background()

	admin := h.appConn(t, w.ownerID)
	if n := countAs(t, admin, `SELECT count(*) FROM webhook_endpoint`) + countAs(t, admin, `SELECT count(*) FROM webhook_delivery`); n != 2 {
		t.Fatalf("the administrator reads %d rows of their webhook and its log", n)
	}
	refusedOutright(t, admin, "read a sealed secret", `SELECT secret_sealed FROM webhook_endpoint`)
	refusedOutright(t, admin, "name another owner", `UPDATE webhook_endpoint SET owner_id = $2 WHERE id = $1`, id, w.bobID)
	refusedOutright(t, admin, "clear the failures", `UPDATE webhook_endpoint SET failures = 0, failing_since = NULL WHERE id = $1`, id)
	refusedOutright(t, admin, "forge a delivered attempt", `INSERT INTO webhook_delivery (endpoint_id, event_id, topic, event, occurred_at, state)
		VALUES ($1, $2, 'ping', '{}', now(), 'delivered')`, id, uuid.New())
	refusedOutright(t, admin, "rewrite the log", `UPDATE webhook_delivery SET state = 'delivered'`)
	refusedOutright(t, admin, "erase the log", `DELETE FROM webhook_delivery`)
	refusedOutright(t, admin, "take a topic nobody emits", `UPDATE webhook_endpoint SET topics = '{armature.links}' WHERE id = $1`, id)
	if _, err := admin.Exec(ctx, `UPDATE webhook_endpoint SET name = 'Renamed' WHERE id = $1`, id); err != nil {
		t.Fatalf("the administrator may rename their webhook: %v", err)
	}

	member := h.appConn(t, w.bobID)
	if n := countAs(t, member, `SELECT count(*) FROM webhook_endpoint`) + countAs(t, member, `SELECT count(*) FROM webhook_delivery`); n != 0 {
		t.Errorf("a member reads %d rows", n)
	}
	refusedOutright(t, member, "a member adds a webhook", `INSERT INTO webhook_endpoint (name, url, secret_sealed, topics) VALUES ('Mine', 'https://x.example', '\x01', '{*}')`)
	touchesNothing(t, member, "a member deletes it", `DELETE FROM webhook_endpoint WHERE id = $1`, id)
	touchesNothing(t, member, "a member turns it off", `UPDATE webhook_endpoint SET enabled = false WHERE id = $1`, id)
	var owner uuid.UUID
	if err := h.super.QueryRow(ctx, `SELECT owner_id FROM webhook_endpoint WHERE id = $1`, id).Scan(&owner); err != nil || owner != w.ownerID {
		t.Errorf("a save through SQL leaves the owner %s (%v)", owner, err)
	}
}

// countAs counts what a statement finds on a connection acting as somebody.
func countAs(t *testing.T, conn *pgx.Conn, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}
