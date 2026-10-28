//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/webhook"
)

// The Armature integration's refusals (#326): what a person who reads but may not edit, an archived project, the
// API's bounds and a token in place of a session are refused, each leaving the traces as they were.

// armatureRows is what Exciter holds of a requirement's traces to Armature issues, read as the superuser, so a
// refusal can be shown to change none of it.
func (h *harness) armatureRows(t *testing.T, requirementID any) string {
	t.Helper()
	var out string
	if err := h.super.QueryRow(context.Background(), `
		SELECT jsonb_build_object(
		    'links', (SELECT coalesce(jsonb_agg(jsonb_build_object('id', l.id, 'issue', l.issue_key, 'relation', l.relation,
		                                                           'reviewed', l.reviewed_rev) ORDER BY l.id), '[]')
		              FROM armature_issue_link l WHERE l.requirement_id = $1),
		    'synced', (SELECT coalesce(jsonb_agg(jsonb_build_object('issue', s.issue_key, 'state', s.state) ORDER BY s.issue_key), '[]')
		               FROM armature_remote_link s WHERE s.requirement_id = $1),
		    'cleared', (SELECT count(*) FROM trace_link_clearance c JOIN armature_issue_link l ON l.id = c.armature_link_id
		                WHERE l.requirement_id = $1),
		    'events', (SELECT count(*) FROM outbox_event WHERE topic = $2 AND payload ->> 'requirementId' = $1::text))::text`,
		requirementID, armature.TopicLinks).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// unchanged fails when the requirement's traces are no longer what before read.
func (h *harness) unchanged(t *testing.T, what string, requirementID any, before string) {
	t.Helper()
	h.settle(t)
	if after := h.armatureRows(t, requirementID); after != before {
		t.Errorf("%s changed the traces:\nbefore %s\nafter  %s", what, before, after)
	}
}

// A person who may read a project and holds an Armature token is refused every trace change with 403, as somebody
// without a token is with 409: the right is Exciter's to judge before Armature is asked.
func TestAViewerWithAnArmatureTokenChangesNoTrace(t *testing.T) {
	key := projectKey(t, "AVW")
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-viewer")
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	owner := api.as(t, home.user)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "the owner connects as alice")
	project := obj(t, want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Viewed"}), http.StatusCreated, "make a project"), "project")
	r1 := obj(t, want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage within 100 ms"}), http.StatusCreated, "write R1"), "requirement")
	link := obj(t, want(t, owner.post(t, "/api/v1/requirements/"+key+"-R1/armature-links", map[string]any{"issueKey": "CP-1", "relation": "implements"}),
		http.StatusCreated, "link CP-1"), "link")
	want(t, owner.patch(t, "/api/v1/requirements/"+key+"-R1", map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}), http.StatusOK, "revise R1")
	h.drained(t)

	viewerID := h.addPerson(t, "guest")
	h.grantAs(t, uuid.MustParse(project["id"].(string)), viewerID, "viewer")
	viewer := api.as(t, viewerID)
	if got := obj(t, want(t, viewer.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "carol")}), http.StatusOK, "the viewer connects"), "account"); got["status"] != "ok" {
		t.Fatalf("the viewer's token is %v", got)
	}
	if states := linkStates(t, viewer, key+"-R1"); len(states) != 1 || states["CP-1 implements"]["suspect"] != true {
		t.Fatalf("the viewer reads the traces as %v", states)
	}
	if n := h.countRows(t, `SELECT count(*) FROM armature_remote_link WHERE requirement_id = $1 AND state = 'synced'`, r1["id"]); n != 1 {
		t.Fatalf("the worker synced %d links, want CP-1's", n)
	}
	before := h.armatureRows(t, r1["id"])

	refuse(t, "a viewer links", viewer.post(t, "/api/v1/requirements/"+key+"-R1/armature-links", map[string]any{"issueKey": "CP-2", "relation": "relates"}),
		http.StatusForbidden, "forbidden")
	refuse(t, "a viewer removes a link", viewer.delete(t, "/api/v1/armature-links/"+link["id"].(string)), http.StatusForbidden, "forbidden")
	refuse(t, "a viewer confirms a link", viewer.post(t, "/api/v1/armature-links/"+link["id"].(string)+"/clear", map[string]any{"revision": 2, "comment": "Still holds."}),
		http.StatusForbidden, "forbidden")
	h.unchanged(t, "the viewer's refused writes", r1["id"], before)
	if states := linkStates(t, owner, key+"-R1"); states["CP-1 implements"]["suspect"] != true {
		t.Errorf("a refused confirmation cleared the link: %v", states)
	}
}

// A trashed requirement's link and an archived project's links are refused with what to do about them, and stay.
func TestAnArchivedProjectAndTheTrashKeepTheirArmatureTraces(t *testing.T) {
	key := projectKey(t, "AAR")
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-archived")
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	owner := api.as(t, home.user)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "the owner connects as alice")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Archived"}), http.StatusCreated, "make a project")
	var ids, links []string
	for i, issue := range []string{"CP-1", "CP-2"} {
		reqKey := key + "-R" + strconv.Itoa(i+1)
		r := obj(t, want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Requirement " + issue}), http.StatusCreated, "write "+reqKey), "requirement")
		l := obj(t, want(t, owner.post(t, "/api/v1/requirements/"+reqKey+"/armature-links", map[string]any{"issueKey": issue, "relation": "implements"}),
			http.StatusCreated, "link "+issue), "link")
		want(t, owner.patch(t, "/api/v1/requirements/"+reqKey, map[string]any{"title": "Revised " + issue, "baseRevision": 1}), http.StatusOK, "revise "+reqKey)
		ids, links = append(ids, r["id"].(string)), append(links, l["id"].(string))
	}
	h.drained(t)

	t.Run("a link of a trashed requirement is not confirmed, and none is made on it", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/requirements/"+key+"-R2"), http.StatusNoContent, "trash R2")
		h.drained(t)
		before := h.armatureRows(t, ids[1])
		refuse(t, "confirming in the trash", owner.post(t, "/api/v1/armature-links/"+links[1]+"/clear", map[string]any{"revision": 2, "comment": "Still holds."}),
			http.StatusConflict, "requirement_trashed")
		refuse(t, "linking in the trash", owner.post(t, "/api/v1/requirements/"+key+"-R2/armature-links", map[string]any{"issueKey": "CP-3", "relation": "relates"}),
			http.StatusConflict, "requirement_trashed")
		h.unchanged(t, "the refused writes in the trash", ids[1], before)
	})

	t.Run("an archived project's links are neither made, removed nor confirmed", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/projects/"+key+"/archive", nil), http.StatusOK, "archive the project")
		h.settle(t)
		before := h.armatureRows(t, ids[0])
		refuse(t, "confirming while archived", owner.post(t, "/api/v1/armature-links/"+links[0]+"/clear", map[string]any{"revision": 2, "comment": "Still holds."}),
			http.StatusConflict, "project_archived")
		refuse(t, "linking while archived", owner.post(t, "/api/v1/requirements/"+key+"-R1/armature-links", map[string]any{"issueKey": "CP-3", "relation": "relates"}),
			http.StatusConflict, "project_archived")
		refuse(t, "removing while archived", owner.delete(t, "/api/v1/armature-links/"+links[0]), http.StatusConflict, "project_archived")
		h.unchanged(t, "the refused writes while archived", ids[0], before)
	})
}

// The lookup and the search refuse what is past their bounds by the field, and take what is just within them.
func TestArmatureLookupsKeepToTheirBounds(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-bounds")
	slug := stubTenant(t)
	owner := api.as(t, home.user)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "the owner connects as alice")

	keys := func(n int) string {
		q := url.Values{}
		for i := 1; i <= n; i++ {
			q.Add("key", "CP-"+strconv.Itoa(i))
		}
		return q.Encode()
	}
	if msg := fieldError(t, owner.get(t, "/api/v1/armature/issues?"+keys(armature.MaxLookupKeys+1)), "key"); !strings.Contains(msg, strconv.Itoa(armature.MaxLookupKeys)) {
		t.Errorf("too many keys are refused with %q", msg)
	}
	// The same key twice counts once, so the bound is on the issues asked, not on the words sent.
	if got := want(t, owner.get(t, "/api/v1/armature/issues?"+keys(armature.MaxLookupKeys)+"&key=cp-1"), http.StatusOK, "as many keys as allowed"); len(list(t, got, "issues")) != armature.MaxLookupKeys {
		t.Errorf("%d keys answered %d issues", armature.MaxLookupKeys, len(list(t, got, "issues")))
	}

	long := strings.Repeat("x", armature.MaxSearchLength)
	if msg := fieldError(t, owner.get(t, "/api/v1/armature/search?q="+long+"x"), "q"); !strings.Contains(msg, strconv.Itoa(armature.MaxSearchLength)) {
		t.Errorf("a search too long is refused with %q", msg)
	}
	if got := want(t, owner.get(t, "/api/v1/armature/search?q="+long), http.StatusOK, "a search as long as allowed"); got.Body["status"] != "ok" {
		t.Errorf("a search as long as allowed answered %s", got.Raw)
	}
	for _, limit := range []string{"abc", "0"} {
		if msg := fieldError(t, owner.get(t, "/api/v1/armature/search?q=pipeline&limit="+limit), "limit"); !strings.Contains(msg, strconv.Itoa(armature.SearchMaxLimit)) {
			t.Errorf("limit=%s is refused with %q", limit, msg)
		}
	}
	if found := list(t, want(t, owner.get(t, "/api/v1/armature/search?q=pipeline&limit="+strconv.Itoa(armature.SearchMaxLimit)), http.StatusOK, "the largest limit"), "issues"); len(found) != 1 {
		t.Errorf("the largest limit found %v", found)
	}
}

// Only a session stores or forgets a person's Armature token: an Exciter token, even one that may write, is refused
// with session_only and the stored token stays.
func TestOnlyASessionStoresAnArmatureToken(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-session")
	slug := stubTenant(t)
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	_, full := makeToken(t, bob, map[string]any{"name": "armature-writer"})
	script := api.withToken(bobID, full)

	refuse(t, "a token stores an Armature token", script.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusForbidden, "session_only")
	if n := h.countRows(t, `SELECT count(*) FROM armature_token WHERE user_id = $1`, bobID); n != 0 {
		t.Errorf("a refused store left %d tokens", n)
	}
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "bob connects in his session")
	var stored uuid.UUID
	if err := h.super.QueryRow(t.Context(), `SELECT id FROM armature_token WHERE user_id = $1`, bobID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	refuse(t, "a token replaces the Armature token", script.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "reader")}), http.StatusForbidden, "session_only")
	refuse(t, "a token forgets the Armature token", script.delete(t, "/api/v1/armature/account/token"), http.StatusForbidden, "session_only")
	if n := h.countRows(t, `SELECT count(*) FROM armature_token WHERE user_id = $1 AND id = $2 AND NOT read_only`, bobID, stored); n != 1 {
		t.Error("a refused token call replaced or forgot bob's Armature token")
	}
	// A check writes only what Armature answers of the stored token, so a token that may write runs it.
	if got := obj(t, want(t, script.post(t, "/api/v1/armature/account/check", nil), http.StatusOK, "a token checks"), "account"); got["status"] != "ok" {
		t.Errorf("a token's check answered %v", got)
	}
}

// The app Exciter is at Armature is set and removed by its fields alone, its secret audited as set or removed and
// never itself; without it a sign-in says what is missing.
func TestTheArmatureAppIsKeptWithoutItsSecret(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-app")
	owner := api.as(t, home.user)
	base := armatureURL(t)
	const appSecret = "exciter-app-secret-326"
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base}), http.StatusOK, "connect Armature")

	tooLong := strings.Repeat("c", armature.MaxClientIDLength+1)
	if msg := fieldError(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base, "clientId": tooLong, "clientSecret": appSecret}), "clientId"); !strings.Contains(msg, strconv.Itoa(armature.MaxClientIDLength)) {
		t.Errorf("a client id too long is refused with %q", msg)
	}
	if n := h.countRows(t, `SELECT count(*) FROM armature_connection WHERE client_id IS NULL AND client_secret IS NULL`); n != 1 {
		t.Error("a refused client id was kept, or its secret")
	}
	longest := strings.Repeat("c", armature.MaxClientIDLength)
	set := obj(t, want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base, "clientId": longest, "clientSecret": appSecret}), http.StatusOK, "set the app"), "connection")
	if set["clientId"] != longest || set["clientSecretSet"] != true {
		t.Errorf("the app reads %v", set)
	}
	removed := obj(t, want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base, "clientSecret": ""}), http.StatusOK, "remove the secret"), "connection")
	if removed["clientId"] != longest || removed["clientSecretSet"] != false {
		t.Errorf("removing the secret left %v", removed)
	}
	for _, change := range []string{"set", "removed"} {
		if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE actor_user_id = $1 AND action = $2 AND data ->> 'clientSecret' = $3`,
			home.user, armature.ActionConnectionSaved, change); n != 1 {
			t.Errorf("the secret is audited as %s %d times, want once", change, n)
		}
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE actor_user_id = $1 AND data::text LIKE '%' || $2 || '%'`, home.user, appSecret); n != 0 {
		t.Errorf("%d audit entries hold the secret", n)
	}

	person := h.addPerson(t, "member")
	h.settle(t)
	h.armature(t).ExchangeAtSignIn(context.Background(), person, "subject")
	h.settle(t)
	signIn, _ := obj(t, want(t, api.as(t, person).get(t, "/api/v1/armature/account"), http.StatusOK, "the account after the sign-in"), "account")["signIn"].(map[string]any)
	if signIn["connected"] != false || !strings.HasPrefix(signIn["message"].(string), "Exciter has no client id and secret for Armature yet") {
		t.Errorf("a sign-in without the app's secret reads %v", signIn)
	}
	if n := h.countRows(t, `SELECT count(*) FROM armature_token WHERE user_id = $1`, person); n != 0 {
		t.Errorf("a sign-in without the app's secret stored %d tokens", n)
	}
}

// Writes with nothing of their own to refuse: an Exciter token that may only read follows no theme, and a link is
// named by an id, a malformed one refused as every path id is.
func TestArmatureWritesRefuseAReadTokenAndAMalformedID(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-scopes")
	slug := stubTenant(t)
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "bob connects")
	_, readOnly := makeToken(t, bob, map[string]any{"name": "theme-reader", "scopes": []string{"read"}})
	script := api.withToken(bobID, readOnly)

	refuse(t, "a read token follows the theme", script.put(t, "/api/v1/armature/theme", nil), http.StatusForbidden, "read_only_token")
	refuse(t, "a read token stops following", script.delete(t, "/api/v1/armature/theme"), http.StatusForbidden, "read_only_token")
	if n := h.countRows(t, `SELECT count(*) FROM user_theme WHERE user_id = $1 AND follow_armature`, bobID); n != 0 {
		t.Error("a refused follow was kept")
	}
	refuse(t, "a link id that is none", bob.delete(t, "/api/v1/armature-links/CP-1"), http.StatusBadRequest, "bad_request")
	refuse(t, "a confirmation of a link id that is none", bob.post(t, "/api/v1/armature-links/CP-1/clear", map[string]any{"revision": 1, "comment": "Fine."}),
		http.StatusBadRequest, "bad_request")
}

// A delivery the secret vouches for whose body is no envelope is taken and ignored, and a move onto a key the
// requirement is linked to already leaves one link and one sync record.
func TestArmatureWebhooksIgnoreWhatIsNoEnvelopeAndMergeAMove(t *testing.T) {
	key := projectKey(t, "AWM")
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-hook-merge")
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	owner := api.as(t, home.user)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "webhookSecret": hookSecret}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "the owner connects as alice")
	orgID := uuid.MustParse(obj(t, want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the connection"), "connection")["armatureOrgId"].(string))
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Merged"}), http.StatusCreated, "make a project")
	r1 := obj(t, want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage"}), http.StatusCreated, "write R1"), "requirement")
	for _, issue := range []string{"CP-1", "CP-2"} {
		want(t, owner.post(t, "/api/v1/requirements/"+key+"-R1/armature-links", map[string]any{"issueKey": issue, "relation": "implements"}), http.StatusCreated, "link "+issue)
	}
	want(t, owner.post(t, "/api/v1/requirements/"+key+"-R1/armature-links", map[string]any{"issueKey": "CP-1", "relation": "verifies"}), http.StatusCreated, "link CP-1 as verifying")
	h.drained(t)
	if n := h.countRows(t, `SELECT count(*) FROM armature_remote_link WHERE requirement_id = $1 AND state = 'synced'`, r1["id"]); n != 2 {
		t.Fatalf("the worker synced %d issues, want CP-1 and CP-2", n)
	}

	t.Run("a signed body that is no envelope answers 204 and changes nothing", func(t *testing.T) {
		before := h.armatureRows(t, r1["id"])
		for _, body := range []string{`[]`, `{"hello":1}`} {
			if status, code := api.deliver(t, hook{secret: hookSecret}, []byte(body)); status != http.StatusNoContent {
				t.Errorf("a signed %s answered %d %s", body, status, code)
			}
		}
		h.unchanged(t, "deliveries that are no envelope", r1["id"], before)
	})

	t.Run("a move onto a key linked already with the same relation leaves one of each", func(t *testing.T) {
		moved := hook{id: uuid.New(), topic: armature.TopicIssueUpdated, version: webhook.Version, orgID: orgID, secret: hookSecret,
			payload: map[string]any{"key": "CP-2", "movedFrom": "CP-1"}, versionHeader: strconv.Itoa(webhook.Version)}
		if status, code := api.deliver(t, moved, moved.body(t)); status != http.StatusNoContent {
			t.Fatalf("the move answered %d %s", status, code)
		}
		h.settle(t)
		states := linkStates(t, owner, key+"-R1")
		if len(states) != 2 || states["CP-2 implements"] == nil || states["CP-2 verifies"] == nil {
			t.Errorf("after the move the links are %v, want CP-2 implementing and verifying", states)
		}
		if n := h.countRows(t, `SELECT count(*) FROM armature_issue_link WHERE requirement_id = $1 AND issue_key = 'CP-2' AND relation = 'implements'`, r1["id"]); n != 1 {
			t.Errorf("%d implements links to CP-2, want one", n)
		}
		var synced []string
		rows, err := h.super.Query(t.Context(), `SELECT issue_key FROM armature_remote_link WHERE requirement_id = $1 ORDER BY issue_key`, r1["id"])
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var k string
			_ = rows.Scan(&k)
			synced = append(synced, k)
		}
		rows.Close()
		if strings.Join(synced, " ") != "CP-2" {
			t.Errorf("after the move the sync records are %v, want CP-2's alone", synced)
		}
	})
}

// The development panel is asked as the reader, of the issues they may see through links that hold: a reader
// without a token, or whose Armature hides the issue, and a broken or detached link get no commit link.
func TestTheDevelopmentPanelIsReadAsTheReaderMaySeeIt(t *testing.T) {
	key := projectKey(t, "ADV")
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-development")
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	owner := api.as(t, home.user)
	bob := api.as(t, h.addPerson(t, "member"))
	carol := api.as(t, h.addPerson(t, "member"))
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	// The stub's admin alone sees SEC-1; bob sees CP but not SEC, and carol holds no token.
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "admin")}), http.StatusOK, "the owner connects as the admin")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "bob connects")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Developed"}), http.StatusCreated, "make a project")
	r1 := obj(t, want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage"}), http.StatusCreated, "write R1"), "requirement")
	hidden := strings.Repeat("a", len(aCommit))
	open := strings.Repeat("c", len(aCommit))
	for issue, sha := range map[string]string{"SEC-1": hidden, "CP-3": open} {
		want(t, owner.post(t, "/api/v1/requirements/"+key+"-R1/armature-links", map[string]any{"issueKey": issue, "relation": "implements"}), http.StatusCreated, "link "+issue)
		stubControl(t, http.MethodPost, slug+"/issues/"+issue+"/commits", map[string]any{"repository": "acme/checkout", "sha": sha + "0000",
			"url": "https://git.example/acme/checkout/commit/" + sha})
	}
	resultLink := func(c *client, who string) map[string]any {
		t.Helper()
		results := list(t, want(t, c.get(t, "/api/v1/requirements/"+key+"-R1/test-results"), http.StatusOK, who+" reads the results"), "results")
		if len(results) != 1 {
			t.Fatalf("%s reads %d results", who, len(results))
		}
		got, _ := results[0].(map[string]any)["armature"].(map[string]any)
		return got
	}
	verifies := junit([2]string{"engage [verifies " + key + "-R1]", "passed"})
	fields := runForm("main")

	fields["commit"] = hidden
	ran(t, owner, key, fields, verifies)
	if got := resultLink(owner, "the admin"); got["issueKey"] != "SEC-1" {
		t.Fatalf("the admin's result links to %v, want SEC-1", got)
	}
	for who, c := range map[string]*client{"bob, whose Armature hides SEC-1": bob, "carol, without a token": carol} {
		if got := resultLink(c, who); got != nil {
			t.Errorf("%s reads the commit link %v", who, got)
		}
	}

	fields["commit"] = open
	ran(t, owner, key, fields, verifies)
	if got := resultLink(bob, "bob"); got["issueKey"] != "CP-3" {
		t.Fatalf("bob's result links to %v, want CP-3", got)
	}
	for _, state := range []string{"broken_at", "detached_at"} {
		h.cleanupExec(t, h.super, `UPDATE armature_issue_link SET `+state+` = now() WHERE requirement_id = $1 AND issue_key = 'CP-3'`, r1["id"])
		h.settle(t)
		if got := resultLink(bob, "bob"); got != nil {
			t.Errorf("with the link to CP-3 %s set, the result links to %v", state, got)
		}
		h.cleanupExec(t, h.super, `UPDATE armature_issue_link SET `+state+` = NULL WHERE requirement_id = $1 AND issue_key = 'CP-3'`, r1["id"])
	}
}

// An assistant reaches Armature only as far as its person's Armature token and its own scope let it: without a token
// it is told where to connect one, and through a read token it makes nothing.
func TestAnAssistantIsToldToConnectArmatureAndAReadTokenLinksNothing(t *testing.T) {
	key := projectKey(t, "AMC")
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-mcp")
	owner := api.as(t, home.user)
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	r1 := obj(t, want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage"}), http.StatusCreated, "write R1"), "requirement")
	link := map[string]any{"reqKey": key + "-R1", "issueKey": "CP-1", "relation": "implements"}
	create := map[string]any{"projectKey": key, "requirementKeys": []string{key + "-R1"}, "armatureProject": "CP", "relation": "implements"}
	_, full := makeToken(t, owner, map[string]any{"name": "assistant"})
	_, reading := makeToken(t, owner, map[string]any{"name": "reading assistant", "scopes": []string{"read"}})
	h.settle(t)

	a := connectAssistant(t, api, full)
	for tool, args := range map[string]map[string]any{"link_armature_issue": link, "create_armature_issue": create} {
		if says := a.refused(t, tool, args); !strings.Contains(says, "Your account, Armature account") {
			t.Errorf("%s without an Armature token says %q", tool, says)
		}
	}
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "the owner connects as alice")
	h.settle(t)
	reader := connectAssistant(t, api, reading)
	for tool, args := range map[string]map[string]any{"link_armature_issue": link, "create_armature_issue": create} {
		if says := reader.refused(t, tool, args); !strings.Contains(says, "can only read") {
			t.Errorf("%s through a read token says %q", tool, says)
		}
	}
	// A moment for anything a refused call might have queued, which the counts would then miss.
	time.Sleep(workerIdle)
	h.drained(t)
	if n := h.countRows(t, `SELECT count(*) FROM armature_issue_link WHERE requirement_id = $1`, r1["id"]); n != 0 {
		t.Errorf("refused tools made %d links", n)
	}
	if found := list(t, want(t, owner.get(t, "/api/v1/armature/search?q=Brakes%20engage"), http.StatusOK, "search for a filed issue"), "issues"); len(found) != 0 {
		t.Errorf("refused tools filed %v in Armature", found)
	}
}
