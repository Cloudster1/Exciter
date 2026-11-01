package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// The templates the projects are made from, and what the demo names in them.
const (
	template              = "software"
	templateNeeds         = "system"
	templateTests         = "test"
	sectionIntroduction   = "Introduction"
	attributeVerification = "Verification method"
	attributeComponent    = "Component"
	attributeAutomated    = "Automated"
	iconArchitecture      = "data"
	colourArchitecture    = "#0369a1"
	revisionComment       = "Added by the suite demo"
	projectDescriptor     = "The specification of %s, written for the suite demo from its code, documentation and issues as of %s, with its test cases and a test run."
)

// Blueprint is the software template with stakeholder needs, test cases, test procedures and architecture views beside
// its types; the demo files its own sections, in the order of the specification.
func Blueprint() project.Blueprint {
	b, _ := project.BlueprintOf(template)
	needs, _ := project.BlueprintOf(templateNeeds)
	tests, _ := project.BlueprintOf(templateTests)
	b.Sections = nil
	b.TypeSpecs = slices.Clone(b.TypeSpecs)
	for _, t := range needs.TypeSpecs {
		if t.Name == TypeStakeholderNeed {
			b.TypeSpecs = append(b.TypeSpecs, t)
		}
	}
	var testTypes []string
	for _, t := range tests.TypeSpecs {
		b.TypeSpecs = append(b.TypeSpecs, t)
		testTypes = append(testTypes, t.Name)
	}
	// A view of the architecture is neither implemented nor verified, so it takes a type no coverage rule asks of.
	b.TypeSpecs = append(b.TypeSpecs, project.TypeSpec{Name: TypeArchitectureView, Icon: iconArchitecture, Colour: colourArchitecture})
	b.AttributeSpecs = slices.Clone(b.AttributeSpecs)
	for _, a := range tests.AttributeSpecs {
		a.Types = testTypes
		b.AttributeSpecs = append(b.AttributeSpecs, a)
	}
	b.Coverage = []project.CoverageSpec{{Type: TypeFunctional, Implemented: true, Verified: true},
		{Type: TypeNonFunctional, Verified: true}, {Type: TypeInterface, Verified: true}, {Type: TypeUseCase, Verified: true}}
	return b
}

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

// world is what a project offers the demo: the ids of the types, statuses and attributes its items name.
type world struct {
	projectID    uuid.UUID
	types        map[string]uuid.UUID
	statuses     map[string]uuid.UUID
	verification *attribute.Def
	component    string
	automated    string
}

// node is one requirement the demo made, as suite_demo_requirement keeps it: an item of the specification by its
// ID, or a view of the architecture by its place.
type node struct {
	id        uuid.UUID
	tool      string
	issue     int
	criterion int
	spec      string
}

// makeProject makes a tool's project with its top-level sections, in the order of the specification, and files the
// architecture after the introduction.
func (s *Service) makeProject(ctx context.Context, actor uuid.UUID, pr Project, key, taken string) (int, error) {
	bp := Blueprint()
	in := project.CreateInput{Key: key, Name: pr.Name, Description: fmt.Sprintf(projectDescriptor, pr.Name, taken), Template: template, Blueprint: &bp}
	if _, _, err := s.projects.Create(ctx, actor, in); err != nil {
		return 0, err
	}
	w, err := s.world(ctx, key)
	if err != nil {
		return 0, err
	}
	b := &builder{s: s, actor: actor, key: key, w: w}
	var (
		prev  *uuid.UUID
		nodes []node
		filed = map[string]bool{}
	)
	if pr.Architecture != nil && !slices.ContainsFunc(pr.Sections, func(sec Section) bool { return sec.Path[0] == sectionIntroduction }) {
		if prev, nodes, err = b.architecture(ctx, pr.Tool, *pr.Architecture, nil); err != nil {
			return b.made, err
		}
	}
	for _, sec := range pr.Sections {
		title := sec.Path[0]
		if filed[title] {
			continue
		}
		filed[title] = true
		intro := ""
		for _, other := range pr.Sections {
			if len(other.Path) == 1 && other.Path[0] == title && intro == "" {
				intro = other.Intro
			}
		}
		id, err := b.create(ctx, requirement.KindSection, Item{Title: title, Statement: intro}, nil, prev)
		if err != nil {
			return b.made, fmt.Errorf("the section %s: %w", title, err)
		}
		prev = &id
		if title == sectionIntroduction && pr.Architecture != nil {
			if prev, nodes, err = b.architecture(ctx, pr.Tool, *pr.Architecture, prev); err != nil {
				return b.made, err
			}
		}
	}
	return b.made, s.record(ctx, w.projectID, nodes)
}

// architecture files the drawings in a top-level section after the one given. Each is kept as issue 0 with the
// view's place as its criterion, so it never meets an item of the specification.
func (b *builder) architecture(ctx context.Context, tool string, sec Section, after *uuid.UUID) (*uuid.UUID, []node, error) {
	section, err := b.create(ctx, requirement.KindSection, Item{Title: sec.Title(), Statement: sec.Intro}, nil, after)
	if err != nil {
		return nil, nil, err
	}
	var (
		nodes []node
		prev  *uuid.UUID
	)
	for i, it := range sec.Items {
		id, err := b.create(ctx, requirement.KindRequirement, it, &section, prev)
		if err != nil {
			return nil, nodes, fmt.Errorf("the architecture view %q: %w", it.Title, err)
		}
		nodes = append(nodes, node{id: id, tool: tool, criterion: i + 1})
		prev = &id
	}
	return &section, nodes, nil
}

// fileSection files one section of the specification with its items, making the sections on its path that are
// missing after those filed before them.
func (s *Service) fileSection(ctx context.Context, actor uuid.UUID, pr Project, key string, at int) (int, error) {
	w, err := s.world(ctx, key)
	if err != nil {
		return 0, err
	}
	b := &builder{s: s, actor: actor, key: key, w: w}
	sec := pr.Sections[at]
	var parent *uuid.UUID
	for i, title := range sec.Path {
		found, last, err := s.childSection(ctx, w.projectID, parent, title)
		if err != nil {
			return 0, err
		}
		if found == nil {
			intro := ""
			if i == len(sec.Path)-1 {
				intro = sec.Intro
			}
			id, err := b.create(ctx, requirement.KindSection, Item{Title: title, Statement: intro}, parent, last)
			if err != nil {
				return 0, fmt.Errorf("the section %s: %w", title, err)
			}
			found = &id
		}
		parent = found
	}
	_, prev, err := s.childSection(ctx, w.projectID, parent, "")
	if err != nil {
		return 0, err
	}
	var nodes []node
	for _, it := range sec.Items {
		id, err := b.create(ctx, requirement.KindRequirement, it, parent, prev)
		if err != nil {
			return b.made, fmt.Errorf("%s %s: %w", it.ID, it.Title, err)
		}
		nodes = append(nodes, node{id: id, tool: pr.Tool, spec: it.ID})
		prev = &id
	}
	return b.made, s.record(ctx, w.projectID, nodes)
}

// world reads what the template made in the project, as the primary has it just after.
func (s *Service) world(ctx context.Context, key string) (*world, error) {
	w := &world{types: map[string]uuid.UUID{}, statuses: map[string]uuid.UUID{}}
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, key).Scan(&w.projectID); err != nil {
			return err
		}
		for sql, into := range map[string]map[string]uuid.UUID{
			`SELECT name, id FROM requirement_type WHERE project_id = $1 AND NOT retired`: w.types,
			`SELECT name, id FROM req_status WHERE project_id = $1`:                       w.statuses,
		} {
			rows, err := tx.Query(ctx, sql, w.projectID)
			if err != nil {
				return err
			}
			for rows.Next() {
				var (
					name string
					id   uuid.UUID
				)
				if err := rows.Scan(&name, &id); err != nil {
					rows.Close()
					return err
				}
				into[name] = id
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		defs, err := attribute.Defs(ctx, tx, w.projectID)
		if err != nil {
			return err
		}
		for _, d := range defs {
			if d.Retired {
				continue
			}
			switch d.Name {
			case attributeVerification:
				w.verification = &d
			case attributeComponent:
				w.component = d.ID.String()
			case attributeAutomated:
				w.automated = d.ID.String()
			}
		}
		return nil
	})
	return w, err
}

// childSection finds the section of a title under parent, the top level when nil, and the last node under parent,
// so what is filed next goes after it. An empty title looks for no section.
func (s *Service) childSection(ctx context.Context, projectID uuid.UUID, parent *uuid.UUID, title string) (*uuid.UUID, *uuid.UUID, error) {
	var found, last *uuid.UUID
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM requirement WHERE project_id = $1 AND parent_id IS NOT DISTINCT FROM $2
			AND kind = 'section' AND title = $3 AND trashed_at IS NULL ORDER BY rank, number LIMIT 1`, projectID, parent, title).Scan(&id)
		switch {
		case err == nil:
			found = &id
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		var after uuid.UUID
		err = tx.QueryRow(ctx, `SELECT id FROM requirement WHERE project_id = $1 AND parent_id IS NOT DISTINCT FROM $2
			AND trashed_at IS NULL ORDER BY rank DESC, number DESC LIMIT 1`, projectID, parent).Scan(&after)
		switch {
		case err == nil:
			last = &after
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		return nil
	})
	return found, last, err
}

// record keeps what each requirement was made from, in one write.
func (s *Service) record(ctx context.Context, projectID uuid.UUID, nodes []node) error {
	if len(nodes) == 0 {
		return nil
	}
	ids, tools, specs := make([]uuid.UUID, len(nodes)), make([]string, len(nodes)), make([]string, len(nodes))
	issues, criteria := make([]int32, len(nodes)), make([]int32, len(nodes))
	for i, n := range nodes {
		ids[i], tools[i], issues[i], criteria[i], specs[i] = n.id, n.tool, int32(n.issue), int32(n.criterion), n.spec
	}
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO suite_demo_requirement (requirement_id, project_id, tool, issue, criterion, spec_id)
			SELECT id, $1, tool, issue, criterion, spec FROM unnest($2::uuid[], $3::text[], $4::int[], $5::int[], $6::text[])
			AS n(id, tool, issue, criterion, spec)`, projectID, ids, tools, issues, criteria, specs)
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
		if err := b.fill(&in, it); err != nil {
			return uuid.Nil, err
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

// fill sets what an item carries besides its title and statement, by the ids its project gives the names.
func (b *builder) fill(in *requirement.CreateInput, it Item) error {
	if it.Rationale != "" {
		in.RationaleMarkdown = &it.Rationale
	}
	if it.Type != "" {
		id, ok := b.w.types[it.Type]
		if !ok {
			return fmt.Errorf("the project has no type %s", it.Type)
		}
		in.TypeID = &id
	}
	if it.Status != "" {
		id, ok := b.w.statuses[it.Status]
		if !ok {
			return fmt.Errorf("the project has no status %s", it.Status)
		}
		in.ImportedStatus = &id
	}
	if it.Priority != "" {
		priority := requirement.Priority(it.Priority)
		in.Priority = &priority
	}
	in.TestCase = it.TestCase
	in.Attributes = map[string]json.RawMessage{}
	if it.Verification != "" && b.w.verification != nil {
		for _, o := range b.w.verification.Options {
			if o.Name == it.Verification {
				in.Attributes[b.w.verification.ID.String()] = json.RawMessage(strconv.Quote(o.ID.String()))
			}
		}
	}
	if it.Component != "" && b.w.component != "" {
		in.Attributes[b.w.component] = json.RawMessage(strconv.Quote(it.Component))
	}
	if it.Automated != nil && b.w.automated != "" {
		in.Attributes[b.w.automated] = json.RawMessage(strconv.FormatBool(*it.Automated))
	}
	return nil
}
