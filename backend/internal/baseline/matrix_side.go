package baseline

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// One side of a baseline's matrix (#153): a baseline's requirements and the links it recorded, or the same scope's
// now, in one shape so the two compare link by link.

// sideLink is one link of a side as seen from its row: a trace link once from each end, an issue or page link once.
type sideLink struct {
	id     uuid.UUID
	row    uuid.UUID
	column trace.MatrixColumn
	// order places the column: a requirement's outline, nil for an issue or a page.
	order []int32
	link  MatrixLink
}

// key matches a link across two sides: the same link seen from the same row in the same direction.
func (l sideLink) key() string { return l.id.String() + "/" + l.row.String() + "/" + l.link.Direction }

type sideRow struct {
	ref  requirement.Ref
	path []int32
}

type matrixSide struct {
	rows  []sideRow
	links []sideLink
}

// fromEnds turns a trace link into what each of its ends' rows sees, kept as q's link types and direction ask; a type
// that reads the same both ways counts either way.
func fromEnds(rows map[uuid.UUID]sideRow, id, source, target uuid.UUID, name, key string, symmetric bool, suspect *bool, q MatrixQuery) []sideLink {
	if len(q.LinkTypes) > 0 && !slices.Contains(q.LinkTypes, key) {
		return nil
	}
	var out []sideLink
	for _, end := range []struct {
		row, other uuid.UUID
		direction  trace.MatrixDirection
	}{{source, target, trace.DirectionOutgoing}, {target, source, trace.DirectionIncoming}} {
		other, ok := rows[end.other]
		if _, here := rows[end.row]; !ok || !here || (q.Direction != trace.DirectionBoth && q.Direction != end.direction && !symmetric) {
			continue
		}
		ref := other.ref
		out = append(out, sideLink{id: id, row: end.row, column: trace.MatrixColumn{Requirement: &ref}, order: other.path,
			link: MatrixLink{Kind: trace.KindTrace, Name: name, TypeKey: key, Direction: string(end.direction), Suspect: suspect}})
	}
	return out
}

func rowsByID(rows []sideRow) map[uuid.UUID]sideRow {
	at := make(map[uuid.UUID]sideRow, len(rows))
	for _, r := range rows {
		at[r.ref.ID] = r
	}
	return at
}

func scanRows(ctx context.Context, tx db.DBTX, projectKey, query string, args ...any) ([]sideRow, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read the matrix's rows: %w", err)
	}
	defer rows.Close()
	var out []sideRow
	for rows.Next() {
		var (
			r        sideRow
			number   int
			statusID *uuid.UUID
			status   requirement.StatusRef
		)
		if err := rows.Scan(&r.ref.ID, &number, &r.ref.Revision, &r.ref.Title, &r.path, &statusID, &status.Name, &status.Category); err != nil {
			return nil, err
		}
		r.ref.ProjectKey, r.ref.Kind, r.ref.Key = projectKey, requirement.KindRequirement, requirement.Key(projectKey, number)
		if statusID != nil {
			status.ID = *statusID
			r.ref.Status = &status
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// baselineMatrixSide reads what a baseline recorded, each requirement titled as its revision was.
func baselineMatrixSide(ctx context.Context, tx db.DBTX, f *found, q MatrixQuery) (*matrixSide, error) {
	rows, err := scanRows(ctx, tx, f.ProjectKey, `
		SELECT i.requirement_id, i.number, i.revision, COALESCE(v.title, ''), i.path, i.status_id, COALESCE(i.status_name, ''),
		       COALESCE(i.status_category, '')
		FROM baseline_item i
		LEFT JOIN requirement_revision v ON v.requirement_id = i.requirement_id AND v.number = i.revision
		WHERE i.baseline_id = $1 AND i.kind = 'requirement' ORDER BY i.path`, f.ID)
	if err != nil {
		return nil, err
	}
	side := &matrixSide{rows: rows}
	switch q.Columns {
	case trace.ColumnsIssues:
		return side, readSideIssues(ctx, tx, side, `
			SELECT link_id, requirement_id, issue_key, relation, status_category, suspect FROM baseline_external_link
			WHERE baseline_id = $1 ORDER BY relation, link_id`, f.ID)
	case trace.ColumnsPages:
		return side, readSidePages(ctx, tx, side, `
			SELECT link_id, requirement_id, page_id, space_key, url, relation, suspect FROM baseline_page_link
			WHERE baseline_id = $1 ORDER BY space_key, page_id, link_id`, f.ID)
	}
	return side, readSideLinks(ctx, tx, side, q, `
		SELECT b.link_id, b.source_id, b.target_id, b.type_name, COALESCE(b.type_key, t.key, ''), COALESCE(t.forward = t.backward, false), b.suspect
		FROM baseline_link b LEFT JOIN link_type t ON t.id = b.type_id
		WHERE b.baseline_id = $1 ORDER BY b.type_name, b.link_id`, f.ID)
}

// nowMatrixSide reads what a baseline of the same scope would record now, as the reader sees it.
func nowMatrixSide(ctx context.Context, tx db.DBTX, f *found, q MatrixQuery) (*matrixSide, error) {
	var root *uuid.UUID
	if f.Root != nil {
		root = &f.Root.ID
	}
	rows, err := scanRows(ctx, tx, f.ProjectKey, requirement.WalkTree+`, scope AS (
			SELECT id FROM requirement WHERE id = $2::uuid AND trashed_at IS NULL
			UNION ALL
			SELECT c.id FROM requirement c JOIN scope s ON c.parent_id = s.id WHERE c.trashed_at IS NULL
		)
		SELECT r.id, r.number, r.revision, r.title, w.path, r.status_id, COALESCE(st.name, ''), COALESCE(st.category, '')
		FROM walk w JOIN requirement r ON r.id = w.id LEFT JOIN req_status st ON st.id = r.status_id
		WHERE r.kind = 'requirement' AND ($2::uuid IS NULL OR w.id IN (SELECT id FROM scope))
		ORDER BY w.path`, f.projectID, root)
	if err != nil {
		return nil, err
	}
	side := &matrixSide{rows: rows}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ref.ID
	}
	switch q.Columns {
	case trace.ColumnsIssues:
		return side, readSideIssues(ctx, tx, side, `
			SELECT a.id, a.requirement_id, a.issue_key, a.relation, a.status_category, r.revision > a.reviewed_rev
			FROM armature_issue_link a JOIN requirement r ON r.id = a.requirement_id
			WHERE a.requirement_id = ANY ($1) AND a.broken_at IS NULL AND a.detached_at IS NULL ORDER BY a.relation, a.id`, ids)
	case trace.ColumnsPages:
		return side, readSidePages(ctx, tx, side, `
			SELECT l.id, l.requirement_id, l.page_id, l.space_key, l.url, l.relation, `+trace.StatorSuspectSQL+`
			FROM stator_page_link l JOIN requirement r ON r.id = l.requirement_id
			WHERE l.requirement_id = ANY ($1) AND l.broken_at IS NULL AND l.detached_at IS NULL ORDER BY l.space_key, l.page_id, l.id`, ids)
	}
	return side, readSideLinks(ctx, tx, side, q, `
		SELECT v.id, v.source_id, v.target_id, t.name, t.key, t.forward = t.backward, v.suspect
		FROM trace_link_state v JOIN link_type t ON t.id = v.type_id
		WHERE v.source_id = ANY ($1) AND v.target_id = ANY ($1) ORDER BY t.name, v.id`, ids)
}

func readSideLinks(ctx context.Context, tx db.DBTX, side *matrixSide, q MatrixQuery, query string, arg any) error {
	rows, err := tx.Query(ctx, query, arg)
	if err != nil {
		return fmt.Errorf("read the matrix's links: %w", err)
	}
	defer rows.Close()
	at := rowsByID(side.rows)
	for rows.Next() {
		var (
			id, source, target uuid.UUID
			name, key          string
			symmetric          bool
			suspect            *bool
		)
		if err := rows.Scan(&id, &source, &target, &name, &key, &symmetric, &suspect); err != nil {
			return err
		}
		side.links = append(side.links, fromEnds(at, id, source, target, name, key, symmetric, suspect, q)...)
	}
	return rows.Err()
}

func readSideIssues(ctx context.Context, tx db.DBTX, side *matrixSide, query string, arg any) error {
	rows, err := tx.Query(ctx, query, arg)
	if err != nil {
		return fmt.Errorf("read the matrix's Armature links: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			l   = sideLink{link: MatrixLink{Kind: trace.KindArmature, Direction: string(trace.Outgoing)}}
			key string
		)
		if err := rows.Scan(&l.id, &l.row, &key, &l.link.Name, &l.link.Status, &l.link.Suspect); err != nil {
			return err
		}
		l.column = trace.MatrixColumn{IssueKey: key}
		side.links = append(side.links, l)
	}
	return rows.Err()
}

func readSidePages(ctx context.Context, tx db.DBTX, side *matrixSide, query string, arg any) error {
	rows, err := tx.Query(ctx, query, arg)
	if err != nil {
		return fmt.Errorf("read the matrix's Stator links: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			l       = sideLink{link: MatrixLink{Kind: trace.KindStator, Direction: string(trace.Outgoing)}}
			page    trace.MatrixPage
			suspect bool
		)
		if err := rows.Scan(&l.id, &l.row, &page.ID, &page.SpaceKey, &page.URL, &l.link.Name, &suspect); err != nil {
			return err
		}
		l.link.Suspect = &suspect
		l.column = trace.MatrixColumn{Page: &page}
		side.links = append(side.links, l)
	}
	return rows.Err()
}
