// Package document holds which ProseMirror documents a statement, a rationale or a rich text value may be, and
// what search reads from them.
package document

import (
	"bytes"
	"encoding/json"
	"slices"
)

// AttrKind is the JSON type an attribute's value has.
type AttrKind string

const (
	KindString  AttrKind = "string"
	KindInteger AttrKind = "integer"
	KindBoolean AttrKind = "boolean"
	// KindIntegers is a list of integers, each within Min and Max.
	KindIntegers AttrKind = "integers"
	// KindNull is an attribute the editor always writes and always leaves empty.
	KindNull AttrKind = "null"
)

// Attr is what one attribute may hold. Min and Max bound each integer; MaxLength counts a string's characters
// or a list's items; Required refuses it left out.
type Attr struct {
	Kind      AttrKind `json:"kind"`
	Required  bool     `json:"required,omitempty"`
	Nullable  bool     `json:"nullable,omitempty"`
	Enum      []string `json:"enum,omitempty"`
	Min       int      `json:"min,omitempty"`
	Max       int      `json:"max,omitempty"`
	MaxLength int      `json:"maxLength,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
	URL       bool     `json:"url,omitempty"`
}

// NodeSpec is one node type: its attributes, the types it may contain (none makes it a leaf), and whether it
// or its inline children take marks.
type NodeSpec struct {
	Attrs       map[string]Attr `json:"attrs,omitempty"`
	Content     []string        `json:"content,omitempty"`
	Inline      bool            `json:"inline,omitempty"`
	AllowsMarks bool            `json:"allowsMarks,omitempty"`
}

// MarkSpec is one mark type and its attributes.
type MarkSpec struct {
	Attrs map[string]Attr `json:"attrs,omitempty"`
}

// Allowlist is every node and mark a stored document may hold.
type Allowlist struct {
	Nodes map[string]NodeSpec `json:"nodes"`
	Marks map[string]MarkSpec `json:"marks"`
}

const (
	// MaxBytes caps a document, requirement.MaxDocumentBytes; a statement longer than this is several requirements.
	MaxBytes = 1 << 20
	// MaxDepth stops a nest of lists and quotes from being a way to exhaust the server.
	MaxDepth = 40
	// MaxHeadingLevel is the deepest heading; the requirement's title is above level 1.
	MaxHeadingLevel = 3
	// MaxHrefLength matches what browsers and proxies reliably carry.
	MaxHrefLength = 2048
	// MaxTableSpan bounds how many rows or columns one cell may cover.
	MaxTableSpan = 100
	// MaxColumnWidth bounds a stored column width, in pixels.
	MaxColumnWidth = 4000
	// MaxListStart bounds where a numbered list may start counting.
	MaxListStart = 1_000_000
	// maxLabelLength bounds a mention's shown name and a link's title.
	maxLabelLength = 256
	// maxLanguageLength bounds a code block's language name.
	maxLanguageLength = 32
	// MaxAltLength bounds an image's description.
	MaxAltLength = 500
	// MaxImageWidth bounds an image's stored width, in pixels, as a column's.
	MaxImageWidth = 4000
	// maxFileNameLength matches the longest file name an upload keeps, objectstore.MaxNameLength.
	maxFileNameLength = 200
)

// The nodes that show a file of the requirement (#21), which name it by attachmentId.
const (
	NodeImage      = "image"
	NodeAttachment = "attachment"
)

// MarkTerm links words to a glossary term (#210) by its termId, for words the matcher would not find.
const MarkTerm = "term"

// UUIDPattern is an id as the API writes it, in lower case.
const UUIDPattern = `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`

var (
	blockNodes  = []string{"paragraph", "heading", "bulletList", "orderedList", "blockquote", "codeBlock", "horizontalRule", "table", NodeImage}
	inlineNodes = []string{"text", "hardBreak", "mention", NodeAttachment}
	cellAttrs   = map[string]Attr{
		"colspan":  {Kind: KindInteger, Min: 1, Max: MaxTableSpan},
		"rowspan":  {Kind: KindInteger, Min: 1, Max: MaxTableSpan},
		"colwidth": {Kind: KindIntegers, Nullable: true, Max: MaxColumnWidth, MaxLength: MaxTableSpan},
		"align":    {Kind: KindString, Nullable: true, Enum: []string{"left", "center", "right"}},
	}
)

// Allowed is the one table the validator, the generated api/document-allowlist.json and the web editor's test
// all read: Stator's, cut to what a requirement says.
var Allowed = Allowlist{
	Nodes: map[string]NodeSpec{
		"doc":       {Content: blockNodes},
		"paragraph": {Content: inlineNodes, AllowsMarks: true},
		"heading": {
			Content:     inlineNodes,
			AllowsMarks: true,
			Attrs:       map[string]Attr{"level": {Kind: KindInteger, Min: 1, Max: MaxHeadingLevel}},
		},
		"bulletList": {Content: []string{"listItem"}},
		"orderedList": {
			Content: []string{"listItem"},
			Attrs: map[string]Attr{
				"start": {Kind: KindInteger, Min: 0, Max: MaxListStart},
				"type":  {Kind: KindString, Nullable: true, Enum: []string{"1", "a", "A", "i", "I"}},
			},
		},
		"listItem":   {Content: blockNodes},
		"blockquote": {Content: blockNodes},
		"codeBlock": {
			Content: []string{"text"},
			Attrs: map[string]Attr{
				"language": {Kind: KindString, Nullable: true, MaxLength: maxLanguageLength, Pattern: `^[a-z0-9][a-z0-9+#-]*$`},
			},
		},
		"horizontalRule": {},
		"table":          {Content: []string{"tableRow"}},
		"tableRow":       {Content: []string{"tableCell", "tableHeader"}},
		"tableCell":      {Content: blockNodes, Attrs: cellAttrs},
		"tableHeader":    {Content: blockNodes, Attrs: cellAttrs},
		"text":           {Inline: true},
		"hardBreak":      {Inline: true},
		"mention": {
			Inline: true,
			Attrs: map[string]Attr{
				// id is a member's id; a mention of somebody who left stays in the text.
				"id":                    {Kind: KindString, Pattern: UUIDPattern},
				"label":                 {Kind: KindString, MaxLength: maxLabelLength, Pattern: `\S`},
				"mentionSuggestionChar": {Kind: KindString, Nullable: true, Enum: []string{"@"}},
			},
		},
		NodeImage: {
			Attrs: map[string]Attr{
				"attachmentId": {Kind: KindString, Pattern: UUIDPattern},
				"alt":          {Kind: KindString, Nullable: true, MaxLength: MaxAltLength},
				"width":        {Kind: KindInteger, Nullable: true, Min: 1, Max: MaxImageWidth},
			},
		},
		NodeAttachment: {
			Inline: true,
			Attrs: map[string]Attr{
				"attachmentId": {Kind: KindString, Pattern: UUIDPattern},
				"fileName":     {Kind: KindString, MaxLength: maxFileNameLength, Pattern: `\S`},
			},
		},
	},
	Marks: map[string]MarkSpec{
		"bold":   {},
		"italic": {},
		"strike": {},
		"code":   {},
		"link": {Attrs: map[string]Attr{
			"href":   {Kind: KindString, MaxLength: MaxHrefLength, URL: true},
			"target": {Kind: KindString, Nullable: true, Enum: []string{"_blank"}},
			"rel":    {Kind: KindString, Nullable: true, Enum: []string{"noopener noreferrer nofollow"}},
			"class":  {Kind: KindNull},
			"title":  {Kind: KindString, Nullable: true, MaxLength: maxLabelLength},
		}},
		MarkTerm: {Attrs: map[string]Attr{
			"termId": {Kind: KindString, Required: true, Pattern: UUIDPattern},
		}},
	},
}

// JSON is the allowlist as api/document-allowlist.json holds it: indented, keys sorted, ending in a newline.
func (a Allowlist) JSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(a); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MaxCommentBytes caps a comment's document (#33); anything longer belongs in the requirement.
const MaxCommentBytes = 64 << 10

// CommentNodes and CommentMarks are what a comment may hold, by name: text and its structure, Stator's, never
// files, tables or rules.
var (
	CommentNodes = []string{"doc", "paragraph", "heading", "bulletList", "orderedList", "listItem", "blockquote", "codeBlock", "hardBreak", "text", "mention"}
	CommentMarks = []string{"bold", "italic", "strike", "code", "link"}
)

// CommentAllowed is the part of Allowed a comment may hold, written to api/comment-allowlist.json for the web
// client's comment editor.
var CommentAllowed = Allowed.Subset(CommentNodes, CommentMarks)

// Subset keeps the named nodes and marks, each as a allows it, and drops every other type from what the kept nodes
// may contain.
func (a Allowlist) Subset(nodes, marks []string) Allowlist {
	out := Allowlist{Nodes: map[string]NodeSpec{}, Marks: map[string]MarkSpec{}}
	for _, name := range nodes {
		spec, ok := a.Nodes[name]
		if !ok {
			panic("document: no node " + name + " to keep")
		}
		var content []string
		for _, child := range spec.Content {
			if slices.Contains(nodes, child) {
				content = append(content, child)
			}
		}
		spec.Content = content
		out.Nodes[name] = spec
	}
	for _, name := range marks {
		spec, ok := a.Marks[name]
		if !ok {
			panic("document: no mark " + name + " to keep")
		}
		out.Marks[name] = spec
	}
	return out
}
