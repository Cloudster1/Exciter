package docx

import "github.com/cloudster1/exciter/backend/internal/document"

type inlineWriter func(w *writer, n document.Node, rp runProps)

// inlineWriters writes each inline node of the allowlist, held to it by a test as blockWriters is.
var inlineWriters map[string]inlineWriter

// markStyles are the marks that style a run; a link is written around its runs.
var markStyles = map[string]func(*runProps){
	"bold":   func(rp *runProps) { rp.bold = true },
	"italic": func(rp *runProps) { rp.italic = true },
	"strike": func(rp *runProps) { rp.strike = true },
	"code":   func(rp *runProps) { rp.style = "InlineCode" },
	"link":   func(rp *runProps) { rp.style = "Hyperlink" },
}

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

// inline writes a block's inline content, its marks as run properties and each run of the same link as one
// hyperlink.
func (w *writer) inline(nodes []document.Node, f frame) {
	for i := 0; i < len(nodes); {
		href := linkOf(nodes[i])
		j := i + 1
		for j < len(nodes) && href != "" && linkOf(nodes[j]) == href {
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
		if href != "" {
			w.hyperlink(href, write)
		} else {
			write()
		}
		i = j
	}
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
