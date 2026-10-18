package docx

import (
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// reader reads one document into a Source.
type reader struct {
	pkg  *pkg
	main string
	rels map[string]rel
	src  *Source

	styles       map[string]*style
	defaultStyle string
	abstracts    map[string]map[int]level
	nums         map[string]*numbering
	counters     map[string]*counter
	media        map[string]*Media
	styleCount   map[string]int
	lossSeen     map[string]bool
	// paragraphs counts the paragraphs with words met so far, as Block.Index numbers them.
	paragraphs int
	list       *listGroup
	err        error
}

// entryMeta is the line an Exciter export writes below each heading: the key, and its revision in either language.
var entryMeta = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,9}-R[0-9]+) · [Rr]evision [0-9]+$`)

// outlineNumber is the number an Exciter export writes before a heading's title, which is Exciter's own outline.
var outlineNumber = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*\.?\s+`)

// tocStyle is a style of Word's table of contents, which its headings make again.
var tocStyle = regexp.MustCompile(`(?i)^toc( [1-9]| heading)?$|^TOC[1-9]$|^TOCHeading$`)

// textHeading is the style an Exciter export gives a heading inside a requirement's text.
var textHeading = regexp.MustCompile(`^TextHeading([1-6])$`)

func (r *reader) docLoss(kind, message string) {
	if r.lossSeen[message] {
		return
	}
	r.lossSeen[message] = true
	r.src.Losses = append(r.src.Losses, Loss{Kind: kind, Message: message})
}

// readBody reads the body element by element, so only one block is held as a tree at a time.
func (r *reader) readBody(doc []byte) error {
	if doc == nil {
		return ErrNotWord
	}
	dec := newDecoder(doc)
	var root, body bool
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ErrNotWord
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case !root:
				if t.Name.Local != "document" {
					return ErrNotWord
				}
				root = true
			case !body:
				body = t.Name.Local == "body"
				if !body {
					if err := dec.Skip(); err != nil {
						return ErrNotWord
					}
				}
			default:
				n, err := tree(dec, t)
				if err != nil {
					return ErrNotWord
				}
				r.block(n)
				if r.err != nil {
					return r.err
				}
			}
		case xml.EndElement:
			if body && t.Name.Local == "body" {
				body = false
			}
		}
	}
	if !root {
		return ErrNotWord
	}
	r.closeList()
	return nil
}

// block reads one element of the body.
func (r *reader) block(n *node) {
	switch n.local() {
	case "p":
		r.paragraph(n)
	case "tbl":
		r.closeList()
		r.table(n)
	case "sdt":
		if content := n.child("sdtContent"); content != nil {
			for _, k := range content.kids {
				r.block(k)
			}
		}
	case "customXml", "ins", "moveTo", "smartTag":
		for _, k := range n.kids {
			r.block(k)
		}
	case "AlternateContent":
		if choice := n.child("Choice"); choice != nil {
			for _, k := range choice.kids {
				r.block(k)
			}
		}
	case "del", "moveFrom":
		if strings.TrimSpace(n.words()) != "" {
			r.docLoss(LossValue, "Deleted text of tracked changes is left out; insertions are kept.")
		}
	case "altChunk":
		r.docLoss(LossValue, "A document embedded in the document is left out.")
	}
}

// para is one paragraph as read: its properties, its content as rich text, and its words.
type para struct {
	styleID, paraID string
	outline         int
	num             *numRef
	rich            []XNode
	text            strings.Builder
	pictures        int
	losses          []Loss
	bookmark        string
}

func (p *para) loss(kind, message string) {
	if !slices.ContainsFunc(p.losses, func(l Loss) bool { return l.Message == message }) {
		p.losses = append(p.losses, Loss{Kind: kind, Message: message})
	}
}

// readParagraph reads a paragraph's properties and content.
func (r *reader) readParagraph(n *node) *para {
	p := &para{outline: -1, paraID: n.attr("paraId")}
	if pPr := n.child("pPr"); pPr != nil {
		if s := pPr.child("pStyle"); s != nil {
			p.styleID = s.attr("val")
		}
		if o := pPr.child("outlineLvl"); o != nil {
			if v, err := strconv.Atoi(o.attr("val")); err == nil {
				p.outline = v
			}
		}
		p.num = numOf(pPr.child("numPr"))
	}
	if p.styleID == "" {
		p.styleID = r.defaultStyle
	}
	in := &inliner{r: r, p: p, dest: &p.rich}
	in.content(n)
	in.unwind()
	return p
}

// numberOf is a paragraph's numbering: its own, or its style's, the paragraph giving the level when its style does not.
func (r *reader) numberOf(p *para) *numRef {
	own, styled := p.num, r.styleNum(p.styleID)
	switch {
	case own == nil:
		return styled
	case own.numID == "" && styled != nil:
		return &numRef{numID: styled.numID, ilvl: own.ilvl, hasLevel: own.hasLevel}
	}
	return own
}

func (r *reader) paragraph(n *node) {
	p := r.readParagraph(n)
	name := r.styleName(p.styleID)
	if tocStyle.MatchString(name) || tocStyle.MatchString(p.styleID) {
		r.docLoss(LossPlace, "The table of contents is left out; the headings make the tree.")
		return
	}
	ref := r.numberOf(p)
	var (
		label    string
		lv       level
		numbered bool
	)
	if ref != nil {
		label, lv, numbered = r.numbered(ref)
	}
	text := strings.TrimSpace(p.text.String())
	if text == "" && p.pictures == 0 {
		return
	}
	r.styleCount[p.styleID]++
	r.paragraphs++
	blocks := r.src.Blocks
	if last := len(blocks) - 1; r.list == nil && last >= 0 && blocks[last].Kind == BlockHeading && blocks[last].Key == "" {
		if m := entryMeta.FindStringSubmatch(text); m != nil {
			blocks[last].Key, r.src.Exported = m[1], true
			return
		}
	}
	if r.styleNamed(p.styleID, "Title") && len(blocks) == 0 && r.list == nil {
		r.src.Title = text
		return
	}
	base := Block{StyleID: p.styleID, StyleName: name, ParaID: p.paraID, Index: r.paragraphs, Bookmark: p.bookmark, Losses: p.losses}
	if lvl := r.headingLevel(p.styleID, p.outline); lvl > 0 || r.styleNamed(p.styleID, "Title") {
		r.closeList()
		base.Kind, base.Level, base.Text = BlockHeading, max(min(lvl, maxHeadingLevel), 1), text
		if numbered && lv.format != "bullet" {
			base.Label = strings.TrimSpace(label)
		}
		r.src.Blocks = append(r.src.Blocks, base)
		return
	}
	listed := strings.HasPrefix(strings.ToLower(name), "list")
	if numbered && (lv.format == "bullet" || (!isLabel(lv) && (p.num != nil && p.num.numID != "" || listed))) {
		r.addListItem(listItem{level: ref.ilvl, ordered: lv.format != "bullet", start: r.counterValue(ref), rich: p.rich, text: text,
			block: base})
		return
	}
	r.closeList()
	if numbered && lv.format != "bullet" {
		base.Label = strings.TrimSpace(label)
	}
	base.Text = text
	base.Caption = r.styleNamed(p.styleID, "caption")
	if text == "" {
		base.Kind, base.Rich = BlockPicture, &XNode{Children: splitObjects(p.rich, "p")}
		r.src.Blocks = append(r.src.Blocks, base)
		return
	}
	tag := "p"
	if m := textHeading.FindStringSubmatch(p.styleID); m != nil {
		tag = "h" + m[1]
	} else if p.styleID == "Code" {
		tag = "pre"
	}
	base.Kind, base.Rich = BlockParagraph, &XNode{Children: splitObjects(p.rich, tag)}
	r.src.Blocks = append(r.src.Blocks, base)
}

// counterValue is the count a numbering's level stands at, which an ordered list starts from.
func (r *reader) counterValue(ref *numRef) int {
	num := r.nums[ref.numID]
	if num == nil {
		return 1
	}
	for key, c := range r.counters {
		if key == "num:"+ref.numID || key == "abstract:"+num.abstract {
			return c.value[ref.ilvl]
		}
	}
	return 1
}

// splitObjects wraps runs of inline content in tag, and leaves the pictures between them on their own, so a
// picture does not open an empty paragraph.
func splitObjects(nodes []XNode, tag string) []XNode {
	var out []XNode
	var run []XNode
	flush := func() {
		if len(run) == 0 {
			return
		}
		if strings.TrimSpace(xwords(run)) != "" || slices.ContainsFunc(run, func(x XNode) bool { return x.Name == "br" }) {
			out = append(out, XNode{Name: tag, Children: run})
		}
		run = nil
	}
	for _, x := range nodes {
		if x.Name == "object" {
			flush()
			out = append(out, x)
			continue
		}
		run = append(run, x)
	}
	flush()
	return out
}

func xwords(nodes []XNode) string {
	var b strings.Builder
	var walk func(XNode)
	walk = func(x XNode) {
		b.WriteString(x.Text)
		for _, c := range x.Children {
			walk(c)
		}
	}
	for _, x := range nodes {
		walk(x)
	}
	return b.String()
}

// listItem is one paragraph of a list, at its level.
type listItem struct {
	level   int
	ordered bool
	start   int
	rich    []XNode
	text    string
	block   Block
}

// listGroup is consecutive list paragraphs, which become one list block.
type listGroup struct{ items []listItem }

func (r *reader) addListItem(it listItem) {
	if r.list == nil {
		r.list = &listGroup{}
	}
	r.list.items = append(r.list.items, it)
}

func (r *reader) closeList() {
	g := r.list
	if g == nil {
		return
	}
	r.list = nil
	first := g.items[0].block
	b := Block{Kind: BlockList, StyleID: first.StyleID, StyleName: first.StyleName, ParaID: first.ParaID, Index: first.Index, Bookmark: first.Bookmark}
	var texts []string
	for _, it := range g.items {
		texts = append(texts, it.text)
		for _, l := range it.block.Losses {
			if !slices.ContainsFunc(b.Losses, func(have Loss) bool { return have.Message == l.Message }) {
				b.Losses = append(b.Losses, l)
			}
		}
	}
	b.Text = strings.Join(texts, "\n")
	b.Rich = &XNode{Children: listNodes(g.items)}
	r.src.Blocks = append(r.src.Blocks, b)
}

// listNodes nests items by level: an item deeper than the one before it goes into a list inside that one.
func listNodes(items []listItem) []XNode {
	var out []XNode
	for i := 0; i < len(items); {
		first := items[i]
		list := XNode{Name: "ul"}
		if first.ordered {
			list.Name = "ol"
			if first.start != 1 {
				list.Attrs = map[string]string{"start": strconv.Itoa(first.start)}
			}
		}
		for i < len(items) && items[i].level == first.level && items[i].ordered == first.ordered {
			li := XNode{Name: "li", Children: splitObjects(items[i].rich, "p")}
			i++
			j := i
			for j < len(items) && items[j].level > first.level {
				j++
			}
			if j > i {
				li.Children = append(li.Children, listNodes(items[i:j])...)
				i = j
			}
			list.Children = append(list.Children, li)
		}
		out = append(out, list)
	}
	return out
}

// finish lists the styles by use, and reads Exciter's own export as its writer laid it out.
func (r *reader) finish() {
	if r.src.Exported {
		r.roles()
	}
	for id, count := range r.styleCount {
		r.src.Styles = append(r.src.Styles, StyleUse{ID: id, Name: r.styleName(id), Count: count})
	}
	sort.Slice(r.src.Styles, func(i, j int) bool {
		a, b := r.src.Styles[i], r.src.Styles[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Name < b.Name
	})
}

// roles mark the blocks of Exciter's own export its writer added around the requirements, and take its outline
// numbers off the headings.
func (r *reader) roles() {
	keyed, glossaryChapter := false, false
	labelled := func(b *Block, word func(words) string) bool {
		return b.Kind == BlockParagraph && b.StyleID == "Label" && (b.Text == word(english) || b.Text == word(german))
	}
	for i := range r.src.Blocks {
		b := &r.src.Blocks[i]
		switch {
		case b.Kind == BlockHeading && b.Key != "":
			b.Text = strings.TrimSpace(outlineNumber.ReplaceAllString(b.Text, ""))
			keyed, glossaryChapter = true, false
		case b.Kind == BlockHeading && b.Bookmark == glossaryBookmark:
			glossaryChapter = true
		}
		switch {
		case glossaryChapter:
			b.Role = RoleGlossary
		case !keyed:
			b.Role = RoleCover
		case b.Kind == BlockTable && (b.TableTitle == english.properties || b.TableTitle == german.properties):
			b.Role = RoleProperties
		case labelled(b, func(w words) string { return w.rationale }):
			b.Role = RoleRationale
		case labelled(b, func(w words) string { return w.links }):
			b.Role = RoleLinks
		case b.StyleID == "Note":
			b.Role = RoleNote
		}
	}
}
