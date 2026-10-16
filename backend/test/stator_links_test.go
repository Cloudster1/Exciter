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

	"github.com/cloudster1/exciter/backend/internal/stator"
	"github.com/cloudster1/exciter/backend/internal/webhook"
)

// Requirements traced to Stator pages (#41) against the stator-stub: pages read as the caller, links made as the
// linker, a link turning suspect when its page moves on, and Stator's webhooks.

// statorStub steers the stub under /_stub/ and answers what it said, for a status of 200 or 204.
func statorStub(t *testing.T, method, path string, body any) map[string]any {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, os.Getenv(envStatorStub)+"/_stub/"+path, reader)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("%s /_stub/%s: %d %s", method, path, resp.StatusCode, raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// statorPages are the stub's pages of a tenant by title.
func statorPages(t *testing.T, tenant string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, p := range statorStub(t, http.MethodGet, tenant+"/pages", nil)["pages"].([]any) {
		page := p.(map[string]any)
		out[page["title"].(string)] = page["id"].(string)
	}
	return out
}

// statorHook is one delivery as Stator posts it.
type statorHook struct {
	id      uuid.UUID
	topic   string
	version int
	payload map[string]any
	// versionHeader is the version header, which Stator sends from its #412 on; absent when empty.
	versionHeader string
}

// deliverStator posts a hook signed with secret and answers the status and the error code.
func (a *apiServer) deliverStator(t *testing.T, k statorHook, secret string) (int, string) {
	t.Helper()
	envelope, contentType := map[string]any{"id": k.id, "topic": k.topic, "occurredAt": time.Now().UTC(), "payload": k.payload}, webhook.ContentTypeJSON
	if k.version == webhook.VersionCloudEvents {
		envelope, contentType = map[string]any{"specversion": webhook.SpecVersion, "id": k.id, "source": "https://stator.example",
			"type": stator.WebhookTypePrefix + k.topic, "time": time.Now().UTC(), "datacontenttype": webhook.ContentTypeJSON, "dataversion": 1,
			"data": k.payload}, webhook.ContentTypeCloudEvents
	} else if k.version != 0 {
		envelope["version"] = k.version
	}
	body, _ := json.Marshal(envelope)
	req, _ := http.NewRequest(http.MethodPost, a.URL+"/api/v1/stator/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set(stator.SignatureHeader, stator.Sign(body, secret))
	if k.versionHeader != "" {
		req.Header.Set(stator.VersionHeader, k.versionHeader)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var answer struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &answer)
	return resp.StatusCode, answer.Error.Code
}

// pageRef is the page every page topic names.
func pageRef(id string, version int, spaceKey string) map[string]any {
	return map[string]any{"page": map[string]any{"id": id, "title": "Brake timing", "version": version, "kind": "page",
		"url": "https://stator.elsewhere.example/s/" + spaceKey + "/p/" + id, "space": map[string]any{"key": spaceKey, "name": spaceKey}}}
}

func TestRequirementsAreTracedToStatorPages(t *testing.T) {
	spgKey := projectKey(t, "SPG")
	h := newHarness(t)
	h.keepStator(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "stator-links")
	tenant := stubTenant(t)
	statorStub(t, http.MethodDelete, tenant, nil)
	owner := api.as(t, home.user)
	bob := api.as(t, h.addPerson(t, "member"))
	carol := api.as(t, h.addPerson(t, "member"))

	// Remote links on the pages are TestRequirementsShowOnTheStatorPagesTheyAreLinkedTo's; here they stay off.
	statorOffers(t)
	want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	want(t, owner.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "admin")}), http.StatusOK, "the owner connects as the stub's admin")
	want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "bob")}), http.StatusOK, "bob connects")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": spgKey, "name": "Paged"}), http.StatusCreated, "make a project")
	for _, body := range []map[string]any{{"title": "Brakes engage within 100 ms"}, {"title": "Log every login"}, {"title": "Safety", "kind": "section"}} {
		want(t, bob.post(t, "/api/v1/projects/"+spgKey+"/requirements", body), http.StatusCreated, "write "+body["title"].(string))
	}
	r1, r3 := spgKey+"-R1", spgKey+"-R3"
	pages := statorPages(t, tenant)
	timing, braking, threat := pages["Brake timing"], pages["Braking system"], pages["Threat model"]
	base := statorURL(t)
	h.settle(t)

	t.Run("pages are read as the caller, and a caller without a token is told why", func(t *testing.T) {
		got := obj(t, want(t, bob.get(t, "/api/v1/stator/pages?id="+timing+"&id="+threat+"&id="+timing), http.StatusOK, "bob looks up pages"))
		results := got["pages"].([]any)
		if got["status"] != "ok" || len(results) != 2 {
			t.Fatalf("the lookup is %v", got)
		}
		first := results[0].(map[string]any)["page"].(map[string]any)
		if first["title"] != "Brake timing" || first["version"] != float64(2) || first["spaceName"] != "Engineering" ||
			first["url"] != base+"/s/ENG/p/"+timing || len(first["path"].([]any)) != 2 {
			t.Errorf("Brake timing reads %v", first)
		}
		if results[1].(map[string]any)["page"] != nil {
			t.Error("bob reads the Threat model, which only the admin may")
		}
		if got := obj(t, want(t, carol.get(t, "/api/v1/stator/pages?id="+timing), http.StatusOK, "carol looks up a page")); got["status"] != "not_connected" || len(got["pages"].([]any)) != 0 {
			t.Errorf("carol without a token reads %v", got)
		}
		refuse(t, "an id that is none", bob.get(t, "/api/v1/stator/pages?id=CP-1"), http.StatusUnprocessableEntity, "validation_failed")

		var titles []string
		for _, each := range list(t, want(t, bob.get(t, "/api/v1/stator/search?q=brak"), http.StatusOK, "bob searches"), "pages") {
			hit := each.(map[string]any)
			titles = append(titles, hit["title"].(string))
			if !strings.HasPrefix(hit["url"].(string), base+"/s/ENG/p/") {
				t.Errorf("a hit opens at %v", hit["url"])
			}
		}
		if strings.Join(titles, ", ") != "Braking system, Brake timing" {
			t.Errorf("searching found %v", titles)
		}
		if n := len(list(t, want(t, owner.get(t, "/api/v1/stator/search?q=home&space=SEC"), http.StatusOK, "the owner searches a space"), "pages")); n != 1 {
			t.Errorf("a search inside SEC found %d pages, want its home", n)
		}
		refuse(t, "a blank search", bob.get(t, "/api/v1/stator/search?q=%20"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "too many results", bob.get(t, "/api/v1/stator/search?q=x&limit=21"), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("a link is made as the linker, to a page they may read", func(t *testing.T) {
		made := obj(t, want(t, bob.post(t, "/api/v1/requirements/"+r1+"/stator-links", map[string]any{"pageId": timing, "relation": "specifies"}),
			http.StatusCreated, "bob links Brake timing"), "link")
		if made["pageVersion"] != float64(2) || made["pageVersionSeen"] != float64(2) || made["suspect"] != false || made["spaceKey"] != "ENG" ||
			made["url"] != base+"/s/ENG/p/"+timing || made["sync"] != "none" || made["reviewedRevision"] != float64(1) || made["createdByName"] == "" {
			t.Errorf("the link reads %v", made)
		}
		want(t, bob.post(t, "/api/v1/requirements/"+r1+"/stator-links", map[string]any{"pageId": braking, "relation": "describes"}), http.StatusCreated, "bob links Braking system")
		refuse(t, "the same link again", bob.post(t, "/api/v1/requirements/"+r1+"/stator-links", map[string]any{"pageId": timing, "relation": "specifies"}),
			http.StatusConflict, "stator_link_exists")
		if msg := fieldError(t, bob.post(t, "/api/v1/requirements/"+r1+"/stator-links", map[string]any{"pageId": threat, "relation": "relates"}), "pageId"); !strings.Contains(msg, "no page you may read") {
			t.Errorf("a page bob may not read is refused with %q", msg)
		}
		fieldError(t, bob.post(t, "/api/v1/requirements/"+r1+"/stator-links", map[string]any{"pageId": timing, "relation": "implements"}), "relation")
		refuse(t, "a section", bob.post(t, "/api/v1/requirements/"+r3+"/stator-links", map[string]any{"pageId": timing, "relation": "relates"}),
			http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "carol without a token", carol.post(t, "/api/v1/requirements/"+r1+"/stator-links", map[string]any{"pageId": timing, "relation": "relates"}),
			http.StatusConflict, "stator_not_connected")
		links := list(t, want(t, carol.get(t, "/api/v1/requirements/"+r1+"/stator-links"), http.StatusOK, "carol lists the links"), "links")
		if len(links) != 2 || links[0].(map[string]any)["relation"] != "specifies" || links[1].(map[string]any)["relation"] != "describes" {
			t.Errorf("the links are %v", links)
		}
	})

	linkTo := func(t *testing.T, page string) map[string]any {
		t.Helper()
		for _, each := range list(t, want(t, bob.get(t, "/api/v1/requirements/"+r1+"/stator-links"), http.StatusOK, "bob lists the links"), "links") {
			if l := each.(map[string]any); l["pageId"] == page {
				return l
			}
		}
		t.Fatalf("%s has no link to %s", r1, page)
		return nil
	}
	suspects := func(t *testing.T) float64 {
		t.Helper()
		for _, each := range list(t, want(t, bob.get(t, "/api/v1/projects/"+spgKey+"/requirements?suspect=true"), http.StatusOK, "the grid's suspects"), "rows") {
			if row := each.(map[string]any); row["key"] == r1 {
				return row["suspectLinks"].(float64)
			}
		}
		return 0
	}

	t.Run("a link turns suspect when the page has a newer version, and is cleared against it", func(t *testing.T) {
		statorStub(t, http.MethodPost, tenant+"/pages/"+timing+"/publish", map[string]any{})
		// The owner has not read the page yet, so their read asks Stator and stores the new version for everyone.
		want(t, owner.get(t, "/api/v1/stator/pages?id="+timing), http.StatusOK, "the owner reads the page")
		h.settle(t)
		l := linkTo(t, timing)
		if l["pageVersion"] != float64(3) || l["pageVersionSeen"] != float64(2) || l["suspect"] != true {
			t.Fatalf("after Stator published version 3 the link reads %v", l)
		}
		if n := suspects(t); n != 1 {
			t.Errorf("the grid counts %v suspect links on %s, want 1", n, r1)
		}
		id := l["id"].(string)
		refuse(t, "clearing against an older version", bob.post(t, "/api/v1/stator-links/"+id+"/clear", map[string]any{"pageVersion": 2, "comment": "Read it."}),
			http.StatusConflict, "revision_moved_on")
		fieldError(t, bob.post(t, "/api/v1/stator-links/"+id+"/clear", map[string]any{"pageVersion": 4, "comment": "Read it."}), "pageVersion")
		fieldError(t, bob.post(t, "/api/v1/stator-links/"+id+"/clear", map[string]any{"pageVersion": 3, "comment": " "}), "comment")
		want(t, bob.patch(t, "/api/v1/requirements/"+r1, map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}), http.StatusOK, "bob revises R1")
		cleared := obj(t, want(t, bob.post(t, "/api/v1/stator-links/"+id+"/clear", map[string]any{"pageVersion": 3, "comment": "The timing table still holds."}),
			http.StatusOK, "bob clears the link"), "link")
		if cleared["suspect"] != false || cleared["pageVersionSeen"] != float64(3) || cleared["reviewedRevision"] != float64(2) {
			t.Errorf("the cleared link reads %v", cleared)
		}
		if n := h.countRows(t, `SELECT count(*) FROM trace_link_clearance WHERE stator_link_id = $1 AND page_version = 3 AND source_rev = 2`, id); n != 1 {
			t.Errorf("the clearance is recorded %d times", n)
		}
		if n := suspects(t); n != 0 {
			t.Errorf("the grid still counts %v suspect links", n)
		}
	})

	t.Run("Stator's webhooks keep the links current, once per event", func(t *testing.T) {
		send := func(what string, k statorHook, wantStatus int, wantCode string) {
			t.Helper()
			status, code := api.deliverStator(t, k, statorWebhookSecret)
			if status != wantStatus || code != wantCode {
				t.Fatalf("%s answered %d %s, want %d %s", what, status, code, wantStatus, wantCode)
			}
			h.settle(t)
		}
		published := statorHook{id: uuid.New(), topic: stator.TopicPagePublished, payload: pageRef(timing, 4, "ENG")}
		send("a version published", published, http.StatusNoContent, "")
		if l := linkTo(t, timing); l["pageVersion"] != float64(4) || l["suspect"] != true {
			t.Errorf("after page.published the link reads %v", l)
		}
		send("the same event again, saying more", statorHook{id: published.id, topic: stator.TopicPagePublished, payload: pageRef(timing, 9, "ENG")}, http.StatusNoContent, "")
		if l := linkTo(t, timing); l["pageVersion"] != float64(4) {
			t.Errorf("a replayed event was acted on: %v", l)
		}
		send("an older version late", statorHook{id: uuid.New(), topic: stator.TopicPagePublished, payload: pageRef(timing, 3, "ENG")}, http.StatusNoContent, "")
		if l := linkTo(t, timing); l["pageVersion"] != float64(4) {
			t.Errorf("an older version went back: %v", l)
		}
		send("a move", statorHook{id: uuid.New(), topic: stator.TopicPageMoved, payload: pageRef(timing, 4, "SEC")}, http.StatusNoContent, "")
		if l := linkTo(t, timing); l["spaceKey"] != "SEC" || l["url"] != base+"/s/SEC/p/"+timing {
			t.Errorf("after page.moved the link reads %v", l)
		}
		send("a move in the CloudEvents envelope", statorHook{id: uuid.New(), topic: stator.TopicPageMoved, version: webhook.VersionCloudEvents,
			versionHeader: strconv.Itoa(webhook.VersionCloudEvents), payload: pageRef(timing, 4, "OPS")}, http.StatusNoContent, "")
		if l := linkTo(t, timing); l["spaceKey"] != "OPS" {
			t.Errorf("after page.moved in version 2 the link reads %v", l)
		}
		send("a deletion", statorHook{id: uuid.New(), topic: stator.TopicPageDeleted, payload: pageRef(timing, 4, "SEC")}, http.StatusNoContent, "")
		if l := linkTo(t, timing); l["brokenAt"] == nil || l["suspect"] != false {
			t.Errorf("after page.deleted the link reads %v", l)
		}
		send("a ping", statorHook{id: uuid.New(), topic: stator.TopicPing, payload: map[string]any{"message": "Stator can reach this address."}}, http.StatusNoContent, "")
		send("a topic Exciter does not read", statorHook{id: uuid.New(), topic: "comment.created", payload: pageRef(braking, 1, "ENG")}, http.StatusNoContent, "")
		unknown := webhook.Versions[len(webhook.Versions)-1] + 1
		send("an envelope of another version", statorHook{id: uuid.New(), topic: stator.TopicPagePublished, version: unknown, payload: pageRef(braking, 7, "ENG")},
			http.StatusUnprocessableEntity, "unknown_version")
		send("a version header Exciter does not read", statorHook{id: uuid.New(), topic: stator.TopicPageDeleted, payload: pageRef(braking, 7, "ENG"),
			versionHeader: strconv.Itoa(unknown)}, http.StatusUnprocessableEntity, "unknown_version")
		send("a CloudEvent under a version 1 header", statorHook{id: uuid.New(), topic: stator.TopicPageDeleted, version: webhook.VersionCloudEvents,
			versionHeader: strconv.Itoa(webhook.Version), payload: pageRef(braking, 7, "ENG")}, http.StatusUnprocessableEntity, "unknown_version")
		if l := linkTo(t, braking); l["brokenAt"] != nil {
			t.Errorf("a refused delivery was acted on: %v", l)
		}
		send("a ping of a Stator that names its version", statorHook{id: uuid.New(), topic: stator.TopicPing, version: webhook.Version,
			versionHeader: strconv.Itoa(webhook.Version)}, http.StatusNoContent, "")
		if status, code := api.deliverStator(t, published, "stator_whs_"+strings.Repeat("x", 43)); status != http.StatusUnauthorized || code != "bad_signature" {
			t.Errorf("a wrong secret answered %d %s", status, code)
		}
		if l := linkTo(t, braking); l["pageVersion"] != float64(3) {
			t.Errorf("a refused delivery changed the link: %v", l)
		}
	})

	t.Run("a link is removed by whoever edits the requirement", func(t *testing.T) {
		id := linkTo(t, timing)["id"].(string)
		want(t, bob.delete(t, "/api/v1/stator-links/"+id), http.StatusNoContent, "bob removes the broken link")
		refuse(t, "removing it again", bob.delete(t, "/api/v1/stator-links/"+id), http.StatusNotFound, "not_found")
		if n := len(list(t, want(t, bob.get(t, "/api/v1/requirements/"+r1+"/stator-links"), http.StatusOK, "the links left"), "links")); n != 1 {
			t.Errorf("%d links are left, want 1", n)
		}
	})

	t.Run("without a connection a requirement shows no pages, and the links stay", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/stator/connection"), http.StatusNoContent, "disconnect Stator")
		h.settle(t)
		if n := len(list(t, want(t, bob.get(t, "/api/v1/requirements/"+r1+"/stator-links"), http.StatusOK, "the links without Stator"), "links")); n != 0 {
			t.Errorf("%d links show without a connection", n)
		}
		if status, code := api.deliverStator(t, statorHook{id: uuid.New(), topic: stator.TopicPing}, statorWebhookSecret); status != http.StatusUnauthorized || code != "bad_signature" {
			t.Errorf("a delivery without a connection answered %d %s", status, code)
		}
		if n := h.countRows(t, `SELECT count(*) FROM stator_page_link l JOIN requirement r ON r.id = l.requirement_id WHERE r.project_id = (SELECT id FROM project WHERE key = $1)`, spgKey); n != 1 {
			t.Errorf("disconnecting left %d links, want 1", n)
		}
	})
}
