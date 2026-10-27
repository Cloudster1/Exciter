package csvio

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// impactGraph is CHK-R1 deriving from CHK-R2 by a suspect link, specified by a page and verified by CHK-R3, in the
// trash and titled like a formula, which leads on to a deleted issue and an outdated failing test and place in code.
func impactGraph() *trace.Graph {
	ref := func(key, title string) *requirement.Ref {
		return &requirement.Ref{ID: uuid.New(), Key: key, Title: title}
	}
	r1, r2, r3 := ref("CHK-R1", "Brakes engage within 100 ms"), ref("CHK-R2", "Stop within 40 m"), ref("CHK-R3", `=HYPERLINK("http://x")`)
	r1.Status = &requirement.StatusRef{Name: "Draft"}
	r3.Trashed = true
	page := &trace.MatrixPage{ID: uuid.New(), SpaceKey: "ENG", Title: "Brake timing"}
	node := func(level int, kind trace.NodeKind, id string) trace.GraphNode {
		return trace.GraphNode{ID: id, Level: level, Kind: kind, Gaps: []trace.Dimension{}}
	}
	centre := node(0, trace.NodeRequirement, r1.ID.String())
	centre.Requirement, centre.Type, centre.Hidden = r1, &requirement.TypeRef{Name: "System"}, 1
	up := node(-1, trace.NodeRequirement, r2.ID.String())
	up.Requirement, up.Gaps = r2, []trace.Dimension{trace.Decomposed, trace.Verified}
	down := node(1, trace.NodeRequirement, r3.ID.String())
	down.Requirement, down.Broken = r3, true
	specifies := node(1, trace.NodePage, "page:"+page.ID.String())
	specifies.Page = page
	issue := node(2, trace.NodeIssue, "issue:WEB-9")
	issue.IssueKey, issue.Broken = "WEB-9", true
	test := node(2, trace.NodeTest, "test:acme/brakes/bench")
	test.Test = &trace.ChainTest{Repository: "acme", Suite: "brakes", Name: "bench", Outcome: "failed", Outdated: true}
	code := node(2, trace.NodeCode, "code:acme/brakes/engage.go:12")
	code.Code = &trace.ChainCode{Repository: "acme", Path: "brakes/engage.go", Line: 12, Outdated: true}
	return &trace.Graph{
		Root:  centre.ID,
		Nodes: []trace.GraphNode{centre, up, down, specifies, issue, test, code},
		Edges: []trace.GraphEdge{
			{From: centre.ID, To: up.ID, TypeKey: "derives", Suspect: true},
			{From: down.ID, To: centre.ID, TypeKey: "verifies"},
			{From: centre.ID, To: specifies.ID, Relation: "specifies"},
			{From: down.ID, To: issue.ID, Relation: "implements", Broken: true},
			{From: down.ID, To: test.ID, Relation: "verifies", Suspect: true},
			{From: down.ID, To: code.ID, Relation: "implements", Suspect: true},
		},
		Truncated: true,
	}
}

func TestAnImpactFileListsWhatTheGraphReachesNearestFirst(t *testing.T) {
	var b strings.Builder
	if err := WriteImpact(&b, impactGraph()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Level,Kind,Name,Title,Type,Status,Reached by,Marks",
		"0,requirement,CHK-R1,Brakes engage within 100 ms,System,Draft,,1 not visible",
		// Every cell passes the guard against formulas, a level above the centre too.
		`'-1,requirement,CHK-R2,Stop within 40 m,,,derives CHK-R1,"suspect; gaps: decomposed, verified"`,
		// The suspect links CHK-R3 leads on by mark the test and the code, not CHK-R3, which a sound link reached.
		`1,requirement,CHK-R3,"'=HYPERLINK(""http://x"")",,,verifies CHK-R1,broken`,
		"1,page,Brake timing,Brake timing,,,specifies CHK-R1,",
		"2,issue,WEB-9,,,,implements CHK-R3,broken",
		"2,test,brakes bench,,,failed,verifies CHK-R3,suspect; failing; outdated",
		"2,code,brakes/engage.go:12,acme,,,implements CHK-R3,suspect; outdated",
		fmt.Sprintf("This file holds the first %d of what the graph reaches. Follow fewer levels or link types to see the rest.", trace.MaxGraphNodes),
	}
	if got := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("wrote\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestAnImpactFileOfAWholeGraphEndsWithItsLastNode(t *testing.T) {
	g := impactGraph()
	g.Truncated = false
	var b strings.Builder
	if err := WriteImpact(&b, g); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	if len(lines) != len(g.Nodes)+1 || !strings.HasPrefix(lines[len(lines)-1], "2,code,") {
		t.Errorf("a whole graph's file ends with %q after %d lines", lines[len(lines)-1], len(lines))
	}
}
