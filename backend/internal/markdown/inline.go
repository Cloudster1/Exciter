package markdown

import (
	"html"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	nethtml "golang.org/x/net/html"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// item is one piece of a paragraph as it is read: an inline node, or a picture, which a document holds as a block
// of its own.
type item struct {
	node  document.Node
	image bool
}

type items []item

func (it items) nodes() []document.Node {
	out := make([]document.Node, 0, len(it))
	for _, i := range it {
		out = append(out, i.node)
	}
	return out
}

// paragraphs splits read inline content at its pictures: the text between them is a paragraph each, the pictures
// blocks of their own.
func (c *converter) paragraphs(content items) []document.Node {
	var out []document.Node
	var run []document.Node
	flush := func() {
		run = trimInline(mergeText(run))
		if len(run) > 0 {
			out = append(out, document.Node{Type: "paragraph", Content: run})
		}
		run = nil
	}
	for _, i := range content {
		if i.image {
			flush()
			out = append(out, i.node)
			continue
		}
		run = append(run, i.node)
	}
	flush()
	return out
}

// mergeText joins neighbouring text of the same style in one pass, which joining them pairwise would make
// quadratic in a line of many pieces.
func mergeText(nodes []document.Node) []document.Node {
	var out []document.Node
	for i := 0; i < len(nodes); {
		n := nodes[i]
		j := i + 1
		if n.Type == "text" {
			var b strings.Builder
			b.WriteString(n.Text)
			for j < len(nodes) && nodes[j].Type == "text" && reflect.DeepEqual(nodes[j].Marks, n.Marks) {
				b.WriteString(nodes[j].Text)
				j++
			}
			n.Text = b.String()
		}
		out = append(out, n)
		i = j
	}
	return out
}

// flatten turns pictures into their words, for a heading, which holds none.
func (c *converter) flatten(content items) []document.Node {
	var out []document.Node
	for _, i := range content {
		if !i.image {
			out = append(out, i.node)
			continue
		}
		if alt, _ := i.node.Attrs["alt"].(string); alt != "" {
			out = append(out, document.Node{Type: "text", Text: alt})
		}
	}
	return out
}

// trimInline drops the spaces at either end of a line of inline nodes, which Markdown does not show.
func trimInline(nodes []document.Node) []document.Node {
	for len(nodes) > 0 && nodes[0].Type == "text" {
		nodes[0].Text = strings.TrimLeftFunc(nodes[0].Text, unicode.IsSpace)
		if nodes[0].Text != "" {
			break
		}
		nodes = nodes[1:]
	}
	for len(nodes) > 0 && nodes[len(nodes)-1].Type == "text" {
		last := len(nodes) - 1
		nodes[last].Text = strings.TrimRightFunc(nodes[last].Text, unicode.IsSpace)
		if nodes[last].Text != "" {
			break
		}
		nodes = nodes[:last]
	}
	return nodes
}

// withMark adds a mark unless one of its type is there, which nested emphasis of one kind would otherwise do twice.
func withMark(marks []document.Mark, m document.Mark) []document.Mark {
	if hasMark(marks, m.Type) {
		return marks
	}
	out := append(slices.Clone(marks), m)
	slices.SortStableFunc(out, func(a, b document.Mark) int { return markRank(a.Type) - markRank(b.Type) })
	return out
}

func textItem(s string, marks []document.Mark) items {
	if s == "" {
		return nil
	}
	return items{{node: document.Node{Type: "text", Text: s, Marks: marks}}}
}

// inlines reads a run of inline nodes under the marks given. A span Render wrote takes the nodes up to its end tag
// as its words.
func (c *converter) inlines(nodes []ast.Node, marks []document.Mark, images bool) items {
	closers := c.spanClosers(nodes)
	c.nest++
	defer func() { c.nest-- }()
	if c.nest > maxInlineNesting {
		c.refuse("This Markdown nests its styles and links too deeply; simplify them.")
		return nil
	}
	var out items
	for i := 0; i < len(nodes); i++ {
		switch n := nodes[i].(type) {
		case *ast.Text:
			s := string(n.Segment.Value(c.src))
			if !n.IsRaw() {
				s = decode(s)
			}
			out = append(out, textItem(s, marks)...)
			switch {
			case n.HardLineBreak():
				out = append(out, item{node: document.Node{Type: "hardBreak", Marks: marks}})
			case n.SoftLineBreak():
				out = append(out, textItem(" ", marks)...)
			}
		case *ast.String:
			out = append(out, textItem(string(n.Value), marks)...)
		case *ast.CodeSpan:
			out = append(out, textItem(c.code(n), withMark(marks, document.Mark{Type: "code"}))...)
		case *ast.Emphasis:
			mark := document.Mark{Type: "italic"}
			if n.Level >= 2 {
				mark.Type = "bold"
			}
			out = append(out, c.inlines(children(n), withMark(marks, mark), images)...)
		case *extast.Strikethrough:
			out = append(out, c.inlines(children(n), withMark(marks, document.Mark{Type: "strike"}), images)...)
		case *ast.Link:
			out = append(out, c.link(n, marks, images)...)
		case *ast.AutoLink:
			label, href := string(n.Label(c.src)), string(n.URL(c.src))
			if n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(href), "mailto:") {
				href = "mailto:" + href
			}
			if !document.SafeHref(href) {
				c.refuse("The address %s is not a web, mail or site address; write one that is.", label)
				continue
			}
			out = append(out, textItem(label, withMark(marks, linkMark(href, "")))...)
		case *ast.Image:
			alt := c.plain(n)
			if !images {
				out = append(out, textItem(alt, marks)...)
				continue
			}
			out = append(out, c.imageItem(decode(string(n.Destination)), alt, "")...)
		case *ast.RawHTML:
			made, skip := c.rawHTML(c.raw(n), nodes[i+1:], closers[i+1]-(i+1), marks, images)
			out = append(out, made...)
			i += skip
		default:
			out = append(out, c.inlines(children(n), marks, images)...)
		}
	}
	return out
}

// linkMark is a link as the editor writes every link, so a link read from Markdown is the one it would have made.
func linkMark(href, title string) document.Mark {
	attrs := map[string]any{"href": href, "target": "_blank", "rel": "noopener noreferrer nofollow", "class": nil, "title": nil}
	if title != "" {
		attrs["title"] = title
	}
	return document.Mark{Type: "link", Attrs: attrs}
}

func (c *converter) code(n *ast.CodeSpan) string {
	var b strings.Builder
	for k := n.FirstChild(); k != nil; k = k.NextSibling() {
		switch t := k.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(c.src))
		case *ast.String:
			b.Write(t.Value)
		}
	}
	return strings.ReplaceAll(b.String(), "\n", " ")
}

func (c *converter) raw(n *ast.RawHTML) string {
	var b strings.Builder
	for i := range n.Segments.Len() {
		seg := n.Segments.At(i)
		b.Write(seg.Value(c.src))
	}
	return b.String()
}

// spanClosers says, for each place in a run, where the next span end tag is at or after it, or the run's length
// when none follows.
func (c *converter) spanClosers(nodes []ast.Node) []int {
	out := make([]int, len(nodes)+1)
	out[len(nodes)] = len(nodes)
	for i := len(nodes) - 1; i >= 0; i-- {
		out[i] = out[i+1]
		if r, ok := nodes[i].(*ast.RawHTML); ok && strings.EqualFold(strings.TrimSpace(c.raw(r)), "</span>") {
			out[i] = i
		}
	}
	return out
}

var brTag = regexp.MustCompile(`(?i)^<br\s*/?>$`)

var uuidText = regexp.MustCompile(document.UUIDPattern)

// rawHTML reads one inline tag, and for a mention Render wrote, the nodes after it up to its end tag, answering how
// many of those it took.
func (c *converter) rawHTML(raw string, rest []ast.Node, end int, marks []document.Mark, images bool) (items, int) {
	if brTag.MatchString(strings.TrimSpace(raw)) {
		return items{{node: document.Node{Type: "hardBreak", Marks: marks}}}, 0
	}
	if strings.HasPrefix(raw, "<!--") {
		return nil, 0
	}
	tok := nethtml.NewTokenizer(strings.NewReader(raw))
	if kind := tok.Next(); kind != nethtml.StartTagToken && kind != nethtml.SelfClosingTagToken {
		c.refuse("A requirement holds no HTML; write it as Markdown instead.")
		return nil, 0
	}
	e := startTag(tok)
	if e.name == "img" {
		if !images {
			return textItem(e.attrs["alt"], marks), 0
		}
		return c.imageItem(e.attrs["src"], e.attrs["alt"], e.attrs["width"]), 0
	}
	if e.name != "span" || e.attrs[exciterAttr] != kindMention || end >= len(rest) {
		c.refuse("A requirement holds no HTML; write it as Markdown instead.")
		return nil, 0
	}
	var words strings.Builder
	for _, n := range rest[:end] {
		words.WriteString(c.plain(n))
	}
	label := strings.TrimPrefix(strings.TrimSpace(words.String()), "@")
	id := e.attrs["data-id"]
	if !uuidText.MatchString(id) || !c.resolve.person(id) || strings.TrimSpace(label) == "" {
		c.refuse("The mention of %s names nobody who is a member here; mention somebody by their id from list_people.", label)
		return nil, end + 1
	}
	return items{{node: document.Node{Type: "mention", Marks: marks, Attrs: map[string]any{
		"id": id, "label": label, "mentionSuggestionChar": "@",
	}}}}, end + 1
}

// link reads a link: one to a file of the requirement is that file, any other the address it gives.
func (c *converter) link(n *ast.Link, marks []document.Mark, images bool) items {
	dest := decode(string(n.Destination))
	if id, ok := strings.CutPrefix(dest, FileScheme); ok {
		name, found := c.resolve.file(id)
		if !found {
			c.refuse("%s is not a file of this requirement; upload it first, then point at it by its id.", dest)
			return nil
		}
		return items{{node: document.Node{Type: document.NodeAttachment, Marks: marks, Attrs: map[string]any{
			"attachmentId": id, "fileName": name,
		}}}}
	}
	if !document.SafeHref(dest) {
		c.refuse("The link to %s is not a web, mail or site address; link to one that is.", dest)
		return c.inlines(children(n), marks, images)
	}
	return c.inlines(children(n), withMark(marks, linkMark(dest, decode(string(n.Title)))), images)
}

// imageItem reads a picture, which can only be a file of the requirement, since a requirement shows its own files.
func (c *converter) imageItem(src, alt, width string) items {
	id, ok := strings.CutPrefix(src, FileScheme)
	if !ok {
		c.refuse("A requirement shows only its own pictures; upload %s as an attachment of the requirement, then point at it with %s<id>.", src, FileScheme)
		return nil
	}
	if _, found := c.resolve.file(id); !found {
		c.refuse("%s is not a file of this requirement; upload it first, then point at it by its id.", src)
		return nil
	}
	attrs := map[string]any{"attachmentId": id, "alt": nil, "width": nil}
	if alt != "" {
		attrs["alt"] = alt
	}
	if width != "" {
		w, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(width), "px"))
		if err != nil || w < 1 || w > document.MaxImageWidth {
			c.refuse("A picture's width is a number of pixels from 1 to %d.", document.MaxImageWidth)
			return nil
		}
		attrs["width"] = w
	}
	return items{{node: document.Node{Type: document.NodeImage, Attrs: attrs}, image: true}}
}

// plain is the words of a node and everything in it, escapes resolved.
func (c *converter) plain(n ast.Node) string {
	var b strings.Builder
	var walk func(n ast.Node, depth int)
	walk = func(n ast.Node, depth int) {
		if depth > document.MaxDepth {
			return
		}
		switch t := n.(type) {
		case *ast.Text:
			v := string(t.Segment.Value(c.src))
			if !t.IsRaw() {
				v = decode(v)
			}
			b.WriteString(v)
			if t.SoftLineBreak() || t.HardLineBreak() {
				b.WriteByte(' ')
			}
			return
		case *ast.String:
			b.Write(t.Value)
			return
		case *ast.CodeSpan:
			b.WriteString(c.code(t))
			return
		case *ast.RawHTML:
			return
		}
		for k := n.FirstChild(); k != nil; k = k.NextSibling() {
			walk(k, depth+1)
		}
	}
	walk(n, 0)
	return b.String()
}

// decode resolves backslash escapes and character references in one pass, as Markdown reads text, so an escaped
// ampersand stays an ampersand.
func decode(s string) string {
	if !strings.ContainsAny(s, `\&`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if i+1 < len(s) && isASCIIPunct(s[i+1]) {
				b.WriteByte(s[i+1])
				i++
				continue
			}
		case '&':
			if m := entityLike.FindString(s[i:]); m != "" {
				if out := html.UnescapeString(m); out != m {
					b.WriteString(strings.ReplaceAll(out, "\x00", replacement))
					i += len(m) - 1
					continue
				}
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isASCIIPunct(c byte) bool {
	return c < 0x80 && unicode.IsPunct(rune(c)) || strings.IndexByte("$+<=>^`|~", c) >= 0
}
