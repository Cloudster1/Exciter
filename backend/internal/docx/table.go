package docx

import (
	"github.com/cloudster1/exciter/backend/internal/document"
)

// tableStart opens a table of the given column widths at the frame's indent;
// a plain table draws no borders, as a layout of columns or pictures.
func (w *writer) tableStart(f frame, widths []int, bordered bool) {
	w.titledTable(f, widths, bordered, "")
}

// titledTable opens a table with a title for whoever cannot see it, by which a reader also tells it apart.
func (w *writer) titledTable(f frame, widths []int, bordered bool, title string) {
	total := 0
	for _, c := range widths {
		total += c
	}
	w.b.WriteString("<w:tbl><w:tblPr>")
	if bordered {
		w.b.WriteString(`<w:tblStyle w:val="ExciterTable"/>`)
	} else {
		w.b.WriteString(`<w:tblStyle w:val="ExciterLayout"/>`)
	}
	w.b.WriteString(`<w:tblW w:w="` + itoa(total) + `" w:type="dxa"/>`)
	if f.indent > 0 {
		w.b.WriteString(`<w:tblInd w:w="` + itoa(f.indent) + `" w:type="dxa"/>`)
	}
	w.b.WriteString(`<w:tblLayout w:type="fixed"/><w:tblLook w:val="0000" w:firstRow="0" w:lastRow="0" w:firstColumn="0" w:lastColumn="0" w:noHBand="1" w:noVBand="1"/>`)
	if title != "" {
		w.b.WriteString(`<w:tblCaption w:val="` + escape(title) + `"/>`)
	}
	w.b.WriteString(`</w:tblPr><w:tblGrid>`)
	for _, c := range widths {
		w.b.WriteString(`<w:gridCol w:w="` + itoa(c) + `"/>`)
	}
	w.b.WriteString("</w:tblGrid>")
}

// tableEnd closes a table with a small paragraph after it: Word would run the
// next block straight on from its border, and a cell has to end on a paragraph.
func (w *writer) tableEnd() {
	w.b.WriteString(`</w:tbl><w:p><w:pPr><w:pStyle w:val="Spacer"/></w:pPr></w:p>`)
}

// merge is how a cell takes part in a merge down its column.
type merge int

const (
	mergeNone merge = iota
	mergeStart
	mergeContinue
)

// cell writes one cell of width, spanning span columns, with a fill and a
// left border when given; a cell ends on a paragraph, as Word requires.
func (w *writer) cell(width, span int, border, fill string, content func(frame), outer frame, depth int) {
	w.cellMerged(width, span, mergeNone, border, fill, "", content, outer, depth)
}

func (w *writer) cellMerged(width, span int, m merge, border, fill, align string, content func(frame), _ frame, _ int) {
	w.b.WriteString(`<w:tc><w:tcPr><w:tcW w:w="` + itoa(width) + `" w:type="dxa"/>`)
	if span > 1 {
		w.b.WriteString(`<w:gridSpan w:val="` + itoa(span) + `"/>`)
	}
	switch m {
	case mergeStart:
		w.b.WriteString(`<w:vMerge w:val="restart"/>`)
	case mergeContinue:
		w.b.WriteString(`<w:vMerge/>`)
	}
	if border != "" {
		w.b.WriteString(`<w:tcBorders><w:left w:val="single" w:sz="24" w:space="0" w:color="` + border + `"/></w:tcBorders>`)
	}
	if fill != "" {
		w.b.WriteString(`<w:shd w:val="clear" w:color="auto" w:fill="` + fill + `"/>`)
	}
	w.b.WriteString("</w:tcPr>")
	inner := frame{width: max(width-2*cellMargin, minColumnWidth), list: 0, align: align}
	start := w.b.Len()
	if content != nil {
		content(inner)
	}
	if w.b.Len() == start {
		w.b.WriteString("<w:p/>")
	}
	w.b.WriteString("</w:tc>")
}

// gridCell is one place in a table's grid: the cell that starts there, or a
// continuation of the cell above it that spans down.
type gridCell struct {
	node  *document.Node
	col   int
	span  int
	merge merge
}

// grid lays a table's rows out on columns, as Word wants them: each cell at
// its column, and each place a cell above spans down into as a continuation.
func grid(table document.Node) ([][]gridCell, int) {
	type down struct{ left, span int }
	spans := map[int]down{}
	var rows [][]gridCell
	width := 0
	for _, row := range table.Content {
		var line []gridCell
		col := 0
		cover := func() {
			for {
				d, ok := spans[col]
				if !ok || d.left == 0 {
					return
				}
				line = append(line, gridCell{col: col, span: d.span, merge: mergeContinue})
				spans[col] = down{d.left - 1, d.span}
				col += d.span
			}
		}
		for i := range row.Content {
			cover()
			cell := &row.Content[i]
			colspan := min(max(intAttr(*cell, "colspan", 1), 1), document.MaxTableSpan)
			rowspan := min(max(intAttr(*cell, "rowspan", 1), 1), document.MaxTableSpan)
			m := mergeNone
			if rowspan > 1 {
				m = mergeStart
				spans[col] = down{rowspan - 1, colspan}
			}
			line = append(line, gridCell{node: cell, col: col, span: colspan, merge: m})
			col += colspan
		}
		// A short row still covers the places cells above it span down into.
		for {
			next := -1
			for k, d := range spans {
				if k >= col && d.left > 0 && (next < 0 || k < next) {
					next = k
				}
			}
			if next < 0 {
				break
			}
			col = next
			cover()
		}
		width = max(width, col)
		rows = append(rows, line)
	}
	return rows, width
}

// columnWidths are a table's column widths in twips: as stored where its
// first row says, the rest sharing what is left, all of it within the frame.
func columnWidths(table document.Node, columns, available int) []int {
	px := make([]int, columns)
	if len(table.Content) > 0 {
		col := 0
		for _, cell := range table.Content[0].Content {
			span := max(intAttr(cell, "colspan", 1), 1)
			for i, v := range intsAttr(cell, "colwidth") {
				if i < span && col+i < columns && v > 0 {
					px[col+i] = v
				}
			}
			col += span
		}
	}
	out := make([]int, columns)
	known, unknown := 0, 0
	for i, v := range px {
		out[i] = v * twipsPerPixel
		known += out[i]
		if v == 0 {
			unknown++
		}
	}
	if unknown > 0 {
		share := max((available-known)/unknown, minColumnWidth)
		for i := range out {
			if out[i] == 0 {
				out[i] = share
			}
		}
	}
	total := 0
	for _, v := range out {
		total += v
	}
	if total > available {
		for i := range out {
			out[i] = max(out[i]*available/total, 1)
		}
	}
	return out
}

// twipsPerPixel reads a width the browser drew at 96 pixels an inch.
const twipsPerPixel = 15

func (w *writer) table(n document.Node, f frame, depth int) {
	rows, columns := grid(n)
	if columns == 0 {
		return
	}
	widths := columnWidths(n, columns, f.width)
	w.tableStart(f, widths, true)
	for _, line := range rows {
		header := len(line) > 0
		for _, c := range line {
			if c.node != nil && c.node.Type != "tableHeader" {
				header = false
			}
		}
		w.b.WriteString("<w:tr>")
		if header {
			w.b.WriteString("<w:trPr><w:tblHeader/></w:trPr>")
		}
		col := 0
		for _, c := range line {
			for col < c.col {
				w.cell(widths[col], 1, "", "", nil, f, depth)
				col++
			}
			width := 0
			for i := c.col; i < min(c.col+c.span, columns); i++ {
				width += widths[i]
			}
			if c.node == nil {
				w.cellMerged(width, c.span, c.merge, "", "", "", nil, f, depth)
				col = c.col + c.span
				continue
			}
			cell := *c.node
			fill := ""
			if cell.Type == "tableHeader" {
				fill = neutralFill
			}
			w.cellMerged(width, c.span, c.merge, "", fill, stringAttr(cell, "align"), func(inner frame) {
				inner.bold = cell.Type == "tableHeader"
				w.blocks(cell.Content, inner, depth+1)
			}, f, depth)
			col = c.col + c.span
		}
		for ; col < columns; col++ {
			w.cell(widths[col], 1, "", "", nil, f, depth)
		}
		w.b.WriteString("</w:tr>")
	}
	w.tableEnd()
}

// boxStyle is how a box looks: its fill and the bar down its left side.
type boxStyle struct {
	fill   string
	border string
}

// box writes blocks inside a one-cell table: a quote, which may hold lists, tables and pictures.
func (w *writer) box(f frame, depth int, look boxStyle, content func(frame)) {
	if depth > document.MaxDepth {
		return
	}
	width := max(f.width, minColumnWidth)
	w.tableStart(f, []int{width}, false)
	w.b.WriteString("<w:tr>")
	w.cell(width, 1, look.border, look.fill, content, f, depth)
	w.b.WriteString("</w:tr>")
	w.tableEnd()
}

func intsAttr(n document.Node, name string) []int {
	list, _ := n.Attrs[name].([]any)
	out := make([]int, 0, len(list))
	for _, v := range list {
		switch x := v.(type) {
		case float64:
			out = append(out, int(x))
		case int:
			out = append(out, x)
		default:
			out = append(out, 0)
		}
	}
	return out
}
