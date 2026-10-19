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

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// The traceability matrix (#38), its axes, link types and direction chosen (#147), Stator pages as its columns
// (#150); docs/api-contract-m5.md.

// MatrixColumns says what a matrix's columns are.
type MatrixColumns string

const (
	ColumnsRequirements MatrixColumns = "requirements"
	ColumnsIssues       MatrixColumns = "issues"
	// ColumnsPages are the Stator pages the rows are linked to (#150).
	ColumnsPages MatrixColumns = "pages"
	// ColumnsTests are the test cases whose latest results on the counting branch verify the rows (#200).
	ColumnsTests MatrixColumns = "tests"
	// ColumnsCode are the files whose tags implement or verify the rows on the counting branch (#201).
	ColumnsCode MatrixColumns = "code"
)

// MatrixColumnKinds lists every MatrixColumns, for the API document; RecordedColumnKinds are those a baseline's
// matrix lays out from what it recorded.
var (
	MatrixColumnKinds   = []MatrixColumns{ColumnsRequirements, ColumnsIssues, ColumnsPages, ColumnsTests, ColumnsCode}
	RecordedColumnKinds = []MatrixColumns{ColumnsRequirements, ColumnsIssues, ColumnsPages}
)

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
	// KindTest is a test case's latest result for the row, named by its outcome (#200).
	KindTest LinkKind = "test"
	// KindCode is a code tag in a file for the row, named by its relation (#201).
	KindCode LinkKind = "code"
)

// LinkKinds lists every LinkKind, for the API document.
var LinkKinds = []LinkKind{KindTrace, KindArmature, KindStator, KindTest, KindCode}

// Gaps says which rows or columns without a link a matrix keeps; empty keeps every one.
type Gaps string

const (
	GapsRows    Gaps = "rows"
	GapsColumns Gaps = "columns"
	GapsBoth    Gaps = "both"
)

// GapKinds lists every Gaps, for the API document.
var GapKinds = []Gaps{GapsRows, GapsColumns, GapsBoth}

// MatrixQuery is what a matrix lays out: the rows a filter keeps in its order, against columns of one kind, counting
// the links of LinkTypes (every type when empty) in Direction, and for issue columns the Relations given.
type MatrixQuery struct {
	Filter        search.Filter
	Sort          search.Sort
	Columns       MatrixColumns
	ColumnProject string
	// ColumnFilter keeps the requirement columns, linked or not (#148); nil leaves them what the page's rows reach.
	ColumnFilter *search.Filter
	LinkTypes    []string
	Direction    MatrixDirection
	Relations    []string
	// ColumnRepository keeps the test columns to one repository's.
	ColumnRepository string
	Gaps             Gaps
	Limit            int
	Offset           int
	// ColumnLimit pages the columns; zero answers every one, and refuses more than MatrixMaxColumns.
	ColumnLimit, ColumnOffset int
}

// MatrixRow is a requirement as a row, and whether the reader may link it.
type MatrixRow struct {
	requirement.Ref
	CanLink bool `json:"canLink"`
	// LastRun is a test case's last result (#252).
	LastRun *LastRun `json:"lastRun,omitempty"`
}

// MatrixColumn is a requirement, an Armature issue by its key, a Stator page, or a test case.
type MatrixColumn struct {
	Requirement *requirement.Ref `json:"requirement"`
	IssueKey    string           `json:"issueKey"`
	Page        *MatrixPage      `json:"page"`
	Test        *MatrixTest      `json:"test,omitempty"`
	Code        *MatrixCode      `json:"code,omitempty"`
	// LastRun is a test case's last result (#252).
	LastRun *LastRun `json:"lastRun,omitempty"`
}

// MatrixCode is a file as a column: its repository and path, and its address at the scanned commit.
type MatrixCode struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	URL        string `json:"url"`
}

// MatrixTest is a test case as a column: its repository, suite and name.
type MatrixTest struct {
	Repository string `json:"repository"`
	Suite      string `json:"suite"`
	Name       string `json:"name"`
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
	// Outdated says a test's result was made against an older revision of the row.
	Outdated bool `json:"outdated,omitempty"`
}

// Matrix is a page of requirements against a page of the columns, and how many links each has in all.
type Matrix struct {
	Rows         []MatrixRow    `json:"rows"`
	Columns      []MatrixColumn `json:"columns"`
	Cells        []MatrixCell   `json:"cells"`
	RowLinks     []int          `json:"rowLinks"`
	ColumnLinks  []int          `json:"columnLinks"`
	Total        int            `json:"total"`
	Limit        int            `json:"limit"`
	Offset       int            `json:"offset"`
	ColumnTotal  int            `json:"columnTotal"`
	ColumnLimit  int            `json:"columnLimit"`
	ColumnOffset int            `json:"columnOffset"`
}

// TooWideError refuses a matrix with more columns than MatrixMaxColumns.
type TooWideError struct{ Columns int }

func (e *TooWideError) Error() string {
	return fmt.Sprintf("This matrix has %d columns, more than the %d a file holds. Narrow the filter or the column filter.",
		e.Columns, MatrixMaxColumns)
}

// ColumnQueryError is a column filter's query refused, said apart from the rows' query.
type ColumnQueryError struct {
	Msg string
	Pos int
}

func (e *ColumnQueryError) Error() string { return "column query: " + e.Msg }

// checkMatrix fills a query's defaults and refuses columns or a direction it does not know.
func checkMatrix(q *MatrixQuery) error {
	if q.Columns == "" {
		q.Columns = ColumnsRequirements
	}
	if !slices.Contains(MatrixColumnKinds, q.Columns) {
		return &FieldError{Field: "columns", Message: "Show requirements, issues, pages, tests or code as the columns."}
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
	if q.Columns != ColumnsRequirements && len(q.LinkTypes) > 0 {
		return &FieldError{Field: "linkType", Message: "Link types narrow requirement columns. Show requirements as the columns, or leave the link types out."}
	}
	if q.Columns != ColumnsTests && q.Columns != ColumnsCode && q.ColumnRepository != "" {
		return &FieldError{Field: "columnRepository", Message: "A repository narrows test and code columns. Show those as the columns, or leave the repository out."}
	}
	if (q.Columns == ColumnsTests || q.Columns == ColumnsCode) && len(q.Relations) > 0 {
		return &FieldError{Field: "relation", Message: "Relations narrow issue and page columns. Show those as the columns, or leave the relations out."}
	}
	if q.Columns != ColumnsRequirements && q.ColumnFilter != nil {
		return &FieldError{Field: "columnQuery", Message: "A column filter keeps requirement columns. Show requirements as the columns, or leave the column filter out."}
	}
	if q.Gaps != "" && !slices.Contains(GapKinds, q.Gaps) {
		return &FieldError{Field: "gaps", Message: "Keep the gaps in rows, columns or both."}
	}
	if (q.Gaps == GapsColumns || q.Gaps == GapsBoth) && q.ColumnFilter == nil {
		return &FieldError{Field: "gaps", Message: "Columns the rows reach always have a link. Add a column filter to find columns without one, or keep the rows' gaps."}
	}
	return nil
}

// Matrix answers a window of the requirements of a project q keeps, in its order, against a window of the
// requirements of its column project, the same project when empty, or of the Armature issues they link to.
func (s *Service) Matrix(ctx context.Context, reader uuid.UUID, projectKey string, q MatrixQuery) (*Matrix, error) {
	if err := checkMatrix(&q); err != nil {
		return nil, err
	}
	out := &Matrix{Rows: []MatrixRow{}, Columns: []MatrixColumn{}, Cells: []MatrixCell{}, RowLinks: []int{}, ColumnLinks: []int{},
		Limit: q.Limit, Offset: q.Offset, ColumnLimit: q.ColumnLimit, ColumnOffset: q.ColumnOffset}
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
		ids := make([]uuid.UUID, len(kept))
		for i, k := range kept {
			ids[i] = k.Ref.ID
		}
		var l *laid
		switch q.Columns {
		case ColumnsIssues:
			l, err = issueLinks(ctx, tx, ids, q.Relations)
		case ColumnsPages:
			l, err = pageLinks(ctx, tx, ids, q.Relations)
		case ColumnsTests:
			l, err = testLinks(ctx, tx, ids, q.ColumnRepository)
		case ColumnsCode:
			l, err = codeLinks(ctx, tx, ids, q.ColumnRepository)
		default:
			l, err = requirementLinks(ctx, tx, reader, other, ids, q)
		}
		if err != nil {
			return err
		}
		if err := l.into(out, kept, p.ArchivedAt == nil, q); err != nil {
			return err
		}
		return markRuns(ctx, tx, out)
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

// CompareIssueKeys orders issue keys by their project, then by number, so ABC-9 comes before ABC-10; a baseline
// orders its matrix's issues the same way.
func CompareIssueKeys(a, b string) int {
	ap, an, _ := strings.Cut(a, "-")
	bp, bn, _ := strings.Cut(b, "-")
	x, _ := strconv.ParseUint(an, 10, 64)
	y, _ := strconv.ParseUint(bn, 10, 64)
	return cmp.Or(cmp.Compare(ap, bp), cmp.Compare(x, y))
}

// markRuns marks each test case among a matrix's rows and requirement columns with its last result.
func markRuns(ctx context.Context, tx db.DBTX, m *Matrix) error {
	var ids []uuid.UUID
	for _, r := range m.Rows {
		ids = append(ids, r.ID)
	}
	for _, c := range m.Columns {
		if c.Requirement != nil {
			ids = append(ids, c.Requirement.ID)
		}
	}
	runs, err := lastRuns(ctx, tx, ids)
	if err != nil {
		return err
	}
	for i := range m.Rows {
		m.Rows[i].LastRun = runs[m.Rows[i].ID]
	}
	for i := range m.Columns {
		if c := m.Columns[i].Requirement; c != nil {
			m.Columns[i].LastRun = runs[c.ID]
		}
	}
	return nil
}
