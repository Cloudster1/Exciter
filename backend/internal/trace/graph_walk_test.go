package trace

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// fixtureLink reads source to target: CHK-2 derives from CHK-1 has CHK-1 above.
type fixtureLink struct {
	id             uuid.UUID
	source, target string
	symmetric      bool
}

// fixtureGraph answers the walk's steps from links between requirements named by key, and issues below them.
type fixtureGraph struct {
	ids     map[string]uuid.UUID
	trashed map[string]bool
	edges   []fixtureLink
	issues  map[string][]string
}

func newFixture() *fixtureGraph {
	return &fixtureGraph{ids: map[string]uuid.UUID{}, trashed: map[string]bool{}, issues: map[string][]string{}}
}

func (f *fixtureGraph) id(key string) uuid.UUID {
	if id, ok := f.ids[key]; ok {
		return id
	}
	f.ids[key] = uuid.New()
	return f.ids[key]
}

func (f *fixtureGraph) link(source, target string, symmetric bool) {
	f.id(source)
	f.id(target)
	f.edges = append(f.edges, fixtureLink{id: uuid.New(), source: source, target: target, symmetric: symmetric})
}

func (f *fixtureGraph) node(key string) GraphNode {
	ref := requirement.Ref{ID: f.id(key), Key: key, Kind: requirement.KindRequirement, Trashed: f.trashed[key]}
	return GraphNode{ID: ref.ID.String(), Kind: NodeRequirement, Requirement: &ref, Broken: ref.Trashed}
}

func (f *fixtureGraph) keyOf(id uuid.UUID) string {
	for k, v := range f.ids {
		if v == id {
			return k
		}
	}
	return ""
}

func (f *fixtureGraph) reach(from []uuid.UUID, direction Direction) []graphLink {
	var out []graphLink
	for _, at := range from {
		key := f.keyOf(at)
		for _, l := range f.edges {
			var other string
			switch {
			case l.source == key && (direction == Outgoing || l.symmetric):
				other = l.target
			case l.target == key && (direction == Incoming || l.symmetric):
				other = l.source
			default:
				continue
			}
			id := l.id
			out = append(out, graphLink{from: at, other: f.node(other),
				edge: GraphEdge{LinkID: &id, From: f.id(l.source).String(), To: f.id(l.target).String()}})
		}
	}
	return out
}

func (f *fixtureGraph) links(_ context.Context, from []uuid.UUID, direction Direction) ([]graphLink, error) {
	return f.reach(from, direction), nil
}

func (f *fixtureGraph) leaves(_ context.Context, from []uuid.UUID) ([]graphLeaf, error) {
	var out []graphLeaf
	for _, at := range from {
		for _, issue := range f.issues[f.keyOf(at)] {
			n := GraphNode{ID: "issue:" + issue, Kind: NodeIssue, IssueKey: issue}
			out = append(out, graphLeaf{from: at, node: n, edge: GraphEdge{From: at.String(), To: n.ID, Relation: "implements"}})
		}
	}
	return out, nil
}

func (f *fixtureGraph) walk(t *testing.T, root string, q GraphQuery) *Graph {
	t.Helper()
	q, err := checkGraphQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	b, err := walk(context.Background(), f, f.node(root), f.id(root), q)
	if err != nil {
		t.Fatal(err)
	}
	b.g.sorted()
	return b.g
}

// levels reads a graph as each node's name and level.
func levels(g *Graph) map[string]int {
	out := map[string]int{}
	for _, n := range g.Nodes {
		out[n.name()] = n.Level
	}
	return out
}

func sameLevels(t *testing.T, got *Graph, want map[string]int) {
	t.Helper()
	have := levels(got)
	if len(have) != len(want) {
		t.Fatalf("nodes = %v, want %v", have, want)
	}
	for k, l := range want {
		if have[k] != l {
			t.Errorf("%s at level %d, want %d (all: %v)", k, have[k], l, have)
		}
	}
}

// A small specification: R derives from S1, which derives from S0; C1 derives from R, and C2 from C1 and from R;
// R relates to X both ways; issues hang below R and C1.
func specification() *fixtureGraph {
	f := newFixture()
	f.link("R", "S1", false)
	f.link("S1", "S0", false)
	f.link("C1", "R", false)
	f.link("C2", "C1", false)
	f.link("C2", "R", false)
	f.link("R", "X", true)
	f.issues["R"] = []string{"ARM-1"}
	f.issues["C1"] = []string{"ARM-2"}
	return f
}

func TestTheGraphGoesUpAndDownToItsDepthEachNodeOnceWhereFirstReached(t *testing.T) {
	f := specification()
	g := f.walk(t, "R", GraphQuery{Direction: GraphBoth, Depth: 2})
	sameLevels(t, g, map[string]int{"R": 0, "S1": -1, "X": -1, "S0": -2, "C1": 1, "C2": 1, "ARM-1": 1, "ARM-2": 2})
	// Six links and two issues; C2's link to C1 joins two nodes already there.
	if len(g.Edges) != 8 {
		t.Errorf("edges = %d, want 8: %+v", len(g.Edges), g.Edges)
	}
	if g.Nodes[0].name() != "R" || g.Truncated {
		t.Errorf("the centre is not first, or the graph was cut: %v", levels(g))
	}
}

func TestTheGraphFollowsOneWayAndStopsAtItsDepth(t *testing.T) {
	f := specification()
	sameLevels(t, f.walk(t, "R", GraphQuery{Direction: GraphUp, Depth: 1}), map[string]int{"R": 0, "S1": -1, "X": -1})
	sameLevels(t, f.walk(t, "R", GraphQuery{Direction: GraphUp, Depth: 3}), map[string]int{"R": 0, "S1": -1, "X": -1, "S0": -2})
	// Down, the symmetric link leads to X as well, and the issues hang below.
	sameLevels(t, f.walk(t, "R", GraphQuery{Direction: GraphDown, Depth: 1}),
		map[string]int{"R": 0, "C1": 1, "C2": 1, "X": 1, "ARM-1": 1})
}

func TestTheGraphComesBackRoundACycleWithoutWalkingItAgain(t *testing.T) {
	f := newFixture()
	f.link("A", "B", false)
	f.link("B", "C", false)
	f.link("C", "A", false)
	g := f.walk(t, "A", GraphQuery{Direction: GraphUp, Depth: 6})
	sameLevels(t, g, map[string]int{"A": 0, "B": -1, "C": -2})
	if len(g.Edges) != 3 {
		t.Errorf("edges = %d, want the cycle's 3", len(g.Edges))
	}
}

func TestTheGraphDoesNotGoOnFromARequirementInTheTrash(t *testing.T) {
	f := specification()
	f.trashed["C1"] = true
	g := f.walk(t, "R", GraphQuery{Direction: GraphDown, Depth: 3})
	sameLevels(t, g, map[string]int{"R": 0, "C1": 1, "C2": 1, "X": 1, "ARM-1": 1})
	for _, n := range g.Nodes {
		if n.name() == "C1" && !n.Broken {
			t.Error("a requirement in the trash is not marked broken")
		}
	}
}

func TestTheGraphStopsAtItsNodesAndSaysSo(t *testing.T) {
	f := newFixture()
	for i := range MaxGraphNodes + 10 {
		f.link(fmt.Sprintf("C%d", i), "R", false)
	}
	g := f.walk(t, "R", GraphQuery{Direction: GraphDown, Depth: 1})
	if len(g.Nodes) != MaxGraphNodes || !g.Truncated {
		t.Fatalf("nodes = %d truncated = %v, want %d and true", len(g.Nodes), g.Truncated, MaxGraphNodes)
	}
	if len(g.Edges) != MaxGraphNodes-1 {
		t.Errorf("edges = %d, want one to each node shown", len(g.Edges))
	}
}

func TestAGraphIsAskedForWhatCanBeWalked(t *testing.T) {
	for _, q := range []GraphQuery{
		{Direction: "sideways"},
		{Depth: GraphMaxDepth + 1},
		{Depth: -1},
		{Relations: []string{"implements", "blocks"}},
	} {
		if _, err := checkGraphQuery(q); err == nil {
			t.Errorf("%+v was taken", q)
		}
	}
	q, err := checkGraphQuery(GraphQuery{Relations: []string{"specifies", "verifies"}})
	if err != nil || q.Direction != GraphBoth || q.Depth != GraphDefaultDepth {
		t.Errorf("defaults = %+v, %v", q, err)
	}
}
