//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"
)

// docOf is a comment body as the editor writes it, one paragraph of words.
func docOf(words string) map[string]any { return commentBody(words)["body"].(map[string]any) }

// An author's assistant asks for reviews and keeps trace links in order; the approver's reads what waits for them,
// discusses it, and finds no way to decide it, which stays theirs (#185).
func TestAnAssistantAsksForReviewsAndKeepsTraceInOrder(t *testing.T) {
	w := newCommentWorld(t, "MRV")
	key := w.key
	api := w.owner.api
	want(t, w.owner.put(t, "/api/v1/projects/"+key+"/approval-rules", map[string]any{"required": 1,
		"approvers": []map[string]any{{"subjectType": "user", "userId": w.daveID}}}), http.StatusOK, "dave approves")
	_, bobSecret := makeToken(t, w.bob, map[string]any{"name": "author's assistant"})
	_, daveSecret := makeToken(t, w.dave, map[string]any{"name": "approver's assistant"})
	w.h.settle(t)
	author := connectAssistant(t, api, bobSecret)

	var keys []string
	for _, title := range []string{"Brakes release within 200 ms", "Brakes report wear", "Brakes are tested yearly"} {
		keys = append(keys, author.call(t, "create_requirement", map[string]any{"projectKey": key, "title": title})["requirement"].(map[string]any)["key"].(string))
	}
	results := list(t, response{Body: author.call(t, "request_reviews", map[string]any{"projectKey": key, "comment": "Ready.",
		"items": []any{map[string]any{"key": keys[0], "revision": 1}, map[string]any{"key": keys[1], "revision": 1}}})}, "results")
	for _, each := range results {
		if outcome := each.(map[string]any)["outcome"]; outcome != "requested" {
			t.Errorf("request_reviews answered %v", results)
		}
	}
	asked := author.call(t, "request_review", map[string]any{"reqKey": keys[2], "revision": 1})["review"].(map[string]any)
	if withdrawn := author.call(t, "withdraw_review", map[string]any{"reviewID": asked["id"]})["review"].(map[string]any); withdrawn["state"] != "withdrawn" {
		t.Errorf("withdraw_review answered %v", withdrawn)
	}
	reviews := list(t, response{Body: author.call(t, "list_reviews", map[string]any{"reqKey": keys[0]})}, "reviews")
	if len(reviews) != 1 {
		t.Fatalf("list_reviews answered %v", reviews)
	}
	reviewID := reviews[0].(map[string]any)["id"]
	if review := author.call(t, "get_review", map[string]any{"reviewID": reviewID})["review"].(map[string]any); review["state"] != "pending" {
		t.Errorf("get_review answered %v", review)
	}
	if approval := author.call(t, "get_approval", map[string]any{"reqKey": keys[0]})["approval"].(map[string]any); approval["pendingReviewId"] != reviewID {
		t.Errorf("get_approval shows no review open: %v", approval)
	}

	w.h.settle(t)
	approver := connectAssistant(t, api, daveSecret)
	waiting := map[string]bool{}
	for _, each := range list(t, response{Body: approver.call(t, "list_waiting_reviews", map[string]any{})}, "reviews") {
		waiting[each.(map[string]any)["requirement"].(map[string]any)["key"].(string)] = true
	}
	if !waiting[keys[0]] || !waiting[keys[1]] || waiting[keys[2]] {
		t.Errorf("dave's assistant finds waiting %v", waiting)
	}
	for name, tool := range approver.tools {
		if strings.Contains(name, "decide") || strings.Contains(name, "approve") || strings.Contains(name, "reject") || strings.HasSuffix(name, "_decision") {
			t.Errorf("the approver's assistant is offered %s: %s", name, tool.Description)
		}
	}

	thread := approver.call(t, "comment_on_requirement", map[string]any{"reqKey": keys[0], "body": docOf("Within 200 ms of what?")})["thread"].(map[string]any)
	threadID := thread["id"].(string)
	author.call(t, "reply_to_comment", map[string]any{"commentID": threadID, "body": docOf("Of the pedal being let go.")})
	edited := approver.call(t, "update_comment", map[string]any{"commentID": threadID, "body": docOf("Within 200 ms of what event?")})["comment"].(map[string]any)
	if edited["editedAt"] == nil {
		t.Errorf("update_comment answered %v", edited)
	}
	author.call(t, "resolve_comment_thread", map[string]any{"threadID": threadID})
	if read := author.call(t, "get_comment", map[string]any{"commentID": threadID})["thread"].(map[string]any); read["resolved"] != true || len(read["comments"].([]any)) != 2 {
		t.Errorf("get_comment answered %v", read)
	}

	author.call(t, "link_requirements", map[string]any{"reqKey": keys[1], "typeKey": "refines", "otherKey": keys[0]})
	author.call(t, "update_requirement", map[string]any{"reqKey": keys[0], "title": "Brakes release within 200 ms of the pedal", "baseRevision": 1})
	suspects := list(t, response{Body: author.call(t, "list_suspect_links", map[string]any{"projectKey": key})}, "links")
	if len(suspects) != 1 {
		t.Fatalf("list_suspect_links answered %v", suspects)
	}
	suspect := suspects[0].(map[string]any)
	cleared := author.call(t, "clear_suspect_link", map[string]any{"linkID": suspect["id"], "sourceRevision": suspect["source"].(map[string]any)["revision"],
		"targetRevision": suspect["target"].(map[string]any)["revision"], "comment": "Wear is reported however fast the brakes release."})
	if moved, ok := cleared["link"].(map[string]any)["suspect"].([]any); !ok || len(moved) != 0 {
		t.Errorf("clear_suspect_link answered a link whose ends moved on: %v", cleared)
	}
	if left := list(t, response{Body: author.call(t, "list_suspect_links", map[string]any{"projectKey": key})}, "links"); len(left) != 0 {
		t.Errorf("a cleared link is still suspect: %v", left)
	}

	author.call(t, "get_coverage", map[string]any{"projectKey": key})
	author.call(t, "get_matrix", map[string]any{"projectKey": key})
	author.call(t, "list_trace_chains", map[string]any{"projectKey": key})
}

// An assistant finds Armature issues, files one from a requirement and confirms the trace after a change, as its
// person's own Armature token lets it (#185).
func TestAnAssistantReachesArmatureAsItsPerson(t *testing.T) {
	key := projectKey(t, "MAR")
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "mcp-armature").user)
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "the owner connects as bob")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	_, secret := makeToken(t, owner, map[string]any{"name": "assistant"})
	h.settle(t)
	a := connectAssistant(t, api, secret)
	for _, partner := range []string{"search_armature_issues", "list_armature_projects", "create_armature_issue", "link_armature_issue"} {
		if hints := a.tools[partner].Annotations; hints.OpenWorldHint == nil || !*hints.OpenWorldHint {
			t.Errorf("%s does not say it reaches Armature", partner)
		}
	}

	if found := list(t, response{Body: a.call(t, "search_armature_issues", map[string]any{"q": "sign-in"})}, "issues"); len(found) != 1 ||
		found[0].(map[string]any)["key"] != "CP-2" {
		t.Errorf("search_armature_issues found %v, want CP-2", found)
	}
	projects := list(t, response{Body: a.call(t, "list_armature_projects", map[string]any{})}, "projects")
	if len(projects) == 0 || projects[0].(map[string]any)["canCreate"] != true {
		t.Fatalf("list_armature_projects answered %v", projects)
	}
	reqKey := a.call(t, "create_requirement", map[string]any{"projectKey": key, "title": "Brakes engage within 100 ms"})["requirement"].(map[string]any)["key"].(string)
	made := a.call(t, "create_armature_issue", map[string]any{"projectKey": key, "requirementKeys": []string{reqKey},
		"armatureProject": projects[0].(map[string]any)["key"], "relation": "implements"})
	if created := made["created"].([]any); len(created) != 1 || made["failed"] != nil {
		t.Fatalf("create_armature_issue answered %v", made)
	}
	a.call(t, "update_requirement", map[string]any{"reqKey": reqKey, "title": "Brakes engage within 80 ms", "baseRevision": 1})
	links := list(t, response{Body: a.call(t, "list_armature_links", map[string]any{"reqKey": reqKey})}, "links")
	if len(links) != 1 || links[0].(map[string]any)["suspect"] != true {
		t.Fatalf("the trace is not suspect after the requirement changed: %v", links)
	}
	cleared := a.call(t, "clear_armature_link", map[string]any{"linkID": links[0].(map[string]any)["id"], "revision": 2, "comment": "Still what the issue builds."})
	if cleared["link"].(map[string]any)["suspect"] == true {
		t.Errorf("clear_armature_link answered %v", cleared)
	}
}

// An assistant finds a Stator page and links a requirement to it, as its person's own Stator token lets it (#185).
func TestAnAssistantReachesStatorAsItsPerson(t *testing.T) {
	key := projectKey(t, "MST")
	h := newHarness(t)
	h.keepStator(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "mcp-stator").user)
	tenant := stubTenant(t)
	statorStub(t, http.MethodDelete, tenant, nil)
	statorOffers(t)
	want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	want(t, owner.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "bob")}), http.StatusOK, "the owner connects as bob")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	_, secret := makeToken(t, owner, map[string]any{"name": "assistant"})
	h.settle(t)
	a := connectAssistant(t, api, secret)

	pages := list(t, response{Body: a.call(t, "search_stator_pages", map[string]any{"q": "brak"})}, "pages")
	if len(pages) == 0 {
		t.Fatal("search_stator_pages finds nothing of brak")
	}
	reqKey := a.call(t, "create_requirement", map[string]any{"projectKey": key, "title": "Brakes engage within 100 ms"})["requirement"].(map[string]any)["key"].(string)
	link := a.call(t, "link_stator_page", map[string]any{"reqKey": reqKey, "pageId": pages[0].(map[string]any)["id"], "relation": "specifies"})["link"].(map[string]any)
	listed := list(t, response{Body: a.call(t, "list_stator_links", map[string]any{"reqKey": reqKey})}, "links")
	if len(listed) != 1 || listed[0].(map[string]any)["id"] != link["id"] {
		t.Errorf("list_stator_links answered %v", listed)
	}
	if sentence := a.refused(t, "clear_stator_link", map[string]any{"linkID": link["id"], "pageVersion": 1, "comment": "Nothing changed."}); sentence == "" {
		t.Error("clearing a link that is not suspect was refused without a sentence")
	}
}
