package csvio

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/trace"
)

// suspectMark follows a link's name in a matrix cell while it waits to be checked.
const suspectMark = " (suspect)"

// WriteMatrix writes a traceability matrix as CSV: a row per requirement, its key and title first, and a column
// per requirement or issue, each cell naming the links between them.
func WriteMatrix(w io.Writer, m *trace.Matrix) error {
	out := csv.NewWriter(w)
	header := []string{"Key", "Title"}
	for _, c := range m.Columns {
		if c.Requirement != nil {
			header = append(header, Cell(c.Requirement.Key))
		} else {
			header = append(header, Cell(c.IssueKey))
		}
	}
	if err := out.Write(header); err != nil {
		return err
	}
	lines := make([][]string, len(m.Rows))
	for i, r := range m.Rows {
		lines[i] = make([]string, len(header))
		lines[i][0], lines[i][1] = Cell(r.Key), Cell(r.Title)
	}
	for _, cell := range m.Cells {
		names := make([]string, len(cell.Links))
		for i, l := range cell.Links {
			names[i] = l.Name
			if l.Suspect {
				names[i] += suspectMark
			}
		}
		lines[cell.Row][cell.Column+2] = Cell(strings.Join(names, "; "))
	}
	if err := out.WriteAll(lines); err != nil {
		return err
	}
	if len(m.Rows) < m.Total {
		if err := out.Write([]string{fmt.Sprintf("This file holds the first %d of the %d requirements the filter keeps. Narrow the filter to export the rest.",
			len(m.Rows), m.Total)}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}
