package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// The template the projects start from, and the sections of it the demo files under.
const (
	template          = "software"
	sectionAbout      = "Introduction"
	sectionIssues     = "Functional requirements"
	attributeRelease  = "Release"
	statusApproved    = "Approved"
	revisionComment   = "Added by the suite demo"
	projectDescriptor = "The requirements of %s, taken on %s from the user stories and acceptance criteria of its issues, for the suite demo."
)

// Build makes the project of each tool the keys name, all at once, and answers how many requirements it made; the
// worker takes the same a step at a time.
func (s *Service) Build(ctx context.Context, actor uuid.UUID, p Plan, keys map[string]string) (int, error) {
	made := 0
	for _, pr := range p.Projects {
		key := keys[pr.Tool]
		if key == "" {
			continue
		}
		n, err := s.makeProject(ctx, actor, pr, key, p.Taken)
		made += n
		for i := 0; err == nil && i < len(pr.Sections); i++ {
			n, err = s.fileSection(ctx, actor, pr, key, i)
			made += n
		}
		if err != nil {
			return made, fmt.Errorf("the project %s: %w", key, err)
		}
	}
	return made, nil
}

// world is what a new project offers the demo: where its requirements go, and the ids its values name.
type world struct {
	projectID  uuid.UUID
	approved   *uuid.UUID
	release    string
	intro      *uuid.UUID
	introLast  *uuid.UUID
	issues     *uuid.UUID
	issuesLast *uuid.UUID
}

// node is one requirement the demo made, as suite_demo_requirement keeps it.
type node struct {
	id        uuid.UUID
	tool      string
	issue     int
	criterion int
}

// makeProject makes a tool's project from the template, with the requirement that says what the tool is.
func (s *Service) makeProject(ctx context.Context, actor uuid.UUID, pr Project, key, taken string) (int, error) {
	in := project.CreateInput{Key: key, Name: pr.Name, Description: fmt.Sprintf(projectDescriptor, pr.Name, taken), Template: template}
	if _, _, err := s.projects.Create(ctx, actor, in); err != nil {
		return 0, err
	}
	w, err := s.world(ctx, key)
	if err != nil {
		return 0, err
	}
	b := &builder{s: s, actor: actor, key: key, w: w}
	about, err := b.create(ctx, requirement.KindRequirement, pr.About, w.intro, w.introLast)
	if err != nil {
		return b.made, err
	}
	nodes := []node{{about, pr.Tool, 0, 0}}
	if pr.Architecture != nil {
		drawn, err := b.architecture(ctx, pr.Tool, *pr.Architecture)
		nodes = append(nodes, drawn...)
		if err != nil {
			return b.made, err
		}
	}
	return b.made, s.record(ctx, w.projectID, nodes)
}

// architecture files the drawings in a top-level section after the introduction. Each is kept as issue 0 with the
// view's place as its criterion, so it never meets the requirement about the tool or an issue's.
func (b *builder) architecture(ctx context.Context, tool string, sec Section) ([]node, error) {
	section, err := b.create(ctx, requirement.KindSection, Item{Title: sec.Title, Statement: sec.Intro}, nil, b.w.intro)
	if err != nil {
		return nil, err
	}
	var (
		nodes []node
		prev  *uuid.UUID
	)
	for i, it := range sec.Items {
		id, err := b.create(ctx, requirement.KindRequirement, it, &section, prev)
		if err != nil {
			return nodes, fmt.Errorf("the architecture view %q: %w", it.Title, err)
		}
		nodes = append(nodes, node{id, tool, 0, i + 1})
		prev = &id
	}
	return nodes, nil
}

// fileSection files one milestone's issues, with their criteria, after the sections filed before it.
func (s *Service) fileSection(ctx context.Context, actor uuid.UUID, pr Project, key string, at int) (int, error) {
	w, err := s.world(ctx, key)
	if err != nil {
		return 0, err
	}
	b := &builder{s: s, actor: actor, key: key, w: w}
	sec := pr.Sections[at]
	section, err := b.create(ctx, requirement.KindSection, Item{Title: sec.Title}, w.issues, w.issuesLast)
	if err != nil {
		return 0, err
	}
	var (
		nodes    []node
		prevItem *uuid.UUID
	)
	for _, it := range sec.Items {
		id, err := b.create(ctx, requirement.KindRequirement, it, &section, prevItem)
		if err != nil {
			return b.made, fmt.Errorf("issue #%d: %w", it.Issue, err)
		}
		nodes = append(nodes, node{id, pr.Tool, it.Issue, 0})
		prevItem = &id
		var prevChild *uuid.UUID
		for i, c := range it.Children {
			child := Item{Title: c.Title, Statement: c.Statement, Approved: it.Approved}
			cid, err := b.create(ctx, requirement.KindRequirement, child, &id, prevChild)
			if err != nil {
				return b.made, fmt.Errorf("issue #%d, criterion %d: %w", it.Issue, i+1, err)
			}
			nodes = append(nodes, node{cid, pr.Tool, it.Issue, i + 1})
			prevChild = &cid
		}
	}
	return b.made, s.record(ctx, w.projectID, nodes)
}

// world reads what the template made in the project, as the primary has it just after.
func (s *Service) world(ctx context.Context, key string) (*world, error) {
	w := &world{}
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, key).Scan(&w.projectID); err != nil {
			return err
		}
		var approved uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM req_status WHERE project_id = $1 AND name = $2`, w.projectID, statusApproved).Scan(&approved)
		if err == nil {
			w.approved = &approved
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		defs, err := attribute.Defs(ctx, tx, w.projectID)
		if err != nil {
			return err
		}
		for _, d := range defs {
			if d.Name == attributeRelease && !d.Retired {
				w.release = d.ID.String()
			}
		}
		for title, into := range map[string][2]**uuid.UUID{sectionAbout: {&w.intro, &w.introLast}, sectionIssues: {&w.issues, &w.issuesLast}} {
			section, last, err := sectionAndLast(ctx, tx, w.projectID, title)
			if err != nil {
				return err
			}
			*into[0], *into[1] = section, last
		}
		return nil
	})
	return w, err
}

// sectionAndLast finds a top-level section by its title, and the last node under it, so the demo files after it.
func sectionAndLast(ctx context.Context, tx db.DBTX, projectID uuid.UUID, title string) (*uuid.UUID, *uuid.UUID, error) {
	var section uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM requirement WHERE project_id = $1 AND parent_id IS NULL AND kind = 'section'
		AND title = $2 AND trashed_at IS NULL ORDER BY rank, number LIMIT 1`, projectID, title).Scan(&section)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var last uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM requirement WHERE project_id = $1 AND parent_id = $2 AND trashed_at IS NULL
		ORDER BY rank DESC, number DESC LIMIT 1`, projectID, section).Scan(&last)
	if errors.Is(err, pgx.ErrNoRows) {
		return &section, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &section, &last, nil
}

// record keeps which issue each requirement came from, in one write.
func (s *Service) record(ctx context.Context, projectID uuid.UUID, nodes []node) error {
	ids, tools, issues, criteria := make([]uuid.UUID, len(nodes)), make([]string, len(nodes)), make([]int32, len(nodes)), make([]int32, len(nodes))
	for i, n := range nodes {
		ids[i], tools[i], issues[i], criteria[i] = n.id, n.tool, int32(n.issue), int32(n.criterion)
	}
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO suite_demo_requirement (requirement_id, project_id, tool, issue, criterion)
			SELECT id, $1, tool, issue, criterion FROM unnest($2::uuid[], $3::text[], $4::int[], $5::int[]) AS n(id, tool, issue, criterion)`,
			projectID, ids, tools, issues, criteria)
		return err
	})
	return err
}

// builder makes one project's requirements, each in its own transaction as an import does.
type builder struct {
	s     *Service
	actor uuid.UUID
	key   string
	w     *world
	made  int
}

func (b *builder) create(ctx context.Context, kind requirement.Kind, it Item, parent, after *uuid.UUID) (uuid.UUID, error) {
	in := requirement.CreateInput{Kind: kind, Title: it.Title, Comment: revisionComment, Place: &requirement.Place{ParentID: parent, AfterID: after}}
	if it.Statement != "" {
		in.StatementMarkdown = &it.Statement
	}
	if kind == requirement.KindRequirement {
		if it.Rationale != "" {
			in.RationaleMarkdown = &it.Rationale
		}
		if it.Release != "" && b.w.release != "" {
			in.Attributes = map[string]json.RawMessage{b.w.release: json.RawMessage(strconv.Quote(it.Release))}
		}
		if it.Approved {
			in.ImportedStatus = b.w.approved
		}
	}
	r, _, err := b.s.requirements.Create(ctx, b.actor, b.key, in)
	if err != nil {
		return uuid.Nil, err
	}
	if kind == requirement.KindRequirement {
		b.made++
	}
	return r.ID, nil
}
