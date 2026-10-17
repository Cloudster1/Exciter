package trace

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// ChainQuery is what a chain report shows: the first level's requirements the grid's filter keeps, in its order, a
// window of them, the levels at the positions given (every one when none is), and only broken lines when asked.
type ChainQuery struct {
	Filter search.Filter
	// Branch reads evidence on a branch other than the counting one, as a trace check asks (#200).
	Branch     string
	Sort       search.Sort
	Levels     []int
	OnlyBroken bool
	Limit      int
	Offset     int
	// MaxLines refuses a page with more lines; a file passes the export's bound and Truncate, and is cut there.
	MaxLines int
	Truncate bool
}

// TooManyLinesError refuses a page of roots whose paths fan out past the lines a page answers.
type TooManyLinesError struct{ Lines, Max int }

func (e *TooManyLinesError) Error() string {
	return fmt.Sprintf("These requirements fan out into %d chains, more than the %d a page shows. Ask for fewer at a time, or narrow the filter.",
		e.Lines, e.Max)
}

// shownLevels are the positions a report shows, next to each other, the first a level of requirements.
func shownLevels(levels []ModelLevel, asked []int) ([]int, error) {
	if len(asked) == 0 {
		all := make([]int, len(levels))
		for i := range all {
			all[i] = i
		}
		return all, nil
	}
	shown := slices.Compact(slices.Sorted(slices.Values(asked)))
	for i, at := range shown {
		switch {
		case at < 0 || at >= len(levels):
			return nil, &FieldError{Field: "level", Message: "There is no level " + strconv.Itoa(at) + ". Pick levels of the project's trace model."}
		case i > 0 && at != shown[i-1]+1:
			return nil, &FieldError{Field: "level", Message: "Show levels that follow one another, so each line reads from one to the next."}
		}
	}
	if levels[shown[0]].Kind != LevelRequirements {
		return nil, &FieldError{Field: "level", Message: "Chains start from requirements. Show a level of requirements first."}
	}
	return shown, nil
}

// Chains answers a page of the trace model's first shown level and every path from each of its requirements down
// the levels shown, as the reader sees the links; a path that ends early is a broken line.
func (s *Service) Chains(ctx context.Context, reader uuid.UUID, projectKey string, q ChainQuery) (*Chains, error) {
	if q.MaxLines == 0 {
		q.MaxLines = MaxChainLines
	}
	out := &Chains{Levels: []ModelLevel{}, Roots: []requirement.Ref{}, Lines: []ChainLine{}, Limit: q.Limit, Offset: q.Offset}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		if err := evidenceOn(ctx, tx, q.Branch); err != nil {
			return err
		}
		model, err := readModel(ctx, tx, p.ID)
		if err != nil || len(model.Levels) == 0 {
			return err
		}
		shown, err := shownLevels(model.Levels, q.Levels)
		if err != nil {
			return err
		}
		for _, at := range shown {
			out.Levels = append(out.Levels, model.Levels[at])
		}
		kept, err := search.Keep(ctx, tx, reader, p, q.Filter, q.Sort)
		if err != nil {
			return err
		}
		first := out.Levels[0]
		roots := slices.DeleteFunc(kept, func(k search.Kept) bool { return k.TypeID == nil || !slices.Contains(first.TypeIDs, *k.TypeID) })
		out.Total = len(roots)
		page := window(roots, q.Limit, q.Offset)
		frontier := make([]uuid.UUID, len(page))
		for i, k := range page {
			out.Roots = append(out.Roots, k.Ref)
			frontier[i] = k.Ref.ID
		}
		below := make([]map[uuid.UUID][]ChainNode, len(out.Levels))
		for i := 1; i < len(out.Levels) && len(frontier) > 0; i++ {
			if below[i], err = step(ctx, tx, reader, p, frontier, out.Levels[i]); err != nil {
				return err
			}
			frontier = reached(below[i])
		}
		return out.lay(below, q)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// reached are the requirements one step found, the next step's frontier.
func reached(found map[uuid.UUID][]ChainNode) []uuid.UUID {
	var ids []uuid.UUID
	for _, nodes := range found {
		for _, n := range nodes {
			if n.Requirement != nil && !slices.Contains(ids, n.Requirement.ID) {
				ids = append(ids, n.Requirement.ID)
			}
		}
	}
	return ids
}

// lay follows every root down the steps found, one line per path, and keeps the broken ones when asked.
func (c *Chains) lay(below []map[uuid.UUID][]ChainNode, q ChainQuery) error {
	var lines []ChainLine
	var walk func(root int, path []ChainNode, level int) bool
	walk = func(root int, path []ChainNode, level int) bool {
		last := path[len(path)-1]
		var next []ChainNode
		if level+1 < len(below) && last.Requirement != nil {
			next = below[level+1][last.Requirement.ID]
		}
		if len(next) == 0 {
			if len(lines) >= q.MaxLines {
				return false
			}
			// A chain that ends in a test failing, skipped or outdated breaks there: the test does not hold.
			broken := len(path) < len(below) || (last.Test != nil && !last.Test.holds())
			lines = append(lines, ChainLine{Root: root, Nodes: slices.Clone(path), Broken: broken})
			return true
		}
		for _, n := range next {
			if !walk(root, append(path, n), level+1) {
				return false
			}
		}
		return true
	}
	for i, root := range c.Roots {
		ref := root
		if !walk(i, []ChainNode{{Requirement: &ref}}, 0) {
			if !q.Truncate {
				return &TooManyLinesError{Lines: q.MaxLines + 1, Max: q.MaxLines}
			}
			c.Truncated = true
			break
		}
	}
	for _, l := range lines {
		if l.Broken {
			c.Broken++
		}
		if !q.OnlyBroken || l.Broken {
			c.Lines = append(c.Lines, l)
		}
	}
	return nil
}

// step reads what the requirements of the frontier reach on the next level, as the reader sees it, by the
// requirement each came from, in a stable order.
func step(ctx context.Context, tx db.DBTX, reader uuid.UUID, p *project.Project, frontier []uuid.UUID, level ModelLevel) (map[uuid.UUID][]ChainNode, error) {
	found := map[uuid.UUID][]ChainNode{}
	add := func(from uuid.UUID, n ChainNode) { found[from] = append(found[from], n) }
	switch level.Kind {
	case LevelRequirements:
		types, directions := make([]uuid.UUID, 0, len(level.Steps)), make([]string, 0, len(level.Steps))
		for _, st := range level.Steps {
			types, directions = append(types, *st.TypeID), append(directions, string(st.Direction))
		}
		rows, err := tx.Query(ctx, `
			SELECT DISTINCT ON (up.id, c.id, v.id) up.id, c.id, c.number, c.title, c.revision,
			       c.status_id, COALESCE(st.name, ''), COALESCE(st.category, ''),
			       CASE WHEN up.direction = 'outgoing' THEN lt.forward ELSE lt.backward END, v.suspect
			FROM trace_link_state v
			JOIN link_type lt ON lt.id = v.type_id
			CROSS JOIN LATERAL (VALUES (v.source_id, v.target_id, 'outgoing'), (v.target_id, v.source_id, 'incoming')) AS up (id, other_id, direction)
			JOIN unnest($2::uuid[], $3::text[]) AS way (type_id, direction) ON way.type_id = v.type_id AND (way.direction = up.direction OR lt.forward = lt.backward)
			JOIN requirement c ON c.id = up.other_id
			LEFT JOIN req_status st ON st.id = c.status_id
			WHERE up.id = ANY ($1) AND c.type_id = ANY ($4) AND c.project_id = $5 AND c.kind = 'requirement' AND c.trashed_at IS NULL
			  AND perm_requirement_viewable(c.id, $6)
			ORDER BY up.id, c.id, v.id`, frontier, types, directions, level.TypeIDs, p.ID, reader)
		if err != nil {
			return nil, fmt.Errorf("follow the trace level %s: %w", level.Name, err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				from     uuid.UUID
				statusID *uuid.UUID
				ref      requirement.Ref
				number   int
				status   requirement.StatusRef
				n        ChainNode
			)
			if err := rows.Scan(&from, &ref.ID, &number, &ref.Title, &ref.Revision, &statusID, &status.Name, &status.Category,
				&n.Via, &n.Suspect); err != nil {
				return nil, err
			}
			ref.ProjectKey, ref.Kind, ref.Key = p.Key, requirement.KindRequirement, requirement.Key(p.Key, number)
			if statusID != nil {
				status.ID = *statusID
				ref.Status = &status
			}
			n.Requirement = &ref
			add(from, n)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for from := range found {
			slices.SortFunc(found[from], func(a, b ChainNode) int { return cmp.Compare(a.Requirement.Key, b.Requirement.Key) })
		}
		return found, nil
	case LevelIssues:
		rows, err := tx.Query(ctx, `
			SELECT l.requirement_id, l.issue_key, l.relation, r.revision > l.reviewed_rev
			FROM armature_issue_link l JOIN requirement r ON r.id = l.requirement_id
			WHERE l.requirement_id = ANY ($1) AND l.relation = ANY ($2) AND l.broken_at IS NULL AND l.detached_at IS NULL
			ORDER BY l.id`, frontier, relationsOf(level))
		if err != nil {
			return nil, fmt.Errorf("follow the trace level %s: %w", level.Name, err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				from uuid.UUID
				n    ChainNode
			)
			if err := rows.Scan(&from, &n.IssueKey, &n.Via, &n.Suspect); err != nil {
				return nil, err
			}
			add(from, n)
		}
		for from := range found {
			slices.SortFunc(found[from], func(a, b ChainNode) int { return CompareIssueKeys(a.IssueKey, b.IssueKey) })
		}
		return found, rows.Err()
	case LevelTests:
		rows, err := tx.Query(ctx, `
			SELECT tr.requirement_id, l.repository, tr.suite, tr.name, tr.outcome, tr.revision < r.revision
			FROM test_result tr
			JOIN test_run_latest l ON l.run_id = tr.run_id AND l.branch = evidence_branch(l.project_id)
			JOIN requirement r ON r.id = tr.requirement_id
			WHERE tr.requirement_id = ANY ($1)
			ORDER BY l.repository, tr.suite, tr.name, l.job`, frontier)
		if err != nil {
			return nil, fmt.Errorf("follow the trace level %s: %w", level.Name, err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				from uuid.UUID
				test ChainTest
			)
			if err := rows.Scan(&from, &test.Repository, &test.Suite, &test.Name, &test.Outcome, &test.Outdated); err != nil {
				return nil, err
			}
			add(from, ChainNode{Test: &test, Via: TestRelations[0], Suspect: test.Outdated})
		}
		return found, rows.Err()
	default:
		rows, err := tx.Query(ctx, `
			SELECT l.requirement_id, l.page_id, l.space_key, l.url, l.relation, `+StatorSuspectSQL+`
			FROM stator_page_link l JOIN requirement r ON r.id = l.requirement_id
			WHERE l.requirement_id = ANY ($1) AND l.relation = ANY ($2) AND l.broken_at IS NULL AND l.detached_at IS NULL
			ORDER BY l.space_key, l.page_id, l.id`, frontier, relationsOf(level))
		if err != nil {
			return nil, fmt.Errorf("follow the trace level %s: %w", level.Name, err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				from uuid.UUID
				page MatrixPage
				n    ChainNode
			)
			if err := rows.Scan(&from, &page.ID, &page.SpaceKey, &page.URL, &n.Via, &n.Suspect); err != nil {
				return nil, err
			}
			n.Page = &page
			add(from, n)
		}
		return found, rows.Err()
	}
}

func relationsOf(level ModelLevel) []string {
	out := make([]string, 0, len(level.Steps))
	for _, st := range level.Steps {
		out = append(out, st.Relation)
	}
	return out
}
