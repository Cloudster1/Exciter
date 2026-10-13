package document

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"
)

// RewriteReferences points every file node of a document named in ids at its new id, as Stator's copy does,
// and reports whether anything changed. Numbers keep their spelling.
func RewriteReferences(body json.RawMessage, ids map[uuid.UUID]uuid.UUID) (json.RawMessage, bool, error) {
	if len(body) == 0 || len(ids) == 0 {
		return body, false, nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, false, err
	}
	if !rewrite(root, ids, 0) {
		return body, false, nil
	}
	out, err := json.Marshal(root)
	return out, true, err
}

// rewrite walks every object, not only a document's content, so a map of rich text values is rewritten too.
func rewrite(node any, ids map[uuid.UUID]uuid.UUID, depth int) bool {
	if depth > MaxDepth*2 {
		return false
	}
	changed := false
	switch n := node.(type) {
	case []any:
		for _, child := range n {
			changed = rewrite(child, ids, depth+1) || changed
		}
	case map[string]any:
		if typ, _ := n["type"].(string); typ == NodeImage || typ == NodeAttachment {
			if attrs, ok := n["attrs"].(map[string]any); ok {
				if text, ok := attrs["attachmentId"].(string); ok {
					if old, err := uuid.Parse(text); err == nil {
						if fresh, ok := ids[old]; ok {
							attrs["attachmentId"] = fresh.String()
							changed = true
						}
					}
				}
			}
		}
		for _, child := range n {
			changed = rewrite(child, ids, depth+1) || changed
		}
	}
	return changed
}
