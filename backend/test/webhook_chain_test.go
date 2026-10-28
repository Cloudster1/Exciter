//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/webhook"
)

// The ends of the webhook delivery chain (#328): the last retry, what turning a webhook off cancels, the log pruned,
// and every sentence a failed attempt can carry, read back from the log as the worker or the API wrote it.

// The sentences a delivery's log shows, as backend/internal/webhook/deliver.go words them for the administrator.
const (
	sentenceOff       = "The webhook was turned off before this was sent."
	sentenceTimeout   = "The receiver did not answer in time. Check that it is running, then send it again."
	sentenceUnreached = "The receiver could not be reached. Check the address, then send it again."
	sentenceNoSecret  = "The webhook's secret does not open with this server's key. Rotate the secret and give the receiver the new one."
	sentenceBlocked   = "This address is inside the server's own network, so Exciter may not post to it. Use the receiver's public address, or ask the operator to add the host to EXCITER_OUTBOUND_ALLOW."
)

const (
	// waitSlack is how far a retry's due time may stray from its backoff: the worker's clock against the database's.
	waitSlack = 5 * time.Second
	// receiverHold keeps a bin from answering past the sender's ten second timeout.
	receiverHold = 15 * time.Second
	// innerAddress is where a redirect sends the sender: inside the network, which no allow list names.
	innerAddress = "http://10.0.0.1/"
	// sideBySide is how many due deliveries two senders share at once.
	sideBySide = 50
	// longAgo is past DeliveryRetention by a day.
	longAgo = webhook.DeliveryRetention + 24*time.Hour
)

// binAnswersWith makes a bin answer as told from now on: a status, a Location to send the sender to, or a hold.
func binAnswersWith(t *testing.T, bin string, answer map[string]any) {
	t.Helper()
	body, _ := json.Marshal(answer)
	req, _ := http.NewRequest(http.MethodPut, os.Getenv(envArmatureStub)+"/_hooks/"+bin+"/status", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("tell the bin %v: %v %v", answer, resp, err)
	}
	resp.Body.Close()
}

// dueNow makes a pending attempt due, so the stack's worker sends it at its next look.
func dueNow(t *testing.T, h *harness, delivery uuid.UUID) {
	t.Helper()
	h.cleanupExec(t, h.super, `UPDATE webhook_delivery SET next_attempt_at = now() WHERE id = $1 AND state = 'pending'`, delivery)
}

// waitBefore is how long after the earlier attempt was tried the later one is due.
func waitBefore(t *testing.T, h *harness, earlier, later uuid.UUID) time.Duration {
	t.Helper()
	var seconds float64
	if err := h.super.QueryRow(context.Background(), `
		SELECT extract(epoch FROM next_attempt_at - (SELECT attempted_at FROM webhook_delivery WHERE id = $2))::float8
		FROM webhook_delivery WHERE id = $1`, later, earlier).Scan(&seconds); err != nil {
		t.Fatal(err)
	}
	return time.Duration(seconds * float64(time.Second))
}

// plant writes a delivery straight into the log, as the worker would have queued it.
func plant(t *testing.T, h *harness, endpoint, state string, manual bool, version int, created, due time.Duration) uuid.UUID {
	t.Helper()
	var next *time.Duration
	if state == string(webhook.StatePending) {
		next = &due
	}
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `
		INSERT INTO webhook_delivery (endpoint_id, event_id, topic, event, occurred_at, state, manual, version, next_attempt_at, created_at)
		VALUES ($1, $2, 'ping', '{}', now(), $3, $4, $5,
		        CASE WHEN $6::float8 IS NULL THEN NULL ELSE now() + make_interval(secs => $6::float8) END,
		        now() - make_interval(secs => $7))
		RETURNING id`, endpoint, uuid.New(), state, manual, version, secondsOf(next), created.Seconds()).Scan(&id); err != nil {
		t.Fatalf("plant a %s delivery: %v", state, err)
	}
	return id
}

func secondsOf(d *time.Duration) *float64 {
	if d == nil {
		return nil
	}
	s := d.Seconds()
	return &s
}

// deliveryState reads one logged attempt's state and sentence.
func deliveryState(t *testing.T, h *harness, id uuid.UUID) (state, why string) {
	t.Helper()
	if err := h.super.QueryRow(context.Background(), `SELECT state, error FROM webhook_delivery WHERE id = $1`, id).Scan(&state, &why); err != nil {
		t.Fatalf("read delivery %s: %v", id, err)
	}
	return state, why
}

// awaitDelivery waits until a logged attempt has left pending, and answers its state and sentence.
func awaitDelivery(t *testing.T, h *harness, id uuid.UUID) (state, why string) {
	t.Helper()
	deadline := time.Now().Add(deliveryWait)
	for {
		if state, why = deliveryState(t, h, id); state != string(webhook.StatePending) {
			return state, why
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivery %s is still pending after %s", id, deliveryWait)
		}
		time.Sleep(workerIdle * 4)
	}
}

func failuresOf(t *testing.T, h *harness, endpoint string) int {
	t.Helper()
	return h.countRows(t, `SELECT failures FROM webhook_endpoint WHERE id = $1`, endpoint)
}

func pendingOf(t *testing.T, h *harness, endpoint string) int {
	t.Helper()
	return h.countRows(t, `SELECT count(*) FROM webhook_delivery WHERE endpoint_id = $1 AND state = 'pending'`, endpoint)
}

// A receiver that never recovers is tried MaxAttempts times, each retry after the next wait of Backoff, and then left.
func TestTheRetriesRunToTheLastAttemptThenStop(t *testing.T) {
	w := newCommentWorld(t, "HKL")
	w.h.keepWebhooks(t)
	h := w.h
	bin, address := hookBin(t)
	binAnswers(t, bin, http.StatusServiceUnavailable)
	id, _ := makeHook(t, w.owner, "Down", address, []string{w.key}, webhook.TopicRequirementRevised)
	want(t, w.bob.patch(t, "/api/v1/requirements/"+w.reqKey, map[string]any{"title": "Brakes engage within 90 ms", "baseRevision": 1}), http.StatusOK, "revise")

	for attempt := 2; attempt <= webhook.MaxAttempts; attempt++ {
		log := h.awaitState(t, id, webhook.TopicRequirementRevised, `"revision": 2`, "pending", attempt)
		if len(log) != attempt || log[attempt-2].State != "failed" || log[attempt-1].Attempt != attempt {
			t.Fatalf("before attempt %d the log reads %+v", attempt, log)
		}
		backoff := webhook.Backoff[attempt-2]
		if wait := waitBefore(t, h, log[attempt-2].ID, log[attempt-1].ID); wait < backoff-waitSlack || wait > backoff+waitSlack {
			t.Errorf("attempt %d is due %s after the one before, want %s", attempt, wait, backoff)
		}
		dueNow(t, h, log[attempt-1].ID)
	}
	log := h.awaitState(t, id, webhook.TopicRequirementRevised, `"revision": 2`, "failed", webhook.MaxAttempts)
	if len(log) != webhook.MaxAttempts {
		t.Fatalf("the chain ended with %d attempts: %+v", len(log), log)
	}
	for _, d := range log {
		if d.State != "failed" || !strings.Contains(d.Error, "503") {
			t.Errorf("attempt %d reads %s: %q", d.Attempt, d.State, d.Error)
		}
	}
	if n := pendingOf(t, h, id); n != 0 {
		t.Errorf("after the last attempt %d attempts are still pending", n)
	}
	got := awaitTopic(t, bin, webhook.TopicRequirementRevised, w.reqKey, webhook.MaxAttempts)
	for _, d := range got {
		if d.env["id"] != got[0].env["id"] {
			t.Errorf("a retry carried event %v, the first %v", d.env["id"], got[0].env["id"])
		}
	}
	if n := failuresOf(t, h, id); n != webhook.MaxAttempts {
		t.Errorf("the webhook counts %d failures, want %d", n, webhook.MaxAttempts)
	}
}

// What is due when a webhook is turned off is cancelled when it comes due, and what the worker turns off cancels the
// automatic attempts it held, but not one somebody asked for by hand.
func TestTurningAWebhookOffCancelsWhatIsDue(t *testing.T) {
	w := newCommentWorld(t, "HKO")
	w.h.keepWebhooks(t)
	h := w.h
	bin, address := hookBin(t)
	binAnswers(t, bin, http.StatusServiceUnavailable)
	id, _ := makeHook(t, w.owner, "Paused", address, []string{w.key}, webhook.TopicRequirementRevised)
	want(t, w.bob.patch(t, "/api/v1/requirements/"+w.reqKey, map[string]any{"title": "Brakes engage within 90 ms", "baseRevision": 1}), http.StatusOK, "revise")
	log := h.awaitState(t, id, webhook.TopicRequirementRevised, `"revision": 2`, "pending", 2)

	want(t, w.owner.patch(t, "/api/v1/webhooks/"+id, map[string]any{"name": "Paused", "url": address, "topics": []string{webhook.TopicRequirementRevised},
		"projects": []string{w.key}, "enabled": false}), http.StatusOK, "turn it off")
	binAnswers(t, bin, http.StatusNoContent)
	dueNow(t, h, log[1].ID)
	log = h.awaitState(t, id, webhook.TopicRequirementRevised, `"revision": 2`, "cancelled", 2)
	if len(log) != 2 || log[1].Error != sentenceOff {
		t.Errorf("the retry due after turning it off reads %+v", log)
	}
	if got := binDeliveries(t, bin); len(got) != 1 {
		t.Errorf("the bin holds %d deliveries, want only the first attempt", len(got))
	}

	t.Run("the worker turning it off cancels its automatic attempts and keeps one asked for by hand", func(t *testing.T) {
		failing, failingAddress := hookBin(t)
		binAnswers(t, failing, http.StatusServiceUnavailable)
		other, _ := makeHook(t, w.owner, "Failing", failingAddress, []string{w.key}, webhook.TopicRequirementRevised)
		h.cleanupExec(t, h.super, `UPDATE webhook_endpoint SET failures = $2, failing_since = now() - interval '25 hours' WHERE id = $1`,
			other, webhook.DisableAfterFailures-1)
		automatic := plant(t, h, other, "pending", false, webhook.Version, 0, time.Hour)
		byHand := plant(t, h, other, "pending", true, webhook.Version, 0, time.Hour)
		want(t, w.bob.patch(t, "/api/v1/requirements/"+w.reqKey, map[string]any{"title": "Brakes engage within 85 ms", "baseRevision": 2}), http.StatusOK, "revise again")
		h.awaitState(t, other, webhook.TopicRequirementRevised, `"revision": 3`, "failed", 1)
		if state, why := deliveryState(t, h, automatic); state != "cancelled" || why != sentenceOff {
			t.Errorf("an automatic attempt due later reads %s: %q", state, why)
		}
		if state, _ := deliveryState(t, h, byHand); state != "pending" {
			t.Errorf("an attempt asked for by hand reads %s, want it kept pending", state)
		}
		if n := h.countRows(t, `SELECT count(*) FROM webhook_endpoint WHERE id = $1 AND NOT enabled AND disabled_reason = $2`, other, webhook.ReasonFailing); n != 1 {
			t.Error("the worker did not turn the failing webhook off")
		}
	})
}

// A webhook on one topic gets no delivery for the others in the project it listens to.
func TestAWebhookHearsOnlyItsTopics(t *testing.T) {
	w := newCommentWorld(t, "HKT")
	w.h.keepWebhooks(t)
	h := w.h
	_, address := hookBin(t)
	id, _ := makeHook(t, w.owner, "Revisions", address, []string{w.key}, webhook.TopicRequirementRevised)
	everything, everythingAddress := hookBin(t)
	makeHook(t, w.owner, "Everything", everythingAddress, []string{w.key}, webhook.TopicAny)

	r2 := w.key + "-R2"
	want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/requirements", map[string]any{"title": "Pedal sensor"}), http.StatusCreated, "write R2")
	want(t, w.bob.post(t, "/api/v1/requirements/"+r2+"/links", map[string]any{"typeKey": "derives", "otherKey": w.reqKey}), http.StatusCreated, "link R2 to R1")
	want(t, w.dave.post(t, "/api/v1/requirements/"+w.reqKey+"/comments", commentBody("Why 100 ms?")), http.StatusCreated, "dave comments")
	want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/baselines", map[string]any{"name": "Release 1"}), http.StatusCreated, "take a baseline")
	want(t, w.bob.patch(t, "/api/v1/requirements/"+w.reqKey, map[string]any{"title": "Brakes engage within 90 ms", "baseRevision": 1}), http.StatusOK, "revise")

	for _, topic := range []string{webhook.TopicLinkCreated, webhook.TopicCommentCreated, webhook.TopicBaselineTaken} {
		awaitTopic(t, everything, topic, w.key, 1)
	}
	h.awaitState(t, id, webhook.TopicRequirementRevised, `"revision": 2`, "delivered", 1)
	h.drained(t)
	if n := h.countRows(t, `SELECT count(*) FROM webhook_delivery WHERE endpoint_id = $1 AND topic <> $2`, id, webhook.TopicRequirementRevised); n != 0 {
		t.Errorf("a webhook on revisions alone was queued %d deliveries of other topics", n)
	}
	if n := h.countRows(t, `SELECT count(*) FROM webhook_delivery WHERE endpoint_id = $1`, id); n != 1 {
		t.Errorf("a webhook on revisions alone holds %d deliveries, want the one revision", n)
	}
}

// Sending again names a delivery of the webhook in its address; another webhook's is not here.
func TestARedeliveryOfAnotherWebhooksDeliveryIsNotFound(t *testing.T) {
	w := newCommentWorld(t, "HKD")
	w.h.keepWebhooks(t)
	bin, address := hookBin(t)
	mine, _ := makeHook(t, w.owner, "Mine", address, []string{w.key}, webhook.TopicRequirementRevised)
	otherBin, otherAddress := hookBin(t)
	theirs, _ := makeHook(t, w.owner, "Theirs", otherAddress, []string{w.key}, webhook.TopicRequirementRevised)
	want(t, w.owner.post(t, "/api/v1/webhooks/"+mine+"/test", nil), http.StatusOK, "ping mine")
	pinged := obj(t, want(t, w.owner.post(t, "/api/v1/webhooks/"+theirs+"/test", nil), http.StatusOK, "ping theirs"), "delivery")

	refuse(t, "a delivery of another webhook", w.owner.post(t, "/api/v1/webhooks/"+mine+"/deliveries/"+pinged["id"].(string)+"/redeliver", nil),
		http.StatusNotFound, "not_found")
	for hook, n := range map[string]int{mine: 1, theirs: 1} {
		if got := len(list(t, want(t, w.owner.get(t, "/api/v1/webhooks/"+hook+"/deliveries"), http.StatusOK, "the log"), "deliveries")); got != n {
			t.Errorf("webhook %s holds %d deliveries after the refusal, want %d", hook, got, n)
		}
	}
	if a, b := len(binDeliveries(t, bin)), len(binDeliveries(t, otherBin)); a != 1 || b != 1 {
		t.Errorf("the bins hold %d and %d deliveries, want one ping each", a, b)
	}
}

// Prune deletes what is past DeliveryRetention and keeps what is still due, however old.
func TestPruneKeepsWhatIsStillDue(t *testing.T) {
	w := newCommentWorld(t, "HKP")
	w.h.keepWebhooks(t)
	h := w.h
	_, address := hookBin(t)
	id, _ := makeHook(t, w.owner, "Old", address, []string{w.key}, webhook.TopicRequirementRevised)
	plant(t, h, id, "delivered", false, webhook.Version, longAgo, 0)
	plant(t, h, id, "failed", false, webhook.Version, longAgo, 0)
	due := plant(t, h, id, "pending", false, webhook.Version, longAgo, time.Hour)
	recent := plant(t, h, id, "delivered", false, webhook.Version, time.Hour, 0)

	// keepWebhooks emptied the log, so the only attempts past retention are the ones planted here.
	gone, err := h.webhooks(t).Prune(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gone != 2 {
		t.Errorf("Prune answered %d, want the old delivered and failed attempts", gone)
	}
	rows, err := h.super.Query(context.Background(), `SELECT id FROM webhook_delivery WHERE endpoint_id = $1 ORDER BY created_at`, id)
	if err != nil {
		t.Fatal(err)
	}
	var left []uuid.UUID
	for rows.Next() {
		var d uuid.UUID
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		left = append(left, d)
	}
	rows.Close()
	if len(left) != 2 || left[0] != due || left[1] != recent {
		t.Errorf("after pruning the log holds %v, want the old pending %s and the recent %s", left, due, recent)
	}
}

// An attempt that never reaches its receiver says why, counts against the webhook and is tried again.
func TestAFailureThatNeverReachedTheReceiverSaysWhy(t *testing.T) {
	w := newCommentWorld(t, "HKF")
	w.h.keepWebhooks(t)
	h := w.h
	slow, slowAddress := hookBin(t)
	binAnswersWith(t, slow, map[string]any{"status": http.StatusNoContent, "holdMs": receiverHold.Milliseconds()})
	silent, _ := makeHook(t, w.owner, "Silent", slowAddress, []string{w.key}, webhook.TopicRequirementRevised)
	nowhere, _ := makeHook(t, w.owner, "Nowhere", "http://receiver.invalid/hook", []string{w.key}, webhook.TopicRequirementRevised)
	want(t, w.bob.patch(t, "/api/v1/requirements/"+w.reqKey, map[string]any{"title": "Brakes engage within 90 ms", "baseRevision": 1}), http.StatusOK, "revise")

	for hook, sentence := range map[string]string{silent: sentenceTimeout, nowhere: sentenceUnreached} {
		log := h.awaitState(t, hook, webhook.TopicRequirementRevised, `"revision": 2`, "pending", 2)
		if len(log) != 2 || log[0].State != "failed" || log[0].Error != sentence {
			t.Errorf("the first attempt reads %+v, want failed with %q", log, sentence)
		}
		if n := h.countRows(t, `SELECT count(*) FROM webhook_delivery WHERE id = $1 AND status IS NULL`, log[0].ID); n != 1 {
			t.Error("an attempt that got no answer logged a status")
		}
		if n := failuresOf(t, h, hook); n != 1 {
			t.Errorf("the webhook counts %d failures, want 1", n)
		}
	}
}

// A secret that no longer opens sends nothing; the administrator is told to rotate it.
func TestASecretThatNoLongerOpensSendsNothing(t *testing.T) {
	w := newCommentWorld(t, "HKX")
	w.h.keepWebhooks(t)
	h := w.h
	bin, address := hookBin(t)
	id, _ := makeHook(t, w.owner, "Copied", address, []string{w.key}, webhook.TopicRequirementRevised)
	donorBin, donorAddress := hookBin(t)
	donor, _ := makeHook(t, w.owner, "Donor", donorAddress, []string{w.key}, webhook.TopicRequirementRevised)
	// A sealed secret opens only for the endpoint it was sealed for, so another's copied over it does not.
	h.cleanupExec(t, h.super, `UPDATE webhook_endpoint SET secret_sealed = (SELECT secret_sealed FROM webhook_endpoint WHERE id = $2) WHERE id = $1`, id, donor)
	want(t, w.bob.patch(t, "/api/v1/requirements/"+w.reqKey, map[string]any{"title": "Brakes engage within 90 ms", "baseRevision": 1}), http.StatusOK, "revise")

	log := h.awaitState(t, id, webhook.TopicRequirementRevised, `"revision": 2`, "pending", 2)
	if len(log) != 2 || log[0].State != "failed" || log[0].Error != sentenceNoSecret {
		t.Errorf("the attempt with a secret that does not open reads %+v", log)
	}
	awaitTopic(t, donorBin, webhook.TopicRequirementRevised, w.reqKey, 1)
	if got := binDeliveries(t, bin); len(got) != 0 {
		t.Errorf("a delivery whose secret does not open was posted %d times", len(got))
	}
	if n := failuresOf(t, h, id); n != 1 {
		t.Errorf("the webhook counts %d failures, want 1", n)
	}
}

// A delivery in an envelope version this release does not send fails once: nothing is posted, retried or counted.
func TestADeliveryInAnUnknownVersionIsNotRetried(t *testing.T) {
	w := newCommentWorld(t, "HKV")
	w.h.keepWebhooks(t)
	h := w.h
	bin, address := hookBin(t)
	id, _ := makeHook(t, w.owner, "Future", address, []string{w.key}, webhook.TopicRequirementRevised)
	const unknown = 99
	planted := plant(t, h, id, "pending", false, unknown, 0, 0)

	state, why := awaitDelivery(t, h, planted)
	wanted := fmt.Sprintf("Nothing was sent: this delivery is in envelope version %d, which this Exciter does not send. Register the webhook for version %s; the events after that are sent in it.",
		unknown, webhook.VersionList())
	if state != "failed" || why != wanted {
		t.Errorf("a delivery in version %d reads %s: %q", unknown, state, why)
	}
	if n := h.countRows(t, `SELECT count(*) FROM webhook_delivery WHERE event_id = (SELECT event_id FROM webhook_delivery WHERE id = $1)`, planted); n != 1 {
		t.Errorf("the event holds %d attempts, want no retry", n)
	}
	if n := failuresOf(t, h, id); n != 0 {
		t.Errorf("the webhook counts %d failures, want none", n)
	}
	if got := binDeliveries(t, bin); len(got) != 0 {
		t.Errorf("a delivery in an unknown version was posted %d times", len(got))
	}
}

// A receiver that redirects into the network is refused there, and the log names no host or port inside it.
func TestARedirectIntoTheNetworkIsRefused(t *testing.T) {
	w := newCommentWorld(t, "HKZ")
	w.h.keepWebhooks(t)
	bin, address := hookBin(t)
	binAnswersWith(t, bin, map[string]any{"status": http.StatusTemporaryRedirect, "location": innerAddress})
	id, _ := makeHook(t, w.owner, "Bouncer", address, []string{w.key}, webhook.TopicRequirementRevised)

	sent := obj(t, want(t, w.owner.post(t, "/api/v1/webhooks/"+id+"/test", nil), http.StatusOK, "ping the bouncer"), "delivery")
	why, _ := sent["error"].(string)
	if sent["state"] != "failed" || sent["status"] != nil || why != sentenceBlocked {
		t.Errorf("a ping redirected into the network reads %v", sent)
	}
	for _, inner := range []string{"10.0.0.1", "armature-stub", ":8080", "/_hooks/"} {
		if strings.Contains(why, inner) {
			t.Errorf("the log names %q: %q", inner, why)
		}
	}
	if got := binDeliveries(t, bin); len(got) != 1 {
		t.Errorf("the bin was posted %d times, want the one it redirected", len(got))
	}
}

// Senders side by side, the stack's worker among them, post each due delivery once.
func TestTwoSendersPostEachDeliveryOnce(t *testing.T) {
	w := newCommentWorld(t, "HKY")
	w.h.keepWebhooks(t)
	h := w.h
	bin, address := hookBin(t)
	id, _ := makeHook(t, w.owner, "Busy", address, []string{w.key}, webhook.TopicRequirementRevised)
	planted := map[string]bool{}
	for range sideBySide {
		planted[plant(t, h, id, "pending", false, webhook.Version, 0, 0).String()] = true
	}

	svc := h.webhooks(t)
	var senders sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		senders.Go(func() {
			if _, err := svc.SendDue(context.Background()); err != nil {
				errs <- err
			}
		})
	}
	senders.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("a sender failed: %v", err)
	}
	deadline := time.Now().Add(deliveryWait)
	for pendingOf(t, h, id) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d deliveries are still pending after %s", pendingOf(t, h, id), deliveryWait)
		}
		time.Sleep(workerIdle * 4)
	}
	if n := h.countRows(t, `SELECT count(*) FROM webhook_delivery WHERE endpoint_id = $1 AND state = 'delivered' AND attempt = 1`, id); n != sideBySide {
		t.Errorf("%d of %d deliveries were delivered at the first attempt", n, sideBySide)
	}
	got := binDeliveries(t, bin)
	seen := map[string]int{}
	for _, d := range got {
		seen[d.Headers[webhook.DeliveryHeader]]++
	}
	if len(got) != sideBySide || len(seen) != sideBySide {
		t.Errorf("the bin took %d posts of %d deliveries, want each of %d once", len(got), len(seen), sideBySide)
	}
	for delivery, n := range seen {
		if !planted[delivery] || n != 1 {
			t.Errorf("delivery %s was posted %d times (planted: %t)", delivery, n, planted[delivery])
		}
	}
}
