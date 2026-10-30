package trace

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/codetrace"
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
	// Each row finds its links by index in either direction and each link its state and type by id: matched by
	// either end at once, every live link of the installation was read and checked for visibility (#477).
	found, err := tx.Query(ctx, `
		SELECT row_end.id, row_end.other_id, v.id, lt.name, lt.key, row_end.direction, v.suspect
		FROM (SELECT DISTINCT unnest($1::uuid[])) AS r(id)
		CROSS JOIN LATERAL (
			SELECT l.id AS link, l.source_id AS id, l.target_id AS other_id, 'outgoing' AS direction FROM trace_link l
			WHERE l.source_id = r.id AND l.deleted_at IS NULL
			UNION ALL
			SELECT l.id, l.target_id, l.source_id, 'incoming' FROM trace_link l
			WHERE l.target_id = r.id AND l.deleted_at IS NULL
		) row_end
		CROSS JOIN LATERAL (SELECT s.id, s.type_id, s.suspect FROM trace_link_state s WHERE s.id = row_end.link OFFSET 0) v
		CROSS JOIN LATERAL (SELECT t.name, t.key, t.forward, t.backward FROM link_type t WHERE t.id = v.type_id OFFSET 0) lt
		WHERE row_end.other_id = ANY ($2)
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
	slices.SortFunc(keys, CompareIssueKeys)
	for _, k := range keys {
		l.columns = append(l.columns, MatrixColumn{IssueKey: k})
	}
	for _, x := range all {
		x.column = slices.Index(keys, x.key)
		l.links = append(l.links, x.laidLink)
	}
	return l, nil
}

// testLinks reads the latest results on the counting branch of the tests that verify the rows; the tests are the
// columns, by repository, suite and name, one test's jobs sharing its column.
func testLinks(ctx context.Context, tx db.DBTX, rows []uuid.UUID, repository string) (*laid, error) {
	l := &laid{}
	if len(rows) == 0 {
		return l, nil
	}
	found, err := tx.Query(ctx, `
		SELECT tr.requirement_id, l.repository, tr.suite, tr.name, tr.outcome, tr.revision < r.revision
		FROM test_result tr
		JOIN test_run_latest l ON l.run_id = tr.run_id AND l.branch = evidence_branch(l.project_id)
		JOIN requirement r ON r.id = tr.requirement_id
		WHERE tr.requirement_id = ANY ($1) AND ($2 = '' OR l.repository = $2)
		ORDER BY l.repository, tr.suite, tr.name, l.job`, rows, repository)
	if err != nil {
		return nil, fmt.Errorf("read the matrix's test results: %w", err)
	}
	type testLink struct {
		test MatrixTest
		laidLink
	}
	all, err := pgx.CollectRows(found, func(row pgx.CollectableRow) (testLink, error) {
		x := testLink{laidLink: laidLink{link: MatrixLink{Kind: KindTest, Direction: Incoming}}}
		err := row.Scan(&x.row, &x.test.Repository, &x.test.Suite, &x.test.Name, &x.link.Name, &x.link.Outdated)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	at := map[MatrixTest]int{}
	for _, x := range all {
		column, ok := at[x.test]
		if !ok {
			column = len(l.columns)
			at[x.test] = column
			test := x.test
			l.columns = append(l.columns, MatrixColumn{Test: &test})
		}
		x.column = column
		l.links = append(l.links, x.laidLink)
	}
	return l, nil
}

// codeLinks reads the code tags on the counting branch that implement or verify the rows; the files are the columns,
// by repository and path, each cell a tag by its relation and line.
func codeLinks(ctx context.Context, tx db.DBTX, rows []uuid.UUID, repository string) (*laid, error) {
	l := &laid{}
	if len(rows) == 0 {
		return l, nil
	}
	found, err := tx.Query(ctx, `
		SELECT t.requirement_id, s.repository, t.path, s.file_url, s.commit_sha, t.line, code_tag_relation(s.project_id, t.word), t.revision < r.revision
		FROM code_tag t
		JOIN code_scan s ON s.id = t.scan_id AND s.branch = evidence_branch(s.project_id)
		JOIN requirement r ON r.id = t.requirement_id
		WHERE t.requirement_id = ANY ($1) AND ($2 = '' OR s.repository = $2) AND code_tag_relation(s.project_id, t.word) IS NOT NULL
		ORDER BY s.repository, t.path, t.line`, rows, repository)
	if err != nil {
		return nil, fmt.Errorf("read the matrix's code: %w", err)
	}
	type codeLink struct {
		file MatrixCode
		laidLink
	}
	all, err := pgx.CollectRows(found, func(row pgx.CollectableRow) (codeLink, error) {
		x := codeLink{laidLink: laidLink{link: MatrixLink{Kind: KindCode, Direction: Incoming}}}
		var (
			template, commit, relation string
			line                       int
		)
		err := row.Scan(&x.row, &x.file.Repository, &x.file.Path, &template, &commit, &line, &relation, &x.link.Outdated)
		x.file.URL = codetrace.FileLink(template, commit, x.file.Path, 1)
		x.link.Name = relation + ", line " + strconv.Itoa(line)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	at := map[[2]string]int{}
	for _, x := range all {
		key := [2]string{x.file.Repository, x.file.Path}
		column, ok := at[key]
		if !ok {
			column = len(l.columns)
			at[key] = column
			file := x.file
			l.columns = append(l.columns, MatrixColumn{Code: &file})
		}
		x.column = column
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
