//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/stator"
)

// An assistant whose person holds no Stator token is told where to connect, and one whose person does confirms a
// suspect link (#327); a print's token is listed nowhere, writes nothing and ends with its owner.

func TestAnAssistantWithoutAStatorTokenIsToldWhereToConnect(t *testing.T) {
	key := projectKey(t, "MSN")
	h := newHarness(t)
	h.keepStator(t)
	statorOffers(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "mcp-stator-none").user)
	tenant := stubTenant(t)
	statorStub(t, http.MethodDelete, tenant, nil)
	t.Cleanup(func() { statorStub(t, http.MethodDelete, tenant, nil) })
	want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	want(t, owner.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "admin")}), http.StatusOK, "the owner connects")
	bob := api.as(t, h.addPerson(t, "member"))
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage within 100 ms"}), http.StatusCreated, "write R1")
	reqKey := key + "-R1"
	timing := statorPages(t, tenant)["Brake timing"]
	_, bobSecret := makeToken(t, bob, map[string]any{"name": "bob's assistant"})
	_, ownerSecret := makeToken(t, owner, map[string]any{"name": "the owner's assistant"})
	h.settle(t)
	linksOf := func() int {
		return h.countRows(t, `SELECT count(*) FROM stator_page_link l JOIN requirement r ON r.id = l.requirement_id JOIN project p ON p.id = r.project_id WHERE p.key = $1`, key)
	}

	t.Run("without a token the search says so and linking is refused with where to connect", func(t *testing.T) {
		a := connectAssistant(t, api, bobSecret)
		found := a.call(t, "search_stator_pages", map[string]any{"q": "brak"})
		if found["status"] != string(stator.StatusNotConnected) || len(found["pages"].([]any)) != 0 {
			t.Errorf("search_stator_pages without a token answered %v", found)
		}
		msg := a.refused(t, "link_stator_page", map[string]any{"reqKey": reqKey, "pageId": timing, "relation": "specifies"})
		if !strings.Contains(msg, "Your account, Stator account") {
			t.Errorf("link_stator_page without a token is refused with %q", msg)
		}
		if n := linksOf(); n != 0 {
			t.Errorf("a refused link_stator_page left %d links", n)
		}
	})

	t.Run("an assistant confirms a suspect link against the page's version", func(t *testing.T) {
		id := obj(t, want(t, owner.post(t, "/api/v1/requirements/"+reqKey+"/stator-links", map[string]any{"pageId": timing, "relation": "specifies"}),
			http.StatusCreated, "the owner links Brake timing"), "link")["id"].(string)
		if status, _ := api.deliverStator(t, statorHook{id: uuid.New(), topic: stator.TopicPagePublished, payload: pageRef(timing, 3, "ENG")}, statorWebhookSecret); status != http.StatusNoContent {
			t.Fatalf("the publish answered %d", status)
		}
		h.settle(t)
		a := connectAssistant(t, api, ownerSecret)
		cleared := a.call(t, "clear_stator_link", map[string]any{"linkID": id, "pageVersion": 3, "comment": "The timing table still holds."})["link"].(map[string]any)
		if cleared["id"] != id || cleared["suspect"] != false || cleared["pageVersionSeen"] != float64(3) {
			t.Errorf("clear_stator_link answered %v", cleared)
		}
		if n := h.countRows(t, `SELECT count(*) FROM trace_link_clearance WHERE stator_link_id = $1 AND page_version = 3 AND comment = 'The timing table still holds.'`, id); n != 1 {
			t.Errorf("the clearance is recorded %d times", n)
		}
	})
}

func TestAPrintsTokenIsListedNowhereWritesNothingAndEndsWithItsOwner(t *testing.T) {
	key := projectKey(t, "PRT")
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "print-tokens").user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Printed"}), http.StatusCreated, "make a project")
	scriptID, _ := makeToken(t, bob, map[string]any{"name": "bob's script"})
	accounts := auth.NewService(h.cluster, cheapPasswords(), time.Hour)
	secret, printID, _, err := accounts.MintRenderToken(db.WithUser(context.Background(), bobID), bobID)
	if err != nil {
		t.Fatalf("a print's token as a print makes it: %v", err)
	}
	h.settle(t)
	ids := func(c *client, path string) []string {
		t.Helper()
		var out []string
		for _, each := range list(t, want(t, c.get(t, path), http.StatusOK, "the tokens at "+path), "tokens") {
			out = append(out, each.(map[string]any)["id"].(string))
		}
		return out
	}

	t.Run("while the print runs its token is listed nowhere", func(t *testing.T) {
		for who, listed := range map[string][]string{"bob's own list": ids(bob, "/api/v1/tokens"), "everyone's": ids(owner, "/api/v1/tokens/everyone")} {
			if !strings.Contains(strings.Join(listed, " "), scriptID) {
				t.Errorf("%s lacks bob's script token, so it proves nothing: %v", who, listed)
			}
			if strings.Contains(strings.Join(listed, " "), printID.String()) {
				t.Errorf("%s lists the print's token", who)
			}
		}
	})

	printer := api.withToken(bobID, secret)
	t.Run("the print's token reads and writes nothing", func(t *testing.T) {
		want(t, printer.get(t, "/api/v1/projects/"+key), http.StatusOK, "the print reads the project")
		refuse(t, "the print writes a requirement", printer.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Printed in"}),
			http.StatusForbidden, "read_only_token")
		if n := h.countRows(t, `SELECT count(*) FROM requirement r JOIN project p ON p.id = r.project_id WHERE p.key = $1`, key); n != 0 {
			t.Errorf("the refused write left %d requirements", n)
		}
	})

	t.Run("switching its owner off ends it", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/users/"+bobID.String()), http.StatusNoContent, "switch bob off")
		h.settle(t)
		refuse(t, "the print reads once bob is switched off", printer.get(t, "/api/v1/projects/"+key), http.StatusUnauthorized, "unauthorized")
	})
}
