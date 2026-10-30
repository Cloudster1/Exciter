//go:build integration

package test

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The trace graph (#211): everything a requirement traces to, up and down across projects, marked, and counted
// where the reader may not see it.

// graphScene is a trace scene whose S1 is also derived into X, a requirement of a second project carol may not read.
type graphScene struct {
	*traceScene
	otherKey, xKey string
	xID            uuid.UUID
}

func newGraphScene(t *testing.T, prefix string) *graphScene {
	t.Helper()
	s := &graphScene{traceScene: newTraceScene(t, prefix), otherKey: projectKey(t, prefix+"X")}
	other := obj(t, want(t, s.owner.post(t, "/api/v1/projects", map[string]any{"key": s.otherKey, "name": "Hidden"}), http.StatusCreated, "make a second project"), "project")
	if _, err := s.h.super.Exec(t.Context(), `DELETE FROM role_assignment WHERE project_id = $1 AND group_id = (SELECT id FROM groups WHERE builtin = 'everyone')`, other["id"]); err != nil {
		t.Fatal(err)
	}
	x := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.otherKey+"/requirements", map[string]any{"title": "Secret derived brake"}),
		http.StatusCreated, "write X"), "requirement")
	s.xKey, s.xID = x["key"].(string), uuid.MustParse(x["id"].(string))
	want(t, s.owner.post(t, "/api/v1/requirements/"+s.xKey+"/links", map[string]any{"typeKey": "derives", "otherKey": s.keys["S1"]}),
		http.StatusCreated, "derive X from S1 across projects")
	s.h.settle(t)
	return s
}

// graphOf reads a graph as c with the query given.
func graphOf(t *testing.T, c *client, key, query string) map[string]any {
	t.Helper()
	return obj(t, want(t, c.get(t, "/api/v1/requirements/"+key+"/graph"+query), http.StatusOK, "read the graph"), "graph")
}

// nodeName names a node of the answer as a person would: a requirement or issue by key, a test by its name, a tag by
// where it is.
func nodeName(n map[string]any) string {
	switch {
	case n["requirement"] != nil:
		return n["requirement"].(map[string]any)["key"].(string)
	case n["issueKey"] != nil && n["issueKey"] != "":
		return n["issueKey"].(string)
	case n["test"] != nil:
		return n["test"].(map[string]any)["name"].(string)
	case n["code"] != nil:
		code := n["code"].(map[string]any)
		return fmt.Sprintf("%s:%v", code["path"], code["line"])
	}
	return n["id"].(string)
}

// graphNodes reads a graph's nodes by name.
func graphNodes(g map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, each := range g["nodes"].([]any) {
		n := each.(map[string]any)
		out[nodeName(n)] = n
	}
	return out
}

// graphLevels reads a graph as each node's name and level.
func graphLevels(g map[string]any) map[string]float64 {
	out := map[string]float64{}
	for name, n := range graphNodes(g) {
		out[name] = n["level"].(float64)
	}
	return out
}

func sameGraph(t *testing.T, what string, g map[string]any, want map[string]float64) {
	t.Helper()
	got := graphLevels(g)
	if len(got) != len(want) {
		t.Errorf("%s reaches %v, want %v", what, got, want)
		return
	}
	for k, v := range want {
		if l, ok := got[k]; !ok || l != v {
			t.Errorf("%s reaches %v, want %v", what, got, want)
			return
		}
	}
}

func TestATraceGraphFollowsTheLinksUpAndDownAsFarAsAsked(t *testing.T) {
	s := newGraphScene(t, "TGW")
	n1, s1, t1 := s.keys["N1"], s.keys["S1"], s.keys["T1"]

	t.Run("both ways, the graph reaches what S1 derives from, what derives from and verifies it, and the issue below", func(t *testing.T) {
		g := graphOf(t, s.owner, s1, "")
		sameGraph(t, "the owner's graph", g, map[string]float64{s1: 0, n1: -1, t1: 1, s.xKey: 1, "WEB-9": 2})
		x := graphNodes(g)[s.xKey]["requirement"].(map[string]any)
		if x["projectKey"] != s.otherKey || g["root"] != graphNodes(g)[s1]["id"] || g["truncated"] != false || g["hidden"] != float64(0) {
			t.Errorf("the graph reads %v", g)
		}
		if typ := graphNodes(g)[t1]["type"].(map[string]any); typ["name"] != "Test" {
			t.Errorf("T1's type reads %v", typ)
		}
		var phrases []string
		for _, each := range g["edges"].([]any) {
			e := each.(map[string]any)
			phrases = append(phrases, fmt.Sprint(e["typeKey"], "/", e["relation"]))
		}
		slices.Sort(phrases)
		if strings.Join(phrases, ",") != "<nil>/implements,derives/<nil>,derives/<nil>,verifies/<nil>" {
			t.Errorf("the edges read %v", phrases)
		}
	})
	t.Run("one way, to a depth, through the types and relations named", func(t *testing.T) {
		sameGraph(t, "up", graphOf(t, s.owner, s1, "?direction=up&depth=1"), map[string]float64{s1: 0, n1: -1})
		sameGraph(t, "down a level", graphOf(t, s.owner, s1, "?direction=down&depth=1"), map[string]float64{s1: 0, t1: 1, s.xKey: 1})
		sameGraph(t, "down through verifies", graphOf(t, s.owner, s1, "?direction=down&type=verifies"), map[string]float64{s1: 0, t1: 1, "WEB-9": 2})
		sameGraph(t, "down without issues", graphOf(t, s.owner, s1, "?direction=down&relation=verifies"), map[string]float64{s1: 0, t1: 1, s.xKey: 1})
		sameGraph(t, "N1 down three", graphOf(t, s.owner, n1, "?direction=down&depth=3"),
			map[string]float64{n1: 0, s1: 1, s.keys["S2"]: 1, t1: 2, s.xKey: 2, "WEB-9": 3})
	})
	t.Run("what cannot be walked is refused on its field", func(t *testing.T) {
		for _, q := range []string{"?direction=sideways", "?depth=9", "?depth=two", "?type=blocks", "?relation=blocks"} {
			refuse(t, q, s.owner.get(t, "/api/v1/requirements/"+s1+"/graph"+q), http.StatusBadRequest, "bad_request")
		}
		refuse(t, "an impact too deep", s.owner.get(t, "/api/v1/requirements/"+s1+"/graph.csv?depth=9"), http.StatusBadRequest, "bad_request")
		refuse(t, "a requirement nobody wrote", s.owner.get(t, "/api/v1/requirements/"+s.key+"-R999/graph"), http.StatusNotFound, "not_found")
		refuse(t, "carol, of a requirement she may not read", s.carol.get(t, "/api/v1/requirements/"+s.xKey+"/graph"), http.StatusNotFound, "not_found")
	})
}

func TestATraceGraphCountsWhatTheReaderMayNotSeeAndNeverNamesIt(t *testing.T) {
	s := newGraphScene(t, "TGH")
	s1 := s.keys["S1"]
	answer := want(t, s.carol.get(t, "/api/v1/requirements/"+s1+"/graph"), http.StatusOK, "carol reads the graph")
	g := obj(t, answer, "graph")
	sameGraph(t, "carol's graph", g, map[string]float64{s1: 0, s.keys["N1"]: -1, s.keys["T1"]: 1, "WEB-9": 2})
	if graphNodes(g)[s1]["hidden"] != float64(1) || g["hidden"] != float64(1) {
		t.Errorf("carol is told of %v hidden at S1, %v in all; want 1", graphNodes(g)[s1]["hidden"], g["hidden"])
	}
	if raw := string(answer.Raw); strings.Contains(raw, s.xKey) || strings.Contains(raw, "Secret derived brake") || strings.Contains(raw, s.xID.String()) {
		t.Errorf("carol's answer names the requirement she may not read: %s", raw)
	}
	if up := graphOf(t, s.carol, s1, "?direction=up"); graphNodes(up)[s1]["hidden"] != float64(0) {
		t.Errorf("going up, carol is told of %v hidden below", graphNodes(up)[s1]["hidden"])
	}

	t.Run("straight through SQL, the count is the asker's own, around what they may see", func(t *testing.T) {
		s1ID := uuid.MustParse(graphNodes(g)[s1]["id"].(string))
		count := func(actor uuid.UUID, id uuid.UUID) int {
			var hidden int
			err := s.h.appConn(t, actor).QueryRow(t.Context(), `SELECT COALESCE(sum(hidden), 0)::int FROM trace_hidden_links($1, $2, NULL)`,
				[]uuid.UUID{id}, []string{"both"}).Scan(&hidden)
			if err != nil {
				t.Fatal(err)
			}
			return hidden
		}
		if got := count(s.carolID, s1ID); got != 1 {
			t.Errorf("carol's count at S1 = %d, want 1", got)
		}
		if got := count(s.ownerID, s1ID); got != 0 {
			t.Errorf("the owner, who sees X, is told of %d hidden", got)
		}
		if got := count(s.carolID, s.xID); got != 0 {
			t.Errorf("around X, which carol may not see, she is told of %d", got)
		}
		var hidden int
		if err := s.h.appConnUnscoped(t).QueryRow(t.Context(), `SELECT COALESCE(sum(hidden), 0)::int FROM trace_hidden_links($1, $2, NULL)`,
			[]uuid.UUID{s1ID}, []string{"both"}).Scan(&hidden); err != nil || hidden != 0 {
			t.Errorf("acting for nobody, the count = %d, %v", hidden, err)
		}
	})
	t.Run("the impact list counts it too, and names it nowhere", func(t *testing.T) {
		resp := want(t, s.carol.get(t, "/api/v1/requirements/"+s1+"/graph.csv"), http.StatusOK, "carol downloads the impact")
		raw := bytes.TrimPrefix(resp.Raw, []byte{0xEF, 0xBB, 0xBF})
		records, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != 5 || strings.Join(records[0], ",") != "Level,Kind,Name,Title,Type,Status,Reached by,Marks" {
			t.Fatalf("the impact reads %v", records)
		}
		if records[1][2] != s1 || records[1][7] != "1 not visible" || bytes.Contains(raw, []byte(s.xKey)) {
			t.Errorf("the centre's row reads %v", records[1])
		}
		if !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
			t.Errorf("the impact is not a download: %v", resp.Header)
		}
	})
}

func TestATraceGraphMarksSuspectAndBrokenLinksGapsAndEvidence(t *testing.T) {
	s := newGraphScene(t, "TGM")
	n1, s1, t1 := s.keys["N1"], s.keys["S1"], s.keys["T1"]
	bench := "bench [verifies " + t1 + "]"
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/coverage-rules", map[string]any{"verified": true}), http.StatusOK, "ask every requirement to be verified")
	ran(t, s.bob, s.key, runForm("main"), junit([2]string{bench, "failed"}))
	scanned(t, s.bob, s.key, scanBody("main", tag("brakes/engage.go", 12, "implements", s1)))
	want(t, s.bob.patch(t, "/api/v1/requirements/"+n1, map[string]any{"title": "Stop within 35 m", "baseRevision": 1}), http.StatusOK, "revise N1")
	if _, err := s.h.super.Exec(t.Context(), `UPDATE armature_issue_link SET broken_at = now() WHERE issue_key = 'WEB-9' AND requirement_id IN
		(SELECT id FROM requirement WHERE project_id = $1)`, s.projectID); err != nil {
		t.Fatal(err)
	}
	s.h.settle(t)

	g := graphOf(t, s.carol, s1, "?depth=3")
	nodes := graphNodes(g)
	sameGraph(t, "the marked graph", g, map[string]float64{s1: 0, n1: -1, t1: 1, "WEB-9": 2, bench: 2, "brakes/engage.go:12": 1})
	t.Run("the link to a revised requirement is suspect", func(t *testing.T) {
		for _, each := range g["edges"].([]any) {
			e := each.(map[string]any)
			if e["to"] == nodes[n1]["id"] && e["suspect"] != true {
				t.Errorf("S1's link to the revised N1 reads %v", e)
			}
		}
	})
	t.Run("a deleted issue is broken, and so is its link", func(t *testing.T) {
		if nodes["WEB-9"]["broken"] != true {
			t.Errorf("WEB-9 reads %v", nodes["WEB-9"])
		}
	})
	t.Run("what the rules ask and nothing gives is a gap", func(t *testing.T) {
		if gaps := fmt.Sprint(nodes[n1]["gaps"]); gaps != "[verified]" {
			t.Errorf("N1, which nothing verifies, has the gaps %s", gaps)
		}
		if gaps := fmt.Sprint(nodes[s1]["gaps"]); gaps != "[]" {
			t.Errorf("S1, which T1 verifies, has the gaps %s", gaps)
		}
	})
	t.Run("a failing test and the code below are nodes, read on the branch asked for", func(t *testing.T) {
		test := nodes[bench]["test"].(map[string]any)
		if test["outcome"] != "failed" || test["outdated"] != false {
			t.Errorf("the test reads %v", test)
		}
		code := nodes["brakes/engage.go:12"]["code"].(map[string]any)
		if code["outdated"] != false || !strings.Contains(code["url"].(string), "brakes/engage.go") {
			t.Errorf("the code reads %v", code)
		}
		if _, ok := graphNodes(graphOf(t, s.carol, s1, "?depth=3&branch=feature"))["brakes/engage.go:12"]; ok {
			t.Error("the code scanned on main shows on another branch")
		}
	})
}
