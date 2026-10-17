package csvio

import (
	"fmt"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/trace"
	"github.com/cloudster1/exciter/backend/internal/xlsx"
)

// The matrix, the chain report and a baseline's matrix as a workbook's sheet (#154), holding what their CSV files
// hold, coloured: links green, suspect ones amber, gaps and breaks red, a baseline's changes by kind.

// MatrixSheet is a matrix as a sheet: the key, title and count of each row, a column per column under a line of their
// counts, a cell naming its links.
func MatrixSheet(name string, m *trace.Matrix) xlsx.Sheet {
	header := []xlsx.Cell{xlsx.Text("Key", xlsx.Header), xlsx.Text("Title", xlsx.Header), xlsx.Text(linksHeading, xlsx.Header)}
	counts := []xlsx.Cell{xlsx.Text("", xlsx.Header), xlsx.Text(linksHeading, xlsx.Header), xlsx.Text("", xlsx.Header)}
	for i, c := range m.Columns {
		header = append(header, xlsx.Text(columnHeading(c), xlsx.Header))
		counts = append(counts, countCell(m.ColumnLinks, i))
	}
	rows := [][]xlsx.Cell{header, counts}
	for i, r := range m.Rows {
		row := make([]xlsx.Cell, len(header))
		row[0], row[1], row[2] = xlsx.Text(r.Key, xlsx.Plain), xlsx.Text(r.Title, xlsx.Plain), countCell(m.RowLinks, i)
		rows = append(rows, row)
	}
	for _, cell := range m.Cells {
		names := make([]string, len(cell.Links))
		style := xlsx.Linked
		for i, l := range cell.Links {
			names[i] = l.Name
			if l.Suspect {
				names[i] += suspectMark
				style = xlsx.Suspect
			}
			if l.Outdated {
				names[i] += outdatedMark
				style = xlsx.Suspect
			}
		}
		rows[cell.Row+2][cell.Column+matrixLead] = xlsx.Text(strings.Join(names, "; "), style)
	}
	if len(m.Rows) < m.Total {
		rows = append(rows, []xlsx.Cell{xlsx.Text(fmt.Sprintf("This file holds the first %d of the %d requirements the filter keeps. Narrow the filter to export the rest.",
			len(m.Rows), m.Total), xlsx.Note)})
	}
	return xlsx.Sheet{Name: name, Rows: rows, FreezeRows: 2, FreezeColumns: 2, Filter: true}
}

// countCell is a row's or column's count of links, red where it is a gap, blank where the matrix gave none.
func countCell(counts []int, i int) xlsx.Cell {
	if i >= len(counts) {
		return xlsx.Text("", xlsx.Plain)
	}
	if counts[i] == 0 {
		return xlsx.Count(0, xlsx.Gap)
	}
	return xlsx.Count(counts[i], xlsx.Plain)
}

// ChainsSheet is a chain report as a sheet: a column per level, each line a path, the level after a broken line's
// last node red, and the level it stops at named last.
func ChainsSheet(name string, c *trace.Chains) xlsx.Sheet {
	header := make([]xlsx.Cell, 0, len(c.Levels)+1)
	for _, l := range c.Levels {
		header = append(header, xlsx.Text(l.Name, xlsx.Header))
	}
	rows := [][]xlsx.Cell{append(header, xlsx.Text(breaksHeading, xlsx.Header))}
	for _, line := range c.Lines {
		row := make([]xlsx.Cell, len(c.Levels)+1)
		for i, n := range line.Nodes {
			style := xlsx.Plain
			if n.Suspect {
				style = xlsx.Suspect
			}
			row[i] = xlsx.Text(nodeName(n), style)
		}
		if line.Broken {
			row[len(line.Nodes)] = xlsx.Text("", xlsx.Break)
			row[len(c.Levels)] = xlsx.Text(c.Levels[len(line.Nodes)-1].Name, xlsx.Break)
		}
		rows = append(rows, row)
	}
	if c.Truncated || len(c.Roots) < c.Total {
		rows = append(rows, []xlsx.Cell{xlsx.Text(fmt.Sprintf("This file holds the chains of the first %d of the %d requirements the filter keeps and at most %d lines. Narrow the filter to export the rest.",
			len(c.Roots), c.Total, MaxExportRows), xlsx.Note)})
	}
	return xlsx.Sheet{Name: name, Rows: rows, FreezeRows: 1, FreezeColumns: 1, Filter: true}
}

var changeStyles = map[baseline.Change]xlsx.Style{baseline.Added: xlsx.Added, baseline.Removed: xlsx.Removed, baseline.Changed: xlsx.Changed}

// BaselineMatrixSheet is a baseline's matrix as a sheet, a cell coloured by how its links changed when compared.
func BaselineMatrixSheet(name string, m *baseline.Matrix) xlsx.Sheet {
	header := []xlsx.Cell{xlsx.Text("Key", xlsx.Header), xlsx.Text("Title", xlsx.Header)}
	for _, c := range m.Columns {
		header = append(header, xlsx.Text(columnHeading(c), xlsx.Header))
	}
	rows := [][]xlsx.Cell{header}
	for _, r := range m.Rows {
		row := make([]xlsx.Cell, len(header))
		row[0], row[1] = xlsx.Text(r.Key, xlsx.Plain), xlsx.Text(r.Title, xlsx.Plain)
		rows = append(rows, row)
	}
	for _, cell := range m.Cells {
		names := make([]string, len(cell.Links))
		style := xlsx.Linked
		for i, l := range cell.Links {
			names[i] = l.Name
			if l.Suspect != nil && *l.Suspect {
				names[i] += suspectMark
				style = xlsx.Suspect
			}
			if l.Change != "" {
				names[i] += " (" + string(l.Change) + ")"
				style = changeStyles[l.Change]
			}
		}
		rows[cell.Row+1][cell.Column+2] = xlsx.Text(strings.Join(names, "; "), style)
	}
	if len(m.Rows) < m.Total {
		rows = append(rows, []xlsx.Cell{xlsx.Text(fmt.Sprintf("This file holds the first %d of the %d requirements the baseline holds.", len(m.Rows), m.Total), xlsx.Note)})
	}
	if note := SigningNote(m.Signing); note != "" {
		rows = append(rows, []xlsx.Cell{xlsx.Text(note, xlsx.Note)})
	}
	return xlsx.Sheet{Name: name, Rows: rows, FreezeRows: 1, FreezeColumns: 2, Filter: true}
}
