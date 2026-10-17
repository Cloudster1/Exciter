package csvio

import (
	"encoding/csv"
	"fmt"
	"io"

	"github.com/cloudster1/exciter/backend/internal/trace"
)

// breaksHeading heads the chain file's last column, the level a broken line stops at (#151).
const breaksHeading = "Breaks after"

// WriteChains writes a chain report as CSV: a column per level shown, each line a path down them, a node named with
// a suspect link marked, and a last column naming the level a broken line stops at.
func WriteChains(w io.Writer, c *trace.Chains) error {
	out := csv.NewWriter(w)
	header := make([]string, 0, len(c.Levels)+1)
	for _, l := range c.Levels {
		header = append(header, Cell(l.Name))
	}
	if err := out.Write(append(header, breaksHeading)); err != nil {
		return err
	}
	for _, line := range c.Lines {
		record := make([]string, len(c.Levels)+1)
		for i, n := range line.Nodes {
			record[i] = Cell(nodeName(n))
		}
		if line.Broken {
			record[len(c.Levels)] = Cell(c.Levels[len(line.Nodes)-1].Name)
		}
		if err := out.Write(record); err != nil {
			return err
		}
	}
	if c.Truncated || len(c.Roots) < c.Total {
		if err := out.Write([]string{fmt.Sprintf("This file holds the chains of the first %d of the %d requirements the filter keeps and at most %d lines. Narrow the filter to export the rest.",
			len(c.Roots), c.Total, MaxExportRows)}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// nodeName names what a line holds at one level: a requirement by its key and title, an issue by its key, a page as
// the matrix names it, and a suspect link that led there marked.
func nodeName(n trace.ChainNode) string {
	name := n.IssueKey
	switch {
	case n.Requirement != nil:
		name = n.Requirement.Key + " " + n.Requirement.Title
	case n.Page != nil:
		name = columnHeading(trace.MatrixColumn{Page: n.Page})
	case n.Code != nil:
		name = fmt.Sprintf("%s:%d", n.Code.Path, n.Code.Line)
	case n.Test != nil:
		name = columnHeading(trace.MatrixColumn{Test: &trace.MatrixTest{Repository: n.Test.Repository, Suite: n.Test.Suite, Name: n.Test.Name}}) +
			" " + n.Test.Outcome
	}
	if n.Suspect {
		name += suspectMark
	}
	return name
}
