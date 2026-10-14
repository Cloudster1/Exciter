//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/netguard"
	"github.com/cloudster1/exciter/backend/internal/secret"
)

// Requirements traced to Armature issues (#27), against the stub: issues read as the caller, links made as the
// linker, and the worker putting each requirement on its issues as whoever changed it.

// drainWait bounds waiting for the outbox, long enough for one retry after an outage.
const drainWait = 20 * time.Second

// linkSync is the worker's handler as cmd/worker builds it: the stack's own worker runs beside it with the same
// address and key, so either may sync.
func (h *harness) linkSync(t *testing.T) *armature.LinkSync {
	t.Helper()
	box, err := secret.New(testSecretKey(t))
	if err != nil {
		t.Fatal(err)
	}
	allow := netguard.ParseAllow(armatureStubHost)
	client := armature.NewClient(allow, map[string]string{armatureURL(t): os.Getenv(envArmatureStub)})
	svc := armature.NewService(h.cluster, box, client, nil, armature.Options{AppURL: testAppURL(t), Allow: allow, Development: true, Log: discard()})
	return armature.NewLinkSync(svc, discard())
}

func testAppURL(t *testing.T) string {
	t.Helper()
	appURL := os.Getenv("EXCITER_TEST_APP_URL")
	if appURL == "" {
		t.Fatal("EXCITER_TEST_APP_URL is not set; run the suite with make test-integration against the running stack")
	}
	return appURL
}

// drained waits until every event is done.
func (h *harness) drained(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(drainWait)
	for h.countRows(t, `SELECT count(*) FROM outbox_event WHERE processed_at IS NULL`) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the outbox did not drain within %s", drainWait)
		}
		time.Sleep(workerIdle)
	}
	h.settle(t)
}

// stubControl steers the stub under /_stub/.
func stubControl(t *testing.T, method, path string, body any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, os.Getenv(envArmatureStub)+"/_stub/"+path, reader)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s /_stub/%s: %d %s", method, path, resp.StatusCode, raw)
	}
}

// linksOn is every remote link the stub holds for a tenant to one url, by issue key.
func linksOn(t *testing.T, tenant, url string) map[string]map[string]any {
	t.Helper()
	resp, err := http.Get(os.Getenv(envArmatureStub) + "/_stub/" + tenant + "/remote-links")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var all struct {
		RemoteLinks []map[string]any `json:"remoteLinks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for _, l := range all.RemoteLinks {
		if l["url"] == url {
			out[l["issueKey"].(string)] = l
		}
	}
	return out
}

func issueKeys(links map[string]map[string]any) string {
	var keys []string
	for key := range links {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return strings.Join(keys, " ")
}

// linkStates is what GET /requirements/{key}/armature-links says, by issue key and relation.
func linkStates(t *testing.T, c *client, reqKey string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, each := range list(t, want(t, c.get(t, "/api/v1/requirements/"+reqKey+"/armature-links"), http.StatusOK, "the links of "+reqKey), "links") {
		l := each.(map[string]any)
		out[l["issueKey"].(string)+" "+l["relation"].(string)] = l
	}
	return out
}

func TestRequirementsAreTracedToArmatureIssues(t *testing.T) {
	lnkKey := projectKey(t, "LNK")
	lnxKey := projectKey(t, "LNX")
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-links")
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	carol := api.as(t, h.addPerson(t, "member"))
	dave := api.as(t, h.addPerson(t, "member"))

	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "the owner connects as alice")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "bob connects")
	want(t, dave.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "reader")}), http.StatusOK, "dave connects with a read-only token")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": lnkKey, "name": "Linked"}), http.StatusCreated, "make a project")
	r1 := obj(t, want(t, bob.post(t, "/api/v1/projects/"+lnkKey+"/requirements", map[string]any{"title": "Brakes engage within 100 ms"}), http.StatusCreated, "write "+lnkKey+"-R1"), "requirement")
	r2 := obj(t, want(t, bob.post(t, "/api/v1/projects/"+lnkKey+"/requirements", map[string]any{"title": "Log every login"}), http.StatusCreated, "write "+lnkKey+"-R2"), "requirement")
	want(t, bob.post(t, "/api/v1/projects/"+lnkKey+"/requirements", map[string]any{"title": "Safety", "kind": "section"}), http.StatusCreated, "write a section")
	url1 := testAppURL(t) + armature.RequirementPath + r1["id"].(string)
	url2 := testAppURL(t) + armature.RequirementPath + r2["id"].(string)
	h.settle(t)
	h.runWorker(t, events.NewMux(nil).Route(armature.TopicLinks, h.linkSync(t)))

	t.Run("issues are read as the caller, and a caller without a token is told why", func(t *testing.T) {
		got := obj(t, want(t, bob.get(t, "/api/v1/armature/issues?key=CP-1&key=cp-2&key=SEC-1&key=CP-1"), http.StatusOK, "bob looks up keys"))
		results := got["issues"].([]any)
		if got["status"] != "ok" || len(results) != 3 {
			t.Fatalf("the lookup is %v", got)
		}
		first := results[0].(map[string]any)["issue"].(map[string]any)
		if first["key"] != "CP-1" || first["summary"] != "Set up the build pipeline" || first["assigneeName"] != "Alice" ||
			first["type"] != "Task" || first["status"].(map[string]any)["category"] != "done" || !strings.HasSuffix(first["url"].(string), "/issues/CP-1") {
			t.Errorf("CP-1 reads %v", first)
		}
		if results[2].(map[string]any)["issue"] != nil {
			t.Error("bob sees SEC-1, which only the admin may")
		}
		if got := obj(t, want(t, carol.get(t, "/api/v1/armature/issues?key=CP-1"), http.StatusOK, "carol looks up a key")); got["status"] != "not_connected" || len(got["issues"].([]any)) != 0 {
			t.Errorf("carol without a token reads %v", got)
		}
		refuse(t, "a key of the wrong shape", bob.get(t, "/api/v1/armature/issues?key=CP"), http.StatusUnprocessableEntity, "validation_failed")
		if moved := obj(t, want(t, bob.get(t, "/api/v1/armature/issues/sec-2"), http.StatusOK, "an old key"), "issue"); moved["key"] != "CP-5" {
			t.Errorf("SEC-2 reads %v, want CP-5", moved["key"])
		}

		for q, keys := range map[string]string{"sign-in": "CP-2", "cp-4": "CP-4", "patch": ""} {
			var found []string
			for _, each := range list(t, want(t, bob.get(t, "/api/v1/armature/search?q="+q), http.StatusOK, "search "+q), "issues") {
				found = append(found, each.(map[string]any)["key"].(string))
			}
			if strings.Join(found, " ") != keys {
				t.Errorf("searching %q found %v, want %s", q, found, keys)
			}
		}
		refuse(t, "a blank search", bob.get(t, "/api/v1/armature/search?q=%20"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "too many results", bob.get(t, "/api/v1/armature/search?q=x&limit=21"), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("a link is made as the linker, to an issue they may see", func(t *testing.T) {
		made := obj(t, want(t, bob.post(t, "/api/v1/requirements/"+lnkKey+"-R1/armature-links", map[string]any{"issueKey": "cp-1", "relation": "implements"}),
			http.StatusCreated, "bob links CP-1"), "link")
		if made["issueKey"] != "CP-1" || made["relation"] != "implements" || made["statusCategory"] != "done" || made["createdByName"] == "" {
			t.Errorf("the link reads %v", made)
		}
		want(t, bob.post(t, "/api/v1/requirements/"+lnkKey+"-R1/armature-links", map[string]any{"issueKey": "CP-1", "relation": "verifies"}), http.StatusCreated, "and as verifying")
		want(t, bob.post(t, "/api/v1/requirements/"+lnkKey+"-R1/armature-links", map[string]any{"issueKey": "SEC-2", "relation": "relates"}), http.StatusCreated, "an old key")
		want(t, bob.post(t, "/api/v1/requirements/"+lnkKey+"-R2/armature-links", map[string]any{"issueKey": "CP-2", "relation": "relates"}), http.StatusCreated, lnkKey+"-R2 to CP-2")
		refuse(t, "the same link again", bob.post(t, "/api/v1/requirements/"+lnkKey+"-R1/armature-links", map[string]any{"issueKey": "CP-1", "relation": "implements"}),
			http.StatusConflict, "armature_link_exists")
		if msg := fieldError(t, bob.post(t, "/api/v1/requirements/"+lnkKey+"-R1/armature-links", map[string]any{"issueKey": "SEC-1", "relation": "implements"}), "issueKey"); !strings.Contains(msg, "SEC-1") {
			t.Errorf("an issue bob may not see is refused with %q", msg)
		}
		fieldError(t, bob.post(t, "/api/v1/requirements/"+lnkKey+"-R1/armature-links", map[string]any{"issueKey": "CP-1", "relation": "blocks"}), "relation")
		refuse(t, "a section", bob.post(t, "/api/v1/requirements/"+lnkKey+"-R3/armature-links", map[string]any{"issueKey": "CP-1", "relation": "relates"}),
			http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "carol without a token", carol.post(t, "/api/v1/requirements/"+lnkKey+"-R1/armature-links", map[string]any{"issueKey": "CP-3", "relation": "relates"}),
			http.StatusConflict, "armature_not_connected")
		refuse(t, "dave with a read-only token", dave.post(t, "/api/v1/requirements/"+lnkKey+"-R1/armature-links", map[string]any{"issueKey": "CP-3", "relation": "relates"}),
			http.StatusConflict, "armature_rejected")
		states := linkStates(t, carol, lnkKey+"-R1")
		if len(states) != 3 || states["CP-5 relates"] == nil || states["CP-1 implements"]["statusCategory"] != "done" {
			t.Errorf("carol, without a token, reads the links as %v", states)
		}
	})

	h.drained(t)
	t.Run("the worker puts one link per issue on Armature, as the person who linked", func(t *testing.T) {
		got := linksOn(t, slug, url1)
		if issueKeys(got) != "CP-1 CP-5" {
			t.Fatalf(lnkKey+"-R1 is on %s, want CP-1 and CP-5", issueKeys(got))
		}
		cp1 := got["CP-1"]
		if cp1["title"] != lnkKey+"-R1 Brakes engage within 100 ms (implements)" || cp1["source"] != armature.LinkSource ||
			cp1["createdByName"] != "Bob" || cp1["iconUrl"] != testAppURL(t)+armature.IconPath {
			t.Errorf("the link on CP-1 is %v", cp1)
		}
		if got["CP-5"]["title"] != lnkKey+"-R1 Brakes engage within 100 ms (relates)" {
			t.Errorf("the link on CP-5 is titled %v", got["CP-5"]["title"])
		}
		for key, l := range linkStates(t, bob, lnkKey+"-R1") {
			if l["sync"] != "synced" {
				t.Errorf("%s reads %v", key, l)
			}
		}
	})

	t.Run("a new revision, a new key and a removed link are synced", func(t *testing.T) {
		want(t, bob.patch(t, "/api/v1/requirements/"+lnkKey+"-R1", map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}), http.StatusOK, "revise "+lnkKey+"-R1")
		h.drained(t)
		if title := linksOn(t, slug, url1)["CP-1"]["title"]; title != lnkKey+"-R1 Brakes engage within 80 ms (implements)" {
			t.Errorf("after the revision CP-1 shows %v", title)
		}
		want(t, owner.put(t, "/api/v1/projects/"+lnkKey+"/key", map[string]any{"key": lnxKey}), http.StatusOK, "rekey the project")
		h.drained(t)
		if title := linksOn(t, slug, url1)["CP-1"]["title"]; title != lnxKey+"-R1 Brakes engage within 80 ms (implements)" {
			t.Errorf("after the new key CP-1 shows %v", title)
		}
		implements := linkStates(t, bob, lnxKey+"-R1")["CP-1 implements"]["id"].(string)
		want(t, bob.delete(t, "/api/v1/armature-links/"+implements), http.StatusNoContent, "remove the implements link")
		h.drained(t)
		if title := linksOn(t, slug, url1)["CP-1"]["title"]; title != lnxKey+"-R1 Brakes engage within 80 ms (verifies)" {
			t.Errorf("with only the verifies link CP-1 shows %v", title)
		}
		verifies := linkStates(t, bob, lnxKey+"-R1")["CP-1 verifies"]["id"].(string)
		want(t, bob.delete(t, "/api/v1/armature-links/"+verifies), http.StatusNoContent, "remove the verifies link")
		want(t, bob.delete(t, "/api/v1/armature-links/"+verifies), http.StatusNotFound, "remove it again")
		h.drained(t)
		if keys := issueKeys(linksOn(t, slug, url1)); keys != "CP-5" {
			t.Errorf("after both links went "+lnxKey+"-R1 is on %s", keys)
		}
	})

	t.Run("a revision leaves a link suspect until an editor confirms it", func(t *testing.T) {
		link := linkStates(t, carol, lnxKey+"-R1")["CP-5 relates"]
		if link["suspect"] != true || link["reviewedRevision"] != float64(1) {
			t.Fatalf("the link made before the revision reads %v", link)
		}
		clear := "/api/v1/armature-links/" + link["id"].(string) + "/clear"
		refuse(t, "an older revision", bob.post(t, clear, map[string]any{"revision": 1, "comment": "Fine."}), http.StatusConflict, "revision_moved_on")
		fieldError(t, bob.post(t, clear, map[string]any{"revision": 3, "comment": "Fine."}), "revision")
		fieldError(t, bob.post(t, clear, map[string]any{"revision": 2, "comment": ""}), "comment")
		refuse(t, "a link that is none", bob.post(t, "/api/v1/armature-links/"+uuid.NewString()+"/clear", map[string]any{"revision": 2, "comment": "Fine."}),
			http.StatusNotFound, "not_found")
		cleared := obj(t, want(t, bob.post(t, clear, map[string]any{"revision": 2, "comment": "CP-5 still covers it."}), http.StatusOK, "bob confirms the link"), "link")
		if cleared["suspect"] != false || cleared["reviewedRevision"] != float64(2) {
			t.Errorf("the confirmed link reads %v", cleared)
		}
		if event := h.lastEvent(t, "link.cleared", r1["id"].(string)); event["armatureLinkId"] != link["id"] {
			t.Errorf("the event reads %v", event)
		}
		if n := h.countRows(t, `SELECT count(*) FROM trace_link_clearance WHERE armature_link_id = $1 AND source_rev = 2`, link["id"]); n != 1 {
			t.Errorf("%d clearances recorded, want 1", n)
		}
	})

	t.Run("an Armature that answers 503 is asked again through the outbox", func(t *testing.T) {
		stubControl(t, http.MethodPut, slug+"/remote-links/outage", map[string]any{"status": http.StatusServiceUnavailable, "count": 1})
		want(t, bob.post(t, "/api/v1/requirements/"+lnxKey+"-R2/armature-links", map[string]any{"issueKey": "CP-3", "relation": "implements"}), http.StatusCreated, "link CP-3")
		h.drained(t)
		if keys := issueKeys(linksOn(t, slug, url2)); keys != "CP-2 CP-3" {
			t.Errorf("after the outage "+lnxKey+"-R2 is on %s", keys)
		}
		if n := h.countRows(t, `
			SELECT count(*) FROM outbox_event
			WHERE topic = $1 AND payload ->> 'requirementId' = $2 AND attempts > 1 AND last_error LIKE '%503%'`,
			armature.TopicLinks, r2["id"]); n != 1 {
			t.Errorf("%d events were tried again after the 503, want 1", n)
		}
	})

	t.Run("a refusal marks the link failed, and the next change tries it again", func(t *testing.T) {
		stubControl(t, http.MethodPut, slug+"/people/bob/read-only-projects", map[string]any{"projects": []string{"CP"}})
		want(t, bob.post(t, "/api/v1/requirements/"+lnxKey+"-R2/armature-links", map[string]any{"issueKey": "CP-4", "relation": "verifies"}), http.StatusCreated, "link CP-4")
		h.drained(t)
		failed := linkStates(t, owner, lnxKey+"-R2")["CP-4 verifies"]
		if failed["sync"] != "failed" || !strings.Contains(failed["error"].(string), "did not let A member") || !strings.Contains(failed["error"].(string), "next change") {
			t.Errorf("a refused link reads %v", failed)
		}
		stubControl(t, http.MethodPut, slug+"/people/bob/read-only-projects", map[string]any{"projects": []string{}})
		want(t, bob.patch(t, "/api/v1/requirements/"+lnxKey+"-R2", map[string]any{"title": "Log every sign-in", "baseRevision": 1}), http.StatusOK, "revise "+lnxKey+"-R2")
		h.drained(t)
		if keys := issueKeys(linksOn(t, slug, url2)); keys != "CP-2 CP-3 CP-4" {
			t.Errorf("after the next change "+lnxKey+"-R2 is on %s", keys)
		}
		if state := linkStates(t, owner, lnxKey+"-R2")["CP-4 verifies"]["sync"]; state != "synced" {
			t.Errorf("CP-4 reads %v after the next change", state)
		}
	})

	t.Run("trash takes the links off, restore puts them back, and a purge takes them off for good", func(t *testing.T) {
		want(t, bob.delete(t, "/api/v1/requirements/"+lnxKey+"-R1"), http.StatusNoContent, "trash "+lnxKey+"-R1")
		h.drained(t)
		if keys := issueKeys(linksOn(t, slug, url1)); keys != "" {
			t.Errorf("a trashed requirement is still on %s", keys)
		}
		want(t, bob.post(t, "/api/v1/projects/"+lnxKey+"/trash/"+r1["id"].(string)+"/restore", nil), http.StatusOK, "restore "+lnxKey+"-R1")
		h.drained(t)
		if keys := issueKeys(linksOn(t, slug, url1)); keys != "CP-5" {
			t.Errorf("a restored requirement is on %s", keys)
		}
		want(t, bob.delete(t, "/api/v1/requirements/"+lnxKey+"-R2"), http.StatusNoContent, "trash "+lnxKey+"-R2")
		want(t, owner.delete(t, "/api/v1/projects/"+lnxKey+"/trash/"+r2["id"].(string)), http.StatusNoContent, "purge "+lnxKey+"-R2")
		h.drained(t)
		if keys := issueKeys(linksOn(t, slug, url2)); keys != "" {
			t.Errorf("a purged requirement is still on %s", keys)
		}
		if n := h.countRows(t, `SELECT count(*) FROM armature_remote_link WHERE requirement_id = $1`, r2["id"]); n != 0 {
			t.Errorf("%d sync records outlived the purge", n)
		}
	})
}
