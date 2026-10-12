package requirement

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// The marks a comparison puts on changed text. They exist only in
// comparisons; the allowlist has neither, so no saved statement carries them.
const (
	MarkDiffInsert = "diffInsert"
	MarkDiffDelete = "diffDelete"
)

// maxDiffCells bounds the table one alignment may fill. Past it, the part
// left after the common start and end is shown as replaced whole, which is
// right, only coarser.
const maxDiffCells = 1 << 22

// node is a document node as the diff reads and writes it. The allowlist
// knows no other fields, so nothing is lost by the round trip.
type node struct {
	Type    string           `json:"type"`
	Attrs   map[string]any   `json:"attrs,omitempty"`
	Content []*node          `json:"content,omitempty"`
	Marks   []map[string]any `json:"marks,omitempty"`
	Text    string           `json:"text,omitempty"`
}

// shapeFree are attributes that may differ between two blocks still compared
// inline: an anchor follows its heading's words, and a cell's looks are not
// the table's shape.
var shapeFree = map[string][]string{
	"heading":     {"id"},
	"tableCell":   {"colwidth", "background", "align"},
	"tableHeader": {"colwidth", "background", "align"},
}

// Diff compares two documents block by block, the older one first. A block
// changed inside comes back as the newer block with its changes marked.
func Diff(from, to json.RawMessage) ([]DiffBlock, error) {
	a, err := topLevel(from)
	if err != nil {
		return nil, err
	}
	b, err := topLevel(to)
	if err != nil {
		return nil, err
	}
	out := []DiffBlock{}
	for _, s := range diffSeq(a, b) {
		raw, err := json.Marshal(s.node())
		if err != nil {
			return nil, err
		}
		out = append(out, DiffBlock{Change: s.change, Node: raw})
	}
	return out, nil
}

func topLevel(raw json.RawMessage) ([]*node, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var doc node
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Type != "doc" {
		return nil, errors.New("a stored statement is not a document")
	}
	return doc.Content, nil
}

// step is one node of an aligned sequence: its older and newer self, and
// for a modified one the newer with the changes marked.
type step struct {
	change   DiffChange
	old, new *node
	merged   *node
}

func (s step) node() *node {
	switch s.change {
	case DiffDeleted:
		return s.old
	case DiffModified:
		return s.merged
	}
	return s.new
}

// diffSeq aligns two lists of sibling blocks: identical ones first, then,
// in each stretch between them, blocks of one type that can be compared
// inline. The rest is deleted before what is inserted in its place.
func diffSeq(a, b []*node) []step {
	ka, kb := keys(a), keys(b)
	var out []step
	i, j := 0, 0
	gap := func(ei, ej int) {
		out = append(out, pairGap(a[i:ei], b[j:ej], ka[i:ei], kb[j:ej])...)
	}
	for _, m := range align(len(a), len(b), func(x, y int) bool { return ka[x] == kb[y] }) {
		gap(m[0], m[1])
		out = append(out, step{change: DiffEqual, old: a[m[0]], new: b[m[1]]})
		i, j = m[0]+1, m[1]+1
	}
	gap(len(a), len(b))
	return out
}

func pairGap(a, b []*node, ka, kb []string) []step {
	var out []step
	i, j := 0, 0
	rest := func(ei, ej int) {
		for ; i < ei; i++ {
			out = append(out, step{change: DiffDeleted, old: a[i]})
		}
		for ; j < ej; j++ {
			out = append(out, step{change: DiffInserted, new: b[j]})
		}
	}
	for _, m := range align(len(a), len(b), func(x, y int) bool { return a[x].Type == b[y].Type }) {
		rest(m[0], m[1])
		x, y := a[m[0]], b[m[1]]
		switch merged, changed, ok := merge(x, y); {
		case ka[m[0]] == kb[m[1]] || (ok && !changed):
			out = append(out, step{change: DiffEqual, old: x, new: y})
		case ok:
			out = append(out, step{change: DiffModified, old: x, new: y, merged: merged})
		default:
			out = append(out, step{change: DiffDeleted, old: x}, step{change: DiffInserted, new: y})
		}
		i, j = m[0]+1, m[1]+1
	}
	rest(len(a), len(b))
	return out
}

// merge marks the changes from x to y inside y, or says the two cannot be
// compared inline because their shape differs.
func merge(x, y *node) (*node, bool, bool) {
	if x.Type != y.Type || !sameAttrs(x, y) {
		return nil, false, false
	}
	out := *y
	switch {
	case isTextblock(y.Type):
		out.Content = inlineDiff(x.Content, y.Content)
		return &out, key(x) != key(y), true
	case y.Type == "table":
		return mergeTable(x, y)
	case len(x.Content) == 0 && len(y.Content) == 0:
		return &out, false, true
	}
	out.Content = nil
	changed := false
	for _, s := range diffSeq(x.Content, y.Content) {
		switch s.change {
		case DiffDeleted:
			out.Content = append(out.Content, marked(s.old, MarkDiffDelete))
			changed = true
		case DiffInserted:
			out.Content = append(out.Content, marked(s.new, MarkDiffInsert))
			changed = true
		case DiffModified:
			out.Content = append(out.Content, s.merged)
			changed = true
		default:
			out.Content = append(out.Content, s.new)
		}
	}
	return &out, changed, true
}

// mergeTable compares two tables cell by cell, which only works while they
// have the same rows and the same cells in each.
func mergeTable(x, y *node) (*node, bool, bool) {
	if len(x.Content) != len(y.Content) {
		return nil, false, false
	}
	out := *y
	out.Content = make([]*node, len(y.Content))
	changed := false
	for r, row := range y.Content {
		old := x.Content[r]
		if old.Type != row.Type || len(old.Content) != len(row.Content) {
			return nil, false, false
		}
		merged := *row
		merged.Content = make([]*node, len(row.Content))
		for c, cell := range row.Content {
			if old.Content[c].Type != cell.Type || attr(old.Content[c], "colspan") != attr(cell, "colspan") || attr(old.Content[c], "rowspan") != attr(cell, "rowspan") {
				return nil, false, false
			}
			m, ch, ok := merge(old.Content[c], cell)
			if !ok {
				return nil, false, false
			}
			merged.Content[c], changed = m, changed || ch
		}
		out.Content[r] = &merged
	}
	return &out, changed, true
}

func attr(n *node, name string) string {
	raw, _ := json.Marshal(n.Attrs[name])
	return string(raw)
}

func sameAttrs(x, y *node) bool {
	strip := func(n *node) string {
		attrs := map[string]any{}
		for k, v := range n.Attrs {
			if !slices.Contains(shapeFree[n.Type], k) {
				attrs[k] = v
			}
		}
		raw, _ := json.Marshal(attrs)
		return string(raw)
	}
	return strip(x) == strip(y)
}

func isTextblock(nodeType string) bool {
	return slices.Contains(document.Allowed.Nodes[nodeType].Content, "text")
}

// marked is a copy of n with the mark on all of its text, as a block deleted
// or inserted inside a block that was otherwise kept.
func marked(n *node, mark string) *node {
	out := *n
	if isInline(n.Type) {
		out.Marks = withMark(n.Marks, mark)
		return &out
	}
	out.Content = make([]*node, len(n.Content))
	for i, child := range n.Content {
		out.Content[i] = marked(child, mark)
	}
	return &out
}

func isInline(nodeType string) bool { return document.Allowed.Nodes[nodeType].Inline }

func withMark(marks []map[string]any, mark string) []map[string]any {
	return append(slices.Clone(marks), map[string]any{"type": mark})
}

// token is a word, a run of spaces, a sign, or an inline node that is not
// text, with the marks it carries: the unit an inline change is made of.
type token struct {
	text  string
	marks []map[string]any
	atom  *node
	key   string
}

var wordPattern = regexp.MustCompile(`[\p{L}\p{N}\p{M}_]+|\s+|.`)

func tokenize(content []*node) []token {
	var out []token
	for _, n := range content {
		if n.Type != "text" {
			out = append(out, token{atom: n, key: "\x01" + key(n)})
			continue
		}
		marks := marksKey(n.Marks)
		for _, word := range wordPattern.FindAllString(n.Text, -1) {
			out = append(out, token{text: word, marks: n.Marks, key: marks + "\x00" + word})
		}
	}
	return out
}

// inlineDiff is y's inline content with the words x had and y lost put back
// where they were, marked deleted, and the words y gained marked inserted.
func inlineDiff(x, y []*node) []*node {
	a, b := tokenize(x), tokenize(y)
	var out []*node
	emit := func(t token, mark string) {
		if t.atom != nil {
			n := *t.atom
			if mark != "" {
				n.Marks = withMark(n.Marks, mark)
			}
			out = append(out, &n)
			return
		}
		marks := t.marks
		if mark != "" {
			marks = withMark(marks, mark)
		}
		if last := len(out) - 1; last >= 0 && out[last].Type == "text" && marksKey(out[last].Marks) == marksKey(marks) {
			out[last].Text += t.text
			return
		}
		out = append(out, &node{Type: "text", Text: t.text, Marks: marks})
	}
	i, j := 0, 0
	gap := func(ei, ej int) {
		for ; i < ei; i++ {
			emit(a[i], MarkDiffDelete)
		}
		for ; j < ej; j++ {
			emit(b[j], MarkDiffInsert)
		}
	}
	for _, m := range align(len(a), len(b), func(x, y int) bool { return a[x].key == b[y].key }) {
		gap(m[0], m[1])
		emit(b[m[1]], "")
		i, j = m[0]+1, m[1]+1
	}
	gap(len(a), len(b))
	return out
}

// align finds the longest common subsequence of two sequences by index,
// after taking off what they start and end with alike.
func align(n, m int, eq func(i, j int) bool) [][2]int {
	var head, tail [][2]int
	lo := 0
	for lo < n && lo < m && eq(lo, lo) {
		head = append(head, [2]int{lo, lo})
		lo++
	}
	hn, hm := n, m
	for hn > lo && hm > lo && eq(hn-1, hm-1) {
		hn, hm = hn-1, hm-1
		tail = append(tail, [2]int{hn, hm})
	}
	slices.Reverse(tail)
	rows, cols := hn-lo, hm-lo
	if rows == 0 || cols == 0 || (rows+1)*(cols+1) > maxDiffCells {
		return append(head, tail...)
	}
	// lengths[r][c] is the common length of the suffixes from lo+r and lo+c.
	width := cols + 1
	lengths := make([]int32, (rows+1)*width)
	for r := rows - 1; r >= 0; r-- {
		for c := cols - 1; c >= 0; c-- {
			switch {
			case eq(lo+r, lo+c):
				lengths[r*width+c] = lengths[(r+1)*width+c+1] + 1
			case lengths[(r+1)*width+c] >= lengths[r*width+c+1]:
				lengths[r*width+c] = lengths[(r+1)*width+c]
			default:
				lengths[r*width+c] = lengths[r*width+c+1]
			}
		}
	}
	middle := head
	for r, c := 0, 0; r < rows && c < cols; {
		switch {
		case eq(lo+r, lo+c):
			middle = append(middle, [2]int{lo + r, lo + c})
			r, c = r+1, c+1
		case lengths[(r+1)*width+c] >= lengths[r*width+c+1]:
			r++
		default:
			c++
		}
	}
	return append(middle, tail...)
}

func keys(ns []*node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = key(n)
	}
	return out
}

// key is a node's canonical JSON: maps marshal with sorted keys, so two
// equal nodes have one key however they were written.
func key(n *node) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

func marksKey(marks []map[string]any) string {
	if len(marks) == 0 {
		return ""
	}
	raw, _ := json.Marshal(marks)
	return string(raw)
}
