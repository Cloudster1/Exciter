package trace

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// The traceability matrix (#38), its axes, link types and direction chosen (#147); docs/api-contract-m5.md.

// MatrixColumns says what a matrix's columns are.
type MatrixColumns string

const (
	ColumnsRequirements MatrixColumns = "requirements"
	ColumnsIssues       MatrixColumns = "issues"
)

// MatrixColumnKinds lists every MatrixColumns, for the API document.
var MatrixColumnKinds = []MatrixColumns{ColumnsRequirements, ColumnsIssues}

// MatrixDirection says which way a link must point from its row to be counted.
type MatrixDirection string

const (
	DirectionBoth     MatrixDirection = "both"
	DirectionOutgoing MatrixDirection = "outgoing"
	DirectionIncoming MatrixDirection = "incoming"
)

// MatrixDirections lists every MatrixDirection, for the API document.
var MatrixDirections = []MatrixDirection{DirectionBoth, DirectionOutgoing, DirectionIncoming}

// LinkKind says what a link of a matrix's cell is: a trace link, or a link to an Armature issue or a Stator page.
type LinkKind string

const (
	KindTrace    LinkKind = "trace"
	KindArmature LinkKind = "armature"
	KindStator   LinkKind = "stator"
)

// LinkKinds lists every LinkKind, for the API document.
var LinkKinds = []LinkKind{KindTrace, KindArmature, KindStator}

// MatrixQuery is what a matrix lays out: the rows a filter keeps in its order, against columns of one kind, counting
// the links of LinkTypes (every type when empty) in Direction, and for issue columns the Relations given.
type MatrixQuery struct {
	Filter        search.Filter
	Sort          search.Sort
	Columns       MatrixColumns
	ColumnProject string
	LinkTypes     []string
	Direction     MatrixDirection
	Relations     []string
	Limit, Offset int
}

// MatrixRow is a requirement as a row, and whether the reader may link it.
type MatrixRow struct {
	requirement.Ref
	CanLink bool `json:"canLink"`
}

// MatrixColumn is a requirement, or an Armature issue by its key.
type MatrixColumn struct {
	Requirement *requirement.Ref `json:"requirement"`
	IssueKey    string           `json:"issueKey"`
}

// MatrixCell is every link between one row and one column, by their places in the answer.
type MatrixCell struct {
	Row    int          `json:"row"`
	Column int          `json:"column"`
	Links  []MatrixLink `json:"links"`
}

// MatrixLink is one link of a cell: a trace link named by its type, or an Armature link by its relation, and which
// way it points from the row.
type MatrixLink struct {
	ID        uuid.UUID `json:"id"`
	Kind      LinkKind  `json:"kind"`
	Name      string    `json:"name"`
	TypeKey   string    `json:"typeKey,omitempty"`
	Direction Direction `json:"direction"`
	Suspect   bool      `json:"suspect"`
}

// Matrix is a page of requirements against every column any of them reaches.
type Matrix struct {
	Rows    []MatrixRow    `json:"rows"`
	Columns []MatrixColumn `json:"columns"`
	Cells   []MatrixCell   `json:"cells"`
	Total   int            `json:"total"`
	Limit   int            `json:"limit"`
	Offset  int            `json:"offset"`
}

// TooWideError refuses a matrix with more columns than MatrixMaxColumns.
type TooWideError struct{ Columns int }

func (e *TooWideError) Error() string {
	return fmt.Sprintf("These requirements reach %d columns, more than the %d a matrix shows. Narrow the filter, or page fewer rows.",
		e.Columns, MatrixMaxColumns)
}

// checkMatrix fills a query's defaults and refuses columns or a direction it does not know.
func checkMatrix(q *MatrixQuery) error {
	if q.Columns == "" {
		q.Columns = ColumnsRequirements
	}
	if !slices.Contains(MatrixColumnKinds, q.Columns) {
		return &FieldError{Field: "columns", Message: "Show requirements or issues as the columns."}
	}
	if q.Direction == "" {
		q.Direction = DirectionBoth
	}
	if !slices.Contains(MatrixDirections, q.Direction) {
		return &FieldError{Field: "direction", Message: "Count the links that point outgoing, incoming or both."}
	}
	if q.Columns == ColumnsRequirements && len(q.Relations) > 0 {
		return &FieldError{Field: "relation", Message: "Relations narrow issue columns. Show issues as the columns, or leave the relations out."}
	}
	if q.Columns == ColumnsIssues && len(q.LinkTypes) > 0 {
		return &FieldError{Field: "linkType", Message: "Link types narrow requirement columns. Show requirements as the columns, or leave the link types out."}
	}
	return nil
}

// Matrix answers a window of the requirements of a project q keeps, in its order, against the requirements of its
// column project, the same project when empty, or the Armature issues they link to.
func (s *Service) Matrix(ctx context.Context, reader uuid.UUID, projectKey string, q MatrixQuery) (*Matrix, error) {
	if err := checkMatrix(&q); err != nil {
		return nil, err
	}
	out := &Matrix{Rows: []MatrixRow{}, Columns: []MatrixColumn{}, Cells: []MatrixCell{}, Limit: q.Limit, Offset: q.Offset}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		other := p
		if q.Columns == ColumnsRequirements && q.ColumnProject != "" && !strings.EqualFold(q.ColumnProject, p.Key) {
			if other, err = project.Find(ctx, tx, reader, q.ColumnProject); errors.Is(err, project.ErrNotFound) {
				return &FieldError{Field: "columnProject", Message: "There is no such project you can see. Pick the columns' project from the list."}
			} else if err != nil {
				return err
			}
		}
		if err := knownLinkTypes(ctx, tx, q.LinkTypes, p.ID, other.ID); err != nil {
			return err
		}
		kept, err := search.Keep(ctx, tx, reader, p, q.Filter, q.Sort)
		if err != nil {
			return err
		}
		out.Total = len(kept)
		page := window(kept, q.Limit, q.Offset)
		at := map[uuid.UUID]int{}
		for i, k := range page {
			out.Rows = append(out.Rows, MatrixRow{Ref: k.Ref, CanLink: k.Edit && p.ArchivedAt == nil})
			at[k.Ref.ID] = i
		}
		if len(page) == 0 {
			return nil
		}
		if q.Columns == ColumnsIssues {
			return issueColumns(ctx, tx, out, at, q.Relations)
		}
		return requirementColumns(ctx, tx, reader, other, out, at, q)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// knownLinkTypes refuses a link type key that neither the installation nor the projects of the two axes have.
func knownLinkTypes(ctx context.Context, tx db.DBTX, keys []string, projects ...uuid.UUID) error {
	if len(keys) == 0 {
		return nil
	}
	var known []string
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(array_agg(DISTINCT key), '{}') FROM link_type
		WHERE key = ANY ($1) AND (project_id IS NULL OR project_id = ANY ($2))`, keys, projects).Scan(&known)
	if err != nil {
		return fmt.Errorf("read the matrix's link types: %w", err)
	}
	for _, k := range keys {
		if !slices.Contains(known, k) {
			return &FieldError{Field: "linkType", Message: fmt.Sprintf("There is no link type %q here. Pick the link types from the list.", k)}
		}
	}
	return nil
}

// cellsOf gathers the links found into cells, each once per row and column, in the columns' order.
type cellsOf map[[2]int][]MatrixLink

func (c cellsOf) add(row, column int, l MatrixLink) {
	c[[2]int{row, column}] = append(c[[2]int{row, column}], l)
}

func (c cellsOf) into(m *Matrix) {
	for at, links := range c {
		m.Cells = append(m.Cells, MatrixCell{Row: at[0], Column: at[1], Links: links})
	}
	slices.SortFunc(m.Cells, func(a, b MatrixCell) int { return cmp.Or(cmp.Compare(a.Row, b.Row), cmp.Compare(a.Column, b.Column)) })
}

// requirementColumns adds the requirements of the column project the rows link to, in outline order. A type that
// reads the same both ways counts in either direction.
func requirementColumns(ctx context.Context, tx db.DBTX, reader uuid.UUID, other *project.Project, m *Matrix, at map[uuid.UUID]int, q MatrixQuery) error {
	ids := make([]uuid.UUID, 0, len(at))
	for id := range at {
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, requirement.WalkTree+`
		SELECT row_end.id, c.id, c.number, c.title, c.revision, c.status_id, COALESCE(st.name, ''), COALESCE(st.category, ''),
		       v.id, lt.name, lt.key, row_end.direction, v.suspect
		FROM trace_link_state v
		JOIN link_type lt ON lt.id = v.type_id
		CROSS JOIN LATERAL (VALUES (v.source_id, v.target_id, 'outgoing'), (v.target_id, v.source_id, 'incoming'))
		     AS row_end (id, other_id, direction)
		JOIN walk w ON w.id = row_end.other_id
		JOIN requirement c ON c.id = w.id
		LEFT JOIN req_status st ON st.id = c.status_id
		WHERE row_end.id = ANY ($2) AND perm_requirement_viewable(c.id, $3)
		  AND (COALESCE(cardinality($4::text[]), 0) = 0 OR lt.key = ANY ($4))
		  AND ($5 = 'both' OR row_end.direction = $5 OR lt.forward = lt.backward)
		ORDER BY w.path, lt.name, v.id`, other.ID, ids, reader, q.LinkTypes, string(q.Direction))
	if err != nil {
		return fmt.Errorf("read the matrix's links: %w", err)
	}
	defer rows.Close()
	columnAt := map[uuid.UUID]int{}
	cells := cellsOf{}
	for rows.Next() {
		var (
			rowID    uuid.UUID
			statusID *uuid.UUID
			ref      requirement.Ref
			number   int
			status   requirement.StatusRef
			link     = MatrixLink{Kind: KindTrace}
		)
		if err := rows.Scan(&rowID, &ref.ID, &number, &ref.Title, &ref.Revision, &statusID, &status.Name, &status.Category,
			&link.ID, &link.Name, &link.TypeKey, &link.Direction, &link.Suspect); err != nil {
			return err
		}
		column, seen := columnAt[ref.ID]
		if !seen {
			ref.ProjectKey, ref.Kind, ref.Key = other.Key, requirement.KindRequirement, requirement.Key(other.Key, number)
			if statusID != nil {
				status.ID = *statusID
				ref.Status = &status
			}
			column = len(m.Columns)
			columnAt[ref.ID] = column
			m.Columns = append(m.Columns, MatrixColumn{Requirement: &ref})
		}
		cells.add(at[rowID], column, link)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(m.Columns) > MatrixMaxColumns {
		return &TooWideError{Columns: len(m.Columns)}
	}
	cells.into(m)
	return nil
}

// issueColumns adds the Armature issues the rows are traced to, neither broken nor detached, by project and number,
// keeping the relations given, every one when none is.
func issueColumns(ctx context.Context, tx db.DBTX, m *Matrix, at map[uuid.UUID]int, relations []string) error {
	ids := make([]uuid.UUID, 0, len(at))
	for id := range at {
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, `
		SELECT l.requirement_id, l.issue_key, l.id, l.relation, r.revision > l.reviewed_rev
		FROM armature_issue_link l JOIN requirement r ON r.id = l.requirement_id
		WHERE l.requirement_id = ANY ($1) AND l.broken_at IS NULL AND l.detached_at IS NULL
		  AND (COALESCE(cardinality($2::text[]), 0) = 0 OR l.relation = ANY ($2))
		ORDER BY l.relation, l.id`, ids, relations)
	if err != nil {
		return fmt.Errorf("read the matrix's Armature links: %w", err)
	}
	type found struct {
		row  uuid.UUID
		key  string
		link MatrixLink
	}
	all, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (found, error) {
		f := found{link: MatrixLink{Kind: KindArmature, Direction: Outgoing}}
		err := row.Scan(&f.row, &f.key, &f.link.ID, &f.link.Name, &f.link.Suspect)
		return f, err
	})
	if err != nil {
		return err
	}
	var keys []string
	for _, f := range all {
		if !slices.Contains(keys, f.key) {
			keys = append(keys, f.key)
		}
	}
	if len(keys) > MatrixMaxColumns {
		return &TooWideError{Columns: len(keys)}
	}
	slices.SortFunc(keys, compareIssueKeys)
	cells := cellsOf{}
	for _, f := range all {
		cells.add(at[f.row], slices.Index(keys, f.key), f.link)
	}
	for _, k := range keys {
		m.Columns = append(m.Columns, MatrixColumn{IssueKey: k})
	}
	cells.into(m)
	return nil
}

// compareIssueKeys orders issue keys by their project, then by number, so ABC-9 comes before ABC-10.
func compareIssueKeys(a, b string) int {
	ap, an, _ := strings.Cut(a, "-")
	bp, bn, _ := strings.Cut(b, "-")
	x, _ := strconv.ParseUint(an, 10, 64)
	y, _ := strconv.ParseUint(bn, 10, 64)
	return cmp.Or(cmp.Compare(ap, bp), cmp.Compare(x, y))
}
