//go:build integration

package test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/stator"
)

// The way back (#42) against the stator-stub: the stack's worker puts each requirement on the pages it is linked to
// as whoever changed it, follows its changes, and leaves off a link a person took off in Stator.

// statorLinksOn is every remote link the stub holds for a tenant to one url, by page.
func statorLinksOn(t *testing.T, tenant, url string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, each := range statorStub(t, http.MethodGet, tenant+"/remote-links", nil)["remoteLinks"].([]any) {
		if l := each.(map[string]any); l["url"] == url {
			out[l["pageId"].(string)] = l
		}
	}
	return out
}

// onPages names the pages of links among names, sorted, for a message that reads.
func onPages(links map[string]map[string]any, names map[string]string) string {
	var titles []string
	for title, id := range names {
		if links[id] != nil {
			titles = append(titles, title)
		}
	}
	slices.Sort(titles)
	return strings.Join(titles, ", ")
}

// statorLinkStates is what GET /requirements/{key}/stator-links says, by page and relation.
func statorLinkStates(t *testing.T, c *client, reqKey string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, each := range list(t, want(t, c.get(t, "/api/v1/requirements/"+reqKey+"/stator-links"), http.StatusOK, "the page links of "+reqKey), "links") {
		l := each.(map[string]any)
		out[l["pageId"].(string)+" "+l["relation"].(string)] = l
	}
	return out
}

func TestRequirementsShowOnTheStatorPagesTheyAreLinkedTo(t *testing.T) {
	srlKey, srxKey := projectKey(t, "SRL"), projectKey(t, "SRX")
	h := newHarness(t)
	h.keepStator(t)
	statorOffersItsOwn(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "stator-remote-links")
	tenant := stubTenant(t)
	statorStub(t, http.MethodDelete, tenant, nil)
	t.Cleanup(func() { statorStub(t, http.MethodDelete, tenant, nil) })
	owner := api.as(t, home.user)
	bobID, daveID := h.addPerson(t, "member"), h.addPerson(t, "member")
	bob, dave := api.as(t, bobID), api.as(t, daveID)

	want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	// A new project key is the owner's change, so the owner's token puts the new titles.
	want(t, owner.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "admin")}), http.StatusOK, "the owner connects as the stub's admin")
	want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "bob")}), http.StatusOK, "bob connects")
	want(t, dave.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "reader")}), http.StatusOK, "dave connects as a reader")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": srlKey, "name": "Shown on pages"}), http.StatusCreated, "make a project")
	var ids []string
	for _, title := range []string{"Brakes engage within 100 ms", "Log every login", "Warn before the brakes fade"} {
		made := obj(t, want(t, bob.post(t, "/api/v1/projects/"+srlKey+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title), "requirement")
		ids = append(ids, made["id"].(string))
	}
	appURL := testAppURL(t)
	url1, url2, url3 := stator.RequirementURL(appURL, uuid.MustParse(ids[0])), stator.RequirementURL(appURL, uuid.MustParse(ids[1])), stator.RequirementURL(appURL, uuid.MustParse(ids[2]))
	pages := statorPages(t, tenant)
	timing, braking := pages["Brake timing"], pages["Braking system"]
	named := map[string]string{"Brake timing": timing, "Braking system": braking}
	link := func(t *testing.T, c *client, reqKey, page, relation string) {
		t.Helper()
		want(t, c.post(t, "/api/v1/requirements/"+reqKey+"/stator-links", map[string]any{"pageId": page, "relation": relation}), http.StatusCreated, "link "+reqKey)
	}

	t.Run("the worker puts one link per page on Stator, as the person who linked", func(t *testing.T) {
		link(t, bob, srlKey+"-R1", timing, "relates")
		link(t, bob, srlKey+"-R1", timing, "specifies")
		link(t, bob, srlKey+"-R1", braking, "describes")
		h.drained(t)
		got := statorLinksOn(t, tenant, url1)
		if onPages(got, named) != "Brake timing, Braking system" {
			t.Fatalf(srlKey+"-R1 is on %s", onPages(got, named))
		}
		on := got[timing]
		if on["title"] != srlKey+"-R1 Brakes engage within 100 ms (specifies)" || on["source"] != stator.LinkSource ||
			on["createdByName"] != "Bob" || on["iconUrl"] != appURL+stator.IconPath {
			t.Errorf("the link on Brake timing is %v", on)
		}
		if got[braking]["title"] != srlKey+"-R1 Brakes engage within 100 ms (describes)" {
			t.Errorf("the link on Braking system is titled %v", got[braking]["title"])
		}
		for key, l := range statorLinkStates(t, bob, srlKey+"-R1") {
			if l["sync"] != "synced" || l["error"] != "" {
				t.Errorf("%s reads %v", key, l)
			}
		}
	})

	t.Run("a new revision, a new key and removed links follow", func(t *testing.T) {
		want(t, bob.patch(t, "/api/v1/requirements/"+srlKey+"-R1", map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}), http.StatusOK, "revise R1")
		h.drained(t)
		if title := statorLinksOn(t, tenant, url1)[timing]["title"]; title != srlKey+"-R1 Brakes engage within 80 ms (specifies)" {
			t.Errorf("after the revision Brake timing shows %v", title)
		}
		want(t, owner.put(t, "/api/v1/projects/"+srlKey+"/key", map[string]any{"key": srxKey}), http.StatusOK, "rekey the project")
		h.drained(t)
		if title := statorLinksOn(t, tenant, url1)[timing]["title"]; title != srxKey+"-R1 Brakes engage within 80 ms (specifies)" {
			t.Errorf("after the new key Brake timing shows %v", title)
		}
		states := statorLinkStates(t, bob, srxKey+"-R1")
		want(t, bob.delete(t, "/api/v1/stator-links/"+states[timing+" specifies"]["id"].(string)), http.StatusNoContent, "remove the specifies link")
		h.drained(t)
		if title := statorLinksOn(t, tenant, url1)[timing]["title"]; title != srxKey+"-R1 Brakes engage within 80 ms (relates)" {
			t.Errorf("with only the relates link Brake timing shows %v", title)
		}
		want(t, bob.delete(t, "/api/v1/stator-links/"+states[timing+" relates"]["id"].(string)), http.StatusNoContent, "remove the relates link")
		h.drained(t)
		if on := onPages(statorLinksOn(t, tenant, url1), named); on != "Braking system" {
			t.Errorf("after both links went R1 is on %s", on)
		}
	})

	t.Run("a Stator that answers 503 is asked again through the outbox", func(t *testing.T) {
		statorStub(t, http.MethodPut, tenant+"/remote-links/outage", map[string]any{"status": http.StatusServiceUnavailable, "count": 1})
		link(t, bob, srxKey+"-R2", timing, "specifies")
		h.drained(t)
		if on := onPages(statorLinksOn(t, tenant, url2), named); on != "Brake timing" {
			t.Errorf("after the outage R2 is on %q", on)
		}
		if n := h.countRows(t, `
			SELECT count(*) FROM outbox_event
			WHERE topic = $1 AND payload ->> 'requirementId' = $2 AND attempts > 1 AND last_error LIKE '%503%'`,
			stator.TopicLinks, ids[1]); n != 1 {
			t.Errorf("%d events were tried again after the 503, want 1", n)
		}
	})

	t.Run("a refusal marks the link failed, and the next change tries it again", func(t *testing.T) {
		link(t, dave, srxKey+"-R2", braking, "relates")
		h.drained(t)
		failed := statorLinkStates(t, owner, srxKey+"-R2")[braking+" relates"]
		if failed["sync"] != "failed" || !strings.Contains(failed["error"].(string), "did not let") || !strings.HasSuffix(failed["error"].(string), "The next change to the requirement tries again.") {
			t.Errorf("a refused link reads %v", failed)
		}
		want(t, bob.patch(t, "/api/v1/requirements/"+srxKey+"-R2", map[string]any{"title": "Log every sign-in", "baseRevision": 1}), http.StatusOK, "bob revises R2")
		h.drained(t)
		if on := onPages(statorLinksOn(t, tenant, url2), named); on != "Brake timing, Braking system" {
			t.Errorf("after bob's change R2 is on %s", on)
		}
		if state := statorLinkStates(t, owner, srxKey+"-R2")[braking+" relates"]["sync"]; state != "synced" {
			t.Errorf("the link reads %v after the next change", state)
		}
	})

	t.Run("a link a person takes off in Stator stays off", func(t *testing.T) {
		remote := statorLinksOn(t, tenant, url1)[braking]["id"]
		statorStub(t, http.MethodDelete, tenant+"/pages/"+braking+"/remote-links?url="+url1, nil)
		removed := map[string]any{"page": pageRef(braking, 3, "ENG")["page"],
			"remoteLink": map[string]any{"id": remote, "url": "http://" + appHost + stator.RequirementPath + ids[0], "title": "x", "source": stator.LinkSource},
			"actor":      map[string]any{"id": uuid.NewString(), "name": "Admin"}}
		if status, _ := api.deliverStator(t, statorHook{id: uuid.New(), topic: stator.TopicRemoteLinkRemoved, payload: removed}, statorWebhookSecret); status != http.StatusNoContent {
			t.Fatalf("the removal answered %d", status)
		}
		h.settle(t)
		detached := statorLinkStates(t, bob, srxKey+"-R1")[braking+" describes"]
		if detached["detachedAt"] == nil {
			t.Fatalf("the link taken off reads %v", detached)
		}
		want(t, bob.patch(t, "/api/v1/requirements/"+srxKey+"-R1", map[string]any{"title": "Brakes engage within 70 ms", "baseRevision": 2}), http.StatusOK, "revise R1 again")
		h.drained(t)
		if on := onPages(statorLinksOn(t, tenant, url1), named); on != "" {
			t.Errorf("a link taken off in Stator came back on %s", on)
		}
	})

	t.Run("trash takes the links off, restore puts them back, and a purge takes them off for good", func(t *testing.T) {
		want(t, bob.delete(t, "/api/v1/requirements/"+srxKey+"-R2"), http.StatusNoContent, "trash R2")
		h.drained(t)
		if on := onPages(statorLinksOn(t, tenant, url2), named); on != "" {
			t.Errorf("a trashed requirement is still on %s", on)
		}
		want(t, bob.post(t, "/api/v1/projects/"+srxKey+"/trash/"+ids[1]+"/restore", nil), http.StatusOK, "restore R2")
		h.drained(t)
		if on := onPages(statorLinksOn(t, tenant, url2), named); on != "Brake timing, Braking system" {
			t.Errorf("a restored requirement is on %s", on)
		}
		want(t, bob.delete(t, "/api/v1/requirements/"+srxKey+"-R2"), http.StatusNoContent, "trash R2 again")
		want(t, owner.delete(t, "/api/v1/projects/"+srxKey+"/trash/"+ids[1]), http.StatusNoContent, "purge R2")
		h.drained(t)
		if on := onPages(statorLinksOn(t, tenant, url2), named); on != "" {
			t.Errorf("a purged requirement is still on %s", on)
		}
		if n := h.countRows(t, `SELECT count(*) FROM stator_remote_link WHERE requirement_id = $1`, ids[1]); n != 0 {
			t.Errorf("%d sync records outlived the purge", n)
		}
	})

	t.Run("nothing is put while Stator takes no remote links, and the first check that finds them catches up", func(t *testing.T) {
		statorOffers(t)
		want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t)}), http.StatusOK, "save the connection again")
		link(t, bob, srxKey+"-R3", timing, "describes")
		h.drained(t)
		if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE topic = $1 AND payload ->> 'requirementId' = $2`, stator.TopicLinks, ids[2]); n != 0 {
			t.Errorf("%d syncs were queued while Stator takes no remote links", n)
		}
		if state := statorLinkStates(t, bob, srxKey+"-R3")[timing+" describes"]["sync"]; state != "none" {
			t.Errorf("the link reads %v while Stator takes no remote links", state)
		}
		statorOffersItsOwn(t)
		want(t, bob.post(t, "/api/v1/stator/account/check", nil), http.StatusOK, "bob checks his token")
		h.drained(t)
		on := statorLinksOn(t, tenant, url3)[timing]
		if on == nil || on["createdByName"] != "Bob" {
			t.Fatalf("the catch-up put %v on Brake timing", on)
		}
		if state := statorLinkStates(t, bob, srxKey+"-R3")[timing+" describes"]["sync"]; state != "synced" {
			t.Errorf("the link reads %v after the catch-up", state)
		}
	})
}
