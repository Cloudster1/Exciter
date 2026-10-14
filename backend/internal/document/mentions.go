package document

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

// Mentions in a document (#33), Stator's: a mention node names a person by id, and nothing else keeps who was named.

// Mention is a person a document names, and the words of the block that first names them.
type Mention struct {
	ID    uuid.UUID
	Block string
}

// Mentions lists the people a document names, each once, in the order they first appear. An id that is not a
// person's uuid names nobody.
func Mentions(root Node) []Mention {
	var out []Mention
	seen := map[uuid.UUID]bool{}
	var walk func(nodes []Node, depth int)
	walk = func(nodes []Node, depth int) {
		if depth > MaxDepth {
			return
		}
		for _, n := range nodes {
			var block string
			for _, c := range n.Content {
				if c.Type != "mention" {
					continue
				}
				raw, _ := c.Attrs["id"].(string)
				id, err := uuid.Parse(raw)
				if err != nil || id.String() != raw || seen[id] {
					continue
				}
				if block == "" {
					block = strings.TrimSpace(strings.ReplaceAll(InlineText(n), "\n", " "))
				}
				seen[id] = true
				out = append(out, Mention{ID: id, Block: block})
			}
			walk(n.Content, depth+1)
		}
	}
	walk(root.Content, 0)
	return out
}

// MentionsIn reads the mentions of a stored comment; none, or one that no longer parses, names nobody.
func MentionsIn(body json.RawMessage) []Mention {
	if len(body) == 0 {
		return nil
	}
	root, err := ParseComment(body)
	if err != nil {
		return nil
	}
	return Mentions(root)
}

// NewMentions are the mentions of after that before does not have: the people an edit names for the first time.
func NewMentions(before, after []Mention) []Mention {
	had := make(map[uuid.UUID]bool, len(before))
	for _, m := range before {
		had[m.ID] = true
	}
	out := []Mention{}
	for _, m := range after {
		if !had[m.ID] {
			out = append(out, m)
		}
	}
	return out
}

// MentionIDs are the people of a list of mentions, never nil.
func MentionIDs(mentions []Mention) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(mentions))
	for _, m := range mentions {
		out = append(out, m.ID)
	}
	return out
}
