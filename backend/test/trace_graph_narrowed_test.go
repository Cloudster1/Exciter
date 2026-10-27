//go:build integration

package test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/stator"
)

// The trace graph against every way it is narrowed (#314): Stator pages, a project's own link types, a section and
// the trash.

// Stator pages traced from a requirement are nodes below it, reached through their relation, and broken once Stator
// says it deleted the page.
func TestATraceGraphDrawsTheStatorPagesOfItsRequirements(t *testing.T) {
	w := newCommentWorld(t, "TGP")
	w.h.keepStator(t)
	tenant := stubTenant(t)
	statorStub(t, http.MethodDelete, tenant, nil)
	statorOffers(t)
	want(t, w.owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	want(t, w.bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "bob")}), http.StatusOK, "bob connects")
	pages := statorPages(t, tenant)
	timing, braking := pages["Brake timing"], pages["Braking system"]
	for _, page := range []string{timing, braking} {
		want(t, w.bob.post(t, "/api/v1/requirements/"+w.reqKey+"/stator-links", map[string]any{"pageId": page, "relation": "specifies"}),
			http.StatusCreated, "R1 is specified by "+page)
	}
	if status, code := w.owner.api.deliverStator(t, statorHook{id: uuid.New(), topic: stator.TopicPageDeleted, payload: pageRef(braking, 3, "ENG")},
		statorWebhookSecret); status != http.StatusNoContent {
		t.Fatalf("Stator's page.deleted answered %d %s", status, code)
	}
	w.h.settle(t)
	pageOf := func(id string) string { return "page:" + id }

	t.Run("each page is a node below its requirement, the deleted one broken", func(t *testing.T) {
		g := graphOf(t, w.carol, w.reqKey, "?relation=specifies")
		sameGraph(t, "the specifying pages", g, map[string]float64{w.reqKey: 0, pageOf(timing): 1, pageOf(braking): 1})
		nodes := graphNodes(g)
		for id, broken := range map[string]bool{timing: false, braking: true} {
			n := nodes[pageOf(id)]
			page, _ := n["page"].(map[string]any)
			if n["kind"] != "page" || n["broken"] != broken || page == nil || page["spaceKey"] == "" {
				t.Errorf("the page %s reads %v", id, n)
			}
		}
		for _, each := range g["edges"].([]any) {
			e := each.(map[string]any)
			if e["from"] != w.reqID || e["relation"] != "specifies" || e["broken"] != (e["to"] == pageOf(braking)) {
				t.Errorf("an edge to a page reads %v", e)
			}
		}
	})
	t.Run("a relation the pages are not linked by reaches none of them", func(t *testing.T) {
		sameGraph(t, "the implementing items", graphOf(t, w.carol, w.reqKey, "?relation=implements"), map[string]float64{w.reqKey: 0})
	})
}

// A graph follows a project's own link type by its key, and refuses another project's on its field.
func TestATraceGraphFollowsAProjectsOwnLinkTypes(t *testing.T) {
	w := newCommentWorld(t, "TGT")
	other := projectKey(t, "TGTO")
	for _, p := range []struct{ project, key string }{{w.key, "tests"}, {other, "audits"}} {
		if p.project == other {
			want(t, w.owner.post(t, "/api/v1/projects", map[string]any{"key": other, "name": "Audited"}), http.StatusCreated, "make a second project")
		}
		want(t, w.owner.post(t, "/api/v1/projects/"+p.project+"/link-types", map[string]any{
			"key": p.key, "name": p.key, "forward": p.key, "backward": "is " + p.key + " by", "counts": "verification",
		}), http.StatusCreated, "make the type "+p.key+" in "+p.project)
	}
	want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/requirements", map[string]any{"title": "Brake bench run"}), http.StatusCreated, "write R2")
	want(t, w.bob.post(t, "/api/v1/requirements/"+w.key+"-R2/links", map[string]any{"typeKey": "tests", "otherKey": w.reqKey}), http.StatusCreated, "R2 tests R1")

	sameGraph(t, "through tests", graphOf(t, w.carol, w.reqKey, "?type=tests"), map[string]float64{w.reqKey: 0, w.key + "-R2": 1})
	sameGraph(t, "through derives", graphOf(t, w.carol, w.reqKey, "?type=derives"), map[string]float64{w.reqKey: 0})
	r := w.carol.get(t, "/api/v1/requirements/"+w.reqKey+"/graph?type=audits")
	refuse(t, "another project's own type", r, http.StatusBadRequest, "bad_request")
	if r.Status == http.StatusBadRequest {
		if msg, _ := obj(t, r, "error", "fields")["type"].(string); msg == "" {
			t.Errorf("the refusal names no field: %s", r.Raw)
		}
	}
}

// A section is never a link's end, so its graph is the section alone. A requirement in the trash answers its graph to
// whoever may open it there, the centre alone and broken since what waits in the trash traces to nothing, and is not
// found by anybody else, as the requirement itself is not.
func TestATraceGraphOfASectionOrOfTheTrash(t *testing.T) {
	w := newCommentWorld(t, "TGS")
	section := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/requirements", map[string]any{"kind": "section", "title": "Braking"}),
		http.StatusCreated, "write a section"), "requirement")["key"].(string)
	want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/requirements", map[string]any{"title": "Brake bench run"}), http.StatusCreated, "write a requirement")
	trashed := w.key + "-R3"
	want(t, w.bob.post(t, "/api/v1/requirements/"+trashed+"/links", map[string]any{"typeKey": "derives", "otherKey": w.reqKey}), http.StatusCreated,
		"R3 derives from R1")

	t.Run("a section", func(t *testing.T) {
		g := graphOf(t, w.carol, section, "")
		sameGraph(t, "the section's graph", g, map[string]float64{section: 0})
		if len(g["edges"].([]any)) != 0 || g["hidden"] != float64(0) {
			t.Errorf("the section's graph reads %v", g)
		}
	})
	t.Run("a requirement in the trash", func(t *testing.T) {
		want(t, w.bob.delete(t, "/api/v1/requirements/"+trashed), http.StatusNoContent, "trash R3")
		refuse(t, "carol, who may not open the trash", w.carol.get(t, "/api/v1/requirements/"+trashed+"/graph"), http.StatusNotFound, "not_found")
		g := graphOf(t, w.bob, trashed, "")
		sameGraph(t, "the trashed requirement's graph", g, map[string]float64{trashed: 0})
		if centre := graphNodes(g)[trashed]; centre["broken"] != true || centre["requirement"].(map[string]any)["trashed"] != true {
			t.Errorf("the centre in the trash reads %v", centre)
		}
		// Bob sees the trash, so from R1 he reaches R3 there, broken; carol is not told of it at all.
		from := graphOf(t, w.bob, w.reqKey, "")
		sameGraph(t, "bob's graph of R1", from, map[string]float64{w.reqKey: 0, trashed: 1})
		if n := graphNodes(from)[trashed]; n["broken"] != true {
			t.Errorf("from R1, the trashed R3 reads %v", n)
		}
		sameGraph(t, "carol's graph of R1", graphOf(t, w.carol, w.reqKey, ""), map[string]float64{w.reqKey: 0})
	})
}
