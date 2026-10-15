//go:build integration

package test

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
)

// A query follows trace links by type from either end, reads approval and suspect links as the page does, and keeps
// requirements at the coverage levels the coverage report gives them.
func TestAQueryFollowsLinksApprovalAndCoverage(t *testing.T) {
	s := newCoverageScene(t, "QTR")
	grid := func(query string) []string {
		t.Helper()
		keys := rowKeys(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/requirements?query="+url.QueryEscape(query)), http.StatusOK, "the grid by "+query))
		sort.Strings(keys)
		return keys
	}
	reqs := func(n ...string) []string {
		out := make([]string, len(n))
		for i, each := range n {
			out[i] = s.req(each)
		}
		sort.Strings(out)
		return out
	}
	// The coverage report's levels and the grid's suspect counts are what the query must agree with.
	agreesWithTheReport := func(t *testing.T) {
		t.Helper()
		s.h.drained(t)
		_, rows, _ := s.coverage(t, s.carol, "")
		for i, dimension := range []string{"decomposed", "implemented", "verified"} {
			for _, level := range []string{"none", "partial", "full", "na"} {
				var keys []string
				for key, levels := range rows {
					if strings.Fields(levels)[i] == level {
						keys = append(keys, key)
					}
				}
				sort.Strings(keys)
				sameList(t, dimension+" = "+level, grid(dimension+" = "+level), keys...)
			}
		}
		var suspect []string
		for _, each := range list(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/requirements"), http.StatusOK, "the grid"), "rows") {
			if row := each.(map[string]any); row["suspectLinks"].(float64) > 0 {
				suspect = append(suspect, row["key"].(string))
			}
		}
		sort.Strings(suspect)
		sameList(t, "suspect > 0", grid(`suspect > 0`), suspect...)
	}

	t.Run("links by type from either end", func(t *testing.T) {
		sameList(t, "derives from R1", grid(`derivesFrom = `+s.req("1")), reqs("2")...)
		sameList(t, "derived into R2", grid(`derivedInto = `+s.req("2")), reqs("1")...)
		sameList(t, "verified by anything", grid(`verifiedBy IS NOT EMPTY`), reqs("1")...)
		sameList(t, "verifies R1 or R2", grid(`verifies IN (`+s.req("1")+`, `+s.req("2")+`)`), reqs("3")...)
		sameList(t, "derives from nothing", grid(`derivesFrom IS EMPTY`), reqs("1", "3", "4")...)
		sameList(t, "not derived into R2, with what has no link", grid(`derivedInto != `+s.req("2")), reqs("2", "3", "4")...)
	})

	t.Run("coverage levels agree with the coverage report, before and after a revision", func(t *testing.T) {
		agreesWithTheReport(t)
		want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/coverage-rules", map[string]any{"decomposed": true, "implemented": true, "verified": true}),
			http.StatusOK, "the rule for every type")
		want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/coverage-rules", map[string]any{"typeId": s.noteType}), http.StatusOK, "the rule for notes")
		agreesWithTheReport(t)
		sameList(t, "fully covered", grid(`decomposed = full AND implemented = full AND verified = full`), reqs("1")...)
		want(t, s.bob.patch(t, "/api/v1/requirements/"+s.req("1"), map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}),
			http.StatusOK, "bob revises R1")
		agreesWithTheReport(t)
		sameList(t, "partly verified after the revision", grid(`verified = partial`), reqs("1")...)
	})

	t.Run("approval as the reviews leave it", func(t *testing.T) {
		sameList(t, "nothing approved yet", grid(`approval = none`), reqs("1", "2", "3", "4")...)
		want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/approval-rules", map[string]any{"required": 1, "authorMayApprove": false,
			"approvers": []any{map[string]any{"subjectType": "user", "userId": s.daveID}}}), http.StatusOK, "dave approves")
		r1 := obj(t, want(t, s.bob.post(t, "/api/v1/requirements/"+s.req("1")+"/reviews", map[string]any{"revision": 2}), http.StatusCreated, "bob asks for R1"), "review")
		want(t, s.bob.post(t, "/api/v1/requirements/"+s.req("2")+"/reviews", map[string]any{"revision": 1}), http.StatusCreated, "bob asks for R2")
		sameList(t, "waiting", grid(`approval = pending`), reqs("1", "2")...)
		want(t, s.dave.post(t, "/api/v1/reviews/"+r1["id"].(string)+"/decisions", map[string]any{"decision": "approve", "revision": 2}),
			http.StatusOK, "dave approves R1")
		sameList(t, "approved", grid(`approval = approved`), reqs("1")...)
		want(t, s.bob.patch(t, "/api/v1/requirements/"+s.req("1"), map[string]any{"title": "Brakes engage within 70 ms", "baseRevision": 2}),
			http.StatusOK, "bob revises R1 again")
		sameList(t, "approved once, not now", grid(`approval = outdated`), reqs("1")...)
		sameList(t, "not approved now", grid(`approval != approved`), reqs("1", "2", "3", "4")...)
	})

	t.Run("a key the project had still names its requirement", func(t *testing.T) {
		old := s.key
		renamed := projectKey(t, "QTN")
		want(t, s.owner.put(t, "/api/v1/projects/"+old+"/key", map[string]any{"key": renamed}), http.StatusOK, "rename the project's key")
		s.key = renamed
		sameList(t, "by the old key", grid(`derivesFrom = `+old+`-R1`), reqs("2")...)
		sameList(t, "by the new key", grid(`key = `+renamed+`-R3`), reqs("3")...)
	})
}
