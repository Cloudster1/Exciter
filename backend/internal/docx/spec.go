package docx

import (
	"encoding/json"
	"encoding/xml"
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/requirement"
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
	root := frame{width: w.pageText()}
	w.linkTerms(doc.Glossary)
	if w.layout == nil {
		w.b.WriteString(xml.Header)
		w.b.WriteString(`<w:document xmlns:w="` + nsW + `" xmlns:r="` + nsR + `" xmlns:wp="` + nsWP + `" xmlns:a="` + nsA + `" xmlns:pic="` + nsPic + `"><w:body>`)
		w.cover(doc, root)
	}
	w.contents(doc, root)
	for i, e := range doc.Entries {
		w.entry(i, e, root)
	}
	if doc.Truncated() {
		w.note(root, w.words.truncated(len(doc.Entries), doc.Total))
	}
	w.glossary(doc.Glossary, root)
	if w.err != nil {
		return nil, w.err
	}
	if w.layout != nil {
		return w.framed(w.b.String()), nil
	}
	w.b.WriteString(defaultSectPr())
	w.b.WriteString(`</w:body></w:document>`)
	return []byte(w.b.String()), nil
}

// defaultSectPr is the export's own page: A4 with even margins.
func defaultSectPr() string {
	return `<w:sectPr><w:pgSz w:w="` + itoa(pageWidth) + `" w:h="` + itoa(pageHeight) + `"/>` +
		`<w:pgMar w:top="` + itoa(margin) + `" w:right="` + itoa(margin) + `" w:bottom="` + itoa(margin) + `" w:left="` + itoa(margin) + `" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr>`
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
		if b.Signatures > 0 {
			w.para(f, paraProps{style: "CoverLine"}, func() {
				w.text(w.words.signing(b.Signed, b.Signatures, b.SignaturesRequired), runProps{bold: b.Signed})
			})
		}
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
	if len(doc.Glossary) > 0 {
		w.para(f, paraProps{style: "TOC1"}, func() {
			w.internal(glossaryBookmark, func() { w.text(w.words.glossary, runProps{}) })
		})
	}
	w.pageBreak()
}

// glossaryBookmark is where the glossary chapter starts, hidden as the entries' bookmarks are.
const glossaryBookmark = "_Glossary"

// termBookmarkOf names a term's entry in the glossary chapter: a letter first and at most 40 characters, as Word
// wants a bookmark's name.
func termBookmarkOf(id string) string { return "term_" + strings.ReplaceAll(id, "-", "") }

// linkTerms makes words linked to a term of the glossary a link to its entry; any other term's words stay plain.
func (w *writer) linkTerms(terms []specdoc.GlossaryTerm) {
	if len(terms) == 0 {
		return
	}
	defined := make(map[string]bool, len(terms))
	for _, t := range terms {
		defined[t.ID.String()] = true
	}
	w.termAnchor = func(termID string) (string, bool) {
		if !defined[strings.ToLower(termID)] {
			return "", false
		}
		return termBookmarkOf(strings.ToLower(termID)), true
	}
}

// glossary is the chapter after the entries: each term by name, its definition, and its words in a muted line.
func (w *writer) glossary(terms []specdoc.GlossaryTerm, f frame) {
	if len(terms) == 0 {
		return
	}
	w.pageBreak()
	w.para(f, paraProps{style: "Heading1"}, func() { w.bookmarked(glossaryBookmark, func() { w.text(w.words.glossary, runProps{}) }) })
	for _, t := range terms {
		w.para(f, paraProps{style: "TermName"}, func() {
			w.bookmarked(termBookmarkOf(t.ID.String()), func() { w.text(t.Name, runProps{bold: true}) })
		})
		w.body(t.Definition, f)
		if len(t.Synonyms) > 0 {
			w.para(f, paraProps{style: "TermWords"}, func() { w.text(w.words.synonyms+": "+strings.Join(t.Synonyms, ", "), runProps{}) })
		}
		if len(t.Avoid) > 0 {
			w.para(f, paraProps{style: "TermWords"}, func() { w.text(w.words.avoid+": "+strings.Join(t.Avoid, ", "), runProps{}) })
		}
	}
}

// bookmarked writes runs inside a bookmark, which internal links name.
func (w *writer) bookmarked(name string, runs func()) {
	id := itoa(w.bookmark)
	w.bookmark++
	w.b.WriteString(`<w:bookmarkStart w:id="` + id + `" w:name="` + name + `"/>`)
	runs()
	w.b.WriteString(`<w:bookmarkEnd w:id="` + id + `"/>`)
}

func entryBookmark(i int) string { return "_Entry" + strconv.Itoa(i+1) }

func (w *writer) pageBreak() {
	w.b.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
}

// entry is one requirement or section: its heading, key and revision, its text, its table and its links.
func (w *writer) entry(i int, e specdoc.Entry, f frame) {
	level := min(e.Depth+1, maxHeadingStyle)
	w.para(f, paraProps{style: "Heading" + itoa(level)}, func() {
		w.bookmarked(entryBookmark(i), func() { w.text(e.Outline+" "+e.Title, runProps{}) })
	})
	w.para(f, paraProps{style: "EntryMeta"}, func() { w.text(w.words.keyAndRevision(e.Key, e.Revision), runProps{}) })
	if e.ChangedFrom != nil {
		w.para(f, paraProps{style: "EntryMeta"}, func() { w.text(w.words.changedFrom(*e.ChangedFrom), runProps{italic: true}) })
	}
	if len(e.Changes) > 0 {
		w.body(e.Changes, f)
	} else {
		w.body(e.Statement, f)
	}
	if len(e.Rationale) > 0 {
		w.para(f, paraProps{style: "Label", keepNext: true}, func() { w.text(w.words.rationale, runProps{}) })
		w.body(e.Rationale, f)
	}
	if t := e.TestCase; t != nil {
		w.testCase(*t, f)
	}
	if v := e.Risk; v != nil {
		w.para(f, paraProps{style: "Label", keepNext: true}, func() { w.text(w.words.pick("Risk", "Risiko"), runProps{}) })
		w.body(requirement.RatingTable(*v, requirement.RatingWords{
			Rating: w.words.pick("Rating", "Bewertung"), Before: w.words.pick("Before", "Vorher"), After: w.words.pick("After", "Nachher"),
			Severity: w.words.pick("Severity", "Schwere"), Probability: w.words.pick("Probability", "Wahrscheinlichkeit"),
			Detection: w.words.pick("Detection", "Entdeckung"), RPN: w.words.pick("RPN", "RPZ"), Level: w.words.pick("Level", "Stufe"),
		}), f)
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
	if len(e.Comments) > 0 {
		w.para(f, paraProps{style: "Label", keepNext: true}, func() { w.text(w.words.pick("Open comments", "Offene Kommentare"), runProps{}) })
		for _, c := range e.Comments {
			w.para(f, paraProps{}, func() {
				w.text(c.Author+", "+w.words.day(c.At)+": ", runProps{bold: true})
				w.text(c.Text, runProps{})
			})
		}
	}
}

// changedFrom says which revision a baseline held that an entry's change marks are counted from.
func (w words) changedFrom(revision int) string {
	if revision == 0 {
		return w.pick("New since the baseline.", "Neu seit der Baseline.")
	}
	return w.pick("Changes since revision ", "Änderungen seit Revision ") + itoa(revision) + "."
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
	w.titledTable(f, widths, true, w.words.properties)
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

// testCase writes what a test case carries: its preconditions, its parameters by iteration and its steps, as tables.
func (w *writer) testCase(t requirement.TestCase, f frame) {
	label := func(text string) {
		w.para(f, paraProps{style: "Label", keepNext: true}, func() { w.text(text, runProps{}) })
	}
	if len(t.Preconditions) > 0 {
		label(w.words.pick("Preconditions", "Vorbedingungen"))
		w.body(t.Preconditions, f)
	}
	if len(t.Parameters) > 0 {
		label(w.words.pick("Parameters", "Parameter"))
		w.body(requirement.ParametersTable(t, w.words.pick("Iteration", "Iteration")), f)
	}
	if len(t.Steps) > 0 {
		label(w.words.pick("Steps", "Schritte"))
		w.body(requirement.StepsTable(t.Steps, w.words.pick("Action", "Aktion"), w.words.pick("Expected result", "Erwartetes Ergebnis")), f)
	}
}
