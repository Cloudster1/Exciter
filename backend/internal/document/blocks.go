package document

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// TextBlock is one text block of a document as the editor counts them, with where each byte of Text sits.
type TextBlock struct {
	// Text is InlineText of the block.
	Text string
	// Offsets holds the editor's offset within the block of each byte of Text, and of its end.
	Offsets []int
	// Code says it is a code block, whose words are not prose.
	Code bool
	// mentions are the byte ranges of Text that name a person.
	mentions [][2]int
}

// TextBlocks lists every paragraph, heading and code block in document order, at any depth and empty ones
// included, as the editor's text blocks.
func TextBlocks(root Node) []TextBlock {
	var out []TextBlock
	collectBlocks(&out, root.Content, 0)
	return out
}

func collectBlocks(out *[]TextBlock, nodes []Node, depth int) {
	if depth > MaxDepth {
		return
	}
	for _, n := range nodes {
		switch n.Type {
		case "paragraph", "heading", "codeBlock":
			*out = append(*out, textBlock(n))
		default:
			collectBlocks(out, n.Content, depth+1)
		}
	}
}

// textBlock counts as ProseMirror does: text in UTF-16 code units, every inline leaf as one.
func textBlock(n Node) TextBlock {
	var b strings.Builder
	var offsets []int
	var mentions [][2]int
	pos := 0
	for _, c := range n.Content {
		switch c.Type {
		case "text":
			for i := 0; i < len(c.Text); {
				r, size := utf8.DecodeRuneInString(c.Text[i:])
				for range size {
					offsets = append(offsets, pos)
				}
				pos += max(utf16.RuneLen(r), 1)
				i += size
			}
			b.WriteString(c.Text)
		case "mention":
			label, _ := c.Attrs["label"].(string)
			shown := "@" + label
			mentions = append(mentions, [2]int{b.Len(), b.Len() + len(shown)})
			for range len(shown) {
				offsets = append(offsets, pos)
			}
			pos++
			b.WriteString(shown)
		case "hardBreak":
			offsets = append(offsets, pos)
			pos++
			b.WriteByte('\n')
		case NodeAttachment:
			pos++
		}
	}
	offsets = append(offsets, pos)
	return TextBlock{Text: b.String(), Offsets: offsets, Code: n.Type == "codeBlock", mentions: mentions}
}

// Position is the editor's offset within the block of a byte offset into Text.
func (b TextBlock) Position(byteOffset int) int {
	if len(b.Offsets) == 0 {
		return 0
	}
	return b.Offsets[min(max(byteOffset, 0), len(b.Offsets)-1)]
}

// Mentioned says whether the bytes from from to to touch a mention, whose words are a person's name.
func (b TextBlock) Mentioned(from, to int) bool {
	// Mentions are in order and apart, so the first that ends after from is the only one to look at.
	i := sort.Search(len(b.mentions), func(i int) bool { return b.mentions[i][1] > from })
	return i < len(b.mentions) && b.mentions[i][0] < to
}

// textBlocksCase is one document of the fixture the web client's test counts again.
type textBlocksCase struct {
	Name   string           `json:"name"`
	Doc    Node             `json:"doc"`
	Blocks []textBlocksJSON `json:"blocks"`
}

type textBlocksJSON struct {
	Text    string `json:"text"`
	Offsets []int  `json:"offsets"`
}

const fixtureMentionID = "0195f000-0000-7000-8000-00000000000a"

func fixturePara(inline ...Node) Node { return Node{Type: "paragraph", Content: inline} }
func fixtureText(s string) Node       { return Node{Type: "text", Text: s} }
func fixtureDoc(blocks ...Node) Node  { return Node{Type: "doc", Content: blocks} }

var textBlocksCases = []struct {
	name string
	doc  Node
}{
	{"plain text", fixtureDoc(fixturePara(fixtureText("The pump shall stop.")))},
	{"characters outside the basic plane", fixtureDoc(fixturePara(fixtureText("Go \U0001D400 now, äöü.")))},
	{"a mention", fixtureDoc(fixturePara(fixtureText("Ask "), Node{Type: "mention", Attrs: map[string]any{"id": fixtureMentionID, "label": "Ada L"}}, fixtureText(" now")))},
	{"a line break", fixtureDoc(fixturePara(fixtureText("one"), Node{Type: "hardBreak"}, fixtureText("two")))},
	{"an attached file", fixtureDoc(fixturePara(fixtureText("see "), Node{Type: NodeAttachment, Attrs: map[string]any{"attachmentId": fixtureMentionID, "fileName": "a.pdf"}}, fixtureText(" here")))},
	{"a nested list", fixtureDoc(Node{Type: "bulletList", Content: []Node{{Type: "listItem", Content: []Node{
		fixturePara(fixtureText("outer")),
		{Type: "orderedList", Content: []Node{{Type: "listItem", Content: []Node{fixturePara(fixtureText("inner"))}}}},
	}}}}, fixturePara(fixtureText("after")))},
	{"a table", fixtureDoc(Node{Type: "table", Content: []Node{{Type: "tableRow", Content: []Node{
		{Type: "tableHeader", Content: []Node{fixturePara(fixtureText("head"))}},
		{Type: "tableCell", Content: []Node{fixturePara(fixtureText("cell")), fixturePara()}},
	}}}})},
	{"headings, quotes, code and empty paragraphs", fixtureDoc(
		Node{Type: "heading", Attrs: map[string]any{"level": float64(2)}, Content: []Node{fixtureText("Scope")}},
		fixturePara(),
		Node{Type: "horizontalRule"},
		Node{Type: "blockquote", Content: []Node{fixturePara(fixtureText("quoted"))}},
		Node{Type: NodeImage, Attrs: map[string]any{"attachmentId": fixtureMentionID}},
		Node{Type: "codeBlock", Content: []Node{fixtureText("x := 1")}},
	)},
}

// TextBlocksFixture is what api/document-textblocks.json holds: documents and their blocks as Go counts them,
// for the web client's test to count again.
func TextBlocksFixture() ([]byte, error) {
	cases := make([]textBlocksCase, 0, len(textBlocksCases))
	for _, c := range textBlocksCases {
		blocks := []textBlocksJSON{}
		for _, b := range TextBlocks(c.doc) {
			blocks = append(blocks, textBlocksJSON{Text: b.Text, Offsets: b.Offsets})
		}
		cases = append(cases, textBlocksCase{Name: c.name, Doc: c.doc, Blocks: blocks})
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(cases); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
