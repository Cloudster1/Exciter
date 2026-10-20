package demo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// stepKind is what one step of a run does. Each takes seconds, so the worker's other events go on between them.
type stepKind int

const (
	stepProject stepKind = iota
	stepSection
	stepLinks
	stepBaseline
)

// step is one event's work: a project made, one of its milestones filed, a project linked, or the baselines.
type step struct {
	kind    stepKind
	project int
	section int
}

// stepsOf is a run in order: a build makes and files every project before it links, so a dependency finds its
// target in any of them.
func stepsOf(p Plan, kind Kind) []step {
	var out []step
	if kind == KindBuild {
		for i, pr := range p.Projects {
			out = append(out, step{kind: stepProject, project: i})
			for j := range pr.Sections {
				out = append(out, step{kind: stepSection, project: i, section: j})
			}
		}
	}
	for i := range p.Projects {
		out = append(out, step{kind: stepLinks, project: i})
	}
	return append(out, step{kind: stepBaseline})
}

// StepCount is how many steps a run of that kind takes for the plan.
func StepCount(p Plan, kind Kind) int { return len(stepsOf(p, kind)) }

// run does one step.
func (s *Service) run(ctx context.Context, actor uuid.UUID, p Plan, keys map[string]string, st step) (Result, error) {
	var res Result
	if st.kind == stepBaseline {
		return res, s.Baseline(ctx, actor, p, keys)
	}
	pr := p.Projects[st.project]
	key := keys[pr.Tool]
	if key == "" {
		return res, fmt.Errorf("the demo has no project for %s", pr.Name)
	}
	var err error
	switch st.kind {
	case stepProject:
		res.Requirements, err = s.makeProject(ctx, actor, pr, key, p.Taken)
	case stepSection:
		res.Requirements, err = s.fileSection(ctx, actor, pr, key, st.section)
	case stepLinks:
		res.Linked, err = s.LinkProject(ctx, actor, p, keys, pr.Tool)
	}
	if err != nil {
		return res, fmt.Errorf("the project %s: %w", key, err)
	}
	return res, nil
}
