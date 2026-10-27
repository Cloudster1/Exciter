package csvio

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/trace"
)

// impactHeader heads the impact list of a requirement's trace graph (#211).
var impactHeader = []string{"Level", "Kind", "Name", "Title", "Type", "Status", "Reached by", "Marks"}

// WriteImpact writes everything a trace graph reaches, nearest first: where it is, what it is, the link that reached
// it and what is marked on it.
func WriteImpact(w io.Writer, g *trace.Graph) error {
	out := csv.NewWriter(w)
	if err := out.Write(impactHeader); err != nil {
		return err
	}
	byID := make(map[string]trace.GraphNode, len(g.Nodes))
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	for _, n := range g.Nodes {
		name, title := impactName(n)
		var kind, status string
		if n.Type != nil {
			kind = n.Type.Name
		}
		if n.Requirement != nil && n.Requirement.Status != nil {
			status = n.Requirement.Status.Name
		}
		if n.Test != nil {
			status = n.Test.Outcome
		}
		via, suspect := reachedBy(g, byID, n)
		record := []string{fmt.Sprint(n.Level), string(n.Kind), name, title, kind, status, via, strings.Join(marks(n, suspect), "; ")}
		for i := range record {
			record[i] = Cell(record[i])
		}
		if err := out.Write(record); err != nil {
			return err
		}
	}
	if g.Truncated {
		if err := out.Write([]string{fmt.Sprintf("This file holds the first %d of what the graph reaches. Follow fewer levels or link types to see the rest.",
			trace.MaxGraphNodes)}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// impactName is how a node is named and titled in the list.
func impactName(n trace.GraphNode) (name, title string) {
	switch {
	case n.Requirement != nil:
		return n.Requirement.Key, n.Requirement.Title
	case n.Page != nil:
		return columnHeading(trace.MatrixColumn{Page: n.Page}), n.Page.Title
	case n.Test != nil:
		return columnHeading(trace.MatrixColumn{Test: &trace.MatrixTest{Repository: n.Test.Repository, Suite: n.Test.Suite, Name: n.Test.Name}}), ""
	case n.Code != nil:
		return fmt.Sprintf("%s:%d", n.Code.Path, n.Code.Line), n.Code.Repository
	}
	return n.IssueKey, ""
}

// reachedBy names the link a node was reached by, from a node nearer the centre, and that node, and says whether
// that link is suspect.
func reachedBy(g *trace.Graph, byID map[string]trace.GraphNode, n trace.GraphNode) (string, bool) {
	if n.Level == 0 {
		return "", false
	}
	for _, e := range g.Edges {
		other := e.From
		if other == n.ID {
			other = e.To
		} else if e.To != n.ID {
			continue
		}
		from, ok := byID[other]
		if !ok || max(from.Level, -from.Level) >= max(n.Level, -n.Level) {
			continue
		}
		name, _ := impactName(from)
		via := e.TypeKey
		if via == "" {
			via = e.Relation
		}
		return via + " " + name, e.Suspect
	}
	return "", false
}

// marks are what the graph marks on a node, and suspect when the link that reached it is, as the graph's page marks
// them: a suspect link further on is marked on the node it reaches.
func marks(n trace.GraphNode, suspect bool) []string {
	var out []string
	if suspect {
		out = append(out, "suspect")
	}
	if n.Broken {
		out = append(out, "broken")
	}
	if n.Test != nil && (n.Test.Outcome == "failed" || n.Test.Outcome == "errored") {
		out = append(out, "failing")
	}
	if (n.Test != nil && n.Test.Outdated) || (n.Code != nil && n.Code.Outdated) {
		out = append(out, "outdated")
	}
	if len(n.Gaps) > 0 {
		gaps := make([]string, len(n.Gaps))
		for i, d := range n.Gaps {
			gaps[i] = string(d)
		}
		out = append(out, "gaps: "+strings.Join(gaps, ", "))
	}
	if n.Hidden > 0 {
		out = append(out, fmt.Sprintf("%d not visible", n.Hidden))
	}
	return out
}
