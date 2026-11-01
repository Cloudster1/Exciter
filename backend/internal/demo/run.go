package demo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/review"
	"github.com/cloudster1/exciter/backend/internal/testexec"
)

// What the test run makes in each project, found again by these names when the demo is linked again.
const (
	BaselineName   = "Demo baseline"
	PlanTitle      = "Every test case"
	ExecutionTitle = "Demo test run"
	// commitLength is how much of the commit a run's version shows, as git abbreviates one.
	commitLength   = 12
	planDescriptor = "Every test case of the specification, in its order."
	closeComment   = "Run by the suite demo against the demo baseline."
)

// RunTests runs every test case of a project against its baseline with the results its specification records. What a
// run stopped half way made is found again, so running it again finishes it rather than making a second.
func (s *Service) RunTests(ctx context.Context, actor uuid.UUID, p Plan, keys map[string]string, tool string) error {
	var pr *Project
	for i := range p.Projects {
		if p.Projects[i].Tool == tool {
			pr = &p.Projects[i]
		}
	}
	key := keys[tool]
	if pr == nil || key == "" {
		return nil
	}
	nodes, err := s.mapped(ctx, map[string]string{tool: key})
	if err != nil {
		return err
	}
	type run struct {
		item Item
		key  string
	}
	var cases []run
	for _, sec := range pr.Sections {
		for _, it := range sec.Items {
			if m, ok := nodes[node{tool: tool, spec: it.ID}]; ok && it.TestCase != nil {
				cases = append(cases, run{it, m.key})
			}
		}
	}
	baselineID, err := s.demoBaseline(ctx, actor, key, p.Taken)
	if err != nil || len(cases) == 0 {
		return err
	}
	planID, err := s.found(ctx, `SELECT t.id FROM test_plan t JOIN project p ON p.id = t.project_id WHERE p.key = $1 AND t.title = $2`, key, PlanTitle)
	if err != nil {
		return err
	}
	if planID == nil {
		in := testexec.PlanInput{Title: PlanTitle, Description: planDescriptor, Scope: testexec.ScopeCases}
		for _, c := range cases {
			in.Cases = append(in.Cases, c.key)
		}
		plan, _, err := s.runs.CreatePlan(ctx, actor, key, in)
		if err != nil {
			return fmt.Errorf("the test plan: %w", err)
		}
		planID = &plan.ID
	}
	executionID, err := s.found(ctx, `SELECT e.id FROM test_execution e JOIN project p ON p.id = e.project_id WHERE p.key = $1 AND e.title = $2`,
		key, ExecutionTitle)
	if err != nil {
		return err
	}
	if executionID == nil {
		in := testexec.ExecutionInput{Title: ExecutionTitle, PlanID: *planID, BaselineID: &baselineID, Version: shortCommit(pr.Commit),
			Testers: []review.Subject{{Type: review.SubjectUser, UserID: &actor}}}
		e, _, err := s.runs.Start(ctx, actor, key, in)
		if err != nil {
			return fmt.Errorf("the test run: %w", err)
		}
		executionID = &e.ID
	} else if open, err := s.found(ctx, `SELECT id FROM test_execution WHERE id = $1 AND state = 'open'`, *executionID); err != nil || open == nil {
		return err
	}
	for _, c := range cases {
		if c.item.Result == "" {
			continue
		}
		outcome := testexec.Outcome(c.item.Result)
		in := testexec.RecordInput{Outcome: &outcome, Note: c.item.Note}
		if outcome == testexec.Passed {
			for i := range c.item.TestCase.Steps {
				in.Steps = append(in.Steps, testexec.StepInput{Step: i + 1, Outcome: testexec.Passed})
			}
		}
		for n := 1; n <= c.item.TestCase.Iterations(); n++ {
			in.Iteration = n
			if _, _, err := s.runs.Record(ctx, actor, *executionID, c.key, in); err != nil {
				return fmt.Errorf("the result of %s: %w", c.item.ID, err)
			}
		}
	}
	if _, _, err := s.runs.Close(ctx, actor, *executionID, testexec.CloseInput{Comment: closeComment}); err != nil {
		return fmt.Errorf("closing the test run: %w", err)
	}
	return nil
}

// demoBaseline takes the project's baseline, or finds the one taken before.
func (s *Service) demoBaseline(ctx context.Context, actor uuid.UUID, key, taken string) (uuid.UUID, error) {
	in := baseline.Input{Name: BaselineName, Description: "The suite demo as its snapshot of " + taken + " made it."}
	b, _, err := s.baselines.Take(ctx, actor, key, in)
	if err == nil {
		return b.ID, nil
	}
	if !errors.Is(err, baseline.ErrNameTaken) {
		return uuid.Nil, fmt.Errorf("the baseline: %w", err)
	}
	id, err := s.found(ctx, `SELECT b.id FROM baseline b JOIN project p ON p.id = b.project_id WHERE p.key = $1 AND b.name = $2`, key, BaselineName)
	if err != nil || id == nil {
		return uuid.Nil, cmpErr(err, baseline.ErrNameTaken)
	}
	return *id, nil
}

// found reads the id a query names, or nil when there is none.
func (s *Service) found(ctx context.Context, sql string, args ...any) (*uuid.UUID, error) {
	var out *uuid.UUID
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, sql, args...).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		out = &id
		return err
	})
	return out, err
}

// shortCommit is the commit a run tested, as its version reads.
func shortCommit(commit string) string {
	if len(commit) > commitLength {
		return commit[:commitLength]
	}
	return commit
}
