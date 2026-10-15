package docx

import (
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/document"
)

type blockWriter func(w *writer, n document.Node, f frame, depth int)

// blockWriters writes each block of the allowlist; a test holds it to the allowlist, so a new block cannot be left
// out of the export unnoticed.
var blockWriters map[string]blockWriter

// parts are the nodes their parent writes, never found on their own.
var parts = map[string]string{
	"doc":         "the document itself",
	"listItem":    "bulletList and orderedList",
	"tableRow":    "table",
	"tableCell":   "table",
	"tableHeader": "table",
}

func init() {
	blockWriters = map[string]blockWriter{
		"paragraph": func(w *writer, n document.Node, f frame, _ int) {
			w.para(f, paraProps{}, func() { w.inline(n.Content, f) })
		},
		"heading":        (*writer).headingBlock,
		"bulletList":     (*writer).list,
		"orderedList":    (*writer).list,
		"blockquote":     (*writer).quote,
		"codeBlock":      func(w *writer, n document.Node, f frame, _ int) { w.code(textOf(n), f) },
		"horizontalRule": (*writer).rule,
		"table":          (*writer).table,
		"image":          (*writer).imageBlock,
	}
}

// blocks writes a run of blocks into a frame.
func (w *writer) blocks(nodes []document.Node, f frame, depth int) {
	if depth > document.MaxDepth {
		return
	}
	for _, n := range nodes {
		if write, ok := blockWriters[n.Type]; ok {
			write(w, n, f, depth)
		}
	}
}

// headingBlock writes a heading inside a requirement's text, which stays out of Word's outline: the entries' own
// headings make that.
func (w *writer) headingBlock(n document.Node, f frame, _ int) {
	level := min(max(intAttr(n, "level", 1), 1), document.MaxHeadingLevel)
	w.para(f, paraProps{style: "TextHeading" + itoa(level), keepNext: true}, func() { w.inline(n.Content, f) })
}

// list writes a list's items, each item's first paragraph carrying its marker and the rest of it set in as far as
// the marker's words.
func (w *writer) list(n document.Node, f frame, depth int) {
	level := min(f.list, maxListLevel)
	num := bulletNum
	if n.Type == "orderedList" {
		num = w.orderedNum(stringAttr(n, "type"), intAttr(n, "start", 1))
	}
	inner := f
	inner.list = f.list + 1
	inner.indent = f.indent + listIndent
	inner.width = max(f.width-listIndent, minColumnWidth)
	for _, item := range n.Content {
		itemFrame := inner
		itemFrame.marker = &marker{num: num, level: level}
		if len(item.Content) == 0 || !carriesMarker(item.Content[0].Type) {
			w.para(itemFrame, paraProps{}, nil)
		}
		w.blocks(item.Content, itemFrame, depth+1)
	}
}

// carriesMarker says whether a block is a paragraph a list marker can sit on.
func carriesMarker(t string) bool { return t == "paragraph" || t == "heading" }

func (w *writer) code(source string, f frame) {
	f.marker = nil
	w.para(f, paraProps{style: "Code"}, func() { w.text(strings.TrimSuffix(source, "\n"), runProps{}) })
}

func (w *writer) rule(_ document.Node, f frame, _ int) {
	w.para(f, paraProps{border: `<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="` + ruleColor + `"/></w:pBdr>`}, nil)
}

// note writes a sentence where something cannot be carried into a document.
func (w *writer) note(f frame, sentence string) {
	w.para(f, paraProps{style: "Note"}, func() { w.text(sentence, runProps{}) })
}

func (w *writer) quote(n document.Node, f frame, depth int) {
	w.box(f, depth, boxStyle{border: quoteBorder}, func(inner frame) { w.blocks(n.Content, inner, depth+1) })
}

func textOf(n document.Node) string {
	var b strings.Builder
	for _, c := range n.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

func stringAttr(n document.Node, name string) string {
	s, _ := n.Attrs[name].(string)
	return s
}

func intAttr(n document.Node, name string, fallback int) int {
	switch v := n.Attrs[name].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
