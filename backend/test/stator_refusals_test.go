//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/stator"
)

// The refusals of the Stator links the rest of the suite never gave (#327): a reader holding a Stator token, an
// archived project, the trash, the API's bounds, deliveries that say nothing, and every sentence of the link sync.

// statorLinkRows is everything a refusal must leave as it was of a project's page links: each link's place, versions,
// revision and clearances, and the worker's record of it on the page.
func statorLinkRows(t *testing.T, h *harness, projectKey string) string {
	t.Helper()
	var rows string
	if err := h.super.QueryRow(t.Context(), `
		SELECT COALESCE(string_agg(format('%s %s %s %s v%s/%s r%s broken=%s cleared=%s sync=%s', l.id, l.relation, l.space_key, l.url,
		           l.page_version, l.page_version_seen, l.reviewed_rev, l.broken_at IS NOT NULL,
		           (SELECT count(*) FROM trace_link_clearance c WHERE c.stator_link_id = l.id),
		           (SELECT string_agg(rl.state || ':' || COALESCE(rl.remote_link_id::text, '-'), ',') FROM stator_remote_link rl
		            WHERE rl.requirement_id = l.requirement_id AND rl.page_id = l.page_id)), '; ' ORDER BY l.id), 'none')
		FROM stator_page_link l JOIN requirement r ON r.id = l.requirement_id JOIN project p ON p.id = r.project_id
		WHERE p.key = $1`, projectKey).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

// statorLinkOf is the one link a requirement has to a page, as its reader lists it.
func statorLinkOf(t *testing.T, c *client, reqKey, page string) map[string]any {
	t.Helper()
	for _, each := range list(t, want(t, c.get(t, "/api/v1/requirements/"+reqKey+"/stator-links"), http.StatusOK, "the page links of "+reqKey), "links") {
		if l := each.(map[string]any); l["pageId"] == page {
			return l
		}
	}
	t.Fatalf("%s has no link to %s", reqKey, page)
	return nil
}

// postStatorBody posts a raw body to the webhook, signed with secret, and answers the status and the error code.
func (a *apiServer) postStatorBody(t *testing.T, body []byte, secret string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, a.URL+"/api/v1/stator/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(stator.SignatureHeader, stator.Sign(body, secret))
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

// A person who may read a project but not change it holds a Stator token and still makes, removes and clears no
// link; neither does anybody in the trash or in an archived project, and nothing of the links or the sync changes.
func TestStatorLinksAreRefusedToReadersTheTrashAndArchivedProjects(t *testing.T) {
	key := projectKey(t, "SRF")
	h := newHarness(t)
	h.keepStator(t)
	statorOffersItsOwn(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "stator-refusals")
	tenant := stubTenant(t)
	statorStub(t, http.MethodDelete, tenant, nil)
	t.Cleanup(func() { statorStub(t, http.MethodDelete, tenant, nil) })
	owner := api.as(t, home.user)
	bobID, veraID := h.addPerson(t, "member"), h.addPerson(t, "member")
	bob, vera := api.as(t, bobID), api.as(t, veraID)

	want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	want(t, owner.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "admin")}), http.StatusOK, "the owner connects")
	want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "bob")}), http.StatusOK, "bob connects")
	want(t, vera.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "vera")}), http.StatusOK, "vera connects")
	project := obj(t, want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Refused pages"}), http.StatusCreated, "make a project"), "project")
	projectID := uuid.MustParse(project["id"].(string))
	// Only those granted the project reach it: bob edits it, and vera only reads it.
	h.cleanupExec(t, h.super, `DELETE FROM project_grant WHERE project_id = $1 AND subject_type = 'everyone'`, projectID)
	h.grantAs(t, projectID, bobID, "editor")
	h.grantAs(t, projectID, veraID, "viewer")
	for _, title := range []string{"Brakes engage within 100 ms", "Log every login"} {
		want(t, bob.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title)
	}
	r1, r2 := key+"-R1", key+"-R2"
	pages := statorPages(t, tenant)
	timing, braking := pages["Brake timing"], pages["Braking system"]
	for _, reqKey := range []string{r1, r2} {
		want(t, bob.post(t, "/api/v1/requirements/"+reqKey+"/stator-links", map[string]any{"pageId": timing, "relation": "specifies"}), http.StatusCreated, "bob links "+reqKey)
	}
	h.drained(t)
	// Stator publishes version 3, so every link waits to be cleared and a refused clear would have had something to do.
	if status, _ := api.deliverStator(t, statorHook{id: uuid.New(), topic: stator.TopicPagePublished, payload: pageRef(timing, 3, "ENG")}, statorWebhookSecret); status != http.StatusNoContent {
		t.Fatalf("the publish answered %d", status)
	}
	h.settle(t)
	l1, l2 := statorLinkOf(t, bob, r1, timing), statorLinkOf(t, bob, r2, timing)
	if l1["suspect"] != true || l1["sync"] != "synced" || l2["suspect"] != true {
		t.Fatalf("before the refusals the links read %v and %v", l1, l2)
	}
	id1, id2 := l1["id"].(string), l2["id"].(string)
	clear := map[string]any{"pageVersion": 3, "comment": "The timing table still holds."}
	before := statorLinkRows(t, h, key)

	t.Run("a viewer holding a Stator token reads the links and changes none", func(t *testing.T) {
		if account := obj(t, want(t, vera.get(t, "/api/v1/stator/account"), http.StatusOK, "vera's account"), "account"); account["status"] != "ok" {
			t.Fatalf("vera's token reads %v, so a refusal would be about the token", account)
		}
		if n := len(list(t, want(t, vera.get(t, "/api/v1/requirements/"+r1+"/stator-links"), http.StatusOK, "vera lists the links"), "links")); n != 1 {
			t.Errorf("vera reads %d links of %s, want 1", n, r1)
		}
		refuse(t, "vera links a page", vera.post(t, "/api/v1/requirements/"+r1+"/stator-links", map[string]any{"pageId": braking, "relation": "describes"}),
			http.StatusForbidden, "forbidden")
		refuse(t, "vera removes a link", vera.delete(t, "/api/v1/stator-links/"+id1), http.StatusForbidden, "forbidden")
		refuse(t, "vera clears a link", vera.post(t, "/api/v1/stator-links/"+id1+"/clear", clear), http.StatusForbidden, "forbidden")
		h.drained(t)
		if after := statorLinkRows(t, h, key); after != before {
			t.Errorf("a viewer's refusals left the links\n%s\nwant\n%s", after, before)
		}
	})

	t.Run("a link is named by its id, and anything else is a bad request", func(t *testing.T) {
		refuse(t, "a removal of no id", bob.delete(t, "/api/v1/stator-links/not-an-id"), http.StatusBadRequest, "bad_request")
		refuse(t, "a clear of no id", bob.post(t, "/api/v1/stator-links/not-an-id/clear", clear), http.StatusBadRequest, "bad_request")
		refuse(t, "a removal of a link nobody has", bob.delete(t, "/api/v1/stator-links/"+uuid.NewString()), http.StatusNotFound, "not_found")
		if after := statorLinkRows(t, h, key); after != before {
			t.Errorf("the refusals left the links\n%s\nwant\n%s", after, before)
		}
	})

	t.Run("a link of a requirement in the trash is not cleared, and the requirement takes no new one", func(t *testing.T) {
		want(t, bob.delete(t, "/api/v1/requirements/"+r2), http.StatusNoContent, "trash R2")
		h.drained(t)
		trashed := statorLinkRows(t, h, key)
		refuse(t, "a clear in the trash", bob.post(t, "/api/v1/stator-links/"+id2+"/clear", clear), http.StatusConflict, "requirement_trashed")
		refuse(t, "a link in the trash", bob.post(t, "/api/v1/requirements/"+r2+"/stator-links", map[string]any{"pageId": braking, "relation": "describes"}),
			http.StatusConflict, "requirement_trashed")
		h.drained(t)
		if after := statorLinkRows(t, h, key); after != trashed {
			t.Errorf("the refusals in the trash left the links\n%s\nwant\n%s", after, trashed)
		}
	})

	t.Run("an archived project's links are neither made, removed nor cleared", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/projects/"+key+"/archive", nil), http.StatusOK, "archive the project")
		h.drained(t)
		archived := statorLinkRows(t, h, key)
		for who, c := range map[string]*client{"the editor": bob, "an administrator": owner} {
			refuse(t, who+" links", c.post(t, "/api/v1/requirements/"+r1+"/stator-links", map[string]any{"pageId": braking, "relation": "describes"}),
				http.StatusForbidden, "forbidden")
			refuse(t, who+" removes a link", c.delete(t, "/api/v1/stator-links/"+id1), http.StatusForbidden, "forbidden")
			refuse(t, who+" clears a link", c.post(t, "/api/v1/stator-links/"+id1+"/clear", clear), http.StatusConflict, "project_archived")
		}
		h.drained(t)
		if after := statorLinkRows(t, h, key); after != archived {
			t.Errorf("the refusals in an archived project left the links\n%s\nwant\n%s", after, archived)
		}
	})
}

// The lookup and the search keep to their bounds, a delivery past its size or that is no envelope is set aside
// unread, and a late publish from the space a page left never moves its link back (#327).
func TestStatorsBoundsAndDeliveriesThatSayNothing(t *testing.T) {
	key := projectKey(t, "SBD")
	h := newHarness(t)
	h.keepStator(t)
	statorOffers(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "stator-bounds")
	tenant := stubTenant(t)
	statorStub(t, http.MethodDelete, tenant, nil)
	t.Cleanup(func() { statorStub(t, http.MethodDelete, tenant, nil) })
	owner := api.as(t, home.user)
	bob := api.as(t, h.addPerson(t, "member"))
	want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "bob")}), http.StatusOK, "bob connects")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Bounded pages"}), http.StatusCreated, "make a project")
	want(t, bob.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage within 100 ms"}), http.StatusCreated, "write R1")
	r1 := key + "-R1"
	timing := statorPages(t, tenant)["Brake timing"]
	want(t, bob.post(t, "/api/v1/requirements/"+r1+"/stator-links", map[string]any{"pageId": timing, "relation": "specifies"}), http.StatusCreated, "bob links Brake timing")
	base := statorURL(t)
	h.settle(t)

	t.Run("a lookup takes at most 50 distinct pages, and a search its bounds", func(t *testing.T) {
		ids := url.Values{}
		for range stator.MaxLookupPages {
			ids.Add("id", uuid.NewString())
		}
		ids.Add("id", ids["id"][0])
		if got := obj(t, want(t, bob.get(t, "/api/v1/stator/pages?"+ids.Encode()), http.StatusOK, "50 pages and one again")); len(got["pages"].([]any)) != stator.MaxLookupPages {
			t.Errorf("50 distinct pages answered %d results", len(got["pages"].([]any)))
		}
		ids.Add("id", uuid.NewString())
		if msg := fieldError(t, bob.get(t, "/api/v1/stator/pages?"+ids.Encode()), "id"); !strings.Contains(msg, "at most 50") {
			t.Errorf("51 distinct pages are refused with %q", msg)
		}

		longest := strings.Repeat("b", stator.MaxSearchLength)
		want(t, bob.get(t, "/api/v1/stator/search?q="+longest), http.StatusOK, "a search of 200 characters")
		if msg := fieldError(t, bob.get(t, "/api/v1/stator/search?q="+longest+"b"), "q"); !strings.Contains(msg, "at most 200 characters") {
			t.Errorf("a search of 201 characters is refused with %q", msg)
		}
		if msg := fieldError(t, bob.get(t, "/api/v1/stator/search?q=brak&limit=abc"), "limit"); !strings.Contains(msg, "1 to 20") {
			t.Errorf("a limit that is no number is refused with %q", msg)
		}
		if msg := fieldError(t, bob.get(t, "/api/v1/stator/search?q=brak&space="+strings.Repeat("S", stator.MaxSpaceKeyLength+1)), "space"); !strings.Contains(msg, "at most 100") {
			t.Errorf("a space key of 101 characters is refused with %q", msg)
		}
	})

	before := statorLinkRows(t, h, key)
	t.Run("a delivery past its size, or that is no envelope, changes nothing", func(t *testing.T) {
		huge, _ := json.Marshal(map[string]any{"id": uuid.New(), "topic": stator.TopicPagePublished, "occurredAt": time.Now().UTC(),
			"payload": merged(pageRef(timing, 9, "SEC"), "padding", strings.Repeat("x", stator.WebhookMaxBytes))})
		if status, code := api.postStatorBody(t, huge, statorWebhookSecret); status != http.StatusRequestEntityTooLarge || code != "too_large" {
			t.Errorf("a delivery past %d bytes answered %d %s", stator.WebhookMaxBytes, status, code)
		}
		for what, body := range map[string]string{
			"no JSON":             "Stator says hello",
			"JSON of no envelope": `{"hello": "world"}`,
			"a list":              `[{"topic": "page.published"}]`,
		} {
			if status, code := api.postStatorBody(t, []byte(body), statorWebhookSecret); status != http.StatusNoContent {
				t.Errorf("a signed body of %s answered %d %s", what, status, code)
			}
		}
		long := statorHook{id: uuid.New(), topic: stator.TopicPageMoved, payload: pageRef(timing, 2, strings.Repeat("S", stator.MaxSpaceKeyLength+1))}
		if status, code := api.deliverStator(t, long, statorWebhookSecret); status != http.StatusNoContent {
			t.Errorf("a move to a space key past its bound answered %d %s", status, code)
		}
		h.settle(t)
		if after := statorLinkRows(t, h, key); after != before {
			t.Errorf("deliveries that say nothing left the links\n%s\nwant\n%s", after, before)
		}
	})

	t.Run("a late publish from the space the page left never moves its link back", func(t *testing.T) {
		send := func(what string, k statorHook) {
			t.Helper()
			if status, code := api.deliverStator(t, k, statorWebhookSecret); status != http.StatusNoContent {
				t.Fatalf("%s answered %d %s", what, status, code)
			}
			h.settle(t)
		}
		// Stator published version 3 in ENG and moved the page to SEC; the move is delivered first.
		send("the move", statorHook{id: uuid.New(), topic: stator.TopicPageMoved, payload: pageRef(timing, 3, "SEC")})
		if l := statorLinkOf(t, bob, r1, timing); l["spaceKey"] != "SEC" || l["url"] != base+"/s/SEC/p/"+timing || l["pageVersion"] != float64(3) || l["suspect"] != true {
			t.Fatalf("after the move the link reads %v", l)
		}
		send("the publish of the same version, late", statorHook{id: uuid.New(), topic: stator.TopicPagePublished, payload: pageRef(timing, 3, "ENG")})
		send("the publish of an older version, late", statorHook{id: uuid.New(), topic: stator.TopicPagePublished, payload: pageRef(timing, 2, "ENG")})
		if l := statorLinkOf(t, bob, r1, timing); l["spaceKey"] != "SEC" || l["url"] != base+"/s/SEC/p/"+timing || l["pageVersion"] != float64(3) {
			t.Errorf("a late publish from ENG moved the link back: %v", l)
		}
		send("a newer version published elsewhere", statorHook{id: uuid.New(), topic: stator.TopicPagePublished, payload: pageRef(timing, 4, "OPS")})
		if l := statorLinkOf(t, bob, r1, timing); l["spaceKey"] != "OPS" || l["url"] != base+"/s/OPS/p/"+timing || l["pageVersion"] != float64(4) {
			t.Errorf("a newer publish did not move the link: %v", l)
		}
	})
}

// Every sentence the link sync writes on a link Stator refused, each ending in what happens next (#327).
func TestEveryRemoteLinkRefusalSaysWhy(t *testing.T) {
	key := projectKey(t, "SRS")
	h := newHarness(t)
	h.keepStator(t)
	statorOffersItsOwn(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "stator-sentences")
	tenant := stubTenant(t)
	statorStub(t, http.MethodDelete, tenant, nil)
	t.Cleanup(func() { statorStub(t, http.MethodDelete, tenant, nil) })
	owner := api.as(t, home.user)
	bob, dave, erin := api.as(t, h.addPerson(t, "member")), api.as(t, h.addPerson(t, "member")), api.as(t, h.addPerson(t, "member"))
	want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	want(t, owner.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "admin")}), http.StatusOK, "the owner connects")
	want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "bob")}), http.StatusOK, "bob connects")
	want(t, dave.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "readonly")}), http.StatusOK, "dave connects with a token that reads")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Refused on pages"}), http.StatusCreated, "make a project")
	for _, title := range []string{"Brakes engage within 100 ms", "Log every login"} {
		want(t, bob.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title)
	}
	r1, r2 := key+"-R1", key+"-R2"
	pages := statorPages(t, tenant)
	timing, braking := pages["Brake timing"], pages["Braking system"]
	const again = "The next change to the requirement tries again."
	failedWith := func(t *testing.T, reqKey, page, says string) {
		t.Helper()
		h.drained(t)
		l := statorLinkOf(t, owner, reqKey, page)
		msg, _ := l["error"].(string)
		if l["sync"] != "failed" || !strings.Contains(msg, says) || !strings.HasSuffix(msg, again) {
			t.Errorf("the link of %s reads %v %q, want failed saying %q and ending %q", reqKey, l["sync"], msg, says, again)
		}
	}

	t.Run("a token that may only read puts nothing", func(t *testing.T) {
		want(t, dave.post(t, "/api/v1/requirements/"+r1+"/stator-links", map[string]any{"pageId": timing, "relation": "specifies"}), http.StatusCreated, "dave links Brake timing")
		failedWith(t, r1, timing, "Stator token may only read. Store one that may write under Your account, Stator account.")
	})

	t.Run("a change by somebody without a token puts nothing", func(t *testing.T) {
		want(t, erin.patch(t, "/api/v1/requirements/"+r1, map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}), http.StatusOK, "erin revises R1")
		failedWith(t, r1, timing, "has not connected a Stator account, so the link was not put on the page.")
	})

	t.Run("a page Stator no longer has is said so", func(t *testing.T) {
		want(t, bob.post(t, "/api/v1/requirements/"+r2+"/stator-links", map[string]any{"pageId": braking, "relation": "describes"}), http.StatusCreated, "bob links Braking system")
		h.drained(t)
		if l := statorLinkOf(t, owner, r2, braking); l["sync"] != "synced" {
			t.Fatalf("before the page went the link reads %v", l)
		}
		// Stator trashes the page without a webhook to tell Exciter, so only the next sync finds it gone.
		statorStub(t, http.MethodDelete, tenant+"/pages/"+braking, nil)
		want(t, bob.patch(t, "/api/v1/requirements/"+r2, map[string]any{"title": "Log every sign-in", "baseRevision": 1}), http.StatusOK, "bob revises R2")
		failedWith(t, r2, braking, "Stator has no page with this id that")
	})
}
