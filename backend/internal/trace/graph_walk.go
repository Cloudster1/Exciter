package trace

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// QueryError refuses a graph asked for with a direction, depth, type or relation that does not exist.
type QueryError struct {
	Field, Message string
}

func (e *QueryError) Error() string { return e.Field + ": " + e.Message }

// graphRelations are every relation the graph reaches below a requirement.
func graphRelations() []string {
	var all []string
	for _, list := range [][]string{IssueRelations, PageRelations, TestRelations, CodeRelations} {
		for _, rel := range list {
			if !slices.Contains(all, rel) {
				all = append(all, rel)
			}
		}
	}
	return all
}

// checkGraphQuery fills in what was left out and refuses what cannot be walked; the link type keys are the
// service's to check, against the project's types.
func checkGraphQuery(q GraphQuery) (GraphQuery, error) {
	if q.Direction == "" {
		q.Direction = GraphBoth
	}
	if !slices.Contains(GraphDirections, q.Direction) {
		return q, &QueryError{"direction", "Say up, down or both."}
	}
	if q.Depth == 0 {
		q.Depth = GraphDefaultDepth
	}
	if q.Depth < 1 || q.Depth > GraphMaxDepth {
		return q, &QueryError{"depth", "Follow the links 1 to " + strconv.Itoa(GraphMaxDepth) + " levels deep."}
	}
	known := graphRelations()
	for _, rel := range q.Relations {
		if !slices.Contains(known, rel) {
			return q, &QueryError{"relation", "There is no relation " + strconv.Quote(rel) + ". Pick " + strings.Join(known, ", ") + "."}
		}
	}
	return q, nil
}

// graphLink is a trace link the walk read from a requirement it stood on, with the requirement at its other end.
type graphLink struct {
	from  uuid.UUID
	other GraphNode
	// project and typeID are the other end's, for the gaps its project's rules find.
	project uuid.UUID
	typeID  *uuid.UUID
	edge    GraphEdge
}

// graphLeaf is an issue, page, test or place in the code below a requirement the walk stood on.
type graphLeaf struct {
	from uuid.UUID
	node GraphNode
	edge GraphEdge
}

// graphReader reads one step of the walk: the database in Graph, a fixture in the tests.
type graphReader interface {
	// links reads the trace links from the requirements given, outgoing ones where they are the source and incoming
	// ones where they are the target, and either way for a type that reads the same both ways.
	links(ctx context.Context, from []uuid.UUID, direction Direction) ([]graphLink, error)
	leaves(ctx context.Context, from []uuid.UUID) ([]graphLeaf, error)
}

// graphBuilder gathers the nodes and edges of a walk, each once.
type graphBuilder struct {
	g       *Graph
	at      map[string]int
	edges   map[string]bool
	project map[uuid.UUID]uuid.UUID
	typeOf  map[uuid.UUID]*uuid.UUID
}

// node adds n at a level unless it is there already; added says it is new, and ok false that the graph was full.
func (b *graphBuilder) node(n GraphNode, level int) (added, ok bool) {
	if _, seen := b.at[n.ID]; seen {
		return false, true
	}
	if len(b.g.Nodes) >= MaxGraphNodes {
		b.g.Truncated = true
		return false, false
	}
	n.Level = level
	if n.Gaps == nil {
		n.Gaps = []Dimension{}
	}
	b.at[n.ID] = len(b.g.Nodes)
	b.g.Nodes = append(b.g.Nodes, n)
	return true, true
}

func (b *graphBuilder) edge(e GraphEdge) {
	key := e.From + "|" + e.To + "|" + e.Relation
	if e.LinkID != nil {
		key = e.LinkID.String()
	}
	if b.edges[key] {
		return
	}
	b.edges[key] = true
	b.g.Edges = append(b.g.Edges, e)
}

// side is one way the walk goes from the centre: up along outgoing links to negative levels, or down along incoming
// ones to positive levels, where the leaves hang.
type side struct {
	direction Direction
	sign      int
	frontier  []uuid.UUID
}

// walk follows the links from root level by level, as q asks, each node once at the level it was first reached.
func walk(ctx context.Context, rd graphReader, root GraphNode, rootID uuid.UUID, q GraphQuery) (*graphBuilder, error) {
	b := &graphBuilder{
		g:       &Graph{Root: root.ID, Direction: q.Direction, Depth: q.Depth, Nodes: []GraphNode{}, Edges: []GraphEdge{}},
		at:      map[string]int{},
		edges:   map[string]bool{},
		project: map[uuid.UUID]uuid.UUID{},
		typeOf:  map[uuid.UUID]*uuid.UUID{},
	}
	b.node(root, 0)
	var sides []*side
	if q.Direction != GraphDown {
		sides = append(sides, &side{direction: Outgoing, sign: -1, frontier: []uuid.UUID{rootID}})
	}
	if q.Direction != GraphUp {
		sides = append(sides, &side{direction: Incoming, sign: 1, frontier: []uuid.UUID{rootID}})
	}
	if root.Broken {
		return b, nil
	}
	for depth := 1; depth <= q.Depth; depth++ {
		for _, s := range sides {
			if len(s.frontier) == 0 {
				continue
			}
			links, err := rd.links(ctx, s.frontier, s.direction)
			if err != nil {
				return nil, err
			}
			var next []uuid.UUID
			for _, l := range links {
				added, ok := b.node(l.other, depth*s.sign)
				if !ok {
					continue
				}
				b.edge(l.edge)
				if added {
					id := l.other.Requirement.ID
					b.project[id], b.typeOf[id] = l.project, l.typeID
					if !l.other.Broken {
						next = append(next, id)
					}
				}
			}
			if s.sign > 0 {
				leaves, err := rd.leaves(ctx, s.frontier)
				if err != nil {
					return nil, err
				}
				for _, l := range leaves {
					if _, ok := b.node(l.node, depth); ok {
						b.edge(l.edge)
					}
				}
			}
			s.frontier = next
		}
	}
	return b, nil
}

// kindOrder lays the kinds out as a reader expects them: requirements, then what implements, describes and verifies them.
var kindOrder = map[NodeKind]int{NodeRequirement: 0, NodeIssue: 1, NodePage: 2, NodeTest: 3, NodeCode: 4}

// sorted orders the nodes nearest first, up before down, by kind and name, and the edges by their ends.
func (g *Graph) sorted() {
	abs := func(n int) int { return max(n, -n) }
	slices.SortStableFunc(g.Nodes, func(a, b GraphNode) int {
		return cmp.Or(cmp.Compare(abs(a.Level), abs(b.Level)), cmp.Compare(a.Level, b.Level),
			cmp.Compare(kindOrder[a.Kind], kindOrder[b.Kind]), cmp.Compare(a.name(), b.name()))
	})
	slices.SortStableFunc(g.Edges, func(a, b GraphEdge) int {
		return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.To, b.To), cmp.Compare(a.Relation, b.Relation))
	})
}

// name is what a node is called in a list: a requirement's key, an issue's key, or where a page, test or tag is.
func (n GraphNode) name() string {
	switch {
	case n.Requirement != nil:
		return n.Requirement.Key
	case n.IssueKey != "":
		return n.IssueKey
	case n.Page != nil:
		return n.Page.SpaceKey + "/" + n.Page.ID.String()
	case n.Test != nil:
		return n.Test.Repository + "/" + n.Test.Suite + "/" + n.Test.Name
	case n.Code != nil:
		return n.Code.Repository + "/" + n.Code.Path + ":" + strconv.Itoa(n.Code.Line)
	}
	return n.ID
}
