package reqifio

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// Placed is what a file of the archive became in the project: an image a statement shows, or a file it names.
type Placed struct {
	ID       string
	Image    bool
	FileName string
}

// Placer says what the file at ref became; false leaves it out of the document.
type Placer func(ref string) (Placed, bool)

// lossNote is one thing a rich text value lost on its way into a document.
type lossNote struct {
	kind    LossKind
	message string
}

// converter turns rich text into a document, noting each kind of loss once.
type converter struct {
	place  Placer
	losses []lossNote
	seen   map[string]bool
}

func (c *converter) lose(kind LossKind, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if c.seen[message] {
		return
	}
	c.seen[message] = true
	c.losses = append(c.losses, lossNote{kind, message})
}

// ToDocument turns a rich text value into a document of the editor, and says in sentences what it could not
// keep. A value the editor cannot hold as it is comes in as its words.
func ToDocument(x *XNode, place Placer) (json.RawMessage, []lossNote) {
	if place == nil {
		place = func(string) (Placed, bool) { return Placed{}, false }
	}
	c := &converter{place: place, seen: map[string]bool{}}
	if x == nil {
		return document.Empty, nil
	}
	blocks := c.blocks(x.Children)
	if len(blocks) == 0 {
		return document.Empty, c.losses
	}
	raw, err := json.Marshal(document.Node{Type: "doc", Content: blocks})
	if err == nil {
		if _, err = document.Parse(raw); err == nil {
			return raw, c.losses
		}
	}
	c.lose(LossFormatting, "Its rich text did not fit the editor, so it came in as plain paragraphs.")
	return PlainDocument(Words(x)), c.losses
}

// sink gathers blocks, and the paragraph or heading inline content is going into.
type sink struct {
	blocks []document.Node
	open   *document.Node
	// explicit says the open paragraph is the file's own, kept even when empty.
	explicit bool
}

func (s *sink) inline(n document.Node) {
	if s.open == nil {
		s.open, s.explicit = &document.Node{Type: "paragraph"}, false
	}
	if last := len(s.open.Content) - 1; last >= 0 && n.Type == "text" && s.open.Content[last].Type == "text" &&
		sameMarks(s.open.Content[last].Marks, n.Marks) {
		s.open.Content[last].Text += n.Text
		return
	}
	s.open.Content = append(s.open.Content, n)
}

// sameMarks says two runs of text look alike, so they are one run.
func sameMarks(a, b []document.Mark) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || fmt.Sprint(a[i].Attrs) != fmt.Sprint(b[i].Attrs) {
			return false
		}
	}
	return true
}

func (s *sink) block(n document.Node) {
	s.flush()
	s.blocks = append(s.blocks, n)
}

// begin opens a paragraph or heading for the inline content that follows.
func (s *sink) begin(n document.Node) {
	s.flush()
	s.open, s.explicit = &n, true
}

// flush ends the open paragraph, trimmed, unless nothing went into it.
func (s *sink) flush() {
	if s.open == nil {
		return
	}
	n := *s.open
	s.open = nil
	content := n.Content[:0]
	for i, child := range n.Content {
		if child.Type == "text" && i == len(n.Content)-1 {
			child.Text = strings.TrimRight(child.Text, " ")
		}
		if child.Type == "text" && child.Text == "" {
			continue
		}
		content = append(content, child)
	}
	n.Content = content
	if len(n.Content) == 0 && !s.explicit {
		return
	}
	s.blocks = append(s.blocks, n)
}

// blocks reads XHTML content as a list of blocks; content that is never empty gets one empty paragraph.
func (c *converter) blocks(nodes []XNode) []document.Node {
	s := &sink{}
	for i := range nodes {
		c.node(&nodes[i], s, nil)
	}
	s.flush()
	return s.blocks
}

func (c *converter) filled(nodes []XNode) []document.Node {
	if out := c.blocks(nodes); len(out) > 0 {
		return out
	}
	return []document.Node{{Type: "paragraph"}}
}

var headingLevels = map[string]int{"h1": 1, "h2": 2, "h3": 3, "h4": 4, "h5": 5, "h6": 6}

// Elements read as what is inside them alone, with nothing lost.
var transparent = map[string]bool{
	"": true, "div": true, "body": true, "span": true, "section": true, "article": true, "thead": true, "tbody": true,
	"tfoot": true, "dl": true, "dt": true, "dd": true, "abbr": true, "acronym": true, "q": true, "big": true,
	"small": true, "bdo": true, "address": true, "font": true, "colgroup": true, "col": true, "param": true,
}

// node reads one XHTML node into s, under the marks of the elements around it.
func (c *converter) node(n *XNode, s *sink, marks []document.Mark) {
	if n.Name == "" && n.Text != "" {
		c.text(n.Text, s, marks)
		return
	}
	if level, ok := headingLevels[n.Name]; ok {
		s.begin(document.Node{Type: "heading", Attrs: map[string]any{"level": min(level, document.MaxHeadingLevel)}})
		c.children(n, s, marks)
		s.flush()
		return
	}
	switch n.Name {
	case "p":
		s.begin(document.Node{Type: "paragraph"})
		c.children(n, s, marks)
		s.flush()
	case "br":
		s.inline(document.Node{Type: "hardBreak"})
	case "ul", "ol":
		s.block(c.list(n))
	case "li":
		s.block(document.Node{Type: "bulletList", Content: []document.Node{{Type: "listItem", Content: c.filled(n.Children)}}})
	case "blockquote":
		s.block(document.Node{Type: "blockquote", Content: c.filled(n.Children)})
	case "pre":
		code := document.Node{Type: "codeBlock"}
		if text := strings.TrimPrefix(Words(&XNode{Children: n.Children}), "\n"); text != "" {
			code.Content = []document.Node{{Type: "text", Text: text}}
		}
		s.block(code)
	case "hr":
		s.block(document.Node{Type: "horizontalRule"})
	case "table":
		c.table(n, s)
	case "object":
		c.object(n, s)
	case "img":
		c.lose(LossImage, "An image it shows from outside the file was left out; images come along inside a .reqifz.")
	case "strong", "b":
		c.children(n, s, withMark(marks, document.Mark{Type: "bold"}))
	case "em", "i", "cite", "dfn", "var":
		c.children(n, s, withMark(marks, document.Mark{Type: "italic"}))
	case "del", "s", "strike":
		c.children(n, s, withMark(marks, document.Mark{Type: "strike"}))
	case "code", "tt", "kbd", "samp":
		c.children(n, s, withMark(marks, document.Mark{Type: "code"}))
	case "a":
		href := strings.TrimSpace(n.Attrs["href"])
		if href != "" && len(href) <= document.MaxHrefLength && document.SafeHref(href) {
			marks = withMark(marks, document.Mark{Type: "link", Attrs: map[string]any{"href": href}})
		}
		c.children(n, s, marks)
	case "u", "ins":
		c.lose(LossFormatting, "Underlining is not kept; the words are.")
		c.children(n, s, marks)
	case "sub", "sup":
		c.lose(LossFormatting, "Subscript and superscript are not kept; the words are.")
		c.children(n, s, marks)
	case "caption":
		s.begin(document.Node{Type: "paragraph"})
		c.children(n, s, marks)
		s.flush()
	default:
		if !transparent[n.Name] {
			c.lose(LossFormatting, "The %s formatting is not kept; the words are.", n.Name)
		}
		c.children(n, s, marks)
	}
}

func (c *converter) children(n *XNode, s *sink, marks []document.Mark) {
	for i := range n.Children {
		c.node(&n.Children[i], s, marks)
	}
}

// withMark adds a mark; code stands alone, as the editor keeps it.
func withMark(marks []document.Mark, m document.Mark) []document.Mark {
	for _, have := range marks {
		if have.Type == "code" || have.Type == m.Type {
			return marks
		}
	}
	if m.Type == "code" {
		return []document.Mark{m}
	}
	return append(append([]document.Mark{}, marks...), m)
}

var whitespace = regexp.MustCompile(`[ \t\r\n\f]+`)

// text adds words as XHTML shows them: runs of white space as one space, none at a paragraph's start.
func (c *converter) text(raw string, s *sink, marks []document.Mark) {
	text := whitespace.ReplaceAllString(raw, " ")
	if s.open == nil && strings.TrimSpace(text) == "" {
		return
	}
	if s.open == nil || len(s.open.Content) == 0 || endsInSpace(s.open.Content[len(s.open.Content)-1]) {
		text = strings.TrimLeft(text, " ")
	}
	if text == "" {
		return
	}
	n := document.Node{Type: "text", Text: text}
	if len(marks) > 0 {
		n.Marks = marks
	}
	s.inline(n)
}

func endsInSpace(n document.Node) bool {
	return n.Type == "hardBreak" || (n.Type == "text" && strings.HasSuffix(n.Text, " "))
}

func (c *converter) list(n *XNode) document.Node {
	list := document.Node{Type: "bulletList"}
	if n.Name == "ol" {
		list.Type = "orderedList"
		if start, err := strconv.Atoi(strings.TrimSpace(n.Attrs["start"])); err == nil && start >= 0 && start <= document.MaxListStart {
			list.Attrs = map[string]any{"start": start}
		}
	}
	for i := range n.Children {
		child := &n.Children[i]
		switch {
		case child.Name == "li":
			list.Content = append(list.Content, document.Node{Type: "listItem", Content: c.filled(child.Children)})
		case child.Name == "" && strings.TrimSpace(child.Text) == "":
		default:
			list.Content = append(list.Content, document.Node{Type: "listItem", Content: c.filled([]XNode{*child})})
		}
	}
	if len(list.Content) == 0 {
		list.Content = []document.Node{{Type: "listItem", Content: []document.Node{{Type: "paragraph"}}}}
	}
	return list
}

func (c *converter) table(n *XNode, s *sink) {
	var rows []document.Node
	var collect func(*XNode)
	collect = func(parent *XNode) {
		for i := range parent.Children {
			child := &parent.Children[i]
			switch child.Name {
			case "tr":
				if row := c.row(child); len(row.Content) > 0 {
					rows = append(rows, row)
				}
			case "thead", "tbody", "tfoot":
				collect(child)
			case "caption":
				c.node(child, s, nil)
			}
		}
	}
	collect(n)
	if len(rows) > 0 {
		s.block(document.Node{Type: "table", Content: rows})
	}
}

func (c *converter) row(tr *XNode) document.Node {
	row := document.Node{Type: "tableRow"}
	for i := range tr.Children {
		cell := &tr.Children[i]
		kind := map[string]string{"td": "tableCell", "th": "tableHeader"}[cell.Name]
		if kind == "" {
			continue
		}
		out := document.Node{Type: kind, Content: c.filled(cell.Children)}
		for _, span := range []string{"colspan", "rowspan"} {
			if v, err := strconv.Atoi(strings.TrimSpace(cell.Attrs[span])); err == nil && v > 1 && v <= document.MaxTableSpan {
				if out.Attrs == nil {
					out.Attrs = map[string]any{}
				}
				out.Attrs[span] = v
			}
		}
		row.Content = append(row.Content, out)
	}
	return row
}

// object brings in what an object shows: an image as an image, another file as a file the text names.
func (c *converter) object(n *XNode, s *sink) {
	alt := strings.TrimSpace(whitespace.ReplaceAllString(Words(n), " "))
	chosen, ok := chooseObject(n)
	var placed Placed
	if ok {
		placed, ok = c.place(strings.TrimSpace(chosen.Attrs["data"]))
	}
	if !ok {
		ref := ""
		if chosen != nil {
			ref = " " + strings.TrimSpace(chosen.Attrs["data"])
		}
		c.lose(LossImage, "The file%s it shows could not come along; upload the .reqifz with its files to bring them.", ref)
		if alt != "" {
			c.text(alt, s, nil)
		}
		return
	}
	if !placed.Image {
		s.inline(document.Node{Type: document.NodeAttachment, Attrs: map[string]any{"attachmentId": placed.ID, "fileName": placed.FileName}})
		return
	}
	image := document.Node{Type: document.NodeImage, Attrs: map[string]any{"attachmentId": placed.ID}}
	if alt != "" {
		image.Attrs["alt"] = truncate(alt, document.MaxAltLength)
	}
	s.block(image)
}

func truncate(s string, limit int) string {
	if r := []rune(s); len(r) > limit {
		return string(r[:limit])
	}
	return s
}

// Words is the text of rich text alone, with a line end after every block, as a field without formatting
// holds it.
func Words(n *XNode) string {
	var b strings.Builder
	var walk func(*XNode)
	walk = func(x *XNode) {
		if x.Name == "" {
			b.WriteString(x.Text)
		}
		for i := range x.Children {
			walk(&x.Children[i])
		}
		if blockElements[x.Name] {
			b.WriteString("\n")
		}
	}
	walk(n)
	return b.String()
}

var blockElements = map[string]bool{
	"p": true, "div": true, "li": true, "tr": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "pre": true, "blockquote": true, "br": true, "caption": true, "dt": true, "dd": true,
}

var lineEnds = regexp.MustCompile(`\n\s*\n+`)

// PlainDocument is plain text as a document: a paragraph for each run of lines, a line break inside one.
func PlainDocument(text string) json.RawMessage {
	doc := document.Node{Type: "doc"}
	for _, para := range lineEnds.Split(strings.TrimSpace(text), -1) {
		block := document.Node{Type: "paragraph"}
		for i, line := range strings.Split(para, "\n") {
			if i > 0 {
				block.Content = append(block.Content, document.Node{Type: "hardBreak"})
			}
			if line = strings.TrimSpace(whitespace.ReplaceAllString(line, " ")); line != "" {
				block.Content = append(block.Content, document.Node{Type: "text", Text: line})
			}
		}
		if len(block.Content) > 0 {
			doc.Content = append(doc.Content, block)
		}
	}
	if len(doc.Content) == 0 {
		return document.Empty
	}
	out, _ := json.Marshal(doc)
	return out
}

// FileRef is where an export put a file of the project a document shows: its path in the archive and its type.
type FileRef func(attachmentID string) (ref, contentType string, ok bool)

// FromDocument turns a document of the editor into rich text as ReqIF holds it, inside one div. Files the
// export does not carry are left out, an image entirely and a named file as its name.
func FromDocument(raw json.RawMessage, file FileRef) *XNode {
	if file == nil {
		file = func(string) (string, string, bool) { return "", "", false }
	}
	div := XNode{Name: "div"}
	var root document.Node
	if len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, &root) == nil {
		div.Children = fromNodes(root.Content, file)
	}
	return &XNode{Children: []XNode{div}}
}

var blockNames = map[string]string{
	"paragraph": "p", "bulletList": "ul", "orderedList": "ol", "listItem": "li", "blockquote": "blockquote",
	"table": "table", "tableRow": "tr", "tableCell": "td", "tableHeader": "th",
}

// markNames are the elements of the marks XHTML writes; a link to a glossary term is written as its words.
var markNames = map[string]string{"bold": "strong", "italic": "em", "strike": "del", "code": "code", "link": "a"}

func fromNodes(nodes []document.Node, file FileRef) []XNode {
	var out []XNode
	for _, n := range nodes {
		switch n.Type {
		case "text":
			out = append(out, marked(XNode{Text: n.Text}, n.Marks))
		case "hardBreak":
			out = append(out, XNode{Name: "br"})
		case "horizontalRule":
			out = append(out, XNode{Name: "hr"})
		case "heading":
			level := 1
			if l, ok := n.Attrs["level"].(float64); ok {
				level = int(l)
			}
			out = append(out, XNode{Name: fmt.Sprintf("h%d", level), Children: fromNodes(n.Content, file)})
		case "codeBlock":
			out = append(out, XNode{Name: "pre", Children: fromNodes(n.Content, file)})
		case "mention":
			label, _ := n.Attrs["label"].(string)
			out = append(out, XNode{Text: "@" + label})
		case document.NodeImage, document.NodeAttachment:
			id, _ := n.Attrs["attachmentId"].(string)
			ref, contentType, ok := file(id)
			if ok {
				object := XNode{Name: "object", Attrs: map[string]string{"data": ref, "type": contentType}}
				if alt, _ := n.Attrs["alt"].(string); alt != "" {
					object.Children = []XNode{{Text: alt}}
				} else if name, _ := n.Attrs["fileName"].(string); name != "" {
					object.Children = []XNode{{Text: name}}
				}
				out = append(out, object)
			} else if name, _ := n.Attrs["fileName"].(string); name != "" {
				out = append(out, XNode{Text: name})
			}
		default:
			name, ok := blockNames[n.Type]
			if !ok {
				out = append(out, fromNodes(n.Content, file)...)
				continue
			}
			x := XNode{Name: name, Children: fromNodes(n.Content, file)}
			for _, attr := range []string{"colspan", "rowspan", "start"} {
				if v, ok := n.Attrs[attr].(float64); ok && (v > 1 || (attr == "start" && v != 1)) {
					if x.Attrs == nil {
						x.Attrs = map[string]string{}
					}
					x.Attrs[attr] = strconv.Itoa(int(v))
				}
			}
			out = append(out, x)
		}
	}
	return out
}

// marked wraps text in the elements of its marks, the first mark outermost.
func marked(text XNode, marks []document.Mark) XNode {
	out := text
	for i := len(marks) - 1; i >= 0; i-- {
		name, ok := markNames[marks[i].Type]
		if !ok {
			continue
		}
		x := XNode{Name: name, Children: []XNode{out}}
		if name == "a" {
			href, _ := marks[i].Attrs["href"].(string)
			x.Attrs = map[string]string{"href": href}
		}
		out = x
	}
	return out
}
