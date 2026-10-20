// Package markdown writes a requirement's documents as Markdown and reads Markdown back into one the allowlist
// accepts (#186), adapted from Stator's; docs/markdown.md lists how each node is written.
package markdown

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
)

// MaxSourceBytes caps what is read, as document.MaxBytes caps the document it becomes.
const MaxSourceBytes = 1 << 20

// exciterAttr names the attribute on the span that carries what Markdown has no syntax for: a mention.
const exciterAttr = "data-exciter"

const kindMention = "mention"

// FileScheme starts the address of a file of the requirement, the attachment's id after it, in pictures and links.
const FileScheme = "exciter:attachment/"

// markOrder is the order marks open in, outermost first: code is innermost because nothing inside a code span is
// read as Markdown.
var markOrder = []string{"link", "bold", "italic", "strike", "code"}

func markRank(t string) int { return slices.Index(markOrder, t) }

// Refusal is Markdown that would not come back as written, said in a sentence that says what to change.
type Refusal struct{ Message string }

func (r *Refusal) Error() string { return r.Message }

// ErrRefused is what every Refusal wraps, so a caller tells it from a fault.
var ErrRefused = errors.New("markdown refused")

func (r *Refusal) Is(target error) bool { return target == ErrRefused }

// Resolve answers what the Markdown names: a file of the requirement by its id, and whether an id is a member's.
// A function that is nil answers no.
type Resolve struct {
	File   func(attachmentID string) (fileName string, ok bool)
	Person func(id string) bool
	// NoDiagrams keeps a fence in a diagram language as code, for a comment, which holds no diagram.
	NoDiagrams bool
}

func (r Resolve) file(id string) (string, bool) {
	if r.File == nil {
		return "", false
	}
	return r.File(id)
}

func (r Resolve) person(id string) bool { return r.Person != nil && r.Person(id) }

// Of is a stored document as Markdown, nil where there is none or where Markdown cannot hold it.
func Of(raw json.RawMessage) *string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var root document.Node
	if json.Unmarshal(raw, &root) != nil || !Holds(root) {
		return nil
	}
	out := Render(root)
	return &out
}

// ForReading is a stored document as Markdown for a reader rather than for writing back: words linked to a
// glossary term are its words. Nil where there is none or where Markdown cannot hold it.
func ForReading(raw json.RawMessage) *string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var root document.Node
	if json.Unmarshal(raw, &root) != nil {
		return nil
	}
	root = withoutTerms(root)
	if !Holds(root) {
		return nil
	}
	out := Render(root)
	return &out
}

func withoutTerms(n document.Node) document.Node {
	if len(n.Marks) > 0 {
		n.Marks = slices.DeleteFunc(slices.Clone(n.Marks), func(m document.Mark) bool { return m.Type == document.MarkTerm })
	}
	if len(n.Content) > 0 {
		content := make([]document.Node, len(n.Content))
		for i, c := range n.Content {
			content[i] = withoutTerms(c)
		}
		n.Content = content
	}
	return n
}

// Members answers whether an id is somebody in the installation, whom a mention may name.
func Members(ctx context.Context, tx db.DBTX) func(string) bool {
	return func(id string) bool {
		var ok bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM app_user WHERE id = $1 AND role IS NOT NULL)`, id).Scan(&ok)
		return err == nil && ok
	}
}
