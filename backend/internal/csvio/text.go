package csvio

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// paragraphBreak is a blank line, which separates the paragraphs of a cell.
var paragraphBreak = regexp.MustCompile(`\r?\n[ \t]*\r?\n`)

// Document is the plain text of a cell as a document: a paragraph for each run of lines between blank lines,
// a line break inside one for every other line end.
func Document(text string) json.RawMessage {
	doc := document.Node{Type: "doc"}
	for _, para := range paragraphBreak.Split(strings.TrimSpace(text), -1) {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		block := document.Node{Type: "paragraph"}
		for i, line := range strings.Split(strings.ReplaceAll(para, "\r\n", "\n"), "\n") {
			if i > 0 {
				block.Content = append(block.Content, document.Node{Type: "hardBreak"})
			}
			if line = strings.TrimSpace(line); line != "" {
				block.Content = append(block.Content, document.Node{Type: "text", Text: line})
			}
		}
		doc.Content = append(doc.Content, block)
	}
	out, _ := json.Marshal(doc)
	return out
}

// PlainText is a document as the cell of a file writes it: its blocks as paragraphs, a blank line between two.
// What a cell cannot hold, such as a table's grid or a list's bullets, is left as its words.
func PlainText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var root document.Node
	if err := json.Unmarshal(raw, &root); err != nil {
		return ""
	}
	parts := make([]string, 0, len(root.Content))
	for _, block := range root.Content {
		if text := document.PlainText(document.Node{Type: "doc", Content: []document.Node{block}}); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}
