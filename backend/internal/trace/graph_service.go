package trace

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/codetrace"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// Graph answers everything the requirement key names traces to, as the reader sees it, walked as q asks.
func (s *Service) Graph(ctx context.Context, reader uuid.UUID, key string, q GraphQuery) (*Graph, error) {
	q, err := checkGraphQuery(q)
	if err != nil {
		return nil, err
	}
	var out *Graph
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := requirement.FindEndpoint(ctx, tx, reader, key)
		if err != nil {
			return err
		}
		rd := &dbGraph{tx: tx, reader: reader, rootProject: e.ProjectID, branch: q.Branch}
		if rd.types, err = graphTypes(ctx, tx, e.ProjectID, q.TypeKeys); err != nil {
			return err
		}
		if len(q.Relations) > 0 {
			rd.relations = q.Relations
		}
		root, err := rd.requirement(ctx, e.ID)
		if err != nil {
			return err
		}
		b, err := walk(ctx, rd, root.other, e.ID, q)
		if err != nil {
			return err
		}
		b.project[e.ID], b.typeOf[e.ID] = e.ProjectID, root.typeID
		if err := rd.gaps(ctx, b); err != nil {
			return err
		}
		if err := rd.hidden(ctx, b, q.Direction); err != nil {
			return err
		}
		b.g.sorted()
		out = b.g
		return nil
	})
	return out, err
}

// graphTypes are the ids of the link types keys names among those the project may use, nil for every type.
func graphTypes(ctx context.Context, tx db.DBTX, projectID uuid.UUID, keys []string) ([]uuid.UUID, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	types, err := usable(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(keys))
	for _, k := range keys {
		i := slices.IndexFunc(types, func(t LinkType) bool { return t.Key == k })
		if i < 0 {
			return nil, &QueryError{"type", "There is no link type " + strconv.Quote(k) + " in this project. Name its link types by key, such as derives."}
		}
		ids = append(ids, types[i].ID)
	}
	return ids, nil
}

// dbGraph reads the walk's steps as the reader sees them.
type dbGraph struct {
	tx          db.DBTX
	reader      uuid.UUID
	rootProject uuid.UUID
	branch      string
	types       []uuid.UUID
	relations   []string
}

// reaches says the walk reaches a relation: every one when none was named.
func (d *dbGraph) reaches(rel string) bool {
	return d.relations == nil || slices.Contains(d.relations, rel)
}

// requirementColumns read a requirement c as a node, with its project p, status st and type t.
const requirementColumns = `c.id, c.number, p.key, c.kind, c.title, c.revision, c.trashed_at IS NOT NULL, c.project_id,
	c.status_id, COALESCE(st.name, ''), COALESCE(st.category, ''), c.type_id, COALESCE(t.name, ''), COALESCE(t.icon, ''),
	COALESCE(t.colour, '')`

const requirementJoins = `JOIN project p ON p.id = c.project_id
	LEFT JOIN req_status st ON st.id = c.status_id
	LEFT JOIN requirement_type t ON t.id = c.type_id`

// scanRequirement reads requirementColumns, after whatever dest asks first.
func scanRequirement(row interface{ Scan(...any) error }, dest ...any) (graphLink, error) {
	var (
		l        graphLink
		ref      requirement.Ref
		number   int
		kind     string
		statusID *uuid.UUID
		status   requirement.StatusRef
		typ      requirement.TypeRef
	)
	dest = append(dest, &ref.ID, &number, &ref.ProjectKey, &kind, &ref.Title, &ref.Revision, &ref.Trashed, &l.project,
		&statusID, &status.Name, &status.Category, &l.typeID, &typ.Name, &typ.Icon, &typ.Colour)
	if err := row.Scan(dest...); err != nil {
		return l, err
	}
	ref.Key, ref.Kind = requirement.Key(ref.ProjectKey, number), requirement.Kind(kind)
	if statusID != nil {
		status.ID = *statusID
		ref.Status = &status
	}
	l.other = GraphNode{ID: ref.ID.String(), Kind: NodeRequirement, Requirement: &ref, Broken: ref.Trashed}
	if l.typeID != nil {
		typ.ID = *l.typeID
		l.other.Type = &typ
	}
	return l, nil
}

// requirement reads the centre of the walk.
func (d *dbGraph) requirement(ctx context.Context, id uuid.UUID) (graphLink, error) {
	row := d.tx.QueryRow(ctx, `SELECT `+requirementColumns+` FROM requirement c `+requirementJoins+` WHERE c.id = $1`, id)
	l, err := scanRequirement(row)
	if err != nil {
		return l, fmt.Errorf("read the centre of the graph: %w", err)
	}
	return l, nil
}

func (d *dbGraph) links(ctx context.Context, from []uuid.UUID, direction Direction) ([]graphLink, error) {
	rows, err := d.tx.Query(ctx, `
		SELECT up.id, v.id, v.source_id, v.target_id, lt.key, lt.forward, v.suspect, `+requirementColumns+`
		FROM trace_link_state v
		JOIN link_type lt ON lt.id = v.type_id
		CROSS JOIN LATERAL (VALUES (v.source_id, v.target_id, 'outgoing'), (v.target_id, v.source_id, 'incoming')) AS up (id, other_id, direction)
		JOIN requirement c ON c.id = up.other_id
		`+requirementJoins+`
		WHERE up.id = ANY ($1) AND (up.direction = $2 OR lt.forward = lt.backward)
		  AND ($3::uuid[] IS NULL OR v.type_id = ANY ($3))
		  AND perm_requirement_viewable(c.id, $4)`, from, string(direction), d.types, d.reader)
	if err != nil {
		return nil, fmt.Errorf("follow the graph's links: %w", err)
	}
	defer rows.Close()
	var out []graphLink
	for rows.Next() {
		var (
			from, link, source, target uuid.UUID
			e                          GraphEdge
		)
		l, err := scanRequirement(rows, &from, &link, &source, &target, &e.TypeKey, &e.Phrase, &e.Suspect)
		if err != nil {
			return nil, err
		}
		e.LinkID, e.From, e.To, e.Broken = &link, source.String(), target.String(), l.other.Broken
		l.from, l.edge = from, e
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b graphLink) int {
		return cmp.Or(cmp.Compare(a.from.String(), b.from.String()), cmp.Compare(a.other.Requirement.Key, b.other.Requirement.Key),
			cmp.Compare(a.edge.LinkID.String(), b.edge.LinkID.String()))
	})
	return out, nil
}

// branchSQL is the branch a project's evidence is read on: the one asked for the centre's project, its counting
// branch otherwise.
const branchSQL = `COALESCE(NULLIF(CASE WHEN %s = $2 THEN $3 END, ''), evidence_counting_branch(%[1]s))`

func (d *dbGraph) leaves(ctx context.Context, from []uuid.UUID) ([]graphLeaf, error) {
	var out []graphLeaf
	for _, read := range []func(context.Context, []uuid.UUID) ([]graphLeaf, error){d.issues, d.pages, d.tests, d.code} {
		found, err := read(ctx, from)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	return out, nil
}

// leafRelations are the relations of one kind the walk reaches, nil for none of them.
func (d *dbGraph) leafRelations(of []string) []string {
	var out []string
	for _, rel := range of {
		if d.reaches(rel) {
			out = append(out, rel)
		}
	}
	return out
}

func (d *dbGraph) issues(ctx context.Context, from []uuid.UUID) ([]graphLeaf, error) {
	relations := d.leafRelations(IssueRelations)
	if relations == nil {
		return nil, nil
	}
	rows, err := d.tx.Query(ctx, `
		SELECT l.requirement_id, l.issue_key, l.relation, l.broken_at IS NOT NULL, l.detached_at IS NOT NULL,
		       l.broken_at IS NULL AND l.detached_at IS NULL AND r.revision > l.reviewed_rev
		FROM armature_issue_link l JOIN requirement r ON r.id = l.requirement_id
		WHERE l.requirement_id = ANY ($1) AND l.relation = ANY ($2)
		ORDER BY l.requirement_id, l.issue_key, l.relation`, from, relations)
	if err != nil {
		return nil, fmt.Errorf("follow the graph's issues: %w", err)
	}
	defer rows.Close()
	var out []graphLeaf
	for rows.Next() {
		var (
			l                graphLeaf
			broken, detached bool
		)
		if err := rows.Scan(&l.from, &l.node.IssueKey, &l.edge.Relation, &broken, &detached, &l.edge.Suspect); err != nil {
			return nil, err
		}
		l.node.ID, l.node.Kind, l.node.Broken = "issue:"+l.node.IssueKey, NodeIssue, broken
		l.edge.From, l.edge.To, l.edge.Broken = l.from.String(), l.node.ID, broken || detached
		out = append(out, l)
	}
	return out, rows.Err()
}

func (d *dbGraph) pages(ctx context.Context, from []uuid.UUID) ([]graphLeaf, error) {
	relations := d.leafRelations(PageRelations)
	if relations == nil {
		return nil, nil
	}
	rows, err := d.tx.Query(ctx, `
		SELECT l.requirement_id, l.page_id, l.space_key, l.url, l.relation, l.broken_at IS NOT NULL, l.detached_at IS NOT NULL,
		       `+StatorSuspectSQL+`
		FROM stator_page_link l JOIN requirement r ON r.id = l.requirement_id
		WHERE l.requirement_id = ANY ($1) AND l.relation = ANY ($2)
		ORDER BY l.requirement_id, l.space_key, l.page_id, l.relation`, from, relations)
	if err != nil {
		return nil, fmt.Errorf("follow the graph's pages: %w", err)
	}
	defer rows.Close()
	var out []graphLeaf
	for rows.Next() {
		var (
			l                graphLeaf
			page             MatrixPage
			broken, detached bool
		)
		if err := rows.Scan(&l.from, &page.ID, &page.SpaceKey, &page.URL, &l.edge.Relation, &broken, &detached, &l.edge.Suspect); err != nil {
			return nil, err
		}
		l.node = GraphNode{ID: "page:" + page.ID.String(), Kind: NodePage, Page: &page, Broken: broken}
		l.edge.From, l.edge.To, l.edge.Broken = l.from.String(), l.node.ID, broken || detached
		out = append(out, l)
	}
	return out, rows.Err()
}

func (d *dbGraph) tests(ctx context.Context, from []uuid.UUID) ([]graphLeaf, error) {
	if d.leafRelations(TestRelations) == nil {
		return nil, nil
	}
	rows, err := d.tx.Query(ctx, `
		SELECT DISTINCT ON (tr.requirement_id, l.repository, tr.suite, tr.name)
		       tr.requirement_id, l.repository, tr.suite, tr.name, tr.outcome, tr.revision < r.revision
		FROM test_result tr
		JOIN test_run_latest l ON l.run_id = tr.run_id AND l.branch = `+fmt.Sprintf(branchSQL, "l.project_id")+`
		JOIN requirement r ON r.id = tr.requirement_id
		WHERE tr.requirement_id = ANY ($1)
		ORDER BY tr.requirement_id, l.repository, tr.suite, tr.name, l.job`, from, d.rootProject, d.branch)
	if err != nil {
		return nil, fmt.Errorf("follow the graph's tests: %w", err)
	}
	defer rows.Close()
	var out []graphLeaf
	for rows.Next() {
		var (
			l    graphLeaf
			test ChainTest
		)
		if err := rows.Scan(&l.from, &test.Repository, &test.Suite, &test.Name, &test.Outcome, &test.Outdated); err != nil {
			return nil, err
		}
		l.node = GraphNode{ID: "test:" + test.Repository + "/" + test.Suite + "/" + test.Name, Kind: NodeTest, Test: &test}
		l.edge = GraphEdge{From: l.from.String(), To: l.node.ID, Relation: TestRelations[0], Suspect: test.Outdated}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (d *dbGraph) code(ctx context.Context, from []uuid.UUID) ([]graphLeaf, error) {
	relations := d.leafRelations(CodeRelations)
	if relations == nil {
		return nil, nil
	}
	rows, err := d.tx.Query(ctx, `
		SELECT t.requirement_id, s.repository, t.path, t.line, code_tag_relation(s.project_id, t.word), t.revision < r.revision,
		       s.file_url, s.commit_sha
		FROM code_tag t
		JOIN code_scan s ON s.id = t.scan_id AND s.branch = `+fmt.Sprintf(branchSQL, "s.project_id")+`
		JOIN requirement r ON r.id = t.requirement_id
		WHERE t.requirement_id = ANY ($1) AND code_tag_relation(s.project_id, t.word) = ANY ($4)
		ORDER BY t.requirement_id, s.repository, t.path, t.line`, from, d.rootProject, d.branch, relations)
	if err != nil {
		return nil, fmt.Errorf("follow the graph's code: %w", err)
	}
	defer rows.Close()
	var out []graphLeaf
	for rows.Next() {
		var (
			l                graphLeaf
			code             ChainCode
			template, commit string
		)
		if err := rows.Scan(&l.from, &code.Repository, &code.Path, &code.Line, &l.edge.Relation, &code.Outdated, &template, &commit); err != nil {
			return nil, err
		}
		code.URL = codetrace.FileLink(template, commit, code.Path, code.Line)
		l.node = GraphNode{ID: "code:" + code.Repository + "/" + code.Path + ":" + strconv.Itoa(code.Line), Kind: NodeCode, Code: &code}
		l.edge.From, l.edge.To, l.edge.Suspect = l.from.String(), l.node.ID, code.Outdated
		out = append(out, l)
	}
	return out, rows.Err()
}

// gaps marks on each requirement the walk reached what its project's coverage rules find missing, the centre's
// project's evidence read on the branch asked for.
func (d *dbGraph) gaps(ctx context.Context, b *graphBuilder) error {
	byProject := map[uuid.UUID][]search.Kept{}
	var projects []uuid.UUID
	for _, n := range b.g.Nodes {
		if n.Requirement == nil || n.Requirement.Trashed || n.Requirement.Kind != requirement.KindRequirement {
			continue
		}
		p := b.project[n.Requirement.ID]
		if _, ok := byProject[p]; !ok {
			projects = append(projects, p)
		}
		byProject[p] = append(byProject[p], search.Kept{Ref: *n.Requirement, TypeID: b.typeOf[n.Requirement.ID]})
	}
	slices.SortFunc(projects, func(a, b uuid.UUID) int { return cmp.Compare(a.String(), b.String()) })
	for _, p := range projects {
		branch := ""
		if p == d.rootProject {
			branch = d.branch
		}
		if _, err := d.tx.Exec(ctx, `SELECT set_config('app.evidence_branch', $1, true)`, branch); err != nil {
			return err
		}
		rows, err := judge(ctx, d.tx, p, byProject[p])
		if err != nil {
			return err
		}
		for _, row := range rows {
			n := &b.g.Nodes[b.at[row.Requirement.ID.String()]]
			for dim, level := range map[Dimension]Level{Decomposed: row.Decomposed, Implemented: row.Implemented, Verified: row.Verified} {
				if level == LevelNone || level == LevelPartial {
					n.Gaps = append(n.Gaps, dim)
				}
			}
			slices.SortFunc(n.Gaps, func(a, b Dimension) int {
				return cmp.Compare(slices.Index(Dimensions, a), slices.Index(Dimensions, b))
			})
		}
	}
	_, err := d.tx.Exec(ctx, `SELECT set_config('app.evidence_branch', '', true)`)
	return err
}

// hidden counts, on each requirement the walk reached, its links the way the walk went from it to requirements the
// reader may not see.
func (d *dbGraph) hidden(ctx context.Context, b *graphBuilder, direction GraphDirection) error {
	var (
		ids        []uuid.UUID
		directions []string
	)
	for _, n := range b.g.Nodes {
		if n.Requirement == nil || n.Requirement.Trashed {
			continue
		}
		way := "both"
		switch {
		case n.Level < 0 || (n.Level == 0 && direction == GraphUp):
			way = string(Outgoing)
		case n.Level > 0 || (n.Level == 0 && direction == GraphDown):
			way = string(Incoming)
		}
		ids, directions = append(ids, n.Requirement.ID), append(directions, way)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := d.tx.Query(ctx, `SELECT id, hidden FROM trace_hidden_links($1, $2, $3)`, ids, directions, d.types)
	if err != nil {
		return fmt.Errorf("count what the graph does not show: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id     uuid.UUID
			hidden int
		)
		if err := rows.Scan(&id, &hidden); err != nil {
			return err
		}
		b.g.Nodes[b.at[id.String()]].Hidden = hidden
		b.g.Hidden += hidden
	}
	return rows.Err()
}
