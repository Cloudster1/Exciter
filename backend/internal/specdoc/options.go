package specdoc

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	doctree "github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// What a Word template asks of a document (#214): which fields each requirement shows and in which order, whether
// its rationale, links and open comments come along, and what changed since a baseline, marked.

// ErrSinceNotBaseline is change marks counted from something that is not a baseline of the document's project.
var ErrSinceNotBaseline = errors.New("not a baseline of the project")

// Choice is one field a template shows: a field of Fields, revision or rationale, or an attribute by its name.
type Choice struct {
	Field     string
	Attribute string
}

// Options shape a document; the zero value leaves it as read.
type Options struct {
	// Fields empty shows every field as read; otherwise these, in order, and the rationale only when named.
	Fields   []Choice
	Links    bool
	Comments bool
	// Since is the baseline change marks are counted from, nil for none.
	Since *uuid.UUID
}

// FieldRevision and FieldRationale are the choices beyond a requirement's table of fields.
const (
	FieldRevision  = "revision"
	FieldRationale = "rationale"
)

// Shape applies a template's options to a document read as the reader, reading comments and changes as they may.
func (s *Service) Shape(ctx context.Context, reader uuid.UUID, doc *Document, projectKey string, o Options) error {
	if len(o.Fields) > 0 {
		for i := range doc.Entries {
			arrange(&doc.Entries[i], o.Fields)
		}
	}
	if !o.Links {
		for i := range doc.Entries {
			doc.Entries[i].Links = []Link{}
		}
	}
	if !o.Comments && o.Since == nil {
		return nil
	}
	return s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		ids := make([]uuid.UUID, 0, len(doc.Entries))
		at := make(map[uuid.UUID]int, len(doc.Entries))
		for i, e := range doc.Entries {
			ids, at[e.ID] = append(ids, e.ID), i
		}
		if o.Comments {
			if err := comments(ctx, tx, doc, ids, at); err != nil {
				return err
			}
		}
		if o.Since != nil {
			return changes(ctx, tx, doc, projectKey, *o.Since, ids, at)
		}
		return nil
	})
}

// arrange keeps the properties a template names, in its order, and the rationale only when it names it.
func arrange(e *Entry, fields []Choice) {
	var out []Property
	rationale := false
	for _, c := range fields {
		switch c.Field {
		case FieldRationale:
			rationale = true
		case FieldRevision:
			out = append(out, Property{Field: FieldRevision, Text: strconv.Itoa(e.Revision)})
		case "attribute":
			if i := slices.IndexFunc(e.Properties, func(p Property) bool { return p.Field == "" && strings.EqualFold(p.Name, c.Attribute) }); i >= 0 {
				out = append(out, e.Properties[i])
			}
		default:
			if i := slices.IndexFunc(e.Properties, func(p Property) bool { return p.Field == c.Field }); i >= 0 {
				out = append(out, e.Properties[i])
			}
		}
	}
	if out == nil {
		out = []Property{}
	}
	e.Properties = out
	if !rationale {
		e.Rationale = nil
	}
}

func comments(ctx context.Context, tx db.DBTX, doc *Document, ids []uuid.UUID, at map[uuid.UUID]int) error {
	rows, err := tx.Query(ctx, `
		SELECT c.requirement_id, COALESCE(NULLIF(u.name, ''), u.email::text, ''), c.created_at, c.body
		FROM comment c
		JOIN comment_thread th ON th.id = c.thread_id
		LEFT JOIN app_user u ON u.id = c.author_id
		WHERE c.requirement_id = ANY ($1) AND c.deleted_at IS NULL AND th.resolved_at IS NULL
		ORDER BY c.requirement_id, th.created_at, c.created_at`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   uuid.UUID
			c    EntryComment
			body []byte
		)
		if err := rows.Scan(&id, &c.Author, &c.At, &body); err != nil {
			return err
		}
		if node, err := doctree.Parse(body); err == nil {
			c.Text = doctree.PlainText(node)
		}
		e := &doc.Entries[at[id]]
		e.Comments = append(e.Comments, c)
	}
	return rows.Err()
}

// changes marks, on each requirement, what changed in its statement since the baseline held it.
func changes(ctx context.Context, tx db.DBTX, doc *Document, projectKey string, since uuid.UUID, ids []uuid.UUID, at map[uuid.UUID]int) error {
	var name string
	err := tx.QueryRow(ctx, `SELECT b.name FROM baseline b JOIN project p ON p.id = b.project_id WHERE b.id = $1 AND upper(p.key) = upper($2)`,
		since, projectKey).Scan(&name)
	if err != nil {
		return ErrSinceNotBaseline
	}
	doc.ChangesSince = name
	rows, err := tx.Query(ctx, `
		SELECT i.requirement_id, i.revision, v.statement
		FROM baseline_item i JOIN requirement_revision v ON v.requirement_id = i.requirement_id AND v.number = i.revision
		WHERE i.baseline_id = $1 AND i.requirement_id = ANY ($2)`, since, ids)
	if err != nil {
		return err
	}
	held := map[uuid.UUID]struct {
		revision  int
		statement json.RawMessage
	}{}
	for rows.Next() {
		var (
			id        uuid.UUID
			revision  int
			statement json.RawMessage
		)
		if err := rows.Scan(&id, &revision, &statement); err != nil {
			rows.Close()
			return err
		}
		held[id] = struct {
			revision  int
			statement json.RawMessage
		}{revision, statement}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		e := &doc.Entries[at[id]]
		if e.Kind != requirement.KindRequirement {
			continue
		}
		before, ok := held[id]
		from := 0
		old := json.RawMessage(`{"type":"doc","content":[]}`)
		if ok {
			if before.revision == e.Revision {
				continue
			}
			from, old = before.revision, before.statement
		}
		marked, err := marked(old, e.Statement)
		if err != nil {
			return err
		}
		e.Changes, e.ChangedFrom = marked, &from
	}
	return nil
}

// marked is a statement with what changed since an older one marked: blocks added or removed whole, and words
// within a block changed.
func marked(old, current json.RawMessage) (json.RawMessage, error) {
	blocks, err := requirement.Diff(old, current)
	if err != nil {
		return nil, err
	}
	content := make([]any, 0, len(blocks))
	for _, b := range blocks {
		var node map[string]any
		if err := json.Unmarshal(b.Node, &node); err != nil {
			return nil, err
		}
		switch b.Change {
		case requirement.DiffInserted:
			markAll(node, requirement.MarkDiffInsert)
		case requirement.DiffDeleted:
			markAll(node, requirement.MarkDiffDelete)
		}
		content = append(content, node)
	}
	return json.Marshal(map[string]any{"type": "doc", "content": content})
}

// markAll puts a mark on every piece of text in a node.
func markAll(node map[string]any, mark string) {
	if node["type"] == "text" {
		marks, _ := node["marks"].([]any)
		node["marks"] = append(marks, map[string]any{"type": mark})
		return
	}
	children, _ := node["content"].([]any)
	for _, c := range children {
		if child, ok := c.(map[string]any); ok {
			markAll(child, mark)
		}
	}
}

// Signers names who signed a baseline, in the order they signed, as the reader may read them.
func (s *Service) Signers(ctx context.Context, reader, baselineID uuid.UUID) ([]string, error) {
	var out []string
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT COALESCE(NULLIF(u.name, ''), u.email::text, '') FROM baseline_signature g JOIN app_user u ON u.id = g.signer_id
			WHERE g.baseline_id = $1 ORDER BY g.signed_at`, baselineID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			out = append(out, name)
		}
		return rows.Err()
	})
	return out, err
}
