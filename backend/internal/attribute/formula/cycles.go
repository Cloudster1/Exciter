package formula

import (
	"slices"
	"strings"

	"github.com/google/uuid"
)

// Def is a computed attribute as the cycle check sees it: its name and what its formula reads.
type Def struct {
	ID    uuid.UUID
	Name  string
	Reads Reads
}

// CycleError refuses formulas that would read their own value, naming the attributes around the cycle.
type CycleError struct {
	Names []string
}

func (e *CycleError) Error() string {
	var b strings.Builder
	for i, n := range e.Names {
		switch {
		case i == 0:
			b.WriteString(n + " reads ")
		case i == len(e.Names)-1:
			b.WriteString(n + ".")
		default:
			b.WriteString(n + ", which reads ")
		}
	}
	return b.String() + " Take one of them out of the other's formula, or read it of the children."
}

// way is how one attribute reads another: of the same requirement, of its children, or along links of a type.
type way struct {
	self     bool
	children bool
	typeID   uuid.UUID
	out      bool
}

type edge struct {
	to  uuid.UUID
	via way
}

func edgesOf(d Def) []edge {
	var out []edge
	for _, id := range d.Reads.Self {
		out = append(out, edge{id, way{self: true}})
	}
	for _, id := range d.Reads.Children {
		out = append(out, edge{id, way{children: true}})
	}
	for _, l := range d.Reads.Links {
		for _, id := range l.Attributes {
			out = append(out, edge{id, way{typeID: l.TypeID, out: l.Out}})
		}
	}
	return out
}

// Cycles refuses computed attributes that could read themselves on one requirement: only a round through the
// children alone, which have no cycles, or along one acyclic link type one way, is safe.
func Cycles(defs []Def, acyclic func(typeID uuid.UUID) bool) error {
	byID := make(map[uuid.UUID]Def, len(defs))
	for _, d := range defs {
		byID[d.ID] = d
	}
	for _, component := range components(defs, byID) {
		if len(component) == 1 && !slices.ContainsFunc(edgesOf(byID[component[0]]), func(e edge) bool { return e.to == component[0] }) {
			continue
		}
		var ways []way
		for _, id := range component {
			for _, e := range edgesOf(byID[id]) {
				if slices.Contains(component, e.to) && !e.via.self && !slices.Contains(ways, e.via) {
					ways = append(ways, e.via)
				}
			}
		}
		safe := len(ways) == 1 && (ways[0].children || acyclic(ways[0].typeID))
		if !safe {
			return &CycleError{Names: cycleNames(component, byID)}
		}
	}
	return nil
}

// components are the groups of attributes that read one another round, by Tarjan's algorithm.
func components(defs []Def, byID map[uuid.UUID]Def) [][]uuid.UUID {
	index := map[uuid.UUID]int{}
	low := map[uuid.UUID]int{}
	onStack := map[uuid.UUID]bool{}
	var stack []uuid.UUID
	var out [][]uuid.UUID
	next := 0
	var visit func(id uuid.UUID)
	visit = func(id uuid.UUID) {
		index[id], low[id] = next, next
		next++
		stack = append(stack, id)
		onStack[id] = true
		for _, e := range edgesOf(byID[id]) {
			if _, known := byID[e.to]; !known {
				continue
			}
			if _, seen := index[e.to]; !seen {
				visit(e.to)
				low[id] = min(low[id], low[e.to])
			} else if onStack[e.to] {
				low[id] = min(low[id], index[e.to])
			}
		}
		if low[id] == index[id] {
			var group []uuid.UUID
			for {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[top] = false
				group = append(group, top)
				if top == id {
					break
				}
			}
			out = append(out, group)
		}
	}
	for _, d := range defs {
		if _, seen := index[d.ID]; !seen {
			visit(d.ID)
		}
	}
	return out
}

// cycleNames walks one way round a group, back to where it started, by name.
func cycleNames(component []uuid.UUID, byID map[uuid.UUID]Def) []string {
	start := component[0]
	for _, id := range component {
		if byID[id].Name < byID[start].Name {
			start = id
		}
	}
	path := []uuid.UUID{start}
	seen := map[uuid.UUID]bool{start: true}
	var walk func(at uuid.UUID) bool
	walk = func(at uuid.UUID) bool {
		for _, e := range edgesOf(byID[at]) {
			if e.to == start {
				path = append(path, start)
				return true
			}
			if slices.Contains(component, e.to) && !seen[e.to] {
				seen[e.to] = true
				path = append(path, e.to)
				if walk(e.to) {
					return true
				}
				path = path[:len(path)-1]
			}
		}
		return false
	}
	walk(start)
	names := make([]string, len(path))
	for i, id := range path {
		names[i] = byID[id].Name
	}
	return names
}
