//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// sameAnswer fails unless what a tool answered is what the route answered, as JSON reads both.
func sameAnswer(t *testing.T, what string, tool map[string]any, route response) {
	t.Helper()
	var fromRoute map[string]any
	if err := json.Unmarshal(route.Raw, &fromRoute); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if !reflect.DeepEqual(tool, fromRoute) {
		t.Errorf("%s: the tool answered %v, the route %v", what, tool, fromRoute)
	}
}

// The history tools answer what the history routes answer, a revision with its words as Markdown too, and nothing
// of a requirement the token's person may not read (#309).
func TestAnAssistantReadsARequirementsHistory(t *testing.T) {
	key := projectKey(t, "MHI")
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	reqKey := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{
		"title": "Brakes engage within 100 ms", "statement": statementSaying("The brakes engage."), "comment": "First.",
	}), http.StatusCreated, "write R1"), "requirement")["key"].(string)
	r1 := "/api/v1/requirements/" + reqKey
	want(t, f.owner.patch(t, r1, map[string]any{"statement": statementSaying("The brakes engage at once."), "baseRevision": 1, "comment": "Sooner."}),
		http.StatusOK, "write revision 2")
	_, secret := makeToken(t, f.owner, map[string]any{"name": "assistant"})
	f.h.settle(t)
	a := connectAssistant(t, f.api, secret)

	t.Run("list_revisions answers the revisions the route lists", func(t *testing.T) {
		got := a.call(t, "list_revisions", map[string]any{"reqKey": reqKey})
		sameAnswer(t, "list_revisions", got, want(t, f.owner.get(t, r1+"/revisions"), http.StatusOK, "list the revisions"))
		if listed := got["revisions"].([]any); len(listed) != 2 || listed[0].(map[string]any)["comment"] != "Sooner." {
			t.Errorf("list_revisions answered %v", listed)
		}
	})

	t.Run("get_revision answers the revision with its statement as Markdown", func(t *testing.T) {
		got := a.call(t, "get_revision", map[string]any{"reqKey": reqKey, "revisionNumber": 1})
		sameAnswer(t, "get_revision", got, want(t, f.owner.get(t, r1+"/revisions/1?format=markdown"), http.StatusOK, "read revision 1"))
		if md, _ := got["revision"].(map[string]any)["statementMarkdown"].(string); strings.TrimSpace(md) != "The brakes engage." {
			t.Errorf("revision 1 reads as Markdown %q", md)
		}
	})

	t.Run("diff_revisions answers the comparison the route answers", func(t *testing.T) {
		got := a.call(t, "diff_revisions", map[string]any{"reqKey": reqKey, "from": 1, "to": 2})
		sameAnswer(t, "diff_revisions", got, want(t, f.owner.get(t, r1+"/diff?from=1&to=2"), http.StatusOK, "compare 1 with 2"))
		if !strings.Contains(string(want(t, f.owner.get(t, r1+"/diff?from=1&to=2"), http.StatusOK, "compare again").Raw), "at once") {
			t.Error("the comparison does not show the words added")
		}
	})

	t.Run("a guest's token finds no such requirement", func(t *testing.T) {
		_, guestSecret := makeToken(t, f.guest, map[string]any{"name": "assistant"})
		f.h.settle(t)
		guest := connectAssistant(t, f.api, guestSecret)
		for name, args := range map[string]map[string]any{
			"list_revisions": {"reqKey": reqKey},
			"get_revision":   {"reqKey": reqKey, "revisionNumber": 1},
			"diff_revisions": {"reqKey": reqKey},
		} {
			if msg := guest.refused(t, name, args); !strings.Contains(msg, "That requirement was not found") {
				t.Errorf("%s for a guest says %q", name, msg)
			}
		}
	})
}
