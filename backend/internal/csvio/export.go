package csvio

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// ExportColumns are the columns an export may carry besides an attribute, which is named by its id as the grid
// names it; outline and parent always come last.
var ExportColumns = []string{TargetKey, TargetKind, TargetTitle, TargetStatement, TargetRationale, TargetType, TargetStatus,
	TargetOwner, TargetPriority, TargetLabels, TargetLinks, "updated"}

// defaultExport is what an export carries when nobody chose: everything an import into another project reads.
var defaultExport = []string{TargetKey, TargetKind, TargetTitle, TargetStatement, TargetRationale, TargetType, TargetStatus,
	TargetOwner, TargetPriority, TargetLabels, TargetLinks}

// ColumnError is a column an export cannot carry, with the sentence to show.
type ColumnError struct{ Message string }

func (e *ColumnError) Error() string { return e.Message }

// Exported says how much of the grid an export wrote.
type Exported struct {
	Rows    int
	Matched int
}

// Truncated says the grid held more than an export carries.
func (e Exported) Truncated() bool { return e.Rows < e.Matched }

// details are what an export writes that the grid does not show.
type details struct {
	statement, rationale json.RawMessage
	parent, owner        string
	links                []string
}

// Export writes one project's requirements, narrowed and ordered as the grid is, as CSV with the columns
// asked for and outline and parent last, keys kept, up to MaxExportRows of them.
func (s *Service) Export(ctx context.Context, w io.Writer, actor uuid.UUID, projectKey string, f search.Filter, sort search.Sort, columns []string) (Exported, error) {
	var done Exported
	rows, total, err := s.search.Grid(ctx, actor, projectKey, f, sort, MaxExportRows, 0)
	if err != nil {
		return done, err
	}
	done.Matched, done.Rows = total, len(rows)
	var (
		defs   []attribute.Def
		extra  map[uuid.UUID]details
		people map[uuid.UUID]string
	)
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if defs, err = attribute.Defs(ctx, tx, p.ID); err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		if extra, err = exportDetails(ctx, tx, ids); err != nil {
			return err
		}
		people, err = exportPeople(ctx, tx, rows, defs)
		return err
	})
	if err != nil {
		return done, err
	}
	if columns, err = exportColumns(columns, defs); err != nil {
		return done, err
	}
	out := csv.NewWriter(w)
	header := make([]string, len(columns))
	for i, c := range columns {
		header[i] = columnHeader(c, defs)
	}
	if err := out.Write(header); err != nil {
		return done, err
	}
	for _, r := range rows {
		line := make([]string, len(columns))
		for i, c := range columns {
			line[i] = Cell(exportCell(r, extra[r.ID], c, defs, people))
		}
		if err := out.Write(line); err != nil {
			return done, err
		}
	}
	if done.Truncated() {
		if err := out.Write([]string{fmt.Sprintf("This file holds the first %d of the %d requirements the grid shows. Narrow the grid to export the rest.",
			done.Rows, done.Matched)}); err != nil {
			return done, err
		}
	}
	out.Flush()
	return done, out.Error()
}

// exportColumns checks the columns asked for, the default ones when none were, and puts outline and parent last.
func exportColumns(asked []string, defs []attribute.Def) ([]string, error) {
	if len(asked) == 0 {
		asked = append([]string{}, defaultExport...)
		for _, d := range defs {
			if !d.Retired {
				asked = append(asked, d.ID.String())
			}
		}
	}
	var out []string
	for _, c := range asked {
		if c == TargetOutline || c == TargetParent || slices.Contains(out, c) {
			continue
		}
		if !slices.Contains(ExportColumns, c) && defOf(c, defs) == nil {
			return nil, &ColumnError{fmt.Sprintf("%q is not a column. Ask for %s, or an attribute's id.", c, strings.Join(ExportColumns, ", "))}
		}
		out = append(out, c)
	}
	return append(out, TargetOutline, TargetParent), nil
}

func defOf(id string, defs []attribute.Def) *attribute.Def {
	for i := range defs {
		if defs[i].ID.String() == id {
			return &defs[i]
		}
	}
	return nil
}

// columnHeader is the header an import maps back to the same target.
func columnHeader(column string, defs []attribute.Def) string {
	if d := defOf(column, defs); d != nil {
		return AttributePrefix + d.Name
	}
	return column
}

// exportDetails reads the statement, rationale, parent, owner and outgoing links of the requirements exported.
func exportDetails(ctx context.Context, tx db.DBTX, ids []uuid.UUID) (map[uuid.UUID]details, error) {
	out := map[uuid.UUID]details{}
	rows, err := tx.Query(ctx, `
		SELECT r.id, r.statement, r.rationale, COALESCE(pp.key || '-R' || pr.number, ''), COALESCE(o.email::text, '')
		FROM requirement r
		LEFT JOIN requirement pr ON pr.org_id = r.org_id AND pr.id = r.parent_id
		LEFT JOIN project pp ON pp.org_id = pr.org_id AND pp.id = pr.project_id
		LEFT JOIN app_user o ON o.id = r.owner_id
		WHERE r.org_id = current_org_id() AND r.id = ANY ($1)`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			id uuid.UUID
			d  details
		)
		if err := rows.Scan(&id, &d.statement, &d.rationale, &d.parent, &d.owner); err != nil {
			rows.Close()
			return nil, err
		}
		out[id] = d
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	links, err := tx.Query(ctx, `
		SELECT l.source_id, lt.key || ':' || op.key || '-R' || ot.number
		FROM trace_link l
		JOIN link_type lt ON lt.org_id = l.org_id AND lt.id = l.type_id
		JOIN requirement ot ON ot.org_id = l.org_id AND ot.id = l.target_id
		JOIN project op ON op.org_id = ot.org_id AND op.id = ot.project_id
		WHERE l.org_id = current_org_id() AND l.source_id = ANY ($1) AND l.deleted_at IS NULL
		ORDER BY l.created_at, l.id`, ids)
	if err != nil {
		return nil, err
	}
	defer links.Close()
	for links.Next() {
		var (
			id   uuid.UUID
			link string
		)
		if err := links.Scan(&id, &link); err != nil {
			return nil, err
		}
		d := out[id]
		d.links = append(d.links, link)
		out[id] = d
	}
	return out, links.Err()
}

// exportPeople are the addresses of the people attribute values name, which a file writes them by.
func exportPeople(ctx context.Context, tx db.DBTX, rows []search.Row, defs []attribute.Def) (map[uuid.UUID]string, error) {
	var ids []uuid.UUID
	for _, r := range rows {
		for _, d := range defs {
			var id uuid.UUID
			if d.Kind == attribute.User && json.Unmarshal(r.Attributes[d.ID.String()], &id) == nil {
				ids = append(ids, id)
			}
		}
	}
	out := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	found, err := tx.Query(ctx, `SELECT id, email::text FROM app_user WHERE id = ANY ($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer found.Close()
	for found.Next() {
		var (
			id    uuid.UUID
			email string
		)
		if err := found.Scan(&id, &email); err != nil {
			return nil, err
		}
		out[id] = email
	}
	return out, found.Err()
}

// exportCell is one value of one requirement as the file writes it.
func exportCell(r search.Row, d details, column string, defs []attribute.Def, people map[uuid.UUID]string) string {
	switch column {
	case TargetKey:
		return r.Key
	case TargetKind:
		return string(r.Kind)
	case TargetTitle:
		return r.Title
	case TargetStatement:
		return PlainText(d.statement)
	case TargetRationale:
		return PlainText(d.rationale)
	case TargetType:
		if r.Type != nil {
			return r.Type.Name
		}
	case TargetStatus:
		if r.Status != nil {
			return r.Status.Name
		}
	case TargetOwner:
		return d.owner
	case TargetPriority:
		if r.Priority != nil {
			return string(*r.Priority)
		}
	case TargetLabels:
		return strings.Join(r.Labels, ", ")
	case TargetLinks:
		return strings.Join(d.links, ", ")
	case "updated":
		return r.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
	case TargetOutline:
		return r.Outline
	case TargetParent:
		return d.parent
	}
	if def := defOf(column, defs); def != nil {
		return attributeText(*def, r.Attributes[column], people)
	}
	return ""
}

// attributeText is a value as a cell writes it: options and people by name, documents as plain text.
func attributeText(d attribute.Def, raw json.RawMessage, people map[uuid.UUID]string) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	optionName := func(id string) string {
		for _, o := range d.Options {
			if o.ID.String() == id {
				return o.Name
			}
		}
		return ""
	}
	switch d.Kind {
	case attribute.RichText:
		return PlainText(raw)
	case attribute.Select:
		var id string
		_ = json.Unmarshal(raw, &id)
		return optionName(id)
	case attribute.MultiSelect:
		var ids []string
		_ = json.Unmarshal(raw, &ids)
		names := make([]string, 0, len(ids))
		for _, id := range ids {
			names = append(names, optionName(id))
		}
		return strings.Join(names, ", ")
	case attribute.User:
		var id uuid.UUID
		_ = json.Unmarshal(raw, &id)
		return people[id]
	case attribute.Number:
		var n float64
		_ = json.Unmarshal(raw, &n)
		return strconv.FormatFloat(n, 'f', -1, 64)
	case attribute.Boolean:
		var b bool
		_ = json.Unmarshal(raw, &b)
		return strconv.FormatBool(b)
	}
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// formulaStarts are what a spreadsheet takes for the start of a formula.
const formulaStarts = "=+-@\t\r"

// Cell is a value as a spreadsheet should read it: text, never a formula. What somebody typed lands in
// another person's spreadsheet, and "=" there is a program.
func Cell(s string) string {
	if s == "" || !strings.ContainsRune(formulaStarts, rune(s[0])) {
		return s
	}
	return "'" + s
}

// uncell undoes Cell, so a file exported here imports back as it was.
func uncell(s string) string {
	if len(s) > 1 && s[0] == '\'' && strings.ContainsRune(formulaStarts, rune(s[1])) {
		return s[1:]
	}
	return s
}
