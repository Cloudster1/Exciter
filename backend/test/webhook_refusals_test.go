//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/webhook"
)

// What a webhook's operations refuse and record, and a move posted to the webhooks of both its projects (#328).

// cloudEventFromTheStack holds a CloudEvent the stack's worker sent to what api/openapi.json promises, as asDocumented
// does for one this process sent; its source is the stack's own address rather than this process's.
func cloudEventFromTheStack(t *testing.T, d received, secret string) {
	t.Helper()
	doc := httpapi.Spec()
	schema := doc.Webhooks["event"]["post"].RequestBody.Content["application/cloudevents+json"].Schema
	if err := doc.ValidateJSON(schema, []byte(d.Body)); err != nil {
		t.Errorf("the body does not fit the documented CloudEvent: %v\n%s", err, d.Body)
	}
	source, _ := d.env["source"].(string)
	if d.Headers["Content-Type"] != webhook.ContentTypeCloudEvents || d.env["specversion"] != webhook.SpecVersion || !strings.HasPrefix(source, "http") ||
		d.Headers[webhook.VersionHeader] != strconv.Itoa(webhook.VersionCloudEvents) {
		t.Errorf("the CloudEvent reads %v with headers %v", d.env, d.Headers)
	}
	if !webhook.VerifyTimestamped([]byte(d.Body), secret, d.Headers[webhook.TimestampHeader], d.Headers[webhook.TimestampedSignatureHeader], signedWithin, d.At) {
		t.Errorf("the timestamp %q and its signature %q do not verify", d.Headers[webhook.TimestampHeader], d.Headers[webhook.TimestampedSignatureHeader])
	}
}

// awaitMoved waits until a bin holds a move in either envelope, and answers every move it holds.
func awaitMoved(t *testing.T, bin string) []received {
	t.Helper()
	deadline := time.Now().Add(deliveryWait)
	for {
		var moved []received
		for _, d := range binDeliveries(t, bin) {
			if d.env["topic"] == webhook.TopicRequirementMoved || d.env["type"] == webhook.TypePrefix+webhook.TopicRequirementMoved {
				moved = append(moved, d)
			}
		}
		if len(moved) > 0 {
			return moved
		}
		if time.Now().After(deadline) {
			t.Fatalf("no move reached the bin in %s", deliveryWait)
		}
		time.Sleep(workerIdle * 4)
	}
}

// Saving and rotating are each recorded once, without the secret or the address's path.
func TestSavingAndRotatingAreAuditedWithoutTheSecret(t *testing.T) {
	w := newCommentWorld(t, "HKU")
	w.h.keepWebhooks(t)
	h := w.h
	h.started(t)
	_, address := hookBin(t)
	id, first := makeHook(t, w.owner, "Audited", address, []string{w.key}, webhook.TopicRequirementRevised)
	path := "/api/v1/webhooks/" + id
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}

	want(t, w.owner.patch(t, path, map[string]any{"name": "Audited again", "url": address, "topics": []string{webhook.TopicAny}, "projects": []string{w.key}}),
		http.StatusOK, "save it")
	rotated := obj(t, want(t, w.owner.post(t, path+"/rotate-secret", nil), http.StatusOK, "rotate"), "webhook")
	second, _ := rotated["secret"].(string)
	if !strings.HasPrefix(second, webhook.SecretPrefix) || second == first {
		t.Fatalf("the rotation answered %v", rotated)
	}

	updated := h.auditRows(t, audit.ActionWebhookUpdated, uuid.MustParse(id))
	if len(updated) != 1 || updated[0]["name"] != "Audited again" || updated[0]["host"] != parsed.Host || updated[0]["byProvider"] != false {
		t.Errorf("the save is recorded as %v", updated)
	}
	secretRotated := h.auditRows(t, audit.ActionWebhookSecretRotated, uuid.MustParse(id))
	if len(secretRotated) != 1 || secretRotated[0]["name"] != "Audited again" || secretRotated[0]["byProvider"] != false {
		t.Errorf("the rotation is recorded as %v", secretRotated)
	}
	raw, _ := json.Marshal(append(updated, secretRotated...))
	for _, kept := range []string{first, second, webhook.SecretPrefix, parsed.Path} {
		if strings.Contains(string(raw), kept) {
			t.Errorf("the record holds %q: %s", kept, raw)
		}
	}
}

// Deleting, rotating, pinging and sending again take no body: what they refuse is an id that is none, and a token held
// to some projects. Nothing changes either way.
func TestWebhookOperationsRefuseABadIdAndAProjectsToken(t *testing.T) {
	w := newCommentWorld(t, "HKB")
	w.h.keepWebhooks(t)
	h := w.h
	h.started(t)
	bin, address := hookBin(t)
	id, _ := makeHook(t, w.owner, "Kept", address, []string{w.key}, webhook.TopicRequirementRevised)
	path := "/api/v1/webhooks/" + id
	pinged := obj(t, want(t, w.owner.post(t, path+"/test", nil), http.StatusOK, "ping"), "delivery")["id"].(string)
	redeliver := path + "/deliveries/" + pinged + "/redeliver"
	var sealed []byte
	if err := h.super.QueryRow(context.Background(), `SELECT secret_sealed FROM webhook_endpoint WHERE id = $1`, id).Scan(&sealed); err != nil {
		t.Fatal(err)
	}

	for what, got := range map[string]response{
		"delete by a bad id":              w.owner.delete(t, "/api/v1/webhooks/not-an-id"),
		"rotate by a bad id":              w.owner.post(t, "/api/v1/webhooks/not-an-id/rotate-secret", nil),
		"ping by a bad id":                w.owner.post(t, "/api/v1/webhooks/not-an-id/test", nil),
		"send again by a bad webhook id":  w.owner.post(t, "/api/v1/webhooks/not-an-id/deliveries/"+pinged+"/redeliver", nil),
		"send again by a bad delivery id": w.owner.post(t, path+"/deliveries/not-an-id/redeliver", nil),
	} {
		refuse(t, what, got, http.StatusBadRequest, "bad_request")
	}

	_, secret := makeToken(t, w.owner, map[string]any{"name": "limited", "projects": []string{w.key}})
	limited := newAPIServer(t, h).withToken(w.ownerID, secret)
	for what, got := range map[string]response{
		"delete":     limited.delete(t, path),
		"rotate":     limited.post(t, path+"/rotate-secret", nil),
		"ping":       limited.post(t, path+"/test", nil),
		"send again": limited.post(t, redeliver, nil),
	} {
		refuse(t, "a token held to a project may not "+what, got, http.StatusForbidden, "projects_token")
	}

	if n := h.countRows(t, `SELECT count(*) FROM webhook_endpoint WHERE id = $1 AND secret_sealed = $2`, id, sealed); n != 1 {
		t.Error("the webhook is gone or its secret changed after the refusals")
	}
	if n := h.countRows(t, `SELECT count(*) FROM webhook_delivery WHERE endpoint_id = $1`, id); n != 1 {
		t.Errorf("the log holds %d attempts after the refusals, want the one ping", n)
	}
	if got := binDeliveries(t, bin); len(got) != 1 {
		t.Errorf("the bin took %d posts, want the one ping", len(got))
	}
	for _, action := range []string{audit.ActionWebhookSecretRotated, audit.ActionWebhookDeleted} {
		if rows := h.auditRows(t, action, uuid.MustParse(id)); len(rows) != 0 {
			t.Errorf("a refusal was recorded as %s: %v", action, rows)
		}
	}
}

// A move to another project reaches the webhooks of either project once, in the envelope each is on, naming where
// the requirement went.
func TestAMoveReachesTheWebhooksOfBothProjects(t *testing.T) {
	s := newMoveScene(t, "MH")
	s.f.h.keepWebhooks(t)
	h := s.f.h
	to := projectKey(t, "MHT")
	s.project(t, to, "Supplier", "editor", true)
	fromBin, fromAddress := hookBin(t)
	fromID, fromSecret := makeHook(t, s.f.owner, "Left", fromAddress, []string{s.key}, webhook.TopicAny)
	cloudBin, cloudAddress := hookBin(t)
	cloud := obj(t, want(t, s.f.owner.post(t, "/api/v1/webhooks", map[string]any{"name": "Left as CloudEvents", "url": cloudAddress,
		"topics": []string{webhook.TopicAny}, "projects": []string{s.key}, "version": webhook.VersionCloudEvents}), http.StatusCreated, "make one on version 2"), "webhook")
	cloudSecret, _ := cloud["secret"].(string)
	toBin, toAddress := hookBin(t)
	toID, toSecret := makeHook(t, s.f.owner, "Arrived", toAddress, []string{to}, webhook.TopicAny)

	from := s.key + "-R4"
	toKey := to + "-R1"
	want(t, s.move(s.editor, t, from, map[string]any{"projectKey": to, "place": place(nil, nil)}), http.StatusOK, "move R4 away")
	h.drained(t)

	for _, each := range []struct {
		name, id, bin, secret string
		cloudEvent            bool
	}{
		{"the source's", fromID, fromBin, fromSecret, false},
		{"the source's CloudEvents", cloud["id"].(string), cloudBin, cloudSecret, true},
		{"the destination's", toID, toBin, toSecret, false},
	} {
		if n := h.countRows(t, `SELECT count(*) FROM webhook_delivery WHERE endpoint_id = $1 AND topic = $2`, each.id, webhook.TopicRequirementMoved); n != 1 {
			t.Errorf("%s webhook was queued the move %d times, want once", each.name, n)
		}
		moved := awaitMoved(t, each.bin)
		if len(moved) != 1 {
			t.Errorf("%s webhook was posted the move %d times, want once", each.name, len(moved))
			continue
		}
		p := moved[0].payload()
		if each.cloudEvent {
			p, _ = moved[0].env["data"].(map[string]any)
		}
		req, _ := p["requirement"].(map[string]any)
		project, _ := req["project"].(map[string]any)
		actor, _ := p["actor"].(map[string]any)
		address, _ := req["url"].(string)
		if p["fromKey"] != from || p["toKey"] != toKey || req["key"] != toKey || project["key"] != to ||
			!strings.HasSuffix(address, "/projects/"+to+"/r/"+toKey) || actor["id"] != s.editor.user.String() {
			t.Errorf("%s webhook reads the move as %s", each.name, moved[0].Body)
		}
		if each.cloudEvent {
			cloudEventFromTheStack(t, moved[0], each.secret)
		} else {
			asDocumented(t, moved[0], each.secret)
		}
	}
}
