package trace

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

var (
	// ErrCoverageRuleNotFound answers a rule that is not there, or of a project the caller may not view.
	ErrCoverageRuleNotFound = errors.New("coverage rule not found")
	// ErrNotCoverageKeeper refuses changing a project's coverage rules to anybody who does not administer it.
	ErrNotCoverageKeeper = errors.New("only the project's administrators set its coverage rules")
)

// WithCoverageCache answers coverage from cache for a minute after it was read, and clears it when a rule changes.
func (s *Service) WithCoverageCache(c *CoverageCache) *Service {
	s.coverage = c
	return s
}

const selectCoverageRules = `
SELECT c.id, c.type_id, COALESCE(t.name, ''), c.decomposed, c.implemented, c.verified
FROM coverage_rule c LEFT JOIN requirement_type t ON t.id = c.type_id`

func readCoverageRules(ctx context.Context, tx db.DBTX, where string, args ...any) ([]CoverageRule, error) {
	rows, err := tx.Query(ctx, selectCoverageRules+` WHERE `+where+` ORDER BY c.type_id IS NOT NULL, lower(COALESCE(t.name, '')), c.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("read the coverage rules: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CoverageRule])
}

// CoverageRules are what a project's coverage asks of each type, the rule for every other type first.
func (s *Service) CoverageRules(ctx context.Context, reader uuid.UUID, projectKey string) ([]CoverageRule, error) {
	out := []CoverageRule{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		found, err := readCoverageRules(ctx, tx, `c.project_id = $1`, p.ID)
		if len(found) > 0 {
			out = found
		}
		return err
	})
	return out, err
}

// SaveCoverageRule sets the rule of one type, or the project's rule for every other type.
func (s *Service) SaveCoverageRule(ctx context.Context, actor uuid.UUID, projectKey string, in CoverageRuleInput) (*CoverageRule, db.LSN, error) {
	var (
		out       *CoverageRule
		projectID uuid.UUID
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if !p.Can.Administer {
			return ErrNotCoverageKeeper
		}
		if p.ArchivedAt != nil {
			return project.ErrArchived
		}
		projectID = p.ID
		if in.TypeID != nil {
			var found bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM requirement_type WHERE id = $1 AND project_id = $2)`,
				*in.TypeID, p.ID).Scan(&found); err != nil {
				return err
			}
			if !found {
				return &FieldError{Field: "typeId", Message: "That type is not one of this project's. Pick one from its types, or leave it out for every other type."}
			}
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO coverage_rule (project_id, type_id, decomposed, implemented, verified, updated_by) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (project_id, type_id) DO UPDATE
			SET decomposed = EXCLUDED.decomposed, implemented = EXCLUDED.implemented, verified = EXCLUDED.verified,
			    updated_by = EXCLUDED.updated_by, updated_at = now()
			RETURNING id`, p.ID, in.TypeID, in.Decomposed, in.Implemented, in.Verified, actor).Scan(&id); err != nil {
			return fmt.Errorf("save the coverage rule: %w", err)
		}
		rules, err := readCoverageRules(ctx, tx, `c.id = $1`, id)
		if err != nil {
			return err
		}
		out = &rules[0]
		return audit.Write(ctx, tx, audit.Entry{
			Action: audit.ActionCoverageRuleSaved, TargetType: "coverage_rule", TargetID: &id, Actor: actor,
			Data: map[string]any{"projectKey": p.Key, "type": out.TypeName, "decomposed": out.Decomposed,
				"implemented": out.Implemented, "verified": out.Verified},
		})
	})
	if err == nil {
		s.coverage.Forget(ctx, projectID)
	}
	return out, lsn, err
}

// DeleteCoverageRule removes a coverage rule; its type then follows the rule for every other type, if any.
func (s *Service) DeleteCoverageRule(ctx context.Context, actor, ruleID uuid.UUID) (db.LSN, error) {
	var projectID uuid.UUID
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var typeName string
		err := tx.QueryRow(ctx, `
			SELECT c.project_id, COALESCE(t.name, '') FROM coverage_rule c LEFT JOIN requirement_type t ON t.id = c.type_id
			WHERE c.id = $1`, ruleID).Scan(&projectID, &typeName)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCoverageRuleNotFound
		}
		if err != nil {
			return err
		}
		var administer, archived bool
		if err := tx.QueryRow(ctx, `SELECT perm_project_holds(id, $2, 'administer'), archived_at IS NOT NULL FROM project WHERE id = $1`,
			projectID, actor).Scan(&administer, &archived); err != nil {
			return err
		}
		switch {
		case !administer:
			return ErrNotCoverageKeeper
		case archived:
			return project.ErrArchived
		}
		if _, err := tx.Exec(ctx, `DELETE FROM coverage_rule WHERE id = $1`, ruleID); err != nil {
			return fmt.Errorf("remove the coverage rule: %w", err)
		}
		return audit.Write(ctx, tx, audit.Entry{
			Action: audit.ActionCoverageRuleRemoved, TargetType: "coverage_rule", TargetID: &ruleID, Actor: actor,
			Data: map[string]any{"projectId": projectID, "type": typeName},
		})
	})
	if err == nil {
		s.coverage.Forget(ctx, projectID)
	}
	return lsn, err
}

// ParseLevelFilter reads the levels a coverage report keeps its rows at, one per dimension.
func ParseLevelFilter(v url.Values) (LevelFilter, error) {
	var f LevelFilter
	for _, d := range []struct {
		name Dimension
		into *Level
	}{{Decomposed, &f.Decomposed}, {Implemented, &f.Implemented}, {Verified, &f.Verified}} {
		raw := v.Get(string(d.name))
		if raw != "" && !slices.Contains(Levels, Level(raw)) {
			return f, &FieldError{Field: string(d.name), Message: "Keep the requirements at none, partial, full or na."}
		}
		*d.into = Level(raw)
	}
	return f, nil
}

// judgeLinks counts the links of each requirement kept, as the reader sees them, and judges it by its rule.
const judgeLinks = `
SELECT k.id,
       COALESCE(own.decomposed, every.decomposed, false), COALESCE(own.implemented, every.implemented, false),
       COALESCE(own.verified, every.verified, false),
       t.decomposing, t.decomposing_suspect, a.implementing, a.implementing_done, a.implementing_suspect,
       t.verifying, t.verifying_suspect, a.verifying, a.verifying_done, a.verifying_suspect
FROM unnest($2::uuid[], $3::uuid[]) AS k (id, type_id)
JOIN requirement r ON r.id = k.id
LEFT JOIN coverage_rule own ON own.project_id = $1 AND own.type_id = k.type_id
LEFT JOIN coverage_rule every ON every.project_id = $1 AND every.type_id IS NULL
CROSS JOIN LATERAL (
    SELECT count(*) FILTER (WHERE lt.counts = 'decomposition')::int AS decomposing,
           count(*) FILTER (WHERE lt.counts = 'decomposition' AND v.suspect)::int AS decomposing_suspect,
           count(*) FILTER (WHERE lt.counts = 'verification')::int AS verifying,
           count(*) FILTER (WHERE lt.counts = 'verification' AND v.suspect)::int AS verifying_suspect
    FROM trace_link_state v
    JOIN link_type lt ON lt.id = v.type_id
    JOIN requirement s ON s.id = v.source_id
    -- What waits in the trash covers nothing.
    WHERE v.target_id = k.id AND s.trashed_at IS NULL AND lt.counts <> 'nothing'
) t
CROSS JOIN LATERAL (
    SELECT count(*) FILTER (WHERE l.relation = 'implements')::int AS implementing,
           count(*) FILTER (WHERE l.relation = 'implements' AND l.done)::int AS implementing_done,
           count(*) FILTER (WHERE l.relation = 'implements' AND l.suspect)::int AS implementing_suspect,
           count(*) FILTER (WHERE l.relation = 'verifies')::int AS verifying,
           count(*) FILTER (WHERE l.relation = 'verifies' AND l.done)::int AS verifying_done,
           count(*) FILTER (WHERE l.relation = 'verifies' AND l.suspect)::int AS verifying_suspect
    -- A category Armature has not told since the issue moved is not taken for done.
    FROM (SELECT al.relation, al.status_category = 'done' AND NOT al.status_stale AS done, r.revision > al.reviewed_rev AS suspect
          FROM armature_issue_link al
          WHERE al.requirement_id = k.id AND al.broken_at IS NULL AND al.detached_at IS NULL) l
) a`

// judge answers the coverage row of each requirement kept, in their order.
func judge(ctx context.Context, tx db.DBTX, projectID uuid.UUID, kept []search.Kept) ([]CoverageRow, error) {
	ids := make([]uuid.UUID, len(kept))
	types := make([]*uuid.UUID, len(kept))
	for i, k := range kept {
		ids[i], types[i] = k.Ref.ID, k.TypeID
	}
	rows, err := tx.Query(ctx, judgeLinks, projectID, ids, types)
	if err != nil {
		return nil, fmt.Errorf("count the links that cover: %w", err)
	}
	judged := map[uuid.UUID]CoverageRow{}
	for rows.Next() {
		var (
			id   uuid.UUID
			asks Asks
			c    Counted
			row  CoverageRow
		)
		if err := rows.Scan(&id, &asks.Decomposed, &asks.Implemented, &asks.Verified,
			&c.Decomposing, &c.DecomposingSuspect, &c.Implementing, &c.ImplementingDone, &c.ImplementingSuspect,
			&c.VerifyingLinks, &c.VerifyingLinksSuspect, &c.VerifyingIssues, &c.VerifyingIssuesDone, &c.VerifyingIssuesSuspect); err != nil {
			rows.Close()
			return nil, err
		}
		row.Decomposed, row.Implemented, row.Verified = Judge(asks, c)
		judged[id] = row
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]CoverageRow, 0, len(kept))
	for _, k := range kept {
		row, ok := judged[k.Ref.ID]
		if !ok {
			continue
		}
		row.Requirement = k.Ref
		out = append(out, row)
	}
	return out, nil
}

// Coverage answers how far the requirements of a project f keeps are covered, in totals over all of them, and a
// window of those at the levels only names, in outline order, with how many there are.
func (s *Service) Coverage(ctx context.Context, reader uuid.UUID, projectKey string, f search.Filter, only LevelFilter, limit, offset int) (CoverageSummary, []CoverageRow, int, error) {
	var (
		all       []CoverageRow
		projectID uuid.UUID
		cached    bool
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		projectID = p.ID
		if all, cached = s.coverage.Get(ctx, p.ID, reader, f); cached {
			return nil
		}
		kept, err := search.Keep(ctx, tx, reader, p, f)
		if err != nil {
			return err
		}
		all, err = judge(ctx, tx, p.ID, kept)
		return err
	})
	if err != nil {
		return CoverageSummary{}, nil, 0, err
	}
	if !cached {
		s.coverage.Put(ctx, projectID, reader, f, all)
	}
	kept := []CoverageRow{}
	for _, r := range all {
		if only.Keeps(r) {
			kept = append(kept, r)
		}
	}
	return Summarize(all), window(kept, limit, offset), len(kept), nil
}

func window[T any](all []T, limit, offset int) []T {
	if offset >= len(all) {
		return []T{}
	}
	return all[offset:min(len(all), offset+limit)]
}

// TooWideError refuses a matrix with more columns than MatrixMaxColumns.
type TooWideError struct{ Columns int }

func (e *TooWideError) Error() string {
	return fmt.Sprintf("These requirements reach %d columns, more than the %d a matrix shows. Narrow the filter, or page fewer rows.",
		e.Columns, MatrixMaxColumns)
}

// Matrix answers a window of the requirements of a project f keeps, in outline order, against the requirements
// of columnProject, the same project when empty, or the Armature issues they link to.
func (s *Service) Matrix(ctx context.Context, reader uuid.UUID, projectKey string, f search.Filter, columns MatrixColumns, columnProject string, limit, offset int) (*Matrix, error) {
	if columns == "" {
		columns = ColumnsRequirements
	}
	if !slices.Contains(MatrixColumnKinds, columns) {
		return nil, &FieldError{Field: "columns", Message: "Show requirements or issues as the columns."}
	}
	out := &Matrix{Rows: []requirement.Ref{}, Columns: []MatrixColumn{}, Cells: []MatrixCell{}, Limit: limit, Offset: offset}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		other := p
		if columns == ColumnsRequirements && columnProject != "" && !strings.EqualFold(columnProject, p.Key) {
			if other, err = project.Find(ctx, tx, reader, columnProject); errors.Is(err, project.ErrNotFound) {
				return &FieldError{Field: "columnProject", Message: "There is no such project you can see. Pick the columns' project from the list."}
			} else if err != nil {
				return err
			}
		}
		kept, err := search.Keep(ctx, tx, reader, p, f)
		if err != nil {
			return err
		}
		out.Total = len(kept)
		page := window(kept, limit, offset)
		at := map[uuid.UUID]int{}
		for i, k := range page {
			out.Rows = append(out.Rows, k.Ref)
			at[k.Ref.ID] = i
		}
		if len(page) == 0 {
			return nil
		}
		if columns == ColumnsIssues {
			return issueColumns(ctx, tx, out, at)
		}
		return requirementColumns(ctx, tx, reader, other, out, at)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
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

// requirementColumns adds the requirements of the column project the rows link to, either way, in outline order.
func requirementColumns(ctx context.Context, tx db.DBTX, reader uuid.UUID, other *project.Project, m *Matrix, at map[uuid.UUID]int) error {
	ids := make([]uuid.UUID, 0, len(at))
	for id := range at {
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, requirement.WalkTree+`
		SELECT row_end.id, c.id, c.number, c.title, c.revision, c.status_id, COALESCE(st.name, ''), COALESCE(st.category, ''),
		       lt.name, v.suspect
		FROM trace_link_state v
		JOIN link_type lt ON lt.id = v.type_id
		CROSS JOIN LATERAL (VALUES (v.source_id, v.target_id), (v.target_id, v.source_id)) AS row_end (id, other_id)
		JOIN walk w ON w.id = row_end.other_id
		JOIN requirement c ON c.id = w.id
		LEFT JOIN req_status st ON st.id = c.status_id
		WHERE row_end.id = ANY ($2) AND perm_requirement_viewable(c.id, $3)
		ORDER BY w.path, lt.name, v.id`, other.ID, ids, reader)
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
			link     MatrixLink
		)
		if err := rows.Scan(&rowID, &ref.ID, &number, &ref.Title, &ref.Revision, &statusID, &status.Name, &status.Category,
			&link.Name, &link.Suspect); err != nil {
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

// issueColumns adds the Armature issues the rows are traced to, neither broken nor detached, by project and number.
func issueColumns(ctx context.Context, tx db.DBTX, m *Matrix, at map[uuid.UUID]int) error {
	ids := make([]uuid.UUID, 0, len(at))
	for id := range at {
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, `
		SELECT l.requirement_id, l.issue_key, l.relation, r.revision > l.reviewed_rev
		FROM armature_issue_link l JOIN requirement r ON r.id = l.requirement_id
		WHERE l.requirement_id = ANY ($1) AND l.broken_at IS NULL AND l.detached_at IS NULL
		ORDER BY l.relation, l.id`, ids)
	if err != nil {
		return fmt.Errorf("read the matrix's Armature links: %w", err)
	}
	type found struct {
		row  uuid.UUID
		key  string
		link MatrixLink
	}
	all, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (found, error) {
		var f found
		err := row.Scan(&f.row, &f.key, &f.link.Name, &f.link.Suspect)
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
