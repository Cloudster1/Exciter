package document

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// ErrInvalid is what every refusal wraps, so a handler can answer 422.
var ErrInvalid = errors.New("invalid document")

// InvalidError carries a sentence the author can act on.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

func (e *InvalidError) Is(target error) bool { return target == ErrInvalid }

func invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}

// Node is one ProseMirror node as the editor stores it.
type Node struct {
	Type    string         `json:"type"`
	Text    string         `json:"text,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Marks   []Mark         `json:"marks,omitempty"`
	Content []Node         `json:"content,omitempty"`
}

// Mark is one mark on an inline node.
type Mark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// Empty is the document with nothing in it, which a statement left out becomes.
var Empty = json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`)

// Parse decodes a document and holds it to the allowlist; what is refused is said in a sentence that names
// what to change.
func Parse(body json.RawMessage) (Node, error) {
	if len(body) > MaxBytes {
		return Node{}, invalid("This text is too long; keep it under %d MB, or split it into several requirements.", MaxBytes>>20)
	}
	return parse(body, &Allowed)
}

// ParseComment decodes a comment's document and holds it to CommentAllowed, answering the decoded node so the
// caller can read its words.
func ParseComment(body json.RawMessage) (Node, error) {
	if len(body) > MaxCommentBytes {
		return Node{}, invalid("This comment is too long; keep it under %d KB, or put the text in the requirement.", MaxCommentBytes>>10)
	}
	return parse(body, &CommentAllowed)
}

func parse(body json.RawMessage, list *Allowlist) (Node, error) {
	var root Node
	dec := json.NewDecoder(bytes.NewReader(body))
	// A field the schema does not name would be stored and served back unread, so it is refused.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&root); err != nil || dec.More() {
		return Node{}, invalid("This text is not a document the editor can read; reload the editor and save again.")
	}
	if root.Type != "doc" {
		return Node{}, invalid(`This text must be a document with "type":"doc".`)
	}
	return root, check(list, root, NodeSpec{Content: []string{"doc"}}, 0)
}

func check(list *Allowlist, n Node, parent NodeSpec, depth int) error {
	if depth > MaxDepth {
		return invalid("This text is nested too deeply; flatten its lists and quotes.")
	}
	spec, ok := list.Nodes[n.Type]
	if !ok {
		return invalid("This text holds a %q block, which the editor cannot show; take it out.", n.Type)
	}
	if !slices.Contains(parent.Content, n.Type) {
		return invalid("This text puts a %q where it cannot go; move it out.", n.Type)
	}
	if n.Type == "text" {
		if n.Text == "" {
			return invalid("This text holds an empty piece of text; save it again from the editor.")
		}
	} else if n.Text != "" {
		return invalid("This text gives a %q text of its own, which only text may have.", n.Type)
	}
	if len(spec.Content) == 0 && len(n.Content) > 0 {
		return invalid("This text puts content inside a %q, which holds none.", n.Type)
	}
	if err := checkAttrs(n.Attrs, spec.Attrs, fmt.Sprintf("a %q", n.Type)); err != nil {
		return err
	}
	if len(n.Marks) > 0 && !(spec.Inline && parent.AllowsMarks) {
		return invalid("This text styles a %q where styles cannot go; remove the formatting.", n.Type)
	}
	seen := map[string]bool{}
	for _, m := range n.Marks {
		ms, ok := list.Marks[m.Type]
		if !ok {
			return invalid("This text uses a %q style, which the editor cannot show; take it out.", m.Type)
		}
		if seen[m.Type] {
			return invalid("This text applies the %q style twice to the same words.", m.Type)
		}
		seen[m.Type] = true
		if err := checkAttrs(m.Attrs, ms.Attrs, fmt.Sprintf("the %q style", m.Type)); err != nil {
			return err
		}
	}
	for _, child := range n.Content {
		if err := check(list, child, spec, depth+1); err != nil {
			return err
		}
	}
	return nil
}

var patterns = map[string]*regexp.Regexp{}

func init() {
	compile := func(attrs map[string]Attr) {
		for _, a := range attrs {
			if a.Pattern != "" {
				patterns[a.Pattern] = regexp.MustCompile(a.Pattern)
			}
		}
	}
	for _, n := range Allowed.Nodes {
		compile(n.Attrs)
	}
	for _, m := range Allowed.Marks {
		compile(m.Attrs)
	}
}

func checkAttrs(attrs map[string]any, allowed map[string]Attr, owner string) error {
	for name, value := range attrs {
		rule, ok := allowed[name]
		if !ok {
			return invalid("This text gives %s an attribute %q, which the editor does not know; take it out.", owner, name)
		}
		if !attrValid(rule, value) {
			return invalid("This text gives %s the attribute %s=%s, which is not allowed; pick it again in the editor.", owner, name, shortJSON(value))
		}
	}
	for name, rule := range allowed {
		if _, ok := attrs[name]; rule.Required && !ok {
			return invalid("This text gives %s no %s; pick it again in the editor.", owner, name)
		}
	}
	return nil
}

func attrValid(rule Attr, value any) bool {
	if value == nil {
		return rule.Nullable || rule.Kind == KindNull
	}
	switch rule.Kind {
	case KindNull:
		return false
	case KindBoolean:
		_, ok := value.(bool)
		return ok
	case KindInteger:
		return integerIn(value, rule)
	case KindIntegers:
		list, ok := value.([]any)
		if !ok || len(list) > rule.MaxLength {
			return false
		}
		for _, item := range list {
			if !integerIn(item, rule) {
				return false
			}
		}
		return true
	case KindString:
		s, ok := value.(string)
		if !ok {
			return false
		}
		if rule.MaxLength > 0 && utf8.RuneCountInString(s) > rule.MaxLength {
			return false
		}
		if rule.Enum != nil && !slices.Contains(rule.Enum, s) {
			return false
		}
		if rule.Pattern != "" && !patterns[rule.Pattern].MatchString(s) {
			return false
		}
		return !rule.URL || SafeHref(s)
	}
	return false
}

func integerIn(value any, rule Attr) bool {
	f, ok := value.(float64)
	return ok && f == math.Trunc(f) && f >= float64(rule.Min) && f <= float64(rule.Max)
}

func shortJSON(value any) string {
	out, _ := json.Marshal(value)
	const limit = 60
	if len(out) > limit {
		return string(out[:limit]) + "..."
	}
	return string(out)
}

var allowedSchemes = []string{"http", "https", "mailto"}

// SafeHref admits web and mail addresses and links within the site, and nothing a browser would run or send
// somewhere unexpected.
func SafeHref(href string) bool {
	if href == "" || utf8.RuneCountInString(href) > MaxHrefLength || !utf8.ValidString(href) {
		return false
	}
	for _, r := range href {
		// Browsers drop tabs and newlines inside a URL, so "java\tscript:" would run; a backslash reads as a
		// slash and makes "/\host" leave the site.
		if r < 0x20 || r == 0x7f || r == '\\' || r == ' ' {
			return false
		}
	}
	if strings.HasPrefix(href, "//") {
		return false
	}
	end := strings.IndexAny(href, "/?#")
	colon := strings.IndexByte(href, ':')
	if colon < 0 || (end >= 0 && end < colon) {
		return true
	}
	scheme := strings.ToLower(href[:colon])
	if !slices.Contains(allowedSchemes, scheme) {
		return false
	}
	if scheme == "mailto" {
		return len(href) > colon+1
	}
	u, err := url.Parse(href)
	return err == nil && u.Host != ""
}
