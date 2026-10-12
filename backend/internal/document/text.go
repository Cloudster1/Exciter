package document

import "strings"

// PlainText is the words of a document, one block per line and table cells separated by tabs, for the search
// index (#20) and anywhere markup cannot go.
func PlainText(root Node) string {
	var b strings.Builder
	writeBlocks(&b, root.Content, 0)
	return strings.TrimSpace(b.String())
}

func writeBlocks(b *strings.Builder, blocks []Node, depth int) {
	if depth > MaxDepth {
		return
	}
	for _, n := range blocks {
		switch n.Type {
		case "paragraph", "heading", "codeBlock":
			if text := InlineText(n); text != "" {
				b.WriteString(text)
				b.WriteByte('\n')
			}
		case "tableRow":
			cells := make([]string, 0, len(n.Content))
			for _, cell := range n.Content {
				var inner strings.Builder
				writeBlocks(&inner, cell.Content, depth+1)
				cells = append(cells, strings.ReplaceAll(strings.TrimSpace(inner.String()), "\n", " "))
			}
			b.WriteString(strings.Join(cells, "\t"))
			b.WriteByte('\n')
		default:
			writeBlocks(b, n.Content, depth+1)
		}
	}
}

// InlineText flattens a block's inline children: text, mentions as @Name and breaks as newlines.
func InlineText(n Node) string {
	var b strings.Builder
	for _, c := range n.Content {
		switch c.Type {
		case "text":
			b.WriteString(c.Text)
		case "mention":
			label, _ := c.Attrs["label"].(string)
			b.WriteString("@" + label)
		case "hardBreak":
			b.WriteByte('\n')
		}
	}
	return b.String()
}
