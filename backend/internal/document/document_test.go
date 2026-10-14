package document

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAcceptsWhatTheEditorWrites(t *testing.T) {
	doc := `{"type":"doc","content":[
		{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Scope"}]},
		{"type":"paragraph","content":[{"type":"text","text":"The basket ","marks":[{"type":"bold"}]},
			{"type":"mention","attrs":{"id":"0195f000-0000-7000-8000-00000000000a","label":"Ada"}},
			{"type":"text","text":" pays","marks":[{"type":"link","attrs":{"href":"https://example.test/pay","target":"_blank","rel":"noopener noreferrer nofollow","class":null}}]}]},
		{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]}]}]},
		{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","attrs":{"colspan":1,"rowspan":1,"colwidth":null},"content":[{"type":"paragraph"}]}]}]},
		{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"x := 1"}]}]}`
	if _, err := Parse(json.RawMessage(doc)); err != nil {
		t.Fatalf("a document the editor writes was refused: %v", err)
	}
	if _, err := Parse(Empty); err != nil {
		t.Fatalf("the empty document was refused: %v", err)
	}
}

func TestRefusesWhatTheEditorCannotShow(t *testing.T) {
	for name, doc := range map[string]string{
		"not a document":     `{"type":"paragraph"}`,
		"an unknown block":   `{"type":"doc","content":[{"type":"panel"}]}`,
		"an unknown field":   `{"type":"doc","extra":1}`,
		"a heading too deep": `{"type":"doc","content":[{"type":"heading","attrs":{"level":4}}]}`,
		"a script link":      `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"javascript:alert(1)"}}]}]}]}`,
		"a diff mark":        `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"diffInsert"}]}]}]}`,
		"empty text":         `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":""}]}]}`,
		"a block in text":    `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"paragraph"}]}]}`,
		"a style twice":      `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"bold"},{"type":"bold"}]}]}]}`,
		"two documents":      `{"type":"doc"}{"type":"doc"}`,
	} {
		if _, err := Parse(json.RawMessage(doc)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s was not refused: %v", name, err)
		}
	}
}

func TestPlainTextReadsBlocksAndCells(t *testing.T) {
	doc, err := Parse(json.RawMessage(`{"type":"doc","content":[
		{"type":"paragraph","content":[{"type":"text","text":"Pay "},{"type":"mention","attrs":{"id":"0195f000-0000-7000-8000-00000000000a","label":"Ada"}}]},
		{"type":"table","content":[{"type":"tableRow","content":[
			{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]},
			{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := PlainText(doc); got != "Pay @Ada\na\tb" {
		t.Errorf("plain text = %q", got)
	}
}

// The web editor's test reads the allowlist from api/, so a stale copy would let the two drift apart.
func TestTheCheckedInAllowlistIsCurrent(t *testing.T) {
	want, err := Allowed.JSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "document-allowlist.json"))
	if err != nil || string(got) != string(want) {
		t.Fatalf("api/document-allowlist.json is out of date; run make openapi and commit the result (%v)", err)
	}
	want, err = CommentAllowed.JSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join("..", "..", "..", "api", "comment-allowlist.json"))
	if err != nil || string(got) != string(want) {
		t.Fatalf("api/comment-allowlist.json is out of date; run make openapi and commit the result (%v)", err)
	}
}
