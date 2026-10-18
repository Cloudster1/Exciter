package docx

import (
	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

type inlineWriter func(w *writer, n document.Node, rp runProps)

// inlineWriters writes each inline node of the allowlist, held to it by a test as blockWriters is.
var inlineWriters map[string]inlineWriter

// markStyles are the marks that style a run; a link, and a term's link to the glossary, is written around its
// runs.
var markStyles = map[string]func(*runProps){
	"bold":   func(rp *runProps) { rp.bold = true },
	"italic": func(rp *runProps) { rp.italic = true },
	"strike": func(rp *runProps) { rp.strike = true },
	"code":   func(rp *runProps) { rp.style = "InlineCode" },
	"link":   func(rp *runProps) { rp.style = "Hyperlink" },
	// A term reads as the words around it.
	document.MarkTerm: func(*runProps) {},
	// Changes since a baseline are tracked changes (#214).
	requirement.MarkDiffInsert: func(rp *runProps) { rp.change = changeInserted },
	requirement.MarkDiffDelete: func(rp *runProps) { rp.change = changeDeleted },
}

// The elements Word wraps an inserted and a deleted run in.
const (
	changeInserted = "ins"
	changeDeleted  = "del"
)

func init() {
	inlineWriters = map[string]inlineWriter{
		"text":      func(w *writer, n document.Node, rp runProps) { w.text(n.Text, rp) },
		"hardBreak": func(w *writer, _ document.Node, rp runProps) { w.text("\n", rp) },
		"mention": func(w *writer, n document.Node, rp runProps) {
			label := stringAttr(n, "label")
			if label == "" {
				label = w.words.somebody
			}
			rp.color = accentText
			w.text("@"+label, rp)
		},
		// A file the text names is its name: the document travels where the file does not.
		document.NodeAttachment: func(w *writer, n document.Node, rp runProps) { w.text(stringAttr(n, "fileName"), rp) },
	}
}

// inline writes a block's inline content, its marks as run properties and each run of the same link, or of the
// same term's link to the glossary, as one hyperlink.
func (w *writer) inline(nodes []document.Node, f frame) {
	for i := 0; i < len(nodes); {
		href := linkOf(nodes[i])
		anchor := ""
		if href == "" {
			anchor = w.termBookmark(nodes[i])
		}
		j := i + 1
		for j < len(nodes) && ((href != "" && linkOf(nodes[j]) == href) ||
			(anchor != "" && linkOf(nodes[j]) == "" && w.termBookmark(nodes[j]) == anchor)) {
			j++
		}
		run := nodes[i:j]
		write := func() {
			for _, n := range run {
				write, ok := inlineWriters[n.Type]
				if !ok {
					continue
				}
				rp := runProps{bold: f.bold}
				for _, m := range n.Marks {
					if style, ok := markStyles[m.Type]; ok {
						style(&rp)
					}
				}
				if hasMark(n, "code") && hasMark(n, "link") {
					rp.style = "Hyperlink"
				}
				write(w, n, rp)
			}
		}
		switch {
		case href != "":
			w.hyperlink(href, write)
		case anchor != "":
			w.internal(anchor, write)
		default:
			write()
		}
		i = j
	}
}

// termBookmark is where in the document the term a node is linked to is defined, or "" for none.
func (w *writer) termBookmark(n document.Node) string {
	if w.termAnchor == nil {
		return ""
	}
	for _, m := range n.Marks {
		if m.Type == document.MarkTerm {
			id, _ := m.Attrs["termId"].(string)
			if bookmark, ok := w.termAnchor(id); ok {
				return bookmark
			}
		}
	}
	return ""
}

func linkOf(n document.Node) string {
	for _, m := range n.Marks {
		if m.Type == "link" {
			href, _ := m.Attrs["href"].(string)
			return href
		}
	}
	return ""
}

func hasMark(n document.Node, t string) bool {
	for _, m := range n.Marks {
		if m.Type == t {
			return true
		}
	}
	return false
}
