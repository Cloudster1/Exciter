package csvio

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/baseline"
)

// WriteBaselineMatrix writes a baseline's matrix as CSV (#153): a row per requirement it holds, its key and title
// first, a column per column, each cell naming its links, a suspect one marked and, when compared, how each changed.
// SigningNote says where a baseline's signing stands, empty while nobody signed it.
func SigningNote(s *baseline.Signing) string {
	switch {
	case s == nil || s.Signatures == 0:
		return ""
	case s.Signed:
		return fmt.Sprintf("This baseline is signed, with %d of %d signatures, and never changes.", s.Signatures, s.Required)
	}
	return fmt.Sprintf("This baseline is being signed: %d of %d signatures so far. It never changes.", s.Signatures, s.Required)
}

func WriteBaselineMatrix(w io.Writer, m *baseline.Matrix) error {
	out := csv.NewWriter(w)
	header := []string{"Key", "Title"}
	for _, c := range m.Columns {
		header = append(header, Cell(columnHeading(c)))
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
			if l.Suspect != nil && *l.Suspect {
				names[i] += suspectMark
			}
			if l.Change != "" {
				names[i] += " (" + string(l.Change) + ")"
			}
		}
		lines[cell.Row][cell.Column+2] = Cell(strings.Join(names, "; "))
	}
	if err := out.WriteAll(lines); err != nil {
		return err
	}
	if len(m.Rows) < m.Total {
		if err := out.Write([]string{fmt.Sprintf("This file holds the first %d of the %d requirements the baseline holds.", len(m.Rows), m.Total)}); err != nil {
			return err
		}
	}
	if note := SigningNote(m.Signing); note != "" {
		if err := out.Write([]string{note}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}
