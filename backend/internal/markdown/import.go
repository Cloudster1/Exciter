package markdown

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	nethtml "golang.org/x/net/html"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// replacement stands in for bytes that are not text.
const replacement = "�"

// maxParseCost bounds the brackets of each line times its length: the work a link or tag that never closes costs
// the parser.
const maxParseCost = 50_000_000

// maxInlineNesting bounds how deep emphasis and links are followed.
const maxInlineNesting = 2 * document.MaxDepth

var parser = goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough)).Parser()

// Convert reads Markdown as a document for the caller to hold to its allowlist. Where Stator keeps going with a
// warning, a requirement's text is normative and its writer at hand, so what would not come across is refused.
func Convert(src string, resolve Resolve) (document.Node, error) {
	if len(src) > MaxSourceBytes {
		return document.Node{}, &Refusal{fmt.Sprintf("This Markdown is over %d MB; split it into several requirements.", MaxSourceBytes>>20)}
	}
	raw := bytes.ReplaceAll(bytes.ToValidUTF8([]byte(src), []byte(replacement)), []byte{0}, []byte(replacement))
	if nestsTooDeeply(raw) {
		return document.Node{}, &Refusal{"This Markdown nests its lists and quotes too deeply; flatten them."}
	}
	if tooTangled(raw) {
		return document.Node{}, &Refusal{"This Markdown has long lines full of brackets; break them into shorter lines."}
	}
	c := &converter{src: raw, resolve: resolve}
	content := c.blocks(children(parser.Parse(text.NewReader(raw))), 0)
	if c.refusal != "" {
		return document.Node{}, &Refusal{c.refusal}
	}
	if len(content) == 0 {
		content = []document.Node{{Type: "paragraph"}}
	}
	return document.Node{Type: "doc", Content: content}, nil
}

// nestsTooDeeply looks for a line that opens more quotes and list items than a document nests, before the parser
// spends time with the square of it.
func nestsTooDeeply(src []byte) bool {
	// Nesting by indentation alone takes two columns a level.
	const maxIndent = 4 * document.MaxDepth
	for line := range bytes.SplitSeq(src, []byte("\n")) {
		indent := 0
		for indent < len(line) && (line[indent] == ' ' || line[indent] == '\t') {
			indent++
		}
		if indent > maxIndent {
			return true
		}
		depth, i := 0, 0
		for i < len(line) {
			for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
				i++
			}
			if i >= len(line) {
				break
			}
			switch c := line[i]; {
			case c == '>':
				i++
			case (c == '-' || c == '*' || c == '+') && (i+1 == len(line) || line[i+1] == ' ' || line[i+1] == '\t'):
				i++
			case c >= '0' && c <= '9':
				j := i
				for j < len(line) && j-i < 10 && line[j] >= '0' && line[j] <= '9' {
					j++
				}
				if j >= len(line) || (line[j] != '.' && line[j] != ')') {
					i = len(line)
					continue
				}
				i = j + 1
			default:
				i = len(line)
				continue
			}
			depth++
			if depth > document.MaxDepth {
				return true
			}
		}
	}
	return false
}

func tooTangled(src []byte) bool {
	cost := 0
	for line := range bytes.SplitSeq(src, []byte("\n")) {
		cost += (bytes.Count(line, []byte("[")) + bytes.Count(line, []byte("<"))) * len(line)
		if cost > maxParseCost {
			return true
		}
	}
	return false
}

type converter struct {
	src     []byte
	resolve Resolve
	// refusal is the first sentence saying what would not come back as written.
	refusal string
	nest    int
}

func (c *converter) refuse(format string, args ...any) {
	if c.refusal == "" {
		c.refusal = fmt.Sprintf(format, args...)
	}
}

func children(n ast.Node) []ast.Node {
	var out []ast.Node
	for k := n.FirstChild(); k != nil; k = k.NextSibling() {
		out = append(out, k)
	}
	return out
}

func (c *converter) blocks(nodes []ast.Node, depth int) []document.Node {
	if depth > document.MaxDepth {
		c.refuse("This Markdown nests its lists and quotes too deeply; flatten them.")
		return nil
	}
	var out []document.Node
	for _, n := range nodes {
		out = append(out, c.block(n, depth)...)
	}
	return out
}

func (c *converter) lines(n ast.Node) string {
	var b strings.Builder
	lines := n.Lines()
	for i := range lines.Len() {
		seg := lines.At(i)
		b.Write(seg.Value(c.src))
	}
	if h, ok := n.(*ast.HTMLBlock); ok && h.HasClosure() {
		b.Write(h.ClosureLine.Value(c.src))
	}
	return b.String()
}

func (c *converter) block(n ast.Node, depth int) []document.Node {
	switch n := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return c.paragraphs(c.inlines(children(n), nil, true))
	case *ast.Heading:
		if n.Level > document.MaxHeadingLevel {
			c.refuse("A requirement's headings go down to level %d; make this one ### or shallower.", document.MaxHeadingLevel)
		}
		level := min(n.Level, document.MaxHeadingLevel)
		content := mergeText(c.flatten(c.inlines(children(n), nil, false)))
		return []document.Node{{Type: "heading", Attrs: map[string]any{"level": level}, Content: trimInline(content)}}
	case *ast.ThematicBreak:
		return []document.Node{{Type: "horizontalRule"}}
	case *ast.CodeBlock:
		return []document.Node{codeBlock(c.lines(n), nil)}
	case *ast.FencedCodeBlock:
		var language any
		if n.Info != nil {
			if fields := strings.Fields(string(n.Language(c.src))); len(fields) > 0 {
				if lang := strings.ToLower(decode(fields[0])); languagePattern.MatchString(lang) && len(lang) <= maxLanguageLength {
					language = lang
				}
			}
		}
		return []document.Node{codeBlock(c.lines(n), language)}
	case *ast.Blockquote:
		content := c.blocks(children(n), depth+1)
		if len(content) == 0 {
			content = []document.Node{{Type: "paragraph"}}
		}
		return []document.Node{{Type: "blockquote", Content: content}}
	case *ast.List:
		return c.list(n, depth)
	case *ast.HTMLBlock:
		return c.htmlBlock(c.lines(n))
	case *extast.Table:
		return c.table(n, depth)
	}
	return nil
}

var languagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+#-]*$`)

// maxLanguageLength matches the longest language name a code block keeps.
const maxLanguageLength = 32

func codeBlock(code string, language any) document.Node {
	code = strings.TrimSuffix(code, "\n")
	n := document.Node{Type: "codeBlock", Attrs: map[string]any{"language": language}}
	if code != "" {
		n.Content = []document.Node{{Type: "text", Text: code}}
	}
	return n
}

func (c *converter) list(n *ast.List, depth int) []document.Node {
	out := document.Node{Type: "bulletList"}
	if n.IsOrdered() {
		start := n.Start
		if start < 0 || start > document.MaxListStart {
			start = 1
		}
		out = document.Node{Type: "orderedList", Attrs: map[string]any{"start": start, "type": nil}}
	}
	for _, item := range children(n) {
		content := c.blocks(children(item), depth+2)
		if len(content) == 0 {
			content = []document.Node{{Type: "paragraph"}}
		}
		out.Content = append(out.Content, document.Node{Type: "listItem", Content: content})
	}
	return []document.Node{out}
}

// table reads a GFM table: its header row as header cells, each column's alignment on every cell of it.
func (c *converter) table(n *extast.Table, depth int) []document.Node {
	if depth+2 > document.MaxDepth {
		c.refuse("This Markdown nests its lists and quotes too deeply; flatten them.")
		return nil
	}
	out := document.Node{Type: "table"}
	for _, row := range children(n) {
		cellType := "tableCell"
		if _, ok := row.(*extast.TableHeader); ok {
			cellType = "tableHeader"
		}
		made := document.Node{Type: "tableRow"}
		for _, cell := range children(row) {
			attrs := map[string]any{"colspan": 1, "rowspan": 1, "colwidth": nil}
			if tc, ok := cell.(*extast.TableCell); ok {
				switch tc.Alignment {
				case extast.AlignLeft:
					attrs["align"] = "left"
				case extast.AlignCenter:
					attrs["align"] = "center"
				case extast.AlignRight:
					attrs["align"] = "right"
				}
			}
			content := c.paragraphs(c.inlines(children(cell), nil, true))
			if len(content) == 0 {
				content = []document.Node{{Type: "paragraph"}}
			}
			made.Content = append(made.Content, document.Node{Type: cellType, Attrs: attrs, Content: content})
		}
		if len(made.Content) > 0 {
			out.Content = append(out.Content, made)
		}
	}
	if len(out.Content) == 0 {
		return nil
	}
	return []document.Node{out}
}

// htmlBlock reads an HTML block: a comment, which keeps two lists apart, and pictures Render writes; any other
// HTML is refused.
func (c *converter) htmlBlock(raw string) []document.Node {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || (strings.HasPrefix(trimmed, "<!--") && strings.HasSuffix(trimmed, "-->")) {
		return nil
	}
	if images, ok := c.imagesOnly(trimmed); ok {
		return images
	}
	c.refuse("A requirement holds no HTML; write it as Markdown instead.")
	return nil
}

// element is one start tag, read by the HTML tokenizer.
type element struct {
	name  string
	attrs map[string]string
}

func startTag(tok *nethtml.Tokenizer) element {
	name, more := tok.TagName()
	e := element{name: string(name), attrs: map[string]string{}}
	for more {
		var k, v []byte
		k, v, more = tok.TagAttr()
		e.attrs[string(k)] = string(v)
	}
	return e
}

func (c *converter) imagesOnly(raw string) ([]document.Node, bool) {
	tok := nethtml.NewTokenizer(strings.NewReader(raw))
	var out []document.Node
	for {
		switch tok.Next() {
		case nethtml.ErrorToken:
			return out, len(out) > 0
		case nethtml.TextToken:
			if strings.TrimSpace(string(tok.Text())) != "" {
				return nil, false
			}
		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			e := startTag(tok)
			if e.name != "img" {
				return nil, false
			}
			out = append(out, c.imageItem(e.attrs["src"], e.attrs["alt"], e.attrs["width"]).nodes()...)
		case nethtml.EndTagToken:
		default:
			return nil, false
		}
	}
}
