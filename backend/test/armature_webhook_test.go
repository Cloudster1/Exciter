//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/webhook"
)

// Armature's webhooks (#29): a delivery the installation's secret vouches for clears what it announces and writes
// what it says of linked issues, once per event, as the admin role.

// hookSecret is a webhook secret of Armature's shape.
var hookSecret = armature.WebhookSecretPrefix + strings.Repeat("w", 43)

// hook is one delivery as Armature posts it; a test spoils one part of it at a time.
type hook struct {
	id      uuid.UUID
	topic   string
	version int
	orgID   uuid.UUID
	payload map[string]any
	secret  string
	// stamp is the timestamp header; signedStamp the one the timestamped signature covers.
	stamp, signedStamp string
	// versionHeader is the version header, absent when empty.
	versionHeader string
}

// body is the hook's envelope: version 2 a CloudEvent as agreed in Armature #286, any other Armature's EnvelopeV1.
func (k hook) body(t *testing.T) []byte {
	t.Helper()
	envelope := map[string]any{"version": k.version, "id": k.id, "topic": k.topic, "orgId": k.orgID, "occurredAt": time.Now().UTC(), "payload": k.payload}
	if k.version == webhook.VersionCloudEvents {
		envelope = map[string]any{"specversion": webhook.SpecVersion, "id": k.id, "source": "https://armature.example", "type": armature.WebhookTypePrefix + k.topic,
			"time": time.Now().UTC(), "datacontenttype": webhook.ContentTypeJSON, "dataversion": 1, "data": k.payload}
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// deliver posts body with the hook's signatures and answers the status and the error code.
func (a *apiServer) deliver(t *testing.T, k hook, body []byte) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, a.URL+"/api/v1/armature/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", webhook.ContentTypeJSON)
	if k.version == webhook.VersionCloudEvents {
		req.Header.Set("Content-Type", webhook.ContentTypeCloudEvents)
	}
	req.Header.Set(armature.SignatureHeader, armature.Sign(body, k.secret))
	if k.versionHeader != "" {
		req.Header.Set(armature.VersionHeader, k.versionHeader)
	}
	if k.stamp != "" {
		req.Header.Set(armature.TimestampHeader, k.stamp)
		req.Header.Set(armature.TimestampedSignatureHeader, armature.SignTimestamped(body, k.secret, k.signedStamp))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &envelope)
	return resp.StatusCode, envelope.Error.Code
}

func TestArmatureWebhooksRefreshLinkedIssues(t *testing.T) {
	whkKey := projectKey(t, "WHK")
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-hooks")
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	owner := api.as(t, home.user)
	bob := api.as(t, h.addPerson(t, "member"))

	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "bob connects")
	orgID := uuid.MustParse(obj(t, want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the connection"), "connection")["armatureOrgId"].(string))
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	event := func(topic string, payload map[string]any) hook {
		return hook{id: uuid.New(), topic: topic, version: webhook.Version, orgID: orgID, payload: payload, secret: hookSecret, stamp: stamp, signedStamp: stamp,
			versionHeader: strconv.Itoa(webhook.Version)}
	}
	send := func(k hook) (int, string) {
		t.Helper()
		return api.deliver(t, k, k.body(t))
	}
	acted := func(what string, k hook) {
		t.Helper()
		if status, code := send(k); status != http.StatusNoContent {
			t.Fatalf("%s answered %d %s", what, status, code)
		}
		h.settle(t)
	}

	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": whkKey, "name": "Hooked"}), http.StatusCreated, "make a project")
	var ids []string
	for _, title := range []string{"Brakes engage within 100 ms", "Log every login", "Rotate keys"} {
		r := obj(t, want(t, bob.post(t, "/api/v1/projects/"+whkKey+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title), "requirement")
		ids = append(ids, r["id"].(string))
	}
	r1, r2, r3 := whkKey+"-R1", whkKey+"-R2", whkKey+"-R3"
	for _, l := range []struct{ req, key string }{{r1, "CP-1"}, {r1, "CP-2"}, {r2, "CP-4"}, {r3, "CP-3"}} {
		want(t, bob.post(t, "/api/v1/requirements/"+l.req+"/armature-links", map[string]any{"issueKey": l.key, "relation": "implements"}), http.StatusCreated, l.req+" to "+l.key)
	}
	h.runWorker(t, events.NewMux(nil).Route(armature.TopicLinks, h.linkSync(t)))
	h.drained(t)
	held := func(reqID, key string) string {
		t.Helper()
		for issue, l := range linksOn(t, slug, testAppURL(t)+armature.RequirementPath+reqID) {
			if issue == key {
				return l["id"].(string)
			}
		}
		return ""
	}

	t.Run("only a delivery the secret vouches for is taken", func(t *testing.T) {
		ping := event("ping", map[string]any{"message": "Armature can reach this address."})
		if status, code := send(ping); status != http.StatusUnauthorized || code != "bad_signature" {
			t.Errorf("a delivery before any secret is stored answered %d %s", status, code)
		}
		want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "webhookSecret": hookSecret}), http.StatusOK, "store the secret")
		for name, k := range map[string]hook{
			"another secret":                {id: uuid.New(), topic: "ping", version: 1, orgID: orgID, secret: hookSecret + "x"},
			"a timestamp it does not cover": {id: uuid.New(), topic: "ping", version: 1, orgID: orgID, secret: hookSecret, stamp: stamp, signedStamp: stamp + "1"},
			"another Armature":              {id: uuid.New(), topic: "ping", version: 1, orgID: uuid.New(), secret: hookSecret, stamp: stamp, signedStamp: stamp},
		} {
			if status, code := send(k); status != http.StatusUnauthorized || code != "bad_signature" {
				t.Errorf("%s answered %d %s", name, status, code)
			}
		}
		if status, _ := send(ping); status != http.StatusNoContent {
			t.Errorf("a signed ping answered %d", status)
		}
		older := ping
		older.id, older.stamp, older.version, older.orgID, older.versionHeader = uuid.New(), "", 0, uuid.Nil, ""
		if status, _ := send(older); status != http.StatusNoContent {
			t.Errorf("an older Armature's ping, without a version, a timestamp or an org, answered %d", status)
		}
		cloud := event("ping", map[string]any{"message": "Armature can reach this address."})
		cloud.version, cloud.versionHeader = webhook.VersionCloudEvents, strconv.Itoa(webhook.VersionCloudEvents)
		if status, code := send(cloud); status != http.StatusNoContent {
			t.Errorf("a CloudEvents ping answered %d %s", status, code)
		}
		unknown := webhook.Versions[len(webhook.Versions)-1] + 1
		later := event("ping", nil)
		later.version, later.versionHeader = unknown, strconv.Itoa(unknown)
		if status, code := send(later); status != http.StatusUnprocessableEntity || code != "unknown_version" {
			t.Errorf("a version %d envelope answered %d %s", unknown, status, code)
		}
		// The header is read before the body, so a delivery it names as unreadable is refused whatever the body says.
		named := event(armature.TopicIssueDeleted, map[string]any{"key": "CP-4"})
		named.versionHeader = strconv.Itoa(unknown)
		if status, code := send(named); status != http.StatusUnprocessableEntity || code != "unknown_version" {
			t.Errorf("a version %d header on a version 1 body answered %d %s", unknown, status, code)
		}
		mixed := event(armature.TopicIssueDeleted, map[string]any{"key": "CP-4"})
		mixed.versionHeader = strconv.Itoa(webhook.VersionCloudEvents)
		if status, code := send(mixed); status != http.StatusUnprocessableEntity || code != "unknown_version" {
			t.Errorf("a version 1 body under a version 2 header answered %d %s", status, code)
		}
		if l := linkStates(t, bob, r2)["CP-4 implements"]; l["brokenAt"] != nil {
			t.Errorf("a refused delivery was acted on: %v", l)
		}
		unsaid := event("ping", nil)
		unsaid.version = unknown
		if status, code := send(unsaid); status != http.StatusUnprocessableEntity || code != "unknown_version" {
			t.Errorf("a version %d body under a version 1 header answered %d %s", unknown, status, code)
		}
		if status, _ := send(event("issue.commented.someday", map[string]any{"key": "CP-1"})); status != http.StatusNoContent {
			t.Errorf("a topic Exciter does not know answered %d", status)
		}
		big := event("ping", map[string]any{"message": strings.Repeat("x", armature.WebhookMaxBytes)})
		if status, code := api.deliver(t, big, big.body(t)); status != http.StatusRequestEntityTooLarge || code != "too_large" {
			t.Errorf("a delivery over the limit answered %d %s", status, code)
		}
	})

	t.Run("a transition stores the category for everyone, and a replay changes nothing", func(t *testing.T) {
		done := event(armature.TopicIssueTransitioned, map[string]any{"key": "CP-1", "toStatusCategory": "in_progress"})
		acted("the move to in progress", done)
		if l := linkStates(t, bob, r1)["CP-1 implements"]; l["statusCategory"] != "in_progress" || l["statusStale"] != false {
			t.Errorf("after the transition CP-1 reads %v", l)
		}
		acted("a transition without its category", event(armature.TopicIssueTransitioned, map[string]any{"key": "cp-1"}))
		if l := linkStates(t, bob, r1)["CP-1 implements"]; l["statusCategory"] != "in_progress" || l["statusStale"] != true {
			t.Errorf("a transition Armature did not name reads %v", l)
		}
		later := event(armature.TopicIssueTransitioned, map[string]any{"key": "CP-1", "toStatusCategory": "done"})
		later.version, later.versionHeader = webhook.VersionCloudEvents, strconv.Itoa(webhook.VersionCloudEvents)
		acted("the move to done, in the CloudEvents envelope", later)
		acted("the first delivery again", done)
		if l := linkStates(t, bob, r1)["CP-1 implements"]; l["statusCategory"] != "done" || l["statusStale"] != false {
			t.Errorf("a replayed transition changed CP-1 to %v", l)
		}
	})

	t.Run("an update clears what the caller was answered", func(t *testing.T) {
		before := obj(t, want(t, bob.get(t, "/api/v1/armature/issues/CP-3"), http.StatusOK, "CP-3"), "issue")["summary"]
		stubPatch(t, slug, "CP-3", map[string]any{"summary": "Written again"})
		if again := obj(t, want(t, bob.get(t, "/api/v1/armature/issues/CP-3"), http.StatusOK, "CP-3 again"), "issue")["summary"]; again != before {
			t.Fatalf("CP-3 was not served from the cache: %v", again)
		}
		acted("the update", event(armature.TopicIssueUpdated, map[string]any{"key": "CP-3"}))
		if now := obj(t, want(t, bob.get(t, "/api/v1/armature/issues/CP-3"), http.StatusOK, "CP-3 now"), "issue")["summary"]; now != "Written again" {
			t.Errorf("after the update CP-3 reads %v", now)
		}
	})

	t.Run("a moved issue takes its links along", func(t *testing.T) {
		remote := held(ids[0], "CP-2")
		if remote == "" {
			t.Fatal("the worker put no link on CP-2")
		}
		moved := stubMove(t, slug, "CP-2", "SEC")
		acted("the move", event(armature.TopicIssueUpdated, map[string]any{"key": moved, "movedFrom": "CP-2"}))
		states := linkStates(t, bob, r1)
		if states[moved+" implements"] == nil || states["CP-2 implements"] != nil {
			t.Errorf("after the move %s's links are %v", r1, states)
		}
		var record string
		if err := h.super.QueryRow(t.Context(), `SELECT remote_link_id::text FROM armature_remote_link WHERE requirement_id = $1 AND issue_key = $2`,
			ids[0], moved).Scan(&record); err != nil || record != remote {
			t.Errorf("the sync record did not follow the move: %v %v", record, err)
		}
	})

	t.Run("a deleted issue breaks its links", func(t *testing.T) {
		acted("the deletion", event(armature.TopicIssueDeleted, map[string]any{"key": "CP-4"}))
		if l := linkStates(t, bob, r2)["CP-4 implements"]; l["brokenAt"] == nil {
			t.Errorf("after the deletion CP-4 reads %v", l)
		}
		if n := h.countRows(t, `SELECT count(*) FROM armature_remote_link WHERE requirement_id = $1`, ids[1]); n != 0 {
			t.Errorf("%d sync records of a deleted issue stayed", n)
		}
	})

	t.Run("a link somebody took off stays off, and one the worker took off is no removal", func(t *testing.T) {
		remote := held(ids[0], "CP-1")
		removed := func(reqID, key, linkID string) hook {
			return event(armature.TopicRemoteLinkRemoved, map[string]any{"key": key, "linkId": linkID, "url": "http://" + appHost + armature.RequirementPath + reqID,
				"source": armature.LinkSource, "relation": "page"})
		}
		acted("another product's link", event(armature.TopicRemoteLinkRemoved, map[string]any{"key": "CP-1", "linkId": remote, "url": "https://stator.example/r/" + ids[0]}))
		if l := linkStates(t, bob, r1)["CP-1 implements"]; l["detachedAt"] != nil {
			t.Fatal("a link of another product detached CP-1")
		}
		stubControl(t, http.MethodDelete, slug+"/issues/CP-1/remote-links?url="+testAppURL(t)+armature.RequirementPath+ids[0], nil)
		acted("the removal", removed(ids[0], "CP-1", remote))
		if l := linkStates(t, bob, r1)["CP-1 implements"]; l["detachedAt"] == nil {
			t.Errorf("after the removal CP-1 reads %v", l)
		}
		want(t, bob.patch(t, "/api/v1/requirements/"+r1, map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}), http.StatusOK, "revise "+r1)
		h.drained(t)
		if held(ids[0], "CP-1") != "" {
			t.Error("the worker put the removed link back")
		}

		cp3 := held(ids[2], "CP-3")
		want(t, bob.delete(t, "/api/v1/requirements/"+r3), http.StatusNoContent, "trash "+r3)
		h.drained(t)
		acted("the worker's own removal", removed(ids[2], "CP-3", cp3))
		want(t, bob.post(t, "/api/v1/projects/"+whkKey+"/trash/"+ids[2]+"/restore", nil), http.StatusOK, "restore "+r3)
		h.drained(t)
		if l := linkStates(t, bob, r3)["CP-3 implements"]; l["detachedAt"] != nil || held(ids[2], "CP-3") == "" {
			t.Errorf("a link the worker took off for the trash is %v after the restore", l)
		}
	})
}

// stubPatch changes an issue in the stub as a person in Armature would.
func stubPatch(t *testing.T, tenant, key string, change map[string]any) {
	t.Helper()
	stubCall(t, http.MethodPatch, tenant+"/issues/"+key, change)
}

// stubMove moves an issue to another project of the stub and answers its new key.
func stubMove(t *testing.T, tenant, key, project string) string {
	t.Helper()
	return stubCall(t, http.MethodPost, tenant+"/issues/"+key+"/move", map[string]any{"projectKey": project})["issue"].(map[string]any)["key"].(string)
}

func stubCall(t *testing.T, method, path string, body any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, os.Getenv(envArmatureStub)+"/_stub/"+path, bytes.NewReader(raw))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("%s /_stub/%s: %d %v", method, path, resp.StatusCode, err)
	}
	return out
}
