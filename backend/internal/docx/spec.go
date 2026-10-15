package docx

import (
	"encoding/json"
	"encoding/xml"
	"strconv"

	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/specdoc"
)

// maxHeadingStyle is the deepest of Word's heading styles a document uses; deeper entries take it too.
const maxHeadingStyle = 6

// contentsDepth is how deep the contents list entries: sections and the two levels below them.
const contentsDepth = 3

// propertyShare is the share of a property table's width its names take, in percent.
const propertyShare = 30

// document is word/document.xml: the cover, the contents, then every entry.
func (w *writer) document(doc *specdoc.Document) ([]byte, error) {
	w.b.WriteString(xml.Header)
	w.b.WriteString(`<w:document xmlns:w="` + nsW + `" xmlns:r="` + nsR + `" xmlns:wp="` + nsWP + `" xmlns:a="` + nsA + `" xmlns:pic="` + nsPic + `"><w:body>`)
	root := frame{width: textWidth}
	w.cover(doc, root)
	w.contents(doc, root)
	for i, e := range doc.Entries {
		w.entry(i, e, root)
	}
	if doc.Truncated() {
		w.note(root, w.words.truncated(len(doc.Entries), doc.Total))
	}
	w.b.WriteString(`<w:sectPr><w:pgSz w:w="` + itoa(pageWidth) + `" w:h="` + itoa(pageHeight) + `"/>`)
	w.b.WriteString(`<w:pgMar w:top="` + itoa(margin) + `" w:right="` + itoa(margin) + `" w:bottom="` + itoa(margin) + `" w:left="` + itoa(margin) + `" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr>`)
	w.b.WriteString(`</w:body></w:document>`)
	if w.err != nil {
		return nil, w.err
	}
	return []byte(w.b.String()), nil
}

// cover is the first page: what the document is of, the baseline it was taken as, who exported it and when.
func (w *writer) cover(doc *specdoc.Document, f frame) {
	w.para(f, paraProps{style: "Title"}, func() { w.text(doc.Title, runProps{}) })
	w.para(f, paraProps{style: "Subtitle"}, func() { w.text(w.words.of(doc), runProps{}) })
	if b := doc.Baseline; b != nil {
		w.para(f, paraProps{style: "CoverLine"}, func() { w.text(w.words.taken(w.words.day(b.TakenAt), b.TakenBy), runProps{}) })
		w.para(f, paraProps{style: "CoverLine"}, func() {
			if b.LockedAt != nil {
				w.text(w.words.locked(w.words.day(*b.LockedAt)), runProps{bold: true})
			} else {
				w.text(w.words.unlocked, runProps{})
			}
		})
		if b.Description != "" {
			w.para(f, paraProps{style: "CoverLine"}, func() { w.text(b.Description, runProps{}) })
		}
	}
	w.para(f, paraProps{style: "CoverLine"}, func() { w.text(w.words.exported(doc.ExportedBy, w.words.day(doc.ExportedAt)), runProps{}) })
	w.para(f, paraProps{style: "CoverLine"}, func() { w.text(w.words.holds(len(doc.Entries)), runProps{}) })
	w.pageBreak()
}

// contents lists the entries down to contentsDepth, each a link to its heading.
func (w *writer) contents(doc *specdoc.Document, f frame) {
	w.para(f, paraProps{style: "TOCHeading"}, func() { w.text(w.words.contents, runProps{}) })
	for i, e := range doc.Entries {
		if e.Depth >= contentsDepth {
			continue
		}
		w.para(f, paraProps{style: "TOC" + itoa(e.Depth+1)}, func() {
			w.internal(entryBookmark(i), func() { w.text(e.Outline+"\t"+e.Title, runProps{}) })
		})
	}
	w.pageBreak()
}

func entryBookmark(i int) string { return "_Entry" + strconv.Itoa(i+1) }

func (w *writer) pageBreak() {
	w.b.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
}

// entry is one requirement or section: its heading, key and revision, its text, its table and its links.
func (w *writer) entry(i int, e specdoc.Entry, f frame) {
	level := min(e.Depth+1, maxHeadingStyle)
	w.para(f, paraProps{style: "Heading" + itoa(level)}, func() {
		id := itoa(w.bookmark)
		w.bookmark++
		w.b.WriteString(`<w:bookmarkStart w:id="` + id + `" w:name="` + entryBookmark(i) + `"/>`)
		w.text(e.Outline+" "+e.Title, runProps{})
		w.b.WriteString(`<w:bookmarkEnd w:id="` + id + `"/>`)
	})
	w.para(f, paraProps{style: "EntryMeta"}, func() { w.text(w.words.keyAndRevision(e.Key, e.Revision), runProps{}) })
	w.body(e.Statement, f)
	if len(e.Rationale) > 0 {
		w.para(f, paraProps{style: "Label", keepNext: true}, func() { w.text(w.words.rationale, runProps{}) })
		w.body(e.Rationale, f)
	}
	if len(e.Properties) > 0 {
		w.properties(e.Properties, f)
	}
	if len(e.Links) > 0 {
		w.para(f, paraProps{style: "Label", keepNext: true}, func() { w.text(w.words.links, runProps{}) })
		item := f
		item.indent = listIndent
		item.width = max(f.width-listIndent, minColumnWidth)
		for _, l := range e.Links {
			item.marker = &marker{num: bulletNum}
			w.para(item, paraProps{}, func() {
				w.text(l.Phrase+" ", runProps{})
				w.text(l.Key, runProps{bold: true})
				w.text(" "+l.Title, runProps{})
			})
		}
	}
}

// body writes a stored document; one that does not parse is left out rather than written half.
func (w *writer) body(raw json.RawMessage, f frame) {
	var root document.Node
	if len(raw) == 0 || json.Unmarshal(raw, &root) != nil {
		return
	}
	w.blocks(root.Content, f, 0)
}

// properties writes a requirement's fields and attributes as a table of two columns, names and values.
func (w *writer) properties(props []specdoc.Property, f frame) {
	names := f.width * propertyShare / 100
	widths := []int{names, f.width - names}
	w.tableStart(f, widths, true)
	for _, p := range props {
		w.b.WriteString("<w:tr>")
		w.cell(widths[0], 1, "", neutralFill, func(inner frame) {
			w.para(inner, paraProps{}, func() { w.text(w.words.property(p), runProps{bold: true}) })
		}, f, 0)
		w.cell(widths[1], 1, "", "", func(inner frame) {
			switch {
			case p.Document != nil:
				w.body(p.Document, inner)
			case p.Flag != nil:
				w.para(inner, paraProps{}, func() { w.text(w.words.flag(*p.Flag), runProps{}) })
			default:
				w.para(inner, paraProps{}, func() { w.text(w.words.value(p), runProps{}) })
			}
		}, f, 0)
		w.b.WriteString("</w:tr>")
	}
	w.tableEnd()
}
