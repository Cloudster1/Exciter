package csvio

import (
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	risks "github.com/cloudster1/exciter/backend/internal/risk"
	"github.com/cloudster1/exciter/backend/internal/xlsx"
)

// The FMEA table as a workbook (#260): a row a mitigation, the risk's columns on its first row, each level filled in
// its colour.

// RisksSheet lays out the risks with the attributes that show on their types, before, mitigations, after and in
// effect.
func RisksSheet(name string, rows []risks.Row, defs []attribute.Def, people map[uuid.UUID]string) xlsx.Sheet {
	var shown []attribute.Def
	for _, d := range defs {
		if d.Retired {
			continue
		}
		if slices.ContainsFunc(rows, func(r risks.Row) bool { return len(d.TypeIDs) == 0 || slices.Contains(d.TypeIDs, r.Risk.Type.ID) }) {
			shown = append(shown, d)
		}
	}
	header := []string{"Key", "Title", "Type"}
	for _, d := range shown {
		header = append(header, d.Name)
	}
	header = append(header, "Severity", "Probability", "Detection", "RPN", "Level",
		"Mitigated by", "Implemented", "Verified", "In effect",
		"Severity after", "Probability after", "Detection after", "RPN after", "Level after", "State", "Level in effect")
	cells := make([]xlsx.Cell, len(header))
	for i, h := range header {
		cells[i] = xlsx.Text(h, xlsx.Header)
	}
	out := [][]xlsx.Cell{cells}
	for _, r := range rows {
		mitigations := r.Mitigations
		if len(mitigations) == 0 {
			mitigations = []risks.Mitigation{{}}
		}
		for i, m := range mitigations {
			var row []xlsx.Cell
			if i == 0 {
				row = append(row, xlsx.Text(r.Risk.Key, xlsx.Plain), xlsx.Text(r.Risk.Title, xlsx.Plain), xlsx.Text(r.Risk.Type.Name, xlsx.Plain))
				for _, d := range shown {
					row = append(row, xlsx.Text(attributeText(d, r.Attributes[d.ID.String()], people), xlsx.Plain))
				}
				row = append(row, sideCells(r.Rating.Before, r.Rating.BeforeRPN, r.Rating.BeforeLevel)...)
			} else {
				row = make([]xlsx.Cell, 3+len(shown)+5)
			}
			row = append(row, mitigationCells(m)...)
			if i == 0 {
				row = append(row, sideCells(r.Rating.After, r.Rating.AfterRPN, r.Rating.AfterLevel)...)
				state := ""
				if r.Rating.State != nil {
					state = string(*r.Rating.State)
				}
				row = append(row, xlsx.Text(state, xlsx.Plain), levelCell(r.Rating.Level))
			}
			out = append(out, row)
		}
	}
	return xlsx.Sheet{Name: name, Rows: out, FreezeRows: 1, FreezeColumns: 2, Filter: true}
}

func sideCells(s requirement.Side, rpn *int, level *requirement.RiskLevel) []xlsx.Cell {
	number := func(n *int) xlsx.Cell {
		if n == nil {
			return xlsx.Text("", xlsx.Plain)
		}
		return xlsx.Count(*n, xlsx.Plain)
	}
	return []xlsx.Cell{number(s.Severity), number(s.Probability), number(s.Detection), number(rpn), levelCell(level)}
}

// levelCell is a level by name, filled in its colour.
func levelCell(level *requirement.RiskLevel) xlsx.Cell {
	if level == nil {
		return xlsx.Text("", xlsx.Plain)
	}
	return xlsx.Cell{Text: level.Name, Fill: strings.ToUpper(strings.TrimPrefix(level.Colour, "#"))}
}

// HiddenMitigation names a mitigating requirement the reader may not view, as the FMEA table and the report do.
const HiddenMitigation = "A requirement you may not view"

func mitigationCells(m risks.Mitigation) []xlsx.Cell {
	if m.Source.Key == "" && !m.Source.Hidden {
		return []xlsx.Cell{xlsx.Text("", xlsx.Plain), xlsx.Text("", xlsx.Plain), xlsx.Text("", xlsx.Plain), xlsx.Text("", xlsx.Plain)}
	}
	style := xlsx.Gap
	if m.InEffect {
		style = xlsx.Linked
	}
	source := m.Source.Key + " " + m.Source.Title
	if m.Source.Hidden {
		source = HiddenMitigation
	}
	return []xlsx.Cell{xlsx.Text(source, xlsx.Plain), xlsx.Text(string(m.Implemented), xlsx.Plain),
		xlsx.Text(string(m.Verified), xlsx.Plain), xlsx.Text(strconv.FormatBool(m.InEffect), style)}
}
