package document

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func blocksOf(t *testing.T, body string) []TextBlock {
	t.Helper()
	root, err := Parse(json.RawMessage(body))
	if err != nil {
		t.Fatal(err)
	}
	return TextBlocks(root)
}

func TestTextBlocksCountAsTheEditorDoes(t *testing.T) {
	const mention = `{"type":"mention","attrs":{"id":"0195f000-0000-7000-8000-00000000000a","label":"Ada"}}`
	const attachment = `{"type":"attachment","attrs":{"attachmentId":"0195f000-0000-7000-8000-00000000000a","fileName":"a.pdf"}}`
	for _, c := range []struct {
		name    string
		body    string
		text    string
		offsets []int
	}{
		{"plain text", `{"type":"text","text":"ab c"}`, "ab c", []int{0, 1, 2, 3, 4}},
		// U+1D400 is four bytes of UTF-8 and two UTF-16 code units; the umlaut is two bytes and one unit.
		{"outside the basic plane", `{"type":"text","text":"a𝐀ä"}`, "a\U0001D400ä", []int{0, 1, 1, 1, 1, 3, 3, 4}},
		{"a mention", `{"type":"text","text":"x"},` + mention + `,{"type":"text","text":"y"}`, "x@Ada" + "y", []int{0, 1, 1, 1, 1, 2, 3}},
		{"a line break", `{"type":"text","text":"a"},{"type":"hardBreak"},{"type":"text","text":"b"}`, "a\nb", []int{0, 1, 2, 3}},
		{"an attached file", `{"type":"text","text":"a"},` + attachment + `,{"type":"text","text":"b"}`, "ab", []int{0, 2, 3}},
		{"an attached file at the end", `{"type":"text","text":"a"},` + attachment, "a", []int{0, 2}},
	} {
		blocks := blocksOf(t, `{"type":"doc","content":[{"type":"paragraph","content":[`+c.body+`]}]}`)
		if len(blocks) != 1 {
			t.Fatalf("%s: %d blocks", c.name, len(blocks))
		}
		if blocks[0].Text != c.text || !slices.Equal(blocks[0].Offsets, c.offsets) {
			t.Errorf("%s: got %q %v, want %q %v", c.name, blocks[0].Text, blocks[0].Offsets, c.text, c.offsets)
		}
	}
}

func TestTextBlocksFindEveryTextBlockInOrder(t *testing.T) {
	blocks := blocksOf(t, `{"type":"doc","content":[
		{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Scope"}]},
		{"type":"paragraph"},
		{"type":"bulletList","content":[{"type":"listItem","content":[
			{"type":"paragraph","content":[{"type":"text","text":"outer"}]},
			{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"inner"}]}]}]}]}]},
		{"type":"horizontalRule"},
		{"type":"table","content":[{"type":"tableRow","content":[
			{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"cell"}]}]}]}]},
		{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quoted"}]}]},
		{"type":"codeBlock","content":[{"type":"text","text":"x := 1"}]}]}`)
	var got []string
	for _, b := range blocks {
		got = append(got, b.Text)
	}
	if want := []string{"Scope", "", "outer", "inner", "cell", "quoted", "x := 1"}; !slices.Equal(got, want) {
		t.Fatalf("blocks = %q, want %q", got, want)
	}
	if blocks[1].Position(0) != 0 || len(blocks[1].Offsets) != 1 {
		t.Errorf("an empty paragraph counts %v", blocks[1].Offsets)
	}
	if !blocks[6].Code || blocks[0].Code {
		t.Error("only the code block is code")
	}
}

func TestTextIsWhatInlineTextReads(t *testing.T) {
	for _, c := range textBlocksCases {
		var want []string
		var walk func(nodes []Node)
		walk = func(nodes []Node) {
			for _, n := range nodes {
				if n.Type == "paragraph" || n.Type == "heading" || n.Type == "codeBlock" {
					want = append(want, InlineText(n))
					continue
				}
				walk(n.Content)
			}
		}
		walk(c.doc.Content)
		var got []string
		for _, b := range TextBlocks(c.doc) {
			got = append(got, b.Text)
			if len(b.Offsets) != len(b.Text)+1 {
				t.Errorf("%s: %d offsets for %d bytes", c.name, len(b.Offsets), len(b.Text))
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: %q, want %q", c.name, got, want)
		}
	}
}

func TestMentionedFindsTheNamesBytes(t *testing.T) {
	blocks := blocksOf(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"to "},
		{"type":"mention","attrs":{"id":"0195f000-0000-7000-8000-00000000000a","label":"Fast Eddie"}}]}]}`)
	b := blocks[0]
	if !b.Mentioned(4, 8) || b.Mentioned(0, 3) {
		t.Errorf("mention ranges are wrong for %q", b.Text)
	}
	if b.Position(len(b.Text)) != 4 || b.Position(5) != 3 {
		t.Errorf("a mention counts %v", b.Offsets)
	}
}

func TestPositionStaysInsideTheBlock(t *testing.T) {
	b := blocksOf(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"ab"}]}]}`)[0]
	if b.Position(-1) != 0 || b.Position(99) != 2 {
		t.Errorf("out of range offsets are not clamped: %d %d", b.Position(-1), b.Position(99))
	}
	if (TextBlock{}).Position(3) != 0 {
		t.Error("a block without offsets answers 0")
	}
}

// The web client's test counts the same documents in the editor, so a stale copy would let the two drift apart.
func TestTheEditorCountsTextBlocksAsGoDoes(t *testing.T) {
	want, err := TextBlocksFixture()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "document-textblocks.json"))
	if err != nil || string(got) != string(want) {
		t.Fatalf("api/document-textblocks.json is out of date; run make openapi and commit the result (%v)", err)
	}
}
