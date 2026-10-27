//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// pagingScene is two projects of the owner's whose requirements share a word no other test writes: five in the
// first, two of them owned by the owner, and two in the second.
type pagingScene struct {
	f          *projectFixture
	a, b, word string
	ownerID    uuid.UUID
}

func newPagingScene(t *testing.T) *pagingScene {
	t.Helper()
	f := newProjectFixture(t)
	s := &pagingScene{f: f, a: projectKey(t, "GPA"), b: projectKey(t, "GPB"), word: "lamp" + strings.ToLower(mark(t)), ownerID: f.home.user}
	for _, key := range []string{s.a, s.b} {
		want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Lamps " + key}), http.StatusCreated, "make "+key)
	}
	write := func(key, title string, owned bool) {
		body := map[string]any{"title": title, "statement": statementSaying("The " + s.word + " is lit.")}
		if owned {
			body["ownerId"] = s.ownerID
		}
		want(t, f.owner.post(t, "/api/v1/projects/"+key+"/requirements", body), http.StatusCreated, "write "+title)
	}
	// Titles out of outline order, so ordering by the title shows on every page.
	write(s.a, "Lantern on the porch", true)
	write(s.a, "Brass lantern in the hall", false)
	write(s.a, "Spare lantern in the shed", false)
	write(s.a, "Dim the lantern at night", true)
	write(s.a, "Clean the lantern glass", false)
	write(s.b, "Lantern by the gate", false)
	write(s.b, "Lantern on the boat", false)
	return s
}

// pages reads every window of a list, two at a time, and fails when a window repeats a row or the total moves.
func pages(t *testing.T, c *client, path string, keys func(*testing.T, response) []string) ([]string, int) {
	t.Helper()
	const window = 2
	var all []string
	total := -1
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	for offset := 0; ; offset += window {
		got := want(t, c.get(t, fmt.Sprintf("%s%slimit=%d&offset=%d", path, sep, window, offset)), http.StatusOK, fmt.Sprintf("%s from %d", path, offset))
		n := int(got.Body["total"].(float64))
		if total >= 0 && n != total {
			t.Errorf("%s: the total moved from %d to %d at offset %d", path, total, n, offset)
		}
		total = n
		if got.Body["offset"] != float64(offset) || got.Body["limit"] != float64(window) {
			t.Errorf("%s: answered the window %v from %v", path, got.Body["limit"], got.Body["offset"])
		}
		page := keys(t, got)
		for _, key := range page {
			if slices.Contains(all, key) {
				t.Errorf("%s: %s comes again at offset %d", path, key, offset)
			}
		}
		all = append(all, page...)
		if len(page) < window {
			return all, total
		}
	}
}

// The grid turns its pages in the order it is asked for, never showing a row twice, and keeps its window and its
// project to who may read them (#311).
func TestTheGridTurnsItsPagesInEveryOrder(t *testing.T) {
	s := newPagingScene(t)
	grid := "/api/v1/projects/" + s.a + "/requirements"
	r := func(n int) string { return fmt.Sprintf("%s-R%d", s.a, n) }

	t.Run("in outline order", func(t *testing.T) {
		got, total := pages(t, s.f.plain, grid, rowKeys)
		sameList(t, "the pages", got, r(1), r(2), r(3), r(4), r(5))
		if total != 5 {
			t.Errorf("the total is %d", total)
		}
	})
	t.Run("ordered by the title", func(t *testing.T) {
		got, _ := pages(t, s.f.plain, grid+"?sort=title", rowKeys)
		sameList(t, "the pages", got, r(2), r(5), r(4), r(1), r(3))
	})
	t.Run("ordered by a query's ORDER BY, over the column", func(t *testing.T) {
		got, total := pages(t, s.f.plain, grid+"?sort=title&query="+url.QueryEscape("kind = requirement ORDER BY title DESC"), rowKeys)
		sameList(t, "the pages", got, r(3), r(1), r(4), r(5), r(2))
		if total != 5 {
			t.Errorf("the total is %d", total)
		}
	})
	t.Run("a window too wide or before the start is refused on its field", func(t *testing.T) {
		fieldError(t, s.f.plain.get(t, grid+"?limit=501"), "limit")
		fieldError(t, s.f.plain.get(t, grid+"?offset=-1"), "offset")
		refuse(t, "a window too wide", s.f.plain.get(t, grid+"?limit=501"), http.StatusUnprocessableEntity, "validation_failed")
		want(t, s.f.plain.get(t, grid+"?limit=500"), http.StatusOK, "the widest window")
	})
	t.Run("a guest no grant names finds no grid", func(t *testing.T) {
		refuse(t, "an outsider reads the grid", s.f.outside.get(t, grid), http.StatusNotFound, "not_found")
	})
}

// A search turns its pages without repeating a hit, keeps the glossary's terms to the first, and narrows by owner
// and by the day of the latest change (#311).
func TestASearchTurnsItsPagesAndNarrowsByOwnerAndDay(t *testing.T) {
	s := newPagingScene(t)
	want(t, s.f.owner.post(t, "/api/v1/projects/"+s.a+"/terms", map[string]any{"name": "Lamp" + s.word[len("lamp"):]}), http.StatusCreated, "define the word")
	search := func(query string) response {
		t.Helper()
		return want(t, s.f.owner.get(t, "/api/v1/search?q="+s.word+"&project="+s.a+query), http.StatusOK, "search "+query)
	}

	t.Run("the pages hold every hit once, and the terms only the first", func(t *testing.T) {
		got, total := pages(t, s.f.owner, "/api/v1/search?q="+s.word+"&project="+s.a, hitKeys)
		if total != 5 || len(got) != 5 {
			t.Errorf("the pages hold %v of %d", got, total)
		}
		first, second := search("&limit=2"), search("&limit=2&offset=2")
		names := []string{}
		for _, each := range list(t, first, "terms") {
			names = append(names, strings.ToLower(each.(map[string]any)["name"].(string)))
		}
		if !slices.Contains(names, s.word) {
			t.Errorf("the first page's terms are %v", names)
		}
		if terms := list(t, second, "terms"); len(terms) != 0 {
			t.Errorf("the second page answers terms %v", terms)
		}
	})

	t.Run("a window too wide or before the start is refused on its field", func(t *testing.T) {
		fieldError(t, s.f.owner.get(t, "/api/v1/search?q="+s.word+"&limit=101"), "limit")
		fieldError(t, s.f.owner.get(t, "/api/v1/search?q="+s.word+"&offset=-1"), "offset")
	})

	t.Run("by owner, and by the day of the latest change", func(t *testing.T) {
		sameList(t, "the owner's", hitKeys(t, search("&owner="+s.ownerID.String()+"&sort=updated")), s.a+"-R4", s.a+"-R1")
		if got := search("&owner=" + uuid.NewString()).Body["total"]; got != float64(0) {
			t.Errorf("somebody who owns nothing keeps %v", got)
		}
		// Moved back past the trigger that stamps a change, so one requirement was last changed days ago.
		backdate(t, s.f.h, s.a, 3)
		var today string
		if err := s.f.h.super.QueryRow(context.Background(), `SELECT to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD')`).Scan(&today); err != nil {
			t.Fatal(err)
		}
		sameList(t, "changed today", hitKeys(t, search("&updatedAfter="+today+"&sort=updated")), s.a+"-R5", s.a+"-R4", s.a+"-R2", s.a+"-R1")
		sameList(t, "changed before today", hitKeys(t, search("&updatedBefore="+today)), s.a+"-R3")
	})

	t.Run("an owner, a day or an order that is none is refused on its field", func(t *testing.T) {
		for field, query := range map[string]string{"owner": "owner=nope", "updatedAfter": "updatedAfter=2026-13-01", "sort": "sort=oldest"} {
			fieldError(t, s.f.owner.get(t, "/api/v1/search?q="+s.word+"&"+query), field)
		}
	})
}

// backdate moves a requirement's latest change days into the past, past the trigger that stamps it.
func backdate(t *testing.T, h *harness, key string, number int) {
	t.Helper()
	ctx := context.Background()
	tx, err := h.super.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE requirement SET updated_at = now() - interval '3 days'
		WHERE number = $2 AND project_id = (SELECT id FROM project WHERE key = $1)`, key, number); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
}

// A token limited to one project searches, lists and reads views only there, and counts only what it reaches (#311).
func TestATokenLimitedToOneProjectSearchesOnlyThere(t *testing.T) {
	s := newPagingScene(t)
	_, secret := makeToken(t, s.f.owner, map[string]any{"name": "lamps", "projects": []string{s.a}})
	want(t, s.f.owner.post(t, "/api/v1/projects/"+s.b+"/views", map[string]any{"name": "Gate", "shared": true}), http.StatusCreated, "share a view of the other project")
	s.f.h.settle(t)
	script := s.f.api.withToken(s.ownerID, secret)

	everywhere := want(t, s.f.owner.get(t, "/api/v1/search?q="+s.word), http.StatusOK, "the owner searches in a session")
	if everywhere.Body["total"] != float64(7) {
		t.Fatalf("in a session the word is found %v times", everywhere.Body["total"])
	}
	limited := want(t, script.get(t, "/api/v1/search?q="+s.word+"&limit=100"), http.StatusOK, "the token searches")
	keys := hitKeys(t, limited)
	if limited.Body["total"] != float64(5) || len(keys) != 5 || slices.ContainsFunc(keys, func(k string) bool { return !strings.HasPrefix(k, s.a+"-") }) {
		t.Errorf("the token found %v of %v", keys, limited.Body["total"])
	}
	if other := want(t, script.get(t, "/api/v1/search?q="+s.word+"&project="+s.b), http.StatusOK, "the token searches the other project"); other.Body["total"] != float64(0) {
		t.Errorf("the token found %v in the other project", other.Body["total"])
	}
	for _, each := range list(t, want(t, script.get(t, "/api/v1/search/quick?q=lantern&limit=20"), http.StatusOK, "the token's quick search"), "requirements") {
		if key := each.(map[string]any)["requirement"].(map[string]any)["key"].(string); strings.HasPrefix(key, s.b+"-") {
			t.Errorf("the quick search found %s", key)
		}
	}
	if found := list(t, want(t, script.get(t, "/api/v1/search/quick?q=lantern&project="+s.b), http.StatusOK, "quick, in the other project"), "requirements"); len(found) != 0 {
		t.Errorf("the quick search found %v in the other project", found)
	}
	refuse(t, "the other project's grid", script.get(t, "/api/v1/projects/"+s.b+"/requirements"), http.StatusNotFound, "not_found")
	refuse(t, "the other project's views", script.get(t, "/api/v1/projects/"+s.b+"/views"), http.StatusNotFound, "not_found")
	want(t, script.get(t, "/api/v1/projects/"+s.a+"/views"), http.StatusOK, "its own project's views")
}

// Both tools of the search answer an assistant what the API answers, take lists as arrays, and say where a query
// went wrong (#311).
func TestAnAssistantSearchesAndListsAsTheAPIDoes(t *testing.T) {
	s := newPagingScene(t)
	watts := obj(t, want(t, s.f.owner.post(t, "/api/v1/projects/"+s.a+"/attributes", map[string]any{"name": "Watts", "kind": "number"}),
		http.StatusCreated, "define Watts"), "attribute")["id"].(string)
	for _, each := range []struct {
		number int
		label  string
		watts  int
	}{{1, "outside", 60}, {3, "spare", 40}, {4, "outside", 25}} {
		key := fmt.Sprintf("%s-R%d", s.a, each.number)
		want(t, s.f.owner.patch(t, "/api/v1/requirements/"+key, map[string]any{"attributes": map[string]any{watts: each.watts}, "baseRevision": 1}),
			http.StatusOK, "rate "+key)
		want(t, s.f.owner.post(t, "/api/v1/requirements/"+key+"/labels", map[string]any{"name": each.label}), http.StatusOK, "label "+key)
	}
	_, secret := makeToken(t, s.f.owner, map[string]any{"name": "assistant"})
	s.f.h.settle(t)
	a := connectAssistant(t, s.f.api, secret)
	structured := func(m map[string]any) response { return response{Body: m} }

	t.Run("search_requirements answers the hits of GET /search", func(t *testing.T) {
		args := map[string]any{"q": s.word, "project": []string{s.a}, "owner": []string{s.ownerID.String()}, "limit": 10}
		got := a.call(t, "search_requirements", args)
		over := want(t, s.f.owner.get(t, "/api/v1/search?q="+s.word+"&project="+s.a+"&owner="+s.ownerID.String()+"&limit=10"), http.StatusOK, "search over HTTP")
		if !slices.Equal(hitKeys(t, structured(got)), hitKeys(t, over)) || got["total"] != over.Body["total"] || got["total"] != float64(2) {
			t.Errorf("the tool found %v of %v, the API %v of %v", hitKeys(t, structured(got)), got["total"], hitKeys(t, over), over.Body["total"])
		}
	})

	t.Run("list_requirements takes labels and conditions as arrays, and a query's order", func(t *testing.T) {
		got := a.call(t, "list_requirements", map[string]any{"projectKey": s.a, "label": []string{"outside", "spare"}, "attr": []string{watts + ":gte:30"}})
		over := want(t, s.f.owner.get(t, "/api/v1/projects/"+s.a+"/requirements?label=outside&label=spare&attr="+url.QueryEscape(watts+":gte:30")),
			http.StatusOK, "the grid over HTTP")
		sameList(t, "the tool's rows", rowKeys(t, structured(got)), s.a+"-R1", s.a+"-R3")
		sameList(t, "the API's rows", rowKeys(t, over), s.a+"-R1", s.a+"-R3")
		ordered := a.call(t, "list_requirements", map[string]any{"projectKey": s.a, "query": `"Watts" IS NOT EMPTY ORDER BY "Watts" DESC`, "limit": 2})
		sameList(t, "ordered by the query", rowKeys(t, structured(ordered)), s.a+"-R1", s.a+"-R3")
		if ordered["total"] != float64(3) {
			t.Errorf("the ordered list counts %v", ordered["total"])
		}
	})

	t.Run("a query that cannot run is refused with its sentence and where it went wrong", func(t *testing.T) {
		query := "kind = requirement AND ownr = me"
		over := s.f.owner.get(t, "/api/v1/projects/"+s.a+"/requirements?query="+url.QueryEscape(query))
		refuse(t, "the query over HTTP", over, http.StatusBadRequest, "bad_query")
		e := obj(t, over, "error")
		sentence := a.refused(t, "list_requirements", map[string]any{"projectKey": s.a, "query": query})
		if !strings.HasPrefix(sentence, e["message"].(string)) || !strings.Contains(sentence, fmt.Sprintf("character %v", e["position"])) {
			t.Errorf("the tool refused with %q; the API with %v", sentence, e)
		}
	})
}
