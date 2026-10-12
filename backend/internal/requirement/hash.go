package requirement

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

// Normative is what a revision says: changing any of it writes the next revision.
type Normative struct {
	Title      string
	Statement  json.RawMessage
	Rationale  json.RawMessage
	TypeID     *uuid.UUID
	Attributes map[string]json.RawMessage
}

// Hash is the SHA-256 of the canonical JSON of the content: object keys sorted, runs of white space in text
// collapsed and trimmed at the ends of each block, and empty paragraphs at the ends of a document dropped.
func (n Normative) Hash() string {
	attrs := map[string]any{}
	for id, raw := range n.Attributes {
		attrs[id] = canonical(raw)
	}
	var typeID any
	if n.TypeID != nil {
		typeID = n.TypeID.String()
	}
	// Marshal sorts the keys of every map, which is what makes the encoding canonical.
	encoded, _ := json.Marshal(map[string]any{
		"title": collapse(n.Title), "statement": canonical(n.Statement), "rationale": canonical(n.Rationale),
		"typeId": typeID, "attributes": attrs,
	})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// canonical decodes a value and, when it is a document, settles its white space.
func canonical(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	if doc, ok := v.(map[string]any); ok && doc["type"] == "doc" {
		content, _ := doc["content"].([]any)
		blocks := settleBlocks(content)
		for len(blocks) > 0 && emptyParagraph(blocks[0]) {
			blocks = blocks[1:]
		}
		for len(blocks) > 0 && emptyParagraph(blocks[len(blocks)-1]) {
			blocks = blocks[:len(blocks)-1]
		}
		doc["content"] = blocks
		return doc
	}
	return v
}

func settleBlocks(blocks []any) []any {
	out := make([]any, 0, len(blocks))
	for _, b := range blocks {
		node, ok := b.(map[string]any)
		if !ok {
			out = append(out, b)
			continue
		}
		children, _ := node["content"].([]any)
		if hasText(children) {
			node["content"] = settleInline(children)
		} else if len(children) > 0 {
			node["content"] = settleBlocks(children)
		}
		if c, ok := node["content"].([]any); ok && len(c) == 0 {
			delete(node, "content")
		}
		out = append(out, node)
	}
	return out
}

func hasText(nodes []any) bool {
	for _, n := range nodes {
		if m, ok := n.(map[string]any); ok && (m["type"] == "text" || m["type"] == "hardBreak" || m["type"] == "mention") {
			return true
		}
	}
	return false
}

// settleInline collapses white space within a block's text as a reader sees it: one space between words,
// none at the block's ends, and no piece of text left empty.
func settleInline(nodes []any) []any {
	out := make([]any, 0, len(nodes))
	space := true
	for _, n := range nodes {
		m, ok := n.(map[string]any)
		if !ok || m["type"] != "text" {
			out = append(out, n)
			space = false
			continue
		}
		text, _ := m["text"].(string)
		var b strings.Builder
		for _, r := range text {
			if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ' ' {
				if !space {
					b.WriteByte(' ')
				}
				space = true
				continue
			}
			b.WriteRune(r)
			space = false
		}
		if b.Len() > 0 {
			m["text"] = b.String()
			out = append(out, m)
		}
	}
	if len(out) > 0 {
		if last, ok := out[len(out)-1].(map[string]any); ok && last["type"] == "text" {
			trimmed := strings.TrimRight(last["text"].(string), " ")
			if trimmed == "" {
				out = out[:len(out)-1]
			} else {
				last["text"] = trimmed
			}
		}
	}
	return out
}

func emptyParagraph(b any) bool {
	m, ok := b.(map[string]any)
	if !ok || m["type"] != "paragraph" {
		return false
	}
	c, _ := m["content"].([]any)
	return len(c) == 0
}
