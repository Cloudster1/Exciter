//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"
)

// viaIn is the via an answer gives, nil for one written in a session.
func viaIn(entry any) map[string]any {
	v, _ := entry.(map[string]any)["via"].(map[string]any)
	return v
}

// throughToken says whether an answer names the token it was written through.
func throughToken(entry any, name string) bool {
	v := viaIn(entry)
	return v != nil && v["kind"] == "token" && v["name"] == name
}

// What a script or an assistant writes through a token or a connected app says so wherever it is read, what a
// person writes in the browser does not, and a token revoked keeps its name on what it wrote (#190).
func TestWhatIsWrittenThroughATokenSaysSo(t *testing.T) {
	key := projectKey(t, "VIA")
	h := newHarness(t)
	a := newOAuthServer(t, h)
	owner := a.as(t, h.owner(t, "via").user)
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	for _, title := range []string{"Brakes", "Lights"} {
		want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title)
	}
	r1, r2 := key+"-R1", key+"-R2"
	tokenID, secret := makeToken(t, owner, map[string]any{"name": "Nightly script"})
	h.settle(t)
	script := a.withToken(owner.user, secret)

	want(t, script.patch(t, "/api/v1/requirements/"+r1, map[string]any{"title": "Brakes engage", "baseRevision": 1}), http.StatusOK, "revise through the token")
	want(t, script.patch(t, "/api/v1/requirements/"+r1, map[string]any{"priority": "must"}), http.StatusOK, "set the priority through the token")
	want(t, script.post(t, "/api/v1/requirements/"+r1+"/comments", commentBody("Checked overnight.")), http.StatusCreated, "comment through the token")
	want(t, script.post(t, "/api/v1/requirements/"+r1+"/links", map[string]any{"typeKey": "relates", "otherKey": r2}), http.StatusCreated, "link through the token")
	want(t, owner.post(t, "/api/v1/requirements/"+r2+"/comments", commentBody("Written by hand.")), http.StatusCreated, "comment in the browser")
	other := projectKey(t, "VIB")
	madeID := obj(t, want(t, script.post(t, "/api/v1/projects", map[string]any{"key": other, "name": other}), http.StatusCreated,
		"make a project through the token"), "project")["id"]
	h.settle(t)

	check := func(t *testing.T, reader *client, name string) {
		t.Helper()
		revisions := list(t, want(t, reader.get(t, "/api/v1/requirements/"+r1+"/revisions"), http.StatusOK, "list the revisions"), "revisions")
		if !throughToken(revisions[0], name) || viaIn(revisions[1]) != nil {
			t.Errorf("the revisions read %v and %v", viaIn(revisions[0]), viaIn(revisions[1]))
		}
		marked := 0
		for _, e := range list(t, want(t, reader.get(t, "/api/v1/requirements/"+r1+"/activity"), http.StatusOK, "read the activity"), "entries") {
			if throughToken(e, name) {
				marked++
			}
		}
		if marked != 3 {
			t.Errorf("%d activity entries name the token, want the revision, the priority and the link", marked)
		}
		threads := list(t, want(t, reader.get(t, "/api/v1/requirements/"+r1+"/comments"), http.StatusOK, "list the comments"), "threads")
		if !throughToken(threads[0].(map[string]any)["comments"].([]any)[0], name) {
			t.Errorf("the comment reads %v", threads[0])
		}
		byHand := list(t, want(t, reader.get(t, "/api/v1/requirements/"+r2+"/comments"), http.StatusOK, "list the other comments"), "threads")
		if viaIn(byHand[0].(map[string]any)["comments"].([]any)[0]) != nil {
			t.Errorf("a comment written in the browser says it came through something")
		}
		links := list(t, want(t, reader.get(t, "/api/v1/requirements/"+r1+"/links"), http.StatusOK, "list the links"), "links")
		if !throughToken(links[0], name) {
			t.Errorf("the link reads %v", links[0])
		}
		created := 0
		for _, e := range list(t, want(t, reader.get(t, "/api/v1/audit?action=project.created"), http.StatusOK, "read the audit log"), "entries") {
			if throughToken(e, name) && e.(map[string]any)["targetId"] == madeID {
				created++
			}
		}
		if created != 1 {
			t.Errorf("%d audit entries name the token for the project it made", created)
		}
	}
	t.Run("each read names the token", func(t *testing.T) { check(t, owner, "Nightly script") })

	t.Run("a revoked token keeps its name on what it wrote", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/tokens/"+tokenID), http.StatusNoContent, "revoke the token")
		h.settle(t)
		check(t, owner, "Nightly script")
	})

	t.Run("the event the change made names the token", func(t *testing.T) {
		var name string
		if err := h.super.QueryRow(context.Background(), `SELECT payload->'via'->>'name' FROM outbox_event
			WHERE topic = 'requirement.revised' AND payload->>'actorId' = $1 ORDER BY created_at DESC LIMIT 1`, owner.user.String()).Scan(&name); err != nil ||
			name != "Nightly script" {
			t.Errorf("the event names %q: %v", name, err)
		}
	})

	t.Run("an assistant connected with OAuth is named by its client", func(t *testing.T) {
		c, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Desk assistant"})
		access := c.exchange(t, c.allow(t, owner, map[string]any{"allow": true}))["access_token"].(string)
		rpcResult(t, mcpAs(t, a, access, "tools/call", map[string]any{"name": "update_requirement",
			"arguments": map[string]any{"reqKey": r2, "title": "Lights blink", "baseRevision": 1}}))
		h.settle(t)
		v := viaIn(list(t, want(t, owner.get(t, "/api/v1/requirements/"+r2+"/revisions"), http.StatusOK, "list the revisions"), "revisions")[0])
		if v == nil || v["kind"] != "oauth" || v["name"] != "Desk assistant" {
			t.Errorf("a revision through OAuth reads %v", v)
		}
	})
}

// A review asked for and a test run uploaded through a token name it; the same done in the browser name nothing (#321).
func TestAReviewAndATestRunNameTheTokenTheyCameThrough(t *testing.T) {
	s := newReviewScene(t, "VRV")
	s.setRules(t)
	want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Lights dim at night"}), http.StatusCreated, "write R2")
	_, secret := makeToken(t, s.bob, map[string]any{"name": "Review script"})
	script := s.bob.api.withToken(s.bobID, secret)
	s.h.settle(t)

	t.Run("a review", func(t *testing.T) {
		through := obj(t, want(t, script.post(t, s.req+"/reviews", map[string]any{"revision": 1}), http.StatusCreated, "ask through the token"), "review")
		byHand := obj(t, want(t, s.bob.post(t, "/api/v1/requirements/"+s.key+"-R2/reviews", map[string]any{"revision": 1}), http.StatusCreated, "ask in the browser"), "review")
		read := func(id string) map[string]any {
			return obj(t, want(t, s.owner.get(t, "/api/v1/reviews/"+id), http.StatusOK, "read the review"), "review")
		}
		if !throughToken(read(through["id"].(string)), "Review script") || viaIn(read(byHand["id"].(string))) != nil {
			t.Errorf("the reviews read %v and %v", viaIn(read(through["id"].(string))), viaIn(read(byHand["id"].(string))))
		}
	})

	t.Run("a test run", func(t *testing.T) {
		report := junit([2]string{"engage [verifies " + s.key + "-R1]", "passed"})
		through, byHand := ran(t, script, s.key, runForm("main"), report), ran(t, s.bob, s.key, runForm("main"), report)
		if !throughToken(through, "Review script") || viaIn(byHand) != nil {
			t.Errorf("the runs read %v and %v", viaIn(through), viaIn(byHand))
		}
	})
}

// An Armature link made through a token names it; one made in the browser names nothing (#321).
func TestAnArmatureLinkNamesTheTokenItCameThrough(t *testing.T) {
	key := projectKey(t, "VAL")
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "via-armature").user)
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "the owner connects as alice")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage"}), http.StatusCreated, "write R1")
	_, secret := makeToken(t, owner, map[string]any{"name": "Link script"})
	h.settle(t)
	links := "/api/v1/requirements/" + key + "-R1/armature-links"
	want(t, api.withToken(owner.user, secret).post(t, links, map[string]any{"issueKey": "CP-1", "relation": "implements"}), http.StatusCreated, "link through the token")
	want(t, owner.post(t, links, map[string]any{"issueKey": "CP-2", "relation": "relates"}), http.StatusCreated, "link in the browser")
	got := linkStates(t, owner, key+"-R1")
	if !throughToken(got["CP-1 implements"], "Link script") || viaIn(got["CP-2 relates"]) != nil {
		t.Errorf("the links read %v and %v", viaIn(got["CP-1 implements"]), viaIn(got["CP-2 relates"]))
	}
}

// A Stator link made through a token names it; one made in the browser names nothing (#321).
func TestAStatorLinkNamesTheTokenItCameThrough(t *testing.T) {
	key := projectKey(t, "VSL")
	h := newHarness(t)
	h.keepStator(t)
	statorOffersItsOwn(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "via-stator").user)
	tenant := stubTenant(t)
	statorStub(t, http.MethodDelete, tenant, nil)
	t.Cleanup(func() { statorStub(t, http.MethodDelete, tenant, nil) })
	want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	want(t, owner.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "admin")}), http.StatusOK, "the owner connects")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage"}), http.StatusCreated, "write R1")
	_, secret := makeToken(t, owner, map[string]any{"name": "Page script"})
	h.settle(t)
	pages := statorPages(t, tenant)
	timing, braking := pages["Brake timing"], pages["Braking system"]
	links := "/api/v1/requirements/" + key + "-R1/stator-links"
	want(t, api.withToken(owner.user, secret).post(t, links, map[string]any{"pageId": timing, "relation": "specifies"}), http.StatusCreated, "link through the token")
	want(t, owner.post(t, links, map[string]any{"pageId": braking, "relation": "describes"}), http.StatusCreated, "link in the browser")
	got := statorLinkStates(t, owner, key+"-R1")
	if !throughToken(got[timing+" specifies"], "Page script") || viaIn(got[braking+" describes"]) != nil {
		t.Errorf("the links read %v and %v", viaIn(got[timing+" specifies"]), viaIn(got[braking+" describes"]))
	}
}
