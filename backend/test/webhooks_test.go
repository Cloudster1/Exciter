//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/netguard"
	"github.com/cloudster1/exciter/backend/internal/secret"
	"github.com/cloudster1/exciter/backend/internal/webhook"
)

// Outbound webhooks (#48): administrators keep them, the events they take reach a receiver signed, read as their
// owner when sent, retried, redelivered and turned off. The stack's worker sends; the armature-stub's bins receive.

// deliveryWait is how long a test waits for the stack's worker to queue and send a delivery.
const deliveryWait = 30 * time.Second

// webhooks is the service as cmd/api builds it, with the stack's key, so the stack's worker opens what this seals.
func (h *harness) webhooks(t *testing.T) *webhook.Service {
	t.Helper()
	box, err := secret.New(testSecretKey(t))
	if err != nil {
		t.Fatal(err)
	}
	return webhook.NewService(h.cluster, box, webhook.Options{AppURL: "http://" + appHost, Allow: netguard.ParseAllow(armatureStubHost), Log: discard()})
}

// hookBin is a fresh bin on the stub and the address the server posts to.
func hookBin(t *testing.T) (name, address string) {
	t.Helper()
	stub := os.Getenv(envArmatureStub)
	if stub == "" {
		t.Fatalf("%s is not set; run the suite with make test-integration against the running stack", envArmatureStub)
	}
	name = "bin-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, stub+"/_hooks/"+name, nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	})
	return name, stub + "/_hooks/" + name
}

// binAnswers makes a bin answer status from now on.
func binAnswers(t *testing.T, bin string, status int) {
	t.Helper()
	body, _ := json.Marshal(map[string]int{"status": status})
	req, _ := http.NewRequest(http.MethodPut, os.Getenv(envArmatureStub)+"/_hooks/"+bin+"/status", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set the bin's status: %v %v", resp, err)
	}
	resp.Body.Close()
}

// received is one delivery a bin took, when it took it, with its envelope read.
type received struct {
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	At      time.Time         `json:"at"`
	env     map[string]any
}

// signedWithin is how far a delivery's signed time may be from when the bin took it, in whole seconds as signed.
const signedWithin = 10 * time.Second

func (r received) payload() map[string]any {
	p, _ := r.env["payload"].(map[string]any)
	return p
}

func binDeliveries(t *testing.T, bin string) []received {
	t.Helper()
	resp, err := http.Get(os.Getenv(envArmatureStub) + "/_hooks/" + bin)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Deliveries []received `json:"deliveries"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("the bin answered %s", raw)
	}
	for i := range out.Deliveries {
		_ = json.Unmarshal([]byte(out.Deliveries[i].Body), &out.Deliveries[i].env)
	}
	return out.Deliveries
}

// awaitTopic waits until the bin holds n deliveries of a topic whose body names about, and returns them.
func awaitTopic(t *testing.T, bin, topic, about string, n int) []received {
	t.Helper()
	deadline := time.Now().Add(deliveryWait)
	for {
		var found []received
		for _, d := range binDeliveries(t, bin) {
			if d.env["topic"] == topic && strings.Contains(d.Body, about) {
				found = append(found, d)
			}
		}
		if len(found) >= n {
			return found
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s about %s reached the bin %d times in %s, want %d", topic, about, len(found), deliveryWait, n)
		}
		time.Sleep(workerIdle * 4)
	}
}

// deliveryRow is a logged attempt as the database holds it.
type deliveryRow struct {
	ID      uuid.UUID
	Attempt int
	State   string
	Error   string
}

// awaitState waits until the webhook holds at least attempts attempts at a topic whose event names about, the latest
// in the state wanted, and returns the log.
func (h *harness) awaitState(t *testing.T, webhookID, topic, about, want string, attempts int) []deliveryRow {
	t.Helper()
	deadline := time.Now().Add(deliveryWait)
	for {
		rows, err := h.super.Query(context.Background(), `
			SELECT id, attempt, state, error FROM webhook_delivery
			WHERE endpoint_id = $1 AND topic = $2 AND event::text LIKE '%' || $3 || '%'
			ORDER BY attempt`, webhookID, topic, about)
		if err != nil {
			t.Fatal(err)
		}
		var log []deliveryRow
		for rows.Next() {
			var d deliveryRow
			if err := rows.Scan(&d.ID, &d.Attempt, &d.State, &d.Error); err != nil {
				t.Fatal(err)
			}
			log = append(log, d)
		}
		rows.Close()
		if len(log) >= attempts && log[len(log)-1].State == want {
			return log
		}
		if time.Now().After(deadline) {
			t.Fatalf("the %s attempts about %s read %+v after %s, want the last %s", topic, about, log, deliveryWait, want)
		}
		time.Sleep(workerIdle * 4)
	}
}

func asNumber(v any) int {
	n, _ := v.(float64)
	return int(n)
}

// asDocumented holds a delivery to what api/openapi.json promises a receiver: the body fits the envelope, every header
// is there, the version header is the body's, and the timestamped signature verifies under secret, now.
func asDocumented(t *testing.T, d received, secret string) {
	t.Helper()
	doc := httpapi.Spec()
	delivery := doc.Webhooks["event"]["post"]
	if err := doc.ValidateJSON(delivery.RequestBody.Content["application/json"].Schema, []byte(d.Body)); err != nil {
		t.Errorf("the body does not fit the documented envelope: %v\n%s", err, d.Body)
	}
	if d.env["source"] != webhook.Source {
		t.Errorf("the envelope names its source %v, want %s", d.env["source"], webhook.Source)
	}
	for _, p := range delivery.Parameters {
		if p.In == "header" && p.Required && d.Headers[p.Name] == "" {
			t.Errorf("the delivery lacks the documented header %s: %v", p.Name, d.Headers)
		}
	}
	if version := d.Headers[webhook.VersionHeader]; version != strconv.Itoa(asNumber(d.env["version"])) || version != strconv.Itoa(webhook.Version) {
		t.Errorf("the version header says %q and the body %v, want both %d", version, d.env["version"], webhook.Version)
	}
	if !webhook.VerifyTimestamped([]byte(d.Body), secret, d.Headers[webhook.TimestampHeader], d.Headers[webhook.TimestampedSignatureHeader], signedWithin, d.At) {
		t.Errorf("the timestamp %q and its signature %q do not verify", d.Headers[webhook.TimestampHeader], d.Headers[webhook.TimestampedSignatureHeader])
	}
}

// makeHook adds a webhook as c, listening to the projects named, and returns its id and secret.
func makeHook(t *testing.T, c *client, name, address string, projects []string, topics ...string) (id, secretValue string) {
	t.Helper()
	got := obj(t, want(t, c.post(t, "/api/v1/webhooks", map[string]any{"name": name, "url": address, "topics": topics, "projects": projects}),
		http.StatusCreated, "make "+name), "webhook")
	secretValue, _ = got["secret"].(string)
	if !strings.HasPrefix(secretValue, webhook.SecretPrefix) {
		t.Fatalf("a new webhook came without its secret: %v", got)
	}
	return got["id"].(string), secretValue
}

func TestAnAdministratorKeepsWebhooksAndSeesTheSecretOnce(t *testing.T) {
	w := newCommentWorld(t, "HKA")
	w.h.keepWebhooks(t)
	h := w.h
	h.started(t)
	adminID := h.addPerson(t, "admin")
	admin := newAPIServer(t, h).as(t, adminID)
	bin, address := hookBin(t)
	id, plain := makeHook(t, w.owner, "Chat", address, []string{strings.ToLower(w.key)}, webhook.TopicRequirementRevised)
	path := "/api/v1/webhooks/" + id

	t.Run("the secret is in the answer that made it and nowhere after", func(t *testing.T) {
		got := want(t, w.owner.get(t, "/api/v1/webhooks"), http.StatusOK, "list the webhooks")
		if strings.Contains(string(got.Raw), plain) || strings.Contains(string(got.Raw), `"secret"`) {
			t.Errorf("the list carries a secret: %s", got.Raw)
		}
		hooks := list(t, got, "webhooks")
		hook := hooks[0].(map[string]any)
		if len(hooks) != 1 || hook["owner"].(map[string]any)["id"] != w.ownerID.String() || asNumber(hook["version"]) != webhook.Version ||
			hook["projects"].([]any)[0].(map[string]any)["key"] != w.key {
			t.Errorf("the list reads %s", got.Raw)
		}
		var sealed []byte
		if err := h.super.QueryRow(context.Background(), `SELECT secret_sealed FROM webhook_endpoint WHERE id = $1`, id).Scan(&sealed); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(sealed, []byte(plain)) || bytes.Contains(sealed, []byte(webhook.SecretPrefix)) {
			t.Error("the secret is stored as it was shown")
		}
		rows := h.auditRows(t, audit.ActionWebhookCreated, uuid.MustParse(id))
		if raw, _ := json.Marshal(rows); len(rows) != 1 || strings.Contains(string(raw), plain) || strings.Contains(string(raw), "/_hooks/") {
			t.Errorf("the record of a new webhook holds its secret or its path: %s", raw)
		}
	})

	t.Run("a ping reaches the receiver signed with that secret", func(t *testing.T) {
		sent := obj(t, want(t, w.owner.post(t, path+"/test", nil), http.StatusOK, "send a ping"), "delivery")
		if sent["state"] != "delivered" || asNumber(sent["status"]) != http.StatusNoContent || sent["manual"] != true {
			t.Fatalf("the ping reads %v", sent)
		}
		got := binDeliveries(t, bin)
		if len(got) != 1 {
			t.Fatalf("the bin holds %d deliveries", len(got))
		}
		d := got[0]
		if !webhook.Verify([]byte(d.Body), plain, d.Headers[webhook.SignatureHeader]) {
			t.Errorf("the signature %q does not match the body", d.Headers[webhook.SignatureHeader])
		}
		if d.Headers[webhook.EventHeader] != webhook.TopicPing || d.Headers[webhook.DeliveryHeader] != sent["id"] || d.Headers["Content-Type"] != "application/json" {
			t.Errorf("the headers read %v", d.Headers)
		}
		asDocumented(t, d, plain)
		log := list(t, want(t, w.owner.get(t, path+"/deliveries"), http.StatusOK, "read the log"), "deliveries")
		if len(log) != 1 || log[0].(map[string]any)["id"] != sent["id"] {
			t.Errorf("the log reads %v", log)
		}
		fieldError(t, w.owner.get(t, path+"/deliveries?limit=0"), "limit")
	})

	t.Run("a rotated secret signs from then on, and another administrator becomes the owner by saving", func(t *testing.T) {
		rotated := obj(t, want(t, w.owner.post(t, path+"/rotate-secret", nil), http.StatusOK, "rotate"), "webhook")
		fresh, _ := rotated["secret"].(string)
		if fresh == "" || fresh == plain {
			t.Fatalf("the rotation answered %v", rotated)
		}
		want(t, admin.post(t, path+"/test", nil), http.StatusOK, "ping again")
		last := binDeliveries(t, bin)
		if d := last[len(last)-1]; !webhook.Verify([]byte(d.Body), fresh, d.Headers[webhook.SignatureHeader]) || webhook.Verify([]byte(d.Body), plain, d.Headers[webhook.SignatureHeader]) {
			t.Error("the ping after a rotation is not signed with the new secret alone")
		}
		saved := obj(t, want(t, admin.patch(t, path, map[string]any{"name": "Chat room", "url": address, "topics": []string{"*"}, "enabled": false}),
			http.StatusOK, "save as the other administrator"), "webhook")
		if saved["owner"].(map[string]any)["id"] != adminID.String() || saved["enabled"] != false || saved["topics"].([]any)[0] != "*" || len(saved["projects"].([]any)) != 0 {
			t.Errorf("the saved webhook reads %v", saved)
		}
	})

	t.Run("what cannot be a webhook is refused with a sentence", func(t *testing.T) {
		for name, body := range map[string]map[string]any{
			"url":      {"name": "Loop", "url": "http://127.0.0.1:9/hook", "topics": []string{"*"}},
			"topics":   {"name": "Lapse", "url": address, "topics": []string{"armature.links"}},
			"name":     {"name": "chat ROOM", "url": address, "topics": []string{"*"}},
			"projects": {"name": "Gone", "url": address, "topics": []string{"*"}, "projects": []string{"NOPE" + strings.ToUpper(uuid.NewString()[:4])}},
			"version":  {"name": "Next", "url": address, "topics": []string{"*"}, "version": webhook.Version + 1},
		} {
			if msg := fieldError(t, w.owner.post(t, "/api/v1/webhooks", body), name); msg == "" {
				t.Errorf("no sentence for %s", name)
			}
		}
		fieldError(t, w.owner.patch(t, path, map[string]any{"name": "Chat", "url": "ftp://x", "topics": []string{"*"}}), "url")
	})

	t.Run("members and strangers are refused every operation", func(t *testing.T) {
		delivery := list(t, want(t, w.owner.get(t, path+"/deliveries"), http.StatusOK, "the log"), "deliveries")[0].(map[string]any)["id"].(string)
		for what, call := range map[string]func() response{
			"list": func() response { return w.bob.get(t, "/api/v1/webhooks") },
			"create": func() response {
				return w.bob.post(t, "/api/v1/webhooks", map[string]any{"name": "Mine", "url": address, "topics": []string{"*"}})
			},
			"update": func() response {
				return w.bob.patch(t, path, map[string]any{"name": "Mine", "url": address, "topics": []string{"*"}})
			},
			"rotate":     func() response { return w.bob.post(t, path+"/rotate-secret", nil) },
			"test":       func() response { return w.bob.post(t, path+"/test", nil) },
			"deliveries": func() response { return w.bob.get(t, path+"/deliveries") },
			"redeliver":  func() response { return w.bob.post(t, path+"/deliveries/"+delivery+"/redeliver", nil) },
			"delete":     func() response { return w.bob.delete(t, path) },
		} {
			refuse(t, "a member may not "+what, call(), http.StatusForbidden, "forbidden")
		}
		refuse(t, "a ping to no webhook", w.owner.post(t, "/api/v1/webhooks/"+uuid.NewString()+"/test", nil), http.StatusNotFound, "not_found")
		refuse(t, "a redelivery of nothing", w.owner.post(t, path+"/deliveries/"+uuid.NewString()+"/redeliver", nil), http.StatusNotFound, "not_found")
		refuse(t, "a save of no webhook", w.owner.patch(t, "/api/v1/webhooks/"+uuid.NewString(), map[string]any{"name": "X", "url": address, "topics": []string{"*"}}),
			http.StatusNotFound, "not_found")
		refuse(t, "a rotation of no webhook", w.owner.post(t, "/api/v1/webhooks/"+uuid.NewString()+"/rotate-secret", nil), http.StatusNotFound, "not_found")
	})

	t.Run("deleting takes the log with it", func(t *testing.T) {
		want(t, w.owner.delete(t, path), http.StatusNoContent, "delete the webhook")
		if rows := h.auditRows(t, audit.ActionWebhookDeleted, uuid.MustParse(id)); len(rows) != 1 {
			t.Errorf("the deletion is recorded %d times", len(rows))
		}
		refuse(t, "the log of a deleted webhook", w.owner.get(t, path+"/deliveries"), http.StatusNotFound, "not_found")
		refuse(t, "delete it again", w.owner.delete(t, path), http.StatusNotFound, "not_found")
		if n := h.countRows(t, `SELECT count(*) FROM webhook_delivery WHERE endpoint_id = $1`, id); n != 0 {
			t.Errorf("%d attempts outlived their webhook", n)
		}
	})
}
