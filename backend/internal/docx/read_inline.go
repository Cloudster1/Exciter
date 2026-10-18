package docx

import (
	"strconv"
	"strings"
)

// inliner reads a paragraph's content into rich text: runs and their marks, links, fields and pictures.
type inliner struct {
	r    *reader
	p    *para
	dest *[]XNode
	// fields are the complex fields open, innermost last, whose results are being read.
	fields []*field
}

// field is a complex field: its instruction, then its result, which is what the document shows.
type field struct {
	instr  strings.Builder
	result bool
	kids   []XNode
	saved  *[]XNode
}

// The losses a paragraph's content may note.
const (
	lostUnderline = "Underlining is not kept; the words are."
	lostColour    = "Colours and highlighting are not kept; the words are."
	lostScript    = "Subscript and superscript are not kept; the words are."
	lostAnchor    = "A link to a place in the document is kept as its words."
	lostDeleted   = "Deleted text of tracked changes is left out; insertions are kept."
	lostComments  = "Comments are left out."
	lostNotes     = "Footnotes and endnotes are left out."
	lostMath      = "Equations are left out."
	lostTextBox   = "Text boxes are left out."
	lostDrawing   = "A drawing that is no picture, such as a chart or a shape, is left out."
	lostObject    = "An embedded object is kept as the picture it shows."
)

// live says the content read now shows: outside fields, or in a field's result.
func (in *inliner) live() bool {
	return len(in.fields) == 0 || in.fields[len(in.fields)-1].result
}

func (in *inliner) emit(x XNode) {
	if in.live() {
		*in.dest = append(*in.dest, x)
	}
}

// into reads n's content into a list of its own, for a link to wrap.
func (in *inliner) into(n *node) []XNode {
	var kids []XNode
	saved := in.dest
	in.dest = &kids
	in.content(n)
	in.dest = saved
	return kids
}

func (in *inliner) content(n *node) {
	if n == nil {
		return
	}
	for _, k := range n.kids {
		in.element(k)
	}
}

func (in *inliner) element(k *node) {
	switch k.local() {
	case "r":
		in.run(k)
	case "hyperlink":
		kids := in.into(k)
		href := ""
		if id := k.relAttr("id"); id != "" {
			if rel, ok := in.r.rels[id]; ok && rel.external {
				href = rel.target
			}
		} else if k.attr("anchor") != "" {
			in.p.loss(LossFormatting, lostAnchor)
		}
		in.link(href, kids)
	case "fldSimple":
		kids := in.into(k)
		in.link(in.fieldLink(k.attr("instr")), kids)
	case "ins", "moveTo", "smartTag", "customXml", "dir", "bdo":
		in.content(k)
	case "sdt":
		in.content(k.child("sdtContent"))
	case "AlternateContent":
		in.alternative(k, in.content)
	case "del", "moveFrom":
		if strings.TrimSpace(k.words()) != "" {
			in.p.loss(LossValue, lostDeleted)
		}
	case "oMath", "oMathPara":
		in.p.loss(LossValue, lostMath)
	case "bookmarkStart":
		if name := k.attr("name"); in.p.bookmark == "" && name != "" && name != "_GoBack" {
			in.p.bookmark = name
		}
	case "commentRangeStart":
		in.p.loss(LossValue, lostComments)
	}
}

// alternative reads the first choice of markup compatibility, which every reader of the document understands.
func (in *inliner) alternative(k *node, read func(*node)) {
	if choice := k.child("Choice"); choice != nil {
		read(choice)
	} else if fallback := k.child("Fallback"); fallback != nil {
		read(fallback)
	}
}

// link wraps content in a link to href, or lets it stand as its words without one.
func (in *inliner) link(href string, kids []XNode) {
	if href == "" {
		for _, x := range kids {
			in.emit(x)
		}
		return
	}
	in.emit(XNode{Name: "a", Attrs: map[string]string{"href": href}, Children: kids})
}

// fieldLink reads the address of a HYPERLINK field; a link to a place in the document is no link.
func (in *inliner) fieldLink(instr string) string {
	fields := quotedFields(instr)
	if len(fields) == 0 || !strings.EqualFold(fields[0], "HYPERLINK") {
		return ""
	}
	var address, anchor string
	for i := 1; i < len(fields); i++ {
		switch f := fields[i]; {
		case strings.EqualFold(f, `\l`) && i+1 < len(fields):
			anchor = fields[i+1]
			i++
		case strings.HasPrefix(f, `\`):
			i++
		case address == "":
			address = f
		}
	}
	if address == "" {
		if anchor != "" {
			in.p.loss(LossFormatting, lostAnchor)
		}
		return ""
	}
	if anchor != "" {
		address += "#" + anchor
	}
	return address
}

// quotedFields splits a field's instruction at spaces, a quoted part being one field.
func quotedFields(s string) []string {
	var out []string
	var b strings.Builder
	quoted, started := false, false
	for _, c := range s {
		switch {
		case c == '"':
			quoted, started = !quoted, true
		case (c == ' ' || c == '\t') && !quoted:
			if started {
				out = append(out, b.String())
			}
			b.Reset()
			started = false
		default:
			b.WriteRune(c)
			started = true
		}
	}
	if started {
		out = append(out, b.String())
	}
	return out
}

// marks are what a run's properties keep of its look.
type marks struct{ bold, italic, strike, code bool }

func (in *inliner) marks(rPr *node) marks {
	var m marks
	if rPr == nil {
		return m
	}
	m.bold, m.italic = on(rPr.child("b")), on(rPr.child("i"))
	m.strike = on(rPr.child("strike")) || on(rPr.child("dstrike"))
	if s := rPr.child("rStyle"); s != nil {
		m.code = in.r.styleNamed(s.attr("val"), "InlineCode", "Inline Code", "HTML Code")
	}
	if u := rPr.child("u"); u != nil && !strings.EqualFold(u.attr("val"), "none") && in.live() {
		in.p.loss(LossFormatting, lostUnderline)
	}
	if c := rPr.child("color"); c != nil && !strings.EqualFold(c.attr("val"), "auto") && c.attr("val") != "000000" && in.live() {
		in.p.loss(LossFormatting, lostColour)
	}
	if h := rPr.child("highlight"); h != nil && !strings.EqualFold(h.attr("val"), "none") && in.live() {
		in.p.loss(LossFormatting, lostColour)
	}
	if v := rPr.child("vertAlign"); v != nil && v.attr("val") != "baseline" && in.live() {
		in.p.loss(LossFormatting, lostScript)
	}
	return m
}

func (in *inliner) run(k *node) {
	m := in.marks(k.child("rPr"))
	in.runContent(k, m)
}

func (in *inliner) runContent(k *node, m marks) {
	for _, c := range k.kids {
		switch c.local() {
		case "t":
			in.text(c.words(), m)
		case "tab", "ptab":
			in.text("\t", m)
		case "noBreakHyphen":
			in.text("-", m)
		case "br":
			if t := c.attr("type"); t != "page" && t != "column" {
				in.lineBreak()
			}
		case "cr":
			in.lineBreak()
		case "drawing":
			in.drawing(c)
		case "pict", "object":
			in.vml(c)
		case "fldChar":
			in.fieldChar(c.attr("fldCharType"))
		case "instrText":
			if n := len(in.fields); n > 0 && !in.fields[n-1].result {
				in.fields[n-1].instr.WriteString(c.words())
			}
		case "footnoteReference", "endnoteReference":
			in.p.loss(LossValue, lostNotes)
		case "commentReference":
			in.p.loss(LossValue, lostComments)
		case "AlternateContent":
			in.alternative(c, func(n *node) { in.runContent(n, m) })
		case "ruby":
			if base := c.child("rubyBase"); base != nil {
				in.content(base)
			}
		}
	}
}

func (in *inliner) text(s string, m marks) {
	if s == "" || !in.live() {
		return
	}
	in.p.text.WriteString(s)
	x := XNode{Text: s}
	for _, wrap := range []struct {
		on   bool
		name string
	}{{m.code, "code"}, {m.strike, "s"}, {m.italic, "i"}, {m.bold, "b"}} {
		if wrap.on {
			x = XNode{Name: wrap.name, Children: []XNode{x}}
		}
	}
	in.emit(x)
}

func (in *inliner) lineBreak() {
	if in.live() {
		in.p.text.WriteString("\n")
		in.emit(XNode{Name: "br"})
	}
}

func (in *inliner) fieldChar(kind string) {
	switch kind {
	case "begin":
		f := &field{saved: in.dest}
		in.fields = append(in.fields, f)
		in.dest = &f.kids
	case "separate":
		if n := len(in.fields); n > 0 {
			in.fields[n-1].result = true
		}
	case "end":
		n := len(in.fields)
		if n == 0 {
			return
		}
		f := in.fields[n-1]
		in.fields = in.fields[:n-1]
		in.dest = f.saved
		in.link(in.fieldLink(strings.TrimSpace(f.instr.String())), f.kids)
	}
}

// unwind closes the fields a paragraph leaves open, keeping what their results showed.
func (in *inliner) unwind() {
	for len(in.fields) > 0 {
		in.fieldChar("end")
	}
}

// drawing reads a picture Word draws inline or floating: its file, its width and its description.
func (in *inliner) drawing(c *node) {
	if !in.live() {
		return
	}
	if c.find("txbxContent") != nil {
		in.p.loss(LossValue, lostTextBox)
		return
	}
	blip := c.find("blip")
	if blip == nil {
		in.p.loss(LossImage, lostDrawing)
		return
	}
	id := blip.relAttr("embed")
	if id == "" {
		id = blip.relAttr("link")
	}
	width := 0
	if extent := c.find("extent"); extent != nil {
		if cx, err := strconv.ParseInt(extent.attr("cx"), 10, 64); err == nil && cx > 0 {
			width = int(cx / emuPerPixel)
		}
	}
	alt := ""
	if pr := c.find("docPr"); pr != nil {
		alt = pr.attr("descr")
		if alt == "" {
			alt = pr.attr("title")
		}
	}
	in.object(id, alt, width)
}

// vml reads a picture of the older drawing markup, and an embedded object as the picture it shows.
func (in *inliner) vml(c *node) {
	if !in.live() {
		return
	}
	if c.find("textbox") != nil {
		in.p.loss(LossValue, lostTextBox)
		return
	}
	img := c.find("imagedata")
	if img == nil {
		in.p.loss(LossImage, lostDrawing)
		return
	}
	if c.local() == "object" {
		in.p.loss(LossImage, lostObject)
	}
	alt := img.attr("title")
	if shape := c.find("shape"); shape != nil && alt == "" {
		alt = shape.attr("alt")
	}
	in.object(img.relAttr("id"), alt, 0)
}

func (in *inliner) object(id, alt string, width int) {
	m, why := in.r.picture(id)
	if m == nil {
		in.p.loss(LossImage, why)
		return
	}
	x := XNode{Name: "object", Attrs: map[string]string{"data": m.Path, "type": m.ContentType}}
	if width > 0 {
		x.Attrs["width"] = strconv.Itoa(width)
	}
	if alt = strings.TrimSpace(alt); alt != "" {
		x.Children = []XNode{{Text: alt}}
	}
	in.p.pictures++
	in.emit(x)
}
