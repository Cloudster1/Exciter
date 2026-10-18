package docx

import (
	"slices"
	"strconv"
	"strings"
)

// table reads a table into its cells on the table's grid, merges kept as spans; a table of one cell is a box,
// read as a quote.
func (r *reader) table(n *node) {
	var b Block
	if pr := n.child("tblPr"); pr != nil {
		if c := pr.child("tblCaption"); c != nil {
			b.TableTitle = c.attr("val")
		}
	}
	type placed struct {
		row, at int
	}
	var (
		rows    [][]Cell
		header  []bool
		cellsOf = map[[2]int]placed{}
	)
	for _, tr := range flatten(n, "tr") {
		var row []Cell
		col := 0
		isHeader := false
		if pr := tr.child("trPr"); pr != nil {
			isHeader = on(pr.child("tblHeader"))
			if before := pr.child("gridBefore"); before != nil {
				if v, err := strconv.Atoi(before.attr("val")); err == nil && v > 0 {
					col += v
				}
			}
		}
		for _, tc := range flatten(tr, "tc") {
			span, merge := 1, ""
			if pr := tc.child("tcPr"); pr != nil {
				if g := pr.child("gridSpan"); g != nil {
					if v, err := strconv.Atoi(g.attr("val")); err == nil && v > 1 {
						span = v
					}
				}
				if v := pr.child("vMerge"); v != nil {
					merge = "continue"
					if v.attr("val") == "restart" {
						merge = "restart"
					}
				}
			}
			if merge == "continue" {
				if above, ok := cellsOf[[2]int{len(rows) - 1, col}]; ok {
					rows[above.row][above.at].RowSpan = max(rows[above.row][above.at].RowSpan, 1) + 1
					cellsOf[[2]int{len(rows), col}] = above
					col += span
					continue
				}
			}
			cell := r.cell(tc, &b)
			cell.Col = col
			if span > 1 {
				cell.ColSpan = span
			}
			cellsOf[[2]int{len(rows), col}] = placed{row: len(rows), at: len(row)}
			row = append(row, cell)
			col += span
		}
		rows = append(rows, row)
		header = append(header, isHeader)
	}
	if len(rows) == 0 {
		return
	}
	if len(rows) == 1 && len(rows[0]) == 1 {
		r.paragraphs++
		c := rows[0][0]
		box := Block{Kind: BlockParagraph, Index: r.paragraphs, ParaID: c.ParaID, Text: c.Text, Losses: b.Losses,
			Rich: &XNode{Children: []XNode{{Name: "blockquote", Children: c.Rich.Children}}}}
		if strings.TrimSpace(box.Text) != "" || len(c.Rich.Children) > 0 {
			r.src.Blocks = append(r.src.Blocks, box)
		}
		return
	}
	r.src.Tables++
	b.Kind, b.Table, b.Rows = BlockTable, r.src.Tables, rows
	for b.HeaderRows < len(header) && header[b.HeaderRows] {
		b.HeaderRows++
	}
	table := XNode{Name: "table"}
	var lines []string
	for i, row := range rows {
		tr := XNode{Name: "tr"}
		var texts []string
		for _, c := range row {
			x := *c.Rich
			x.Name = "td"
			if i < b.HeaderRows {
				x.Name = "th"
			}
			for attr, v := range map[string]int{"colspan": c.ColSpan, "rowspan": c.RowSpan} {
				if v > 1 {
					if x.Attrs == nil {
						x.Attrs = map[string]string{}
					}
					x.Attrs[attr] = strconv.Itoa(v)
				}
			}
			tr.Children = append(tr.Children, x)
			texts = append(texts, c.Text)
		}
		table.Children = append(table.Children, tr)
		lines = append(lines, strings.Join(texts, " | "))
	}
	b.Rich = &XNode{Children: []XNode{table}}
	b.Text = strings.Join(lines, "\n")
	if len(rows[0]) > 0 {
		b.ParaID = rows[0][0].ParaID
	}
	r.src.Blocks = append(r.src.Blocks, b)
}

// flatten finds the elements of a local name directly inside n, through the content controls and markup around them.
func flatten(n *node, local string) []*node {
	var out []*node
	for _, k := range n.kids {
		switch k.local() {
		case local:
			out = append(out, k)
		case "sdt":
			if content := k.child("sdtContent"); content != nil {
				out = append(out, flatten(content, local)...)
			}
		case "customXml", "ins", "moveTo", "smartTag":
			out = append(out, flatten(k, local)...)
		}
	}
	return out
}

// cell reads a cell's paragraphs; a table inside it is kept as its words.
func (r *reader) cell(tc *node, b *Block) Cell {
	c := Cell{Rich: &XNode{}}
	var texts []string
	var visit func(*node)
	visit = func(n *node) {
		for _, k := range n.kids {
			switch k.local() {
			case "p":
				p := r.readParagraph(k)
				if c.ParaID == "" {
					c.ParaID = p.paraID
				}
				text := strings.TrimSpace(p.text.String())
				if ref := r.numberOf(p); ref != nil {
					if label, lv, ok := r.numbered(ref); ok && lv.format != "bullet" && label != "" && text != "" {
						text = strings.TrimSpace(label) + " " + text
						p.rich = append([]XNode{{Text: strings.TrimSpace(label) + " "}}, p.rich...)
					}
				}
				for _, l := range p.losses {
					addLoss(b, l)
				}
				if text != "" || p.pictures > 0 {
					texts = append(texts, text)
					c.Rich.Children = append(c.Rich.Children, splitObjects(p.rich, "p")...)
				}
			case "tbl":
				addLoss(b, Loss{Kind: LossFormatting, Message: "A table inside a table is kept as its words."})
				if words := strings.Join(strings.Fields(nestedWords(k)), " "); words != "" {
					texts = append(texts, words)
					c.Rich.Children = append(c.Rich.Children, XNode{Name: "p", Children: []XNode{{Text: words}}})
				}
			case "sdt":
				if content := k.child("sdtContent"); content != nil {
					visit(content)
				}
			case "customXml", "ins", "moveTo", "smartTag":
				visit(k)
			}
		}
	}
	visit(tc)
	c.Text = strings.Join(texts, "\n")
	return c
}

// nestedWords are a nested table's words, a space between paragraphs.
func nestedWords(n *node) string {
	var b strings.Builder
	var walk func(*node)
	walk = func(x *node) {
		if x.local() == "t" {
			b.WriteString(x.words())
		}
		for _, k := range x.kids {
			walk(k)
		}
		if x.local() == "p" {
			b.WriteString(" ")
		}
	}
	walk(n)
	return b.String()
}

func addLoss(b *Block, l Loss) {
	if !slices.ContainsFunc(b.Losses, func(have Loss) bool { return have.Message == l.Message }) {
		b.Losses = append(b.Losses, l)
	}
}
