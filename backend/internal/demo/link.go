package demo

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/stator"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// dependsType is the trace link type "Depends on" becomes: issues depend on each other in more ways than one
// requirement derives from another.
const dependsType = "relates"

// Linked is what one linking added, and what it left for later.
type Linked struct {
	Links   int
	Skipped int
	Notes   []string
}

// made is a requirement the demo made, by what it was made from.
type made struct {
	id  uuid.UUID
	key string
}

type issueLink struct {
	requirement uuid.UUID
	issue       string
}

type pageLink struct {
	requirement uuid.UUID
	page        uuid.UUID
	relation    string
}

// existing is what is linked already, read first so linking again calls no partner for it.
type existing struct {
	traces map[[2]uuid.UUID]bool
	issues map[issueLink]bool
	pages  map[pageLink]bool
}

// Link adds what every project of the demo is missing, all at once; the worker takes a project a step.
func (s *Service) Link(ctx context.Context, actor uuid.UUID, p Plan, keys map[string]string) (Linked, error) {
	var out Linked
	for _, pr := range p.Projects {
		got, err := s.LinkProject(ctx, actor, p, keys, pr.Tool)
		out.add(got)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// add counts another step's links in, each note once.
func (l *Linked) add(other Linked) {
	l.Links += other.Links
	l.Skipped += other.Skipped
	for _, n := range other.Notes {
		if !slices.Contains(l.Notes, n) {
			l.Notes = append(l.Notes, n)
		}
	}
}

// LinkProject adds what one tool's project is missing: its dependencies, its issues in Armature and its pages in
// Stator. What a partner cannot take yet is counted as skipped, and linking again adds it.
func (s *Service) LinkProject(ctx context.Context, actor uuid.UUID, p Plan, keys map[string]string, tool string) (Linked, error) {
	var out Linked
	nodes, err := s.mapped(ctx, keys)
	if err != nil {
		return out, err
	}
	have, err := s.existing(ctx, nodes)
	if err != nil {
		return out, err
	}
	if err := s.linkDependencies(ctx, actor, p, tool, nodes, have, &out); err != nil {
		return out, err
	}
	s.linkIssues(ctx, p, tool, nodes, have, &out)
	s.linkPages(ctx, p, tool, nodes, have, &out)
	return out, nil
}

// mapped reads the requirements the demo made in the projects the keys name.
func (s *Service) mapped(ctx context.Context, keys map[string]string) (map[node]made, error) {
	var projectKeys []string
	for _, k := range keys {
		projectKeys = append(projectKeys, k)
	}
	out := map[node]made{}
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT d.requirement_id, d.tool, d.issue, d.criterion, p.key, r.number
			FROM suite_demo_requirement d JOIN requirement r ON r.id = d.requirement_id JOIN project p ON p.id = d.project_id
			WHERE p.key = ANY($1) AND r.trashed_at IS NULL`, projectKeys)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				n          node
				key        string
				number     int
				issue, crt int32
			)
			if err := rows.Scan(&n.id, &n.tool, &issue, &crt, &key, &number); err != nil {
				return err
			}
			n.issue, n.criterion = int(issue), int(crt)
			id := n.id
			n.id = uuid.Nil
			out[n] = made{id: id, key: requirement.Key(key, number)}
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) existing(ctx context.Context, nodes map[node]made) (*existing, error) {
	ids := make([]uuid.UUID, 0, len(nodes))
	for _, m := range nodes {
		ids = append(ids, m.id)
	}
	have := &existing{traces: map[[2]uuid.UUID]bool{}, issues: map[issueLink]bool{}, pages: map[pageLink]bool{}}
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		reads := []struct {
			sql  string
			scan func(scan func(...any) error) error
		}{
			{`SELECT source_id, target_id FROM trace_link WHERE source_id = ANY($1) OR target_id = ANY($1)`, func(scan func(...any) error) error {
				var a, b uuid.UUID
				err := scan(&a, &b)
				have.traces[[2]uuid.UUID{a, b}], have.traces[[2]uuid.UUID{b, a}] = true, true
				return err
			}},
			{`SELECT requirement_id, issue_key FROM armature_issue_link WHERE requirement_id = ANY($1)`, func(scan func(...any) error) error {
				var l issueLink
				err := scan(&l.requirement, &l.issue)
				have.issues[l] = true
				return err
			}},
			{`SELECT requirement_id, page_id, relation FROM stator_page_link WHERE requirement_id = ANY($1)`, func(scan func(...any) error) error {
				var l pageLink
				err := scan(&l.requirement, &l.page, &l.relation)
				have.pages[l] = true
				return err
			}},
		}
		for _, read := range reads {
			rows, err := tx.Query(ctx, read.sql, ids)
			if err != nil {
				return err
			}
			for rows.Next() {
				if err := read.scan(rows.Scan); err != nil {
					rows.Close()
					return err
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	return have, err
}

// items walks every item of one tool's project the demo made, with the requirement it became.
func items(p Plan, tool string, nodes map[node]made, visit func(pr Project, it Item, m made)) {
	for _, pr := range p.Projects {
		if pr.Tool != tool {
			continue
		}
		if m, ok := nodes[node{tool: pr.Tool}]; ok {
			visit(pr, pr.About, m)
		}
		for _, sec := range pr.Sections {
			for _, it := range sec.Items {
				if m, ok := nodes[node{tool: pr.Tool, issue: it.Issue}]; ok {
					visit(pr, it, m)
				}
			}
		}
	}
}

func (s *Service) linkDependencies(ctx context.Context, actor uuid.UUID, p Plan, tool string, nodes map[node]made, have *existing, out *Linked) error {
	var failed error
	items(p, tool, nodes, func(_ Project, it Item, from made) {
		for _, dep := range it.DependsOn {
			to, ok := nodes[node{tool: dep.Tool, issue: dep.Number}]
			if failed != nil || !ok || have.traces[[2]uuid.UUID{from.id, to.id}] {
				continue
			}
			_, _, err := s.trace.Link(ctx, actor, from.key, trace.LinkInput{TypeKey: dependsType, OtherKey: to.key})
			var refused *trace.FieldError
			switch {
			case err == nil:
				out.Links++
				have.traces[[2]uuid.UUID{from.id, to.id}], have.traces[[2]uuid.UUID{to.id, from.id}] = true, true
			case errors.As(err, &refused):
				out.Skipped++
			default:
				failed = err
			}
		}
	})
	return failed
}

// partner keeps one partner's linking going until a refusal says no further call will succeed.
type partner struct {
	name    string
	stopped string
	missing int
	what    string
}

// refused says whether err stops every further call, and notes why.
func (pt *partner) refused(err error, notConfigured, notConnected, rejected, unreachable, readOnly error) bool {
	switch {
	case errors.Is(err, notConfigured):
		pt.stopped = pt.name + " is not connected to this installation, so the demo links nothing there yet. Connect it in the installation settings, then link the demo again."
	case errors.Is(err, notConnected):
		pt.stopped = "Your " + pt.name + " account is not connected, so the demo links nothing there yet. Connect it on your account page, then link the demo again."
	case errors.Is(err, rejected):
		pt.stopped = pt.name + " did not accept your token. Store a new one on your account page, then link the demo again."
	case errors.Is(err, unreachable):
		pt.stopped = pt.name + " could not be reached. Link the demo again once it answers."
	case readOnly != nil && errors.Is(err, readOnly):
		pt.stopped = "Your " + pt.name + " token may only read. Store one that may write on your account page, then link the demo again."
	}
	return pt.stopped != ""
}

func (pt *partner) note(out *Linked) {
	switch {
	case pt.stopped != "":
		out.Notes = append(out.Notes, pt.stopped)
	case pt.missing > 0:
		out.Notes = append(out.Notes, fmt.Sprintf("Some links wait for %s that %s does not have yet. Add %s's suite demo, then link this one again.",
			pt.what, pt.name, pt.name))
	}
}

func (s *Service) linkIssues(ctx context.Context, p Plan, tool string, nodes map[node]made, have *existing, out *Linked) {
	pt := &partner{name: "Armature", what: "issues"}
	defer pt.note(out)
	items(p, tool, nodes, func(pr Project, it Item, m made) {
		if it.Issue == 0 {
			return
		}
		key := Target{Tool: pr.Tool, Number: it.Issue}.IssueKey()
		if have.issues[issueLink{m.id, key}] {
			return
		}
		if s.armature == nil || pt.stopped != "" {
			out.Skipped++
			return
		}
		_, _, err := s.armature.Link(ctx, m.key, armature.LinkInput{IssueKey: key, Relation: armature.Implements})
		switch {
		case err == nil:
			out.Links++
		case errors.Is(err, armature.ErrLinkExists):
		case pt.refused(err, armature.ErrNotConfigured, armature.ErrNotConnected, armature.ErrRejected, armature.ErrUnreachable, armature.ErrReadOnlyToken):
			out.Skipped++
		default:
			pt.missing++
			out.Skipped++
		}
	})
}

func (s *Service) linkPages(ctx context.Context, p Plan, tool string, nodes map[node]made, have *existing, out *Linked) {
	pt := &partner{name: "Stator", what: "pages"}
	defer pt.note(out)
	pages := map[string]uuid.UUID{}
	items(p, tool, nodes, func(_ Project, it Item, m made) {
		for _, pg := range it.Pages {
			if s.stator == nil || pt.stopped != "" {
				out.Skipped++
				continue
			}
			id, seen := pages[pg.Label]
			if !seen {
				var err error
				if id, err = s.stator.LabeledPage(ctx, pg.Label); err != nil {
					if !pt.refused(err, stator.ErrNotConfigured, stator.ErrNotConnected, stator.ErrRejected, stator.ErrUnreachable, nil) {
						pt.stopped = "Stator could not list the demo's pages: " + err.Error() + ". Link the demo again later."
					}
					out.Skipped++
					continue
				}
				pages[pg.Label] = id
			}
			if id == uuid.Nil {
				pt.missing++
				out.Skipped++
				continue
			}
			if have.pages[pageLink{m.id, id, pg.Relation}] {
				continue
			}
			_, _, err := s.stator.Link(ctx, m.key, stator.LinkInput{PageID: id, Relation: stator.Relation(pg.Relation)})
			switch {
			case err == nil:
				out.Links++
			case errors.Is(err, stator.ErrLinkExists):
			case pt.refused(err, stator.ErrNotConfigured, stator.ErrNotConnected, stator.ErrRejected, stator.ErrUnreachable, nil):
				out.Skipped++
			default:
				pt.missing++
				out.Skipped++
			}
		}
	})
}

// Baseline takes a baseline of each project, named after the snapshot; one taken before stays.
func (s *Service) Baseline(ctx context.Context, actor uuid.UUID, p Plan, keys map[string]string) error {
	for _, pr := range p.Projects {
		key := keys[pr.Tool]
		if key == "" {
			continue
		}
		in := baseline.Input{Name: "Snapshot " + p.Taken, Description: "The suite demo as its snapshot of " + p.Taken + " made it."}
		if _, _, err := s.baselines.Take(ctx, actor, key, in); err != nil && !errors.Is(err, baseline.ErrNameTaken) {
			return fmt.Errorf("the baseline of %s: %w", key, err)
		}
	}
	return nil
}
