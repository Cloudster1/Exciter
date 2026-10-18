package csvio

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/trace"
)

const (
	// suspectMark follows a link's name in a matrix cell while it waits to be checked.
	suspectMark = " (suspect)"
	// outdatedMark follows a test's outcome in a cell when the requirement was revised past its result (#200).
	outdatedMark = " (outdated)"
	// linksHeading heads the column, and the line, of how many links each row and column has (#148).
	linksHeading = "Links"
	// matrixLead is the key, title and count before a matrix file's first column.
	matrixLead = 3
)

// columnHeading names a column: a requirement by its key, an issue by its, a page by its title, or its space and id
// where Stator told the reader none.
func columnHeading(c trace.MatrixColumn) string {
	switch {
	case c.Requirement != nil:
		return c.Requirement.Key
	case c.Page != nil && c.Page.Title != "":
		return c.Page.Title
	case c.Page != nil:
		return c.Page.SpaceKey + "/" + c.Page.ID.String()
	case c.Test != nil && c.Test.Suite != "" && c.Test.Suite != c.Test.Name:
		return c.Test.Suite + " " + c.Test.Name
	case c.Test != nil:
		return c.Test.Name
	}
	return c.IssueKey
}

// countAt is the count of the i-th row or column, blank where the matrix gave none.
func countAt(counts []int, i int) string {
	if i >= len(counts) {
		return ""
	}
	return strconv.Itoa(counts[i])
}

// WriteMatrix writes a traceability matrix as CSV: a row per requirement, its key, title and count of links first,
// a column per requirement or issue under a line of their counts, each cell naming the links between them.
func WriteMatrix(w io.Writer, m *trace.Matrix) error {
	out := csv.NewWriter(w)
	header := []string{"Key", "Title", linksHeading}
	counts := []string{"", linksHeading, ""}
	for i, c := range m.Columns {
		header = append(header, Cell(columnHeading(c)))
		counts = append(counts, countAt(m.ColumnLinks, i))
	}
	if err := out.Write(header); err != nil {
		return err
	}
	if err := out.Write(counts); err != nil {
		return err
	}
	lines := make([][]string, len(m.Rows))
	for i, r := range m.Rows {
		lines[i] = make([]string, len(header))
		lines[i][0], lines[i][1], lines[i][2] = Cell(r.Key), Cell(r.Title), countAt(m.RowLinks, i)
	}
	for _, cell := range m.Cells {
		names := make([]string, len(cell.Links))
		for i, l := range cell.Links {
			names[i] = l.Name
			if l.Suspect {
				names[i] += suspectMark
			}
			if l.Outdated {
				names[i] += outdatedMark
			}
		}
		lines[cell.Row][cell.Column+matrixLead] = Cell(strings.Join(names, "; "))
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
