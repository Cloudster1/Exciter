package requirement

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/markdown"
)

// Format is how a read writes the documents it answers (#186).
type Format string

const (
	FormatDocument Format = "document"
	// FormatMarkdown answers each document as Markdown as well, beside it.
	FormatMarkdown Format = "markdown"
)

// Formats lists every Format, for the API document.
var Formats = []Format{FormatDocument, FormatMarkdown}

// Markdown is a statement and a rationale as Markdown, answered when a read asks for it and left out where there
// is none, or where Markdown cannot hold what the document says.
type Markdown struct {
	StatementMarkdown *string `json:"statementMarkdown,omitempty"`
	RationaleMarkdown *string `json:"rationaleMarkdown,omitempty"`
}

func (m *Markdown) fill(statement, rationale json.RawMessage) {
	m.StatementMarkdown, m.RationaleMarkdown = markdown.Of(statement), markdown.Of(rationale)
}

// WithMarkdown adds the statement and the rationale as Markdown.
func (r *Requirement) WithMarkdown() { r.fill(r.Statement, r.Rationale) }

// WithMarkdown adds the entry's statement and rationale as Markdown.
func (e *SpecEntry) WithMarkdown() { e.fill(e.Statement, e.Rationale) }

// WithMarkdown adds the revision's statement and rationale as Markdown.
func (v *Revision) WithMarkdown() { v.fill(v.Statement, v.Rationale) }

// FromMarkdown reads what a writer sent as Markdown into the document its field takes, checked as one sent as a
// document is; the refusal names the Markdown field. Sending both forms of one field is refused.
func FromMarkdown(field string, md *string, doc json.RawMessage, resolve markdown.Resolve, check func(json.RawMessage) error) (json.RawMessage, error) {
	if md == nil {
		return doc, nil
	}
	named := field + "Markdown"
	if len(doc) > 0 {
		return nil, &FieldError{named, "Send the " + field + " as a document or as Markdown, not both."}
	}
	root, err := markdown.Convert(*md, resolve)
	var refusal *markdown.Refusal
	if errors.As(err, &refusal) {
		return nil, &FieldError{named, refusal.Message}
	}
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	if err := check(out); err != nil {
		var invalid *FieldError
		if errors.As(err, &invalid) {
			return nil, &FieldError{named, invalid.Message}
		}
		return nil, err
	}
	return out, nil
}

// fromMarkdown converts a requirement's Markdown fields into the documents they stand for. A rationale of no words
// is none, as an empty one would be.
func fromMarkdown(statement, rationale **string, statementDoc, rationaleDoc *json.RawMessage, resolve markdown.Resolve, clear json.RawMessage) error {
	var err error
	if *statementDoc, err = FromMarkdown("statement", *statement, *statementDoc, resolve, checkStatement); err != nil {
		return err
	}
	if *rationale != nil && strings.TrimSpace(**rationale) == "" && len(*rationaleDoc) == 0 {
		*rationale, *rationaleDoc = nil, clear
		return nil
	}
	*rationaleDoc, err = FromMarkdown("rationale", *rationale, *rationaleDoc, resolve, checkStatement)
	return err
}

func checkStatement(raw json.RawMessage) error {
	_, err := checkDocument("statement", raw)
	return err
}

// filesOf answers the requirement's own files by id, which its Markdown may show and link.
func filesOf(ctx context.Context, tx db.DBTX, requirementID uuid.UUID) (func(string) (string, bool), error) {
	rows, err := tx.Query(ctx, `SELECT id::text, file_name FROM attachment WHERE requirement_id = $1`, requirementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return func(id string) (string, bool) {
		name, ok := names[id]
		return name, ok
	}, rows.Err()
}
