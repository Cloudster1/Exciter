//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// Coverage and the query language against every way they are narrowed (#314): the rules refused where they may not
// change, the report's window, the cache kept per reader, link types and links the reader may not see.

// hiddenScene is a comment world whose R1 is verified by a requirement of a second project only the owner reads.
type hiddenScene struct {
	*commentWorld
	hiddenKey, hiddenReq string
}

const hiddenTitle = "Secret brake bench run"

func newHiddenScene(t *testing.T, prefix string) *hiddenScene {
	t.Helper()
	s := &hiddenScene{commentWorld: newCommentWorld(t, prefix), hiddenKey: projectKey(t, prefix+"H")}
	hidden := obj(t, want(t, s.owner.post(t, "/api/v1/projects", map[string]any{"key": s.hiddenKey, "name": "Hidden"}), http.StatusCreated,
		"make a project only the owner reads"), "project")
	if _, err := s.h.super.Exec(t.Context(), `DELETE FROM project_grant WHERE project_id = $1 AND subject_type = 'everyone'`, hidden["id"]); err != nil {
		t.Fatal(err)
	}
	s.hiddenReq = obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.hiddenKey+"/requirements", map[string]any{"title": hiddenTitle}),
		http.StatusCreated, "write the hidden requirement"), "requirement")["key"].(string)
	want(t, s.owner.post(t, "/api/v1/requirements/"+s.hiddenReq+"/links", map[string]any{"typeKey": "verifies", "otherKey": s.reqKey}),
		http.StatusCreated, "the hidden requirement verifies R1")
	s.h.settle(t)
	return s
}

// cachedCoverage serves coverage through the Valkey cache the stack's worker clears, as the stack's api reads it,
// and answers the cache's store.
func cachedCoverage(t *testing.T, w *commentWorld) (*apiServer, *redis.Client) {
	t.Helper()
	opts, err := redis.ParseURL(w.h.cfg.Valkey.URL)
	if err != nil {
		t.Fatal(err)
	}
	store := redis.NewClient(opts)
	t.Cleanup(func() { _ = store.Close() })
	return newAPIServerWith(t, w.h, func(_ *apiServer, server *httpapi.Server) {
		server.Trace = trace.NewService(w.h.cluster).WithCoverageCache(trace.NewCoverageCache(store, nil))
	}), store
}

// verifiedLevel reads how far c finds R1 verified.
func verifiedLevel(t *testing.T, w *commentWorld, c *client) string {
	t.Helper()
	_, rows, _ := (&coverageScene{commentWorld: w}).coverage(t, c, "")
	return strings.Fields(rows[w.reqKey] + " ? ? ?")[2]
}

// coverageRules reads a project's rules straight from the table, so a refusal is seen to leave them as they were.
func coverageRules(t *testing.T, w *commentWorld) string {
	t.Helper()
	var got string
	if err := w.h.super.QueryRow(t.Context(), `
		SELECT COALESCE(string_agg(id::text || ':' || COALESCE(type_id::text, 'every') || ':' || decomposed::text || implemented::text || verified::text,
		       ',' ORDER BY id), '')
		FROM coverage_rule WHERE project_id = $1`, w.projectID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

// Each reader's coverage is cached apart: the owner, who sees the requirement that verifies R1, and carol, who does
// not, read it one after the other within the cache's minute, in either order, and neither is answered the other's.
func TestCoverageIsCachedForEachReaderApart(t *testing.T) {
	s := newHiddenScene(t, "CVC")
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/coverage-rules", map[string]any{"verified": true}), http.StatusOK, "ask every type to be verified")
	cached, store := cachedCoverage(t, s.commentWorld)
	type reader struct {
		name     string
		c        *client
		verified string
	}
	owner, carol := reader{"the owner", cached.as(t, s.ownerID), "full"}, reader{"carol", cached.as(t, s.carolID), "none"}
	s.h.drained(t)
	key := trace.CoverageKey(s.projectID)
	for _, order := range [][2]reader{{owner, carol}, {carol, owner}} {
		t.Run(order[0].name+" first", func(t *testing.T) {
			if err := store.Del(context.Background(), key).Err(); err != nil {
				t.Fatal(err)
			}
			// Each read twice: the second is answered from the cache, filled by both readers by then.
			for range 2 {
				for _, r := range order {
					if got := verifiedLevel(t, s.commentWorld, r.c); got != r.verified {
						t.Errorf("%s reads R1 verified %q, want %q", r.name, got, r.verified)
					}
				}
			}
			if n, err := store.HLen(context.Background(), key).Result(); err != nil || n != 2 {
				t.Errorf("the cache holds %d entries for the project, want one per reader: %v", n, err)
			}
		})
	}
}

// What a link type counts decides the coverage, read again once the worker has heard of the change. A type turned
// off for the project, or retired, keeps its links and they still count, as the matrix still shows them.
func TestCoverageFollowsWhatALinkTypeCounts(t *testing.T) {
	w := newCommentWorld(t, "CVL")
	made := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/link-types", map[string]any{
		"key": "checks", "name": "Checks", "forward": "checks", "backward": "is checked by", "counts": "nothing",
	}), http.StatusCreated, "make a project's type that counts for nothing"), "linkType")
	id := made["id"].(string)
	want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/requirements", map[string]any{"title": "Brake bench run"}), http.StatusCreated, "write R2")
	want(t, w.bob.post(t, "/api/v1/requirements/"+w.key+"-R2/links", map[string]any{"typeKey": "checks", "otherKey": w.reqKey}),
		http.StatusCreated, "R2 checks R1")
	want(t, w.owner.put(t, "/api/v1/projects/"+w.key+"/coverage-rules", map[string]any{"verified": true}), http.StatusOK, "ask every type to be verified")
	cached, _ := cachedCoverage(t, w)
	carol := cached.as(t, w.carolID)
	w.h.drained(t)
	if got := verifiedLevel(t, w, carol); got != "none" {
		t.Fatalf("R1 checked by a type that counts for nothing reads verified %q, want none", got)
	}

	for _, step := range []struct {
		what     string
		change   func() response
		verified string
	}{
		{"checks made to count for verification", func() response {
			return w.owner.patch(t, "/api/v1/link-types/"+id, map[string]any{"counts": "verification"})
		}, "full"},
		{"checks turned off for the project", func() response {
			return w.owner.put(t, "/api/v1/projects/"+w.key+"/link-types/"+id, map[string]any{"enabled": false})
		}, "full"},
		{"checks retired", func() response {
			return w.owner.patch(t, "/api/v1/link-types/"+id, map[string]any{"retired": true})
		}, "full"},
	} {
		want(t, step.change(), http.StatusOK, step.what)
		w.h.drained(t)
		if got := verifiedLevel(t, w, carol); got != step.verified {
			t.Errorf("once %s, R1 reads verified %q, want %s", step.what, got, step.verified)
		}
	}
}

// A rule changes only by an administrator of an open project; every refusal leaves the rules as they were.
func TestCoverageRulesAreRefusedWhereTheyMayNotChange(t *testing.T) {
	w := newCommentWorld(t, "CVR")
	rules := "/api/v1/projects/" + w.key + "/coverage-rules"
	types := "/api/v1/projects/" + w.key + "/types"
	want(t, w.owner.put(t, rules, map[string]any{"verified": true}), http.StatusOK, "the rule for every type")
	legacy := obj(t, want(t, w.owner.post(t, types, map[string]any{"name": "Legacy", "icon": "test", "colour": "#2255aa"}), http.StatusCreated,
		"make the type Legacy"), "type")["id"].(string)
	want(t, w.owner.patch(t, types+"/"+legacy, map[string]any{"retired": true}), http.StatusOK, "retire Legacy")

	t.Run("a reader is forbidden", func(t *testing.T) {
		before := coverageRules(t, w)
		refuse(t, "carol, a reader", w.carol.put(t, rules, map[string]any{"decomposed": true}), http.StatusForbidden, "forbidden")
		if after := coverageRules(t, w); after != before {
			t.Errorf("the rules read %s after the refusal, were %s", after, before)
		}
	})
	t.Run("a retired type still takes a rule, as its requirements are still counted", func(t *testing.T) {
		rule := obj(t, want(t, w.owner.put(t, rules, map[string]any{"typeId": legacy, "decomposed": true}), http.StatusOK, "the rule for Legacy"), "rule")
		if rule["typeName"] != "Legacy" || rule["decomposed"] != true {
			t.Errorf("the rule for a retired type reads %v", rule)
		}
	})
	var legacyRule string
	for _, each := range list(t, want(t, w.owner.get(t, rules), http.StatusOK, "the rules"), "rules") {
		if r := each.(map[string]any); r["typeId"] == legacy {
			legacyRule = r["id"].(string)
		}
	}
	t.Run("an id that is none, or that nobody has, removes nothing", func(t *testing.T) {
		before := coverageRules(t, w)
		// The removal takes no body, so its id is all there is to refuse: a malformed one is the address's fault.
		refuse(t, "a malformed id", w.owner.delete(t, "/api/v1/coverage-rules/not-a-rule"), http.StatusBadRequest, "bad_request")
		refuse(t, "an id nobody has", w.owner.delete(t, "/api/v1/coverage-rules/"+w.reqID), http.StatusNotFound, "not_found")
		refuse(t, "carol, a reader", w.carol.delete(t, "/api/v1/coverage-rules/"+legacyRule), http.StatusForbidden, "forbidden")
		if after := coverageRules(t, w); after != before {
			t.Errorf("the rules read %s after the refusals, were %s", after, before)
		}
	})
	t.Run("an archived project's rules are neither set nor removed", func(t *testing.T) {
		want(t, w.owner.put(t, "/api/v1/projects/"+w.key+"/archive", nil), http.StatusOK, "archive the project")
		before := coverageRules(t, w)
		refuse(t, "a rule set", w.owner.put(t, rules, map[string]any{"implemented": true}), http.StatusConflict, "project_archived")
		refuse(t, "a rule removed", w.owner.delete(t, "/api/v1/coverage-rules/"+legacyRule), http.StatusConflict, "project_archived")
		if after := coverageRules(t, w); after != before {
			t.Errorf("the rules read %s after the refusals, were %s", after, before)
		}
	})
}

// The report's window is checked like every list's, and one past its end is empty with the right total.
func TestTheCoverageReportPagesWithinItsBounds(t *testing.T) {
	w := newCommentWorld(t, "CVP")
	for _, title := range []string{"Brake pedal reads within 10 ms", "Brake lights follow the pedal"} {
		want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title)
	}
	report := "/api/v1/projects/" + w.key + "/coverage"
	for field, query := range map[string]string{
		"limit":  "?limit=0",
		"offset": "?offset=-1",
	} {
		fieldError(t, w.carol.get(t, report+query), field)
	}
	fieldError(t, w.carol.get(t, report+"?limit="+strconv.Itoa(trace.CoverageMaxLimit+1)), "limit")
	past := want(t, w.carol.get(t, report+"?offset=10"), http.StatusOK, "a window past the end")
	if rows := list(t, past, "rows"); len(rows) != 0 || past.Body["total"] != float64(3) || past.Body["offset"] != float64(10) {
		t.Errorf("past the end the report reads %d rows of %v at %v", len(rows), past.Body["total"], past.Body["offset"])
	}
	last := want(t, w.carol.get(t, report+"?limit=2&offset=2"), http.StatusOK, "the last window")
	if rows := list(t, last, "rows"); len(rows) != 1 || last.Body["total"] != float64(3) {
		t.Errorf("the last window reads %d rows of %v", len(rows), last.Body["total"])
	}
}

// queryKeys reads the keys c's grid keeps for a query, sorted.
func queryKeys(t *testing.T, c *client, projectKey, query string) []string {
	t.Helper()
	keys := rowKeys(t, want(t, c.get(t, "/api/v1/projects/"+projectKey+"/requirements?query="+url.QueryEscape(query)), http.StatusOK, "the grid by "+query))
	sort.Strings(keys)
	return keys
}

// A query never follows a link to a requirement its reader may not see, and a key of one reads as a key nobody has.
func TestAQueryNeverFollowsALinkToWhatItsReaderMayNotSee(t *testing.T) {
	s := newHiddenScene(t, "QHE")
	sameList(t, "the owner's verified by anything", queryKeys(t, s.owner, s.key, `verifiedBy IS NOT EMPTY`), s.reqKey)
	sameList(t, "the owner's verified by the hidden requirement", queryKeys(t, s.owner, s.key, `verifiedBy = `+s.hiddenReq), s.reqKey)
	sameList(t, "carol's verified by anything", queryKeys(t, s.carol, s.key, `verifiedBy IS NOT EMPTY`))
	sameList(t, "carol's verified by nothing", queryKeys(t, s.carol, s.key, `verifiedBy IS EMPTY`), s.reqKey)

	hidden := s.carol.get(t, "/api/v1/projects/"+s.key+"/requirements?query="+url.QueryEscape(`verifiedBy = `+s.hiddenReq))
	nobody := s.carol.get(t, "/api/v1/projects/"+s.key+"/requirements?query="+url.QueryEscape(`verifiedBy = `+s.hiddenKey+`-R999`))
	if hidden.Status != http.StatusOK || nobody.Status != http.StatusOK {
		t.Fatalf("the queries answered %d and %d: %s %s", hidden.Status, nobody.Status, hidden.Raw, nobody.Raw)
	}
	if len(list(t, hidden, "rows")) != 0 || hidden.Body["total"] != nobody.Body["total"] {
		t.Errorf("carol's query by a key she may not see reads %s, by a key nobody has %s", hidden.Raw, nobody.Raw)
	}
	if strings.Contains(string(hidden.Raw), hiddenTitle) {
		t.Errorf("carol's answer names what she may not see: %s", hidden.Raw)
	}
}

// A project's own link type gets a field for each phrase, a symmetric type one for either end, and only live links
// are matched; a retired type keeps its links, so they still are.
func TestAQueryFollowsEveryLinkTypeByItsPhrase(t *testing.T) {
	w := newCommentWorld(t, "QLT")
	tests := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/link-types", map[string]any{
		"key": "tests", "name": "Tests", "forward": "tests", "backward": "is tested by", "counts": "verification",
	}), http.StatusCreated, "make a project's own type"), "linkType")["id"].(string)
	req := func(n int) string { return w.key + "-R" + strconv.Itoa(n) }
	for _, title := range []string{"Brake bench run", "Brake lights follow the pedal", "Brake test on ice"} {
		want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title)
	}
	link := func(from, kind, to string) string {
		return obj(t, want(t, w.bob.post(t, "/api/v1/requirements/"+from+"/links", map[string]any{"typeKey": kind, "otherKey": to}),
			http.StatusCreated, from+" "+kind+" "+to), "link")["id"].(string)
	}
	tested := link(req(2), "tests", req(1))
	link(req(3), "relates", req(1))
	grid := func(query string) []string { return queryKeys(t, w.carol, w.key, query) }

	t.Run("a project's own type reads from each end by its phrase", func(t *testing.T) {
		sameList(t, "tests R1", grid(`tests = `+req(1)), req(2))
		sameList(t, "tested by R2", grid(`testedBy = `+req(2)), req(1))
		sameList(t, "tested by anything", grid(`testedBy IS NOT EMPTY`), req(1))
	})
	t.Run("a symmetric type reads from either end", func(t *testing.T) {
		sameList(t, "relates to R1", grid(`relatesTo = `+req(1)), req(3))
		sameList(t, "relates to R3", grid(`relatesTo = `+req(3)), req(1))
	})
	t.Run("a removed link is matched no more", func(t *testing.T) {
		want(t, w.bob.delete(t, "/api/v1/links/"+tested), http.StatusNoContent, "remove R2 tests R1")
		sameList(t, "tested by anything", grid(`testedBy IS NOT EMPTY`))
		sameList(t, "tests anything", grid(`tests IS NOT EMPTY`))
	})
	t.Run("a retired type's links are still matched", func(t *testing.T) {
		link(req(4), "tests", req(1))
		want(t, w.owner.patch(t, "/api/v1/link-types/"+tests, map[string]any{"retired": true}), http.StatusOK, "retire tests")
		sameList(t, "tested by R4", grid(`testedBy = `+req(4)), req(1))
		sameList(t, "tests R1", grid(`tests = `+req(1)), req(4))
	})
}

// An assistant reads the graph and the coverage as the API answers its person, what they may not see counted alike.
func TestAnAssistantReadsTheGraphAndCoverageAsTheAPIDoes(t *testing.T) {
	s := newHiddenScene(t, "MGC")
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/coverage-rules", map[string]any{"decomposed": true, "verified": true}), http.StatusOK,
		"ask every type to be decomposed and verified")
	want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Brake pedal reads within 10 ms"}), http.StatusCreated, "write R2")
	want(t, s.bob.post(t, "/api/v1/requirements/"+s.key+"-R2/links", map[string]any{"typeKey": "derives", "otherKey": s.reqKey}),
		http.StatusCreated, "R2 derives from R1")
	_, secret := makeToken(t, s.carol, map[string]any{"name": "Carol's assistant"})
	s.h.drained(t)
	a := connectAssistant(t, s.carol.api, secret)

	t.Run("get_trace_graph", func(t *testing.T) {
		for _, c := range []struct {
			query string
			args  map[string]any
		}{
			{"", map[string]any{"reqKey": s.reqKey}},
			{"?direction=down&depth=1", map[string]any{"reqKey": s.reqKey, "direction": "down", "depth": 1}},
			{"?direction=up&depth=1", map[string]any{"reqKey": s.key + "-R2", "direction": "up", "depth": 1}},
		} {
			reqKey := c.args["reqKey"].(string)
			api := graphOf(t, s.carol, reqKey, c.query)
			tool, ok := a.call(t, "get_trace_graph", c.args)["graph"].(map[string]any)
			if !ok {
				t.Fatalf("get_trace_graph %v answered no graph", c.args)
			}
			if !reflect.DeepEqual(graphLevels(tool), graphLevels(api)) || tool["hidden"] != api["hidden"] || tool["direction"] != api["direction"] ||
				tool["depth"] != api["depth"] {
				t.Errorf("get_trace_graph %v reads %v hiding %v, the API %v hiding %v", c.args, graphLevels(tool), tool["hidden"], graphLevels(api), api["hidden"])
			}
		}
		if g := graphOf(t, s.carol, s.reqKey, ""); g["hidden"] != float64(1) {
			t.Errorf("carol is told of %v hidden around R1, want 1", g["hidden"])
		}
	})
	t.Run("get_coverage", func(t *testing.T) {
		for _, c := range []struct {
			query string
			args  map[string]any
		}{
			{"", map[string]any{"projectKey": s.key}},
			{"?verified=none&limit=1", map[string]any{"projectKey": s.key, "verified": "none", "limit": 1}},
		} {
			api := want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/coverage"+c.query), http.StatusOK, "the coverage").Body
			tool := a.call(t, "get_coverage", c.args)
			for _, field := range []string{"summary", "rows", "total", "limit", "offset"} {
				if !reflect.DeepEqual(tool[field], api[field]) {
					t.Errorf("get_coverage %v reads %s %v, the API %v", c.args, field, tool[field], api[field])
				}
			}
		}
	})
}
