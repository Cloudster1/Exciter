package markdown

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// Render writes a document as Markdown. What Markdown cannot hold, Holds says; Render writes it as well as it reads.
func Render(root document.Node) string {
	return renderer{}.blocks(root.Content, 0)
}

// Holds says whether Render's Markdown reads back as the same document, a table's column widths and an empty
// paragraph apart, so a reader is offered Markdown only where it says all there is.
func Holds(root document.Node) bool {
	return holds(root, 0)
}

func holds(n document.Node, depth int) bool {
	if depth > document.MaxDepth {
		return false
	}
	switch n.Type {
	case "orderedList":
		// Markdown numbers with digits alone.
		if t, ok := n.Attrs["type"].(string); ok && t != "1" {
			return false
		}
	case "table":
		for ri, row := range n.Content {
			for _, cell := range row.Content {
				// Markdown's one header row is the first; a span, or a cell of anything but one paragraph, has
				// no place in a row of one line each.
				if (cell.Type == "tableHeader") != (ri == 0) || intAttr(cell, "colspan", 1) != 1 || intAttr(cell, "rowspan", 1) != 1 ||
					len(cell.Content) > 1 || (len(cell.Content) == 1 && cell.Content[0].Type != "paragraph") {
					return false
				}
			}
		}
	case "text":
		// A link to a glossary term has no Markdown, and its words alone would read back unlinked.
		if hasMark(n.Marks, document.MarkTerm) {
			return false
		}
	case "paragraph", "heading":
		last := len(n.Content) - 1
		for i, c := range n.Content {
			// A break that ends a paragraph, or a line break inside text, has nothing after it to show.
			if (c.Type == "hardBreak" && i == last) || strings.ContainsAny(c.Text, "\n\r") {
				return false
			}
		}
	}
	for _, c := range n.Content {
		if !holds(c, depth+1) {
			return false
		}
	}
	return true
}

type renderer struct{}

// blocks renders a run of blocks, a blank line apart. Two lists of the same kind in a row would read as one list,
// so a comment keeps them apart.
func (r renderer) blocks(nodes []document.Node, depth int) string {
	var parts []string
	prev := ""
	for _, n := range nodes {
		out, ok := r.block(n, depth)
		if !ok {
			continue
		}
		if listFamily(prev) != "" && listFamily(prev) == listFamily(n.Type) {
			parts = append(parts, "<!-- -->")
		}
		parts = append(parts, out)
		prev = n.Type
	}
	return strings.Join(parts, "\n\n")
}

func listFamily(t string) string {
	switch t {
	case "bulletList":
		return "bullet"
	case "orderedList":
		return "ordered"
	}
	return ""
}

func (r renderer) block(n document.Node, depth int) (string, bool) {
	if depth > document.MaxDepth {
		return "", false
	}
	switch n.Type {
	case "paragraph":
		out := r.inline(n.Content, ctxBlock)
		return out, out != ""
	case "heading":
		return strings.Repeat("#", intAttr(n, "level", 1)) + " " + r.inline(n.Content, ctxHeading), true
	case "horizontalRule":
		return "---", true
	case "codeBlock":
		return codeFence(textOf(n), stringAttr(n, "language")), true
	case "blockquote":
		return quote(r.blocks(n.Content, depth+1)), true
	case "bulletList", "orderedList":
		return r.list(n, depth), true
	case document.NodeImage:
		return r.image(n), true
	case "table":
		return r.table(n, depth)
	}
	return "", false
}

func (r renderer) list(n document.Node, depth int) string {
	tight := true
	for _, item := range n.Content {
		paragraphs := 0
		for _, c := range item.Content {
			switch {
			case c.Type == "paragraph":
				paragraphs++
			case listFamily(c.Type) != "":
			default:
				tight = false
			}
		}
		if paragraphs > 1 {
			tight = false
		}
	}
	start := intAttr(n, "start", 1)
	items := make([]string, 0, len(n.Content))
	for i, item := range n.Content {
		marker := "-"
		if n.Type == "orderedList" {
			marker = strconv.Itoa(start+i) + "."
		}
		var inner string
		if tight {
			var b strings.Builder
			for _, c := range item.Content {
				out, ok := r.block(c, depth+1)
				if !ok {
					continue
				}
				// Only a list numbered from 1 interrupts a paragraph, so any other starts after a blank line, which
				// costs nothing: a document does not keep whether a list is loose.
				switch {
				case b.Len() == 0:
				case c.Type == "orderedList" && intAttr(c, "start", 1) != 1:
					b.WriteString("\n\n")
				default:
					b.WriteString("\n")
				}
				b.WriteString(out)
			}
			inner = b.String()
		} else {
			inner = r.blocks(item.Content, depth+1)
		}
		if inner == "" {
			items = append(items, marker)
			continue
		}
		items = append(items, prefixLines(inner, marker+" ", strings.Repeat(" ", len(marker)+1)))
	}
	if tight {
		return strings.Join(items, "\n")
	}
	return strings.Join(items, "\n\n")
}

// image writes a picture of the requirement's, as an img element when it has a width Markdown has no syntax for.
func (r renderer) image(n document.Node) string {
	alt, dest := stringAttr(n, "alt"), FileScheme+stringAttr(n, "attachmentId")
	if width := intAttr(n, "width", 0); width > 0 {
		return fmt.Sprintf(`<img src="%s" alt="%s" width="%d">`, htmlText(dest), htmlText(alt), width)
	}
	return "![" + escape(alt, escText, false) + "](" + dest + ")"
}

func (r renderer) table(n document.Node, depth int) (string, bool) {
	var grid [][]string
	var aligns []string
	spans := map[int]int{}
	for ri, row := range n.Content {
		var line []string
		col := 0
		next := func() {
			for spans[col] > 0 {
				spans[col]--
				line = append(line, "")
				col++
			}
		}
		for _, cell := range row.Content {
			next()
			colspan := max(intAttr(cell, "colspan", 1), 1)
			rowspan := max(intAttr(cell, "rowspan", 1), 1)
			if ri == 0 {
				for len(aligns) < col+colspan {
					aligns = append(aligns, "")
				}
				aligns[col] = stringAttr(cell, "align")
			}
			line = append(line, r.cell(cell.Content, depth+1))
			for i := range colspan {
				if i > 0 {
					line = append(line, "")
				}
				if rowspan > 1 {
					spans[col] = rowspan - 1
				}
				col++
			}
		}
		next()
		grid = append(grid, line)
	}
	width := 0
	for _, line := range grid {
		width = max(width, len(line))
	}
	if width == 0 {
		return "", false
	}
	var b strings.Builder
	for i, line := range grid {
		for len(line) < width {
			line = append(line, "")
		}
		b.WriteString("|")
		for _, cell := range line {
			b.WriteString(" " + cell + " |")
		}
		b.WriteString("\n")
		if i == 0 {
			b.WriteString("|")
			for c := range width {
				align := ""
				if c < len(aligns) {
					align = aligns[c]
				}
				switch align {
				case "left":
					b.WriteString(" :-- |")
				case "center":
					b.WriteString(" :-: |")
				case "right":
					b.WriteString(" --: |")
				default:
					b.WriteString(" --- |")
				}
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimSuffix(b.String(), "\n"), true
}

// cell writes a cell's blocks on one line, joined by line breaks; any block but text keeps only its words.
func (r renderer) cell(nodes []document.Node, depth int) string {
	var parts []string
	for _, c := range nodes {
		var out string
		switch c.Type {
		case "paragraph", "heading":
			out = r.inline(c.Content, ctxTable)
		default:
			if depth > document.MaxDepth {
				continue
			}
			words := document.PlainText(document.Node{Type: "doc", Content: []document.Node{c}})
			lines := strings.Split(words, "\n")
			for i, line := range lines {
				lines[i] = escape(line, escTable, false)
			}
			out = strings.Join(lines, "<br>")
		}
		if out != "" {
			parts = append(parts, out)
		}
	}
	return strings.Join(parts, "<br>")
}

// The contexts inline content is written in: a block of its own lines, a heading's one line, or a table cell's.
type inlineCtx int

const (
	ctxBlock inlineCtx = iota
	ctxHeading
	ctxTable
)

// escMode is which characters a piece of text has to keep from Markdown.
type escMode int

const (
	escText escMode = iota
	escHeading
	escTable
)

func (c inlineCtx) esc() escMode {
	switch c {
	case ctxHeading:
		return escHeading
	case ctxTable:
		return escTable
	}
	return escText
}

// inline writes inline nodes with their marks, opened in markOrder and kept open across nodes that share them, so
// bold over a mention is one run.
func (r renderer) inline(nodes []document.Node, ctx inlineCtx) string {
	// A closing asterisk after a space does not close, so spaces at the edge of a styled run move outside it.
	nodes = splitEdges(nodes)
	var b strings.Builder
	var open []document.Mark
	atStart := true
	closeTo := func(keep int) {
		for i := len(open) - 1; i >= keep; i-- {
			b.WriteString(closer(open[i]))
		}
		open = open[:keep]
	}
	for _, n := range nodes {
		marks := styleMarks(n.Marks)
		var code bool
		if last := len(marks) - 1; last >= 0 && marks[last].Type == "code" {
			code = true
			marks = marks[:last]
		}
		keep := 0
		for keep < len(open) && keep < len(marks) && sameMark(open[keep], marks[keep]) {
			keep++
		}
		closeTo(keep)
		for _, m := range marks[keep:] {
			b.WriteString(opener(m))
			open = append(open, m)
			atStart = false
		}
		switch {
		case n.Type == "text" && code:
			b.WriteString(codeSpan(n.Text))
			atStart = false
		case n.Type == "text":
			text := strings.ReplaceAll(n.Text, "\n", " ")
			b.WriteString(escape(text, ctx.esc(), atStart))
			if text != "" {
				atStart = false
			}
		case n.Type == "hardBreak":
			if ctx == ctxBlock {
				b.WriteString("\\\n")
				atStart = true
			} else {
				b.WriteString("<br>")
			}
		default:
			b.WriteString(atom(n, ctx))
			atStart = false
		}
	}
	closeTo(0)
	return b.String()
}

// splitEdges moves the spaces at either end of styled text into text of their own that carries only the link and
// code marks.
func splitEdges(nodes []document.Node) []document.Node {
	out := make([]document.Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Type != "text" || !hasEmphasis(n.Marks) || hasMark(n.Marks, "code") {
			out = append(out, n)
			continue
		}
		core := strings.TrimRightFunc(strings.TrimLeftFunc(n.Text, unicode.IsSpace), unicode.IsSpace)
		if core == "" {
			n.Marks = withoutEmphasis(n.Marks)
			out = append(out, n)
			continue
		}
		lead := n.Text[:strings.Index(n.Text, core)]
		trail := n.Text[len(lead)+len(core):]
		if lead != "" {
			out = append(out, document.Node{Type: "text", Text: lead, Marks: withoutEmphasis(n.Marks)})
		}
		mid := n
		mid.Text = core
		out = append(out, mid)
		if trail != "" {
			out = append(out, document.Node{Type: "text", Text: trail, Marks: withoutEmphasis(n.Marks)})
		}
	}
	return out
}

func hasEmphasis(marks []document.Mark) bool {
	return hasMark(marks, "bold") || hasMark(marks, "italic") || hasMark(marks, "strike")
}

func hasMark(marks []document.Mark, t string) bool {
	return slices.ContainsFunc(marks, func(m document.Mark) bool { return m.Type == t })
}

func withoutEmphasis(marks []document.Mark) []document.Mark {
	var out []document.Mark
	for _, m := range marks {
		if m.Type != "bold" && m.Type != "italic" && m.Type != "strike" {
			out = append(out, m)
		}
	}
	return out
}

// styleMarks keeps the marks Markdown writes, in markOrder.
func styleMarks(marks []document.Mark) []document.Mark {
	var out []document.Mark
	for _, m := range marks {
		if markRank(m.Type) >= 0 {
			out = append(out, m)
		}
	}
	slices.SortStableFunc(out, func(a, b document.Mark) int { return markRank(a.Type) - markRank(b.Type) })
	return out
}

func sameMark(a, b document.Mark) bool {
	if a.Type != b.Type {
		return false
	}
	if a.Type != "link" {
		return true
	}
	return a.Attrs["href"] == b.Attrs["href"] && a.Attrs["title"] == b.Attrs["title"]
}

func opener(m document.Mark) string {
	switch m.Type {
	case "link":
		return "["
	case "bold":
		return "**"
	case "italic":
		return "*"
	case "strike":
		return "~~"
	}
	return ""
}

func closer(m document.Mark) string {
	switch m.Type {
	case "link":
		href, _ := m.Attrs["href"].(string)
		out := "](" + destination(href)
		if title, _ := m.Attrs["title"].(string); title != "" {
			out += ` "` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(title) + `"`
		}
		return out + ")"
	case "bold":
		return "**"
	case "italic":
		return "*"
	case "strike":
		return "~~"
	}
	return ""
}

// atom writes an inline node that is not text: a file as a link to it, a mention as a span whose words read as
// they do on the page.
func atom(n document.Node, ctx inlineCtx) string {
	esc := ctx.esc()
	switch n.Type {
	case document.NodeAttachment:
		return "[" + escape(stringAttr(n, "fileName"), esc, false) + "](" + FileScheme + stringAttr(n, "attachmentId") + ")"
	case "mention":
		return span(kindMention, [][2]string{{"data-id", stringAttr(n, "id")}}, escape("@"+stringAttr(n, "label"), esc, false))
	}
	return ""
}
