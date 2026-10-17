package baseline

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// MatrixQuery is what a baseline's matrix lays out (#153): its columns, the link types and direction counted, a window
// of its rows, and what to compare it with, another baseline's id or now, when With is set.
type MatrixQuery struct {
	Columns   trace.MatrixColumns
	LinkTypes []string
	Direction trace.MatrixDirection
	With      string
	Limit     int
	Offset    int
}

func checkMatrixQuery(q *MatrixQuery) error {
	if q.Columns == "" {
		q.Columns = trace.ColumnsRequirements
	}
	if q.Direction == "" {
		q.Direction = trace.DirectionBoth
	}
	switch {
	case !slices.Contains(trace.RecordedColumnKinds, q.Columns):
		return &FieldError{"columns", "Show requirements, issues or pages as the columns."}
	case !slices.Contains(trace.MatrixDirections, q.Direction):
		return &FieldError{"direction", "Count the links that point outgoing, incoming or both."}
	case q.Columns != trace.ColumnsRequirements && len(q.LinkTypes) > 0:
		return &FieldError{"linkType", "Link types narrow requirement columns. Show requirements as the columns, or leave the link types out."}
	}
	return nil
}

// Matrix answers a window of a baseline's requirements against what they were linked to, each link marked added,
// removed or changed against the other side when one is asked for.
func (s *Service) Matrix(ctx context.Context, reader, id uuid.UUID, q MatrixQuery) (*Matrix, error) {
	if err := checkMatrixQuery(&q); err != nil {
		return nil, err
	}
	out := &Matrix{Rows: []requirement.Ref{}, Columns: []trace.MatrixColumn{}, Cells: []MatrixCell{}, Limit: q.Limit, Offset: q.Offset}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		from, err := get(ctx, tx, id)
		if err != nil {
			return err
		}
		out.Detail, out.Signing = from.Detail, from.Signing
		this, err := baselineMatrixSide(ctx, tx, from, q)
		if err != nil {
			return err
		}
		var other *matrixSide
		switch with := strings.TrimSpace(q.With); with {
		case "":
		case "now":
			var at time.Time
			if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&at); err != nil {
				return err
			}
			out.With = &Side{At: at}
			if other, err = nowMatrixSide(ctx, tx, from, q); err != nil {
				return err
			}
		default:
			later, err := otherBaseline(ctx, tx, from, with)
			if err != nil {
				return err
			}
			out.With = &Side{Baseline: &later.Baseline, At: later.CreatedAt}
			if other, err = baselineMatrixSide(ctx, tx, later, q); err != nil {
				return err
			}
		}
		return lay(out, this, other, q)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// differs says a link both sides hold reads otherwise on the other: suspect or status, where both recorded it.
func differs(a, b MatrixLink) bool {
	return (a.Suspect != nil && b.Suspect != nil && *a.Suspect != *b.Suspect) || (a.Status != nil && b.Status != nil && *a.Status != *b.Status)
}

// lay puts a page of the rows of either side against the columns they reach, in cells, each link marked against the
// other side when there is one: removed from the baseline, added on the other side, or changed between them.
func lay(m *Matrix, this, other *matrixSide, q MatrixQuery) error {
	rows := this.rows
	if other != nil {
		seen := rowsByID(this.rows)
		for _, r := range other.rows {
			if _, ok := seen[r.ref.ID]; !ok {
				rows = append(rows, r)
			}
		}
	}
	m.Total = len(rows)
	rowAt := map[uuid.UUID]int{}
	for i, r := range window(rows, q.Limit, q.Offset) {
		m.Rows = append(m.Rows, r.ref)
		rowAt[r.ref.ID] = i
	}
	var shown []sideLink
	if other == nil {
		shown = this.links
	} else {
		theirs := map[string]sideLink{}
		for _, l := range other.links {
			theirs[l.key()] = l
		}
		ours := map[string]bool{}
		for _, l := range this.links {
			ours[l.key()] = true
			switch later, kept := theirs[l.key()]; {
			case !kept:
				l.link.Change = Removed
			case differs(l.link, later.link):
				l = later
				l.link.Change = Changed
			}
			shown = append(shown, l)
		}
		for _, l := range other.links {
			if !ours[l.key()] {
				l.link.Change = Added
				shown = append(shown, l)
			}
		}
	}
	type column struct {
		trace.MatrixColumn
		order []int32
	}
	columns := map[string]column{}
	keyOf := func(c trace.MatrixColumn) string {
		switch {
		case c.Requirement != nil:
			return c.Requirement.ID.String()
		case c.Page != nil:
			return c.Page.ID.String()
		}
		return c.IssueKey
	}
	for _, l := range shown {
		if _, ok := rowAt[l.row]; ok {
			columns[keyOf(l.column)] = column{l.column, l.order}
		}
	}
	if len(columns) > trace.MatrixMaxColumns {
		return &trace.TooWideError{Columns: len(columns)}
	}
	ordered := make([]column, 0, len(columns))
	for _, c := range columns {
		ordered = append(ordered, c)
	}
	slices.SortFunc(ordered, func(a, b column) int {
		switch {
		case a.Requirement != nil:
			return slices.Compare(a.order, b.order)
		case a.Page != nil:
			return cmp.Or(cmp.Compare(a.Page.SpaceKey, b.Page.SpaceKey), cmp.Compare(a.Page.ID.String(), b.Page.ID.String()))
		}
		return trace.CompareIssueKeys(a.IssueKey, b.IssueKey)
	})
	columnAt := map[string]int{}
	for i, c := range ordered {
		columnAt[keyOf(c.MatrixColumn)] = i
		m.Columns = append(m.Columns, c.MatrixColumn)
	}
	cells := map[[2]int][]MatrixLink{}
	for _, l := range shown {
		if r, ok := rowAt[l.row]; ok {
			at := [2]int{r, columnAt[keyOf(l.column)]}
			cells[at] = append(cells[at], l.link)
		}
	}
	for at, links := range cells {
		m.Cells = append(m.Cells, MatrixCell{Row: at[0], Column: at[1], Links: links})
	}
	slices.SortFunc(m.Cells, func(a, b MatrixCell) int { return cmp.Or(cmp.Compare(a.Row, b.Row), cmp.Compare(a.Column, b.Column)) })
	return nil
}

func window[T any](all []T, limit, offset int) []T {
	if offset >= len(all) {
		return []T{}
	}
	return all[offset:min(len(all), offset+limit)]
}
