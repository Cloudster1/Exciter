package docx

import (
	"context"
	"encoding/xml"
	"strconv"
	"strings"
)

// Page geometry, in twentieths of a point: A4 portrait with margins of an inch, as Word's own A4 template has them.
const (
	pageWidth  = 11906
	pageHeight = 16838
	margin     = 1440
	textWidth  = pageWidth - 2*margin
	// cellMargin is Word's default space between a cell's border and its text.
	cellMargin = 108
	// listIndent is how far each level of a list sits in, and hanging how far its marker hangs out of that.
	listIndent = 720
	hanging    = 360
	// maxListLevel is the deepest level Word numbers.
	maxListLevel = 8
	// minColumnWidth keeps a column that says nothing of its width readable.
	minColumnWidth = 600
)

// Drawing units: Word measures pictures in English Metric Units.
const (
	emuPerPixel = 9525
	emuPerTwip  = 635
)

type media struct {
	rid    string
	target string
	ext    string
	data   []byte
}

type relationship struct {
	id, kind, target string
	external         bool
}

// listNum is one numbering instance: a list numbered on its own, starting where it says.
type listNum struct {
	abstract int
	start    int
}

type writer struct {
	ctx      context.Context
	read     Reader
	words    words
	b        strings.Builder
	rels     []relationship
	media    []media
	nums     []listNum
	pictures int
	// embedded is each file read so far, nil for one left out, so a picture shown twice is carried once.
	embedded map[string]*picture
	spent    int64
	err      error
	bookmark int
}

func newWriter(ctx context.Context, lang string, r Reader) *writer {
	return &writer{ctx: ctx, read: r, words: wordsFor(lang), embedded: map[string]*picture{}}
}

// fail keeps the first error; what follows is still written, and thrown away.
func (w *writer) fail(err error) {
	if w.err == nil {
		w.err = err
	}
}

// rel adds a relationship of the document and returns its id.
func (w *writer) rel(kind, target string, external bool) string {
	id := "rId" + strconv.Itoa(len(w.rels)+firstRel)
	w.rels = append(w.rels, relationship{id: id, kind: kind, target: target, external: external})
	return id
}

// firstRel leaves rId1 to rId3 to the styles, numbering and settings.
const firstRel = 4

func (w *writer) relationships() []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	b.WriteString(`<Relationship Id="rId1" Type="` + relStyles + `" Target="styles.xml"/>`)
	b.WriteString(`<Relationship Id="rId2" Type="` + relNumbering + `" Target="numbering.xml"/>`)
	b.WriteString(`<Relationship Id="rId3" Type="` + relSettings + `" Target="settings.xml"/>`)
	for _, r := range w.rels {
		b.WriteString(`<Relationship Id="` + r.id + `" Type="` + r.kind + `" Target="` + escape(r.target) + `"`)
		if r.external {
			b.WriteString(` TargetMode="External"`)
		}
		b.WriteString(`/>`)
	}
	b.WriteString(`</Relationships>`)
	return []byte(b.String())
}

// frame is where blocks are written: how far in, how wide, inside how many
// lists, and the styling a table cell lends its paragraphs.
type frame struct {
	indent int
	width  int
	list   int
	bold   bool
	align  string
	// marker is the list marker the next paragraph carries, once.
	marker *marker
}

type marker struct {
	num   int
	level int
	// box is a task's check box, written as a character before its words.
	box  string
	used bool
}

// paraProps are a paragraph's properties, written in the order the schema wants them.
type paraProps struct {
	style    string
	keepNext bool
	border   string
	align    string
}

// para writes one paragraph in the frame, taking the frame's list marker
// if it has one waiting.
func (w *writer) para(f frame, pp paraProps, runs func()) {
	var props strings.Builder
	if pp.style != "" {
		props.WriteString(`<w:pStyle w:val="` + pp.style + `"/>`)
	}
	if pp.keepNext {
		props.WriteString("<w:keepNext/>")
	}
	m := f.marker
	if m != nil && m.used {
		m = nil
	}
	if m != nil && m.box == "" {
		props.WriteString(`<w:numPr><w:ilvl w:val="` + itoa(m.level) + `"/><w:numId w:val="` + itoa(m.num) + `"/></w:numPr>`)
	}
	props.WriteString(pp.border)
	switch {
	case m != nil:
		props.WriteString(`<w:ind w:left="` + itoa(f.indent) + `" w:hanging="` + itoa(hanging) + `"/>`)
	case f.indent > 0:
		props.WriteString(`<w:ind w:left="` + itoa(f.indent) + `"/>`)
	}
	align := pp.align
	if align == "" {
		align = f.align
	}
	switch align {
	case "center":
		props.WriteString(`<w:jc w:val="center"/>`)
	case "right":
		props.WriteString(`<w:jc w:val="right"/>`)
	}
	w.b.WriteString("<w:p>")
	if props.Len() > 0 {
		w.b.WriteString("<w:pPr>" + props.String() + "</w:pPr>")
	}
	if m != nil {
		m.used = true
		if m.box != "" {
			w.text(m.box+"\t", runProps{})
		}
	}
	if runs != nil {
		runs()
	}
	w.b.WriteString("</w:p>")
}

// runProps are a run's properties, written in the order the schema wants them.
type runProps struct {
	style  string
	bold   bool
	italic bool
	caps   bool
	strike bool
	color  string
	shade  string
}

func (rp runProps) write(b *strings.Builder) {
	if rp == (runProps{}) {
		return
	}
	b.WriteString("<w:rPr>")
	if rp.style != "" {
		b.WriteString(`<w:rStyle w:val="` + rp.style + `"/>`)
	}
	if rp.bold {
		b.WriteString("<w:b/><w:bCs/>")
	}
	if rp.italic {
		b.WriteString("<w:i/><w:iCs/>")
	}
	if rp.caps {
		b.WriteString("<w:caps/>")
	}
	if rp.strike {
		b.WriteString("<w:strike/>")
	}
	if rp.color != "" {
		b.WriteString(`<w:color w:val="` + rp.color + `"/>`)
	}
	if rp.shade != "" {
		b.WriteString(`<w:shd w:val="clear" w:color="auto" w:fill="` + rp.shade + `"/>`)
	}
	b.WriteString("</w:rPr>")
}

// text writes text as runs: a line break, a tab and the words between them
// each as Word wants them.
func (w *writer) text(s string, rp runProps) {
	if s == "" {
		return
	}
	w.b.WriteString("<w:r>")
	rp.write(&w.b)
	start := 0
	flush := func(end int) {
		if end > start {
			w.b.WriteString(`<w:t xml:space="preserve">` + escape(s[start:end]) + `</w:t>`)
		}
	}
	for i, c := range s {
		switch c {
		case '\n':
			flush(i)
			w.b.WriteString("<w:br/>")
			start = i + 1
		case '\t':
			flush(i)
			w.b.WriteString("<w:tab/>")
			start = i + 1
		case '\r':
			flush(i)
			start = i + 1
		}
	}
	flush(len(s))
	w.b.WriteString("</w:r>")
}

// hyperlink wraps runs in a link by address; a link within the web client, without its address, is no link.
func (w *writer) hyperlink(href string, runs func()) {
	if strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//") {
		if w.read.BaseURL == "" {
			runs()
			return
		}
		href = strings.TrimSuffix(w.read.BaseURL, "/") + href
	}
	id := w.rel(relHyperlink, href, true)
	w.b.WriteString(`<w:hyperlink r:id="` + id + `" w:history="1">`)
	runs()
	w.b.WriteString("</w:hyperlink>")
}

// internal wraps runs in a link to a bookmark of the document, as its contents link to its entries.
func (w *writer) internal(bookmark string, runs func()) {
	w.b.WriteString(`<w:hyperlink w:anchor="` + bookmark + `" w:history="1">`)
	runs()
	w.b.WriteString("</w:hyperlink>")
}

// escape makes text safe in an element or an attribute; a character XML
// cannot carry becomes the replacement character.
func escape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func itoa(n int) string { return strconv.Itoa(n) }
