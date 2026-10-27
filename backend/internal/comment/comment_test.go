package comment

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const saysNothing = "Write something before you post the comment."

// docOf is a comment document of one paragraph holding the given inline nodes.
func docOf(inline ...map[string]any) map[string]any {
	content := make([]any, len(inline))
	for i, n := range inline {
		content[i] = n
	}
	return map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": content}}}
}

func text(words string) map[string]any { return map[string]any{"type": "text", "text": words} }

func mention(label string) map[string]any {
	return map[string]any{"type": "mention", "attrs": map[string]any{"id": "0195f000-0000-7000-8000-0000000000e1", "label": label}}
}

func TestABodyThatSaysNothingIsRefusedOnItsField(t *testing.T) {
	for name, body := range map[string]any{
		"no body":       nil,
		"only spaces":   docOf(text("   ")),
		"an empty line": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph"}}},
	} {
		_, err := cleanBody(body)
		var field *FieldError
		if !errors.As(err, &field) || field.Field != "body" || field.Message != saysNothing {
			t.Errorf("%s: got %v, want the sentence on body", name, err)
		}
	}
}

func TestABodyOutsideTheCommentSubsetIsRefusedWithItsSentence(t *testing.T) {
	for name, body := range map[string]any{
		"a rule":  map[string]any{"type": "doc", "content": []any{map[string]any{"type": "horizontalRule"}}},
		"a table": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "table"}}},
		"no doc":  map[string]any{"type": "paragraph"},
	} {
		_, err := cleanBody(body)
		var field *FieldError
		if !errors.As(err, &field) || field.Field != "body" || field.Message == saysNothing || !strings.HasSuffix(field.Message, ".") {
			t.Errorf("%s: got %v, want a sentence on body that names what is wrong", name, err)
		}
	}
}

// A mention alone is something said, as in Stator: it tells the person named.
func TestALoneMentionIsAComment(t *testing.T) {
	raw, err := cleanBody(docOf(mention("Ada")))
	if err != nil {
		t.Fatalf("a lone mention was refused: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil || back["type"] != "doc" {
		t.Errorf("the body came back as %s: %v", raw, err)
	}
}

func TestWordsAndMentionsAreKeptAsSent(t *testing.T) {
	sent := docOf(text("Ask "), mention("Ada"), text(" now"))
	raw, err := cleanBody(sent)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(sent)
	if string(raw) != string(want) {
		t.Errorf("the body came back as %s, want %s", raw, want)
	}
}
