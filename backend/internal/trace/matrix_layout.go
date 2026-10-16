package trace

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/rql"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// StatorSuspectSQL says whether the page link aliased l of the requirement aliased r is suspect: its page has a newer
// version than the one confirmed, and neither is gone. It lives here, below internal/stator, for the matrix (#150).
const StatorSuspectSQL = `COALESCE(r.trashed_at IS NULL AND l.broken_at IS NULL AND l.detached_at IS NULL AND l.page_version > l.page_version_seen, false)`

// laid is every link between the rows a matrix keeps and the columns it may show, before either is paged, so
// counts and gaps (#148) cover the whole matrix and not a page of it.
type laid struct {
	columns []MatrixColumn
	// filtered says the columns are a column filter's, shown linked or not, rather than what the rows reach.
	filtered bool
	links    []laidLink
}

type laidLink struct {
	row    uuid.UUID
	column int
	link   MatrixLink
}

// requirementLinks reads the trace links between the rows and the requirements of the column project the column
// filter keeps, or every one it has, as the reader sees them.
func requirementLinks(ctx context.Context, tx db.DBTX, reader uuid.UUID, other *project.Project, rows []uuid.UUID, q MatrixQuery) (*laid, error) {
	f := search.Filter{}
	if q.ColumnFilter != nil {
		f = *q.ColumnFilter
	}
	candidates, err := search.Keep(ctx, tx, reader, other, f, search.Sort{})
	var bad *rql.Error
	if q.ColumnFilter != nil && errors.As(err, &bad) {
		return nil, &ColumnQueryError{Msg: bad.Msg, Pos: bad.Pos}
	} else if err != nil {
		return nil, err
	}
	l := &laid{filtered: q.ColumnFilter != nil}
	at := make(map[uuid.UUID]int, len(candidates))
	ids := make([]uuid.UUID, len(candidates))
	for i, c := range candidates {
		ref := c.Ref
		at[ref.ID], ids[i] = i, ref.ID
		l.columns = append(l.columns, MatrixColumn{Requirement: &ref})
	}
	if len(rows) == 0 || len(ids) == 0 {
		return l, nil
	}
	found, err := tx.Query(ctx, `
		SELECT row_end.id, row_end.other_id, v.id, lt.name, lt.key, row_end.direction, v.suspect
		FROM trace_link_state v
		JOIN link_type lt ON lt.id = v.type_id
		CROSS JOIN LATERAL (VALUES (v.source_id, v.target_id, 'outgoing'), (v.target_id, v.source_id, 'incoming'))
		     AS row_end (id, other_id, direction)
		WHERE row_end.id = ANY ($1) AND row_end.other_id = ANY ($2)
		  AND (COALESCE(cardinality($3::text[]), 0) = 0 OR lt.key = ANY ($3))
		  AND ($4 = 'both' OR row_end.direction = $4 OR lt.forward = lt.backward)
		ORDER BY lt.name, v.id`, rows, ids, q.LinkTypes, string(q.Direction))
	if err != nil {
		return nil, fmt.Errorf("read the matrix's links: %w", err)
	}
	l.links, err = pgx.CollectRows(found, func(row pgx.CollectableRow) (laidLink, error) {
		var (
			x     = laidLink{link: MatrixLink{Kind: KindTrace}}
			other uuid.UUID
		)
		err := row.Scan(&x.row, &other, &x.link.ID, &x.link.Name, &x.link.TypeKey, &x.link.Direction, &x.link.Suspect)
		x.column = at[other]
		return x, err
	})
	return l, err
}

// issueLinks reads the Armature links of the rows, neither broken nor detached, of the relations given or every
// one; the issues they reach are the columns, by project and number.
func issueLinks(ctx context.Context, tx db.DBTX, rows []uuid.UUID, relations []string) (*laid, error) {
	l := &laid{}
	if len(rows) == 0 {
		return l, nil
	}
	found, err := tx.Query(ctx, `
		SELECT l.requirement_id, l.issue_key, l.id, l.relation, r.revision > l.reviewed_rev
		FROM armature_issue_link l JOIN requirement r ON r.id = l.requirement_id
		WHERE l.requirement_id = ANY ($1) AND l.broken_at IS NULL AND l.detached_at IS NULL
		  AND (COALESCE(cardinality($2::text[]), 0) = 0 OR l.relation = ANY ($2))
		ORDER BY l.relation, l.id`, rows, relations)
	if err != nil {
		return nil, fmt.Errorf("read the matrix's Armature links: %w", err)
	}
	type issueLink struct {
		key string
		laidLink
	}
	all, err := pgx.CollectRows(found, func(row pgx.CollectableRow) (issueLink, error) {
		x := issueLink{laidLink: laidLink{link: MatrixLink{Kind: KindArmature, Direction: Outgoing}}}
		err := row.Scan(&x.row, &x.key, &x.link.ID, &x.link.Name, &x.link.Suspect)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, x := range all {
		if !slices.Contains(keys, x.key) {
			keys = append(keys, x.key)
		}
	}
	slices.SortFunc(keys, compareIssueKeys)
	for _, k := range keys {
		l.columns = append(l.columns, MatrixColumn{IssueKey: k})
	}
	for _, x := range all {
		x.column = slices.Index(keys, x.key)
		l.links = append(l.links, x.laidLink)
	}
	return l, nil
}

// pageLinks reads the Stator page links of the rows, neither broken nor detached, of the relations given or every
// one, suspect as the requirement page judges them; the pages they reach are the columns, by space, then id.
func pageLinks(ctx context.Context, tx db.DBTX, rows []uuid.UUID, relations []string) (*laid, error) {
	l := &laid{}
	if len(rows) == 0 {
		return l, nil
	}
	found, err := tx.Query(ctx, `
		SELECT l.requirement_id, l.page_id, l.space_key, l.url, l.id, l.relation, `+StatorSuspectSQL+`
		FROM stator_page_link l JOIN requirement r ON r.id = l.requirement_id
		WHERE l.requirement_id = ANY ($1) AND l.broken_at IS NULL AND l.detached_at IS NULL
		  AND (COALESCE(cardinality($2::text[]), 0) = 0 OR l.relation = ANY ($2))
		ORDER BY l.space_key, l.page_id, array_position(ARRAY['specifies', 'describes', 'relates'], l.relation), l.id`, rows, relations)
	if err != nil {
		return nil, fmt.Errorf("read the matrix's Stator links: %w", err)
	}
	at := map[uuid.UUID]int{}
	for found.Next() {
		var (
			x    = laidLink{link: MatrixLink{Kind: KindStator, Direction: Outgoing}}
			page MatrixPage
		)
		if err := found.Scan(&x.row, &page.ID, &page.SpaceKey, &page.URL, &x.link.ID, &x.link.Name, &x.link.Suspect); err != nil {
			found.Close()
			return nil, err
		}
		column, seen := at[page.ID]
		if !seen {
			column = len(l.columns)
			at[page.ID] = column
			l.columns = append(l.columns, MatrixColumn{Page: &page})
		}
		x.column = column
		l.links = append(l.links, x)
	}
	return l, found.Err()
}

// into lays a page of the rows q keeps against a page of the columns, with every one's count of links. Without a
// column filter the columns are what the page's rows reach, as in #38.
func (l *laid) into(m *Matrix, kept []search.Kept, open bool, q MatrixQuery) error {
	rowLinks := map[uuid.UUID]int{}
	columnLinks := make([]int, len(l.columns))
	for _, x := range l.links {
		rowLinks[x.row]++
		columnLinks[x.column]++
	}
	rows := kept
	if q.Gaps == GapsRows || q.Gaps == GapsBoth {
		rows = slices.DeleteFunc(slices.Clone(kept), func(k search.Kept) bool { return rowLinks[k.Ref.ID] > 0 })
	}
	m.Total = len(rows)
	rowAt := map[uuid.UUID]int{}
	for i, k := range window(rows, q.Limit, q.Offset) {
		m.Rows = append(m.Rows, MatrixRow{Ref: k.Ref, CanLink: k.Edit && open})
		m.RowLinks = append(m.RowLinks, rowLinks[k.Ref.ID])
		rowAt[k.Ref.ID] = i
	}
	var columns []int
	if l.filtered {
		for i := range l.columns {
			if columnLinks[i] == 0 || (q.Gaps != GapsColumns && q.Gaps != GapsBoth) {
				columns = append(columns, i)
			}
		}
	} else {
		reached := make([]bool, len(l.columns))
		for _, x := range l.links {
			if _, ok := rowAt[x.row]; ok {
				reached[x.column] = true
			}
		}
		for i, ok := range reached {
			if ok {
				columns = append(columns, i)
			}
		}
	}
	m.ColumnTotal = len(columns)
	if q.ColumnLimit == 0 {
		if len(columns) > MatrixMaxColumns {
			return &TooWideError{Columns: len(columns)}
		}
		m.ColumnLimit = len(columns)
	} else {
		columns = window(columns, q.ColumnLimit, q.ColumnOffset)
	}
	columnAt := map[int]int{}
	for j, i := range columns {
		columnAt[i] = j
		m.Columns = append(m.Columns, l.columns[i])
		m.ColumnLinks = append(m.ColumnLinks, columnLinks[i])
	}
	cells := map[[2]int][]MatrixLink{}
	for _, x := range l.links {
		r, onPage := rowAt[x.row]
		c, shown := columnAt[x.column]
		if onPage && shown {
			cells[[2]int{r, c}] = append(cells[[2]int{r, c}], x.link)
		}
	}
	for at, links := range cells {
		m.Cells = append(m.Cells, MatrixCell{Row: at[0], Column: at[1], Links: links})
	}
	slices.SortFunc(m.Cells, func(a, b MatrixCell) int { return cmp.Or(cmp.Compare(a.Row, b.Row), cmp.Compare(a.Column, b.Column)) })
	return nil
}
