package testexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Rollup is what a case iteration comes to from its steps: failed when any failed, else blocked when any is, and
// once every step is recorded, skipped when all were skipped and passed otherwise; nil while a step is open.
func Rollup(recorded []Outcome, steps int) *Outcome {
	var out *Outcome
	pick := func(o Outcome) *Outcome { return &o }
	for _, o := range recorded {
		switch {
		case o == Failed:
			return pick(Failed)
		case o == Blocked:
			out = pick(Blocked)
		}
	}
	if out != nil || steps == 0 || len(recorded) < steps {
		return out
	}
	for _, o := range recorded {
		if o != Skipped {
			return pick(Passed)
		}
	}
	return pick(Skipped)
}

func validOutcome(o Outcome) bool { return o == Passed || o == Failed || o == Blocked || o == Skipped }

const selectCaseRun = `
SELECT r.id, p.key, r.number, r.title, r.trashed_at IS NOT NULL, c.iteration, c.revision, c."values", c.outcome, c.note, c.source,
       c.recorded_by, COALESCE(u.name, ''), c.recorded_at, c.ci_suite, c.ci_name, c.ci_commit, c.ci_uploaded_at
FROM test_execution_case c
JOIN requirement r ON r.id = c.requirement_id
JOIN project p ON p.id = r.project_id
LEFT JOIN app_user u ON u.id = c.recorded_by`

func scanCaseRun(row pgx.Row) (CaseRun, error) {
	var (
		c          CaseRun
		projectKey string
		number     int
		values     []byte
		by         *uuid.UUID
		byName     string
		suite      string
		name       string
		commit     string
		uploaded   *time.Time
	)
	err := row.Scan(&c.Case.ID, &projectKey, &number, &c.Case.Title, &c.Case.Trashed, &c.Iteration, &c.Revision, &values, &c.Outcome,
		&c.Note, &c.Source, &by, &byName, &c.RecordedAt, &suite, &name, &commit, &uploaded)
	if err != nil {
		return c, err
	}
	c.Case.Key = keyOf(projectKey, number)
	if by != nil {
		c.RecordedBy = &requirement.PersonRef{ID: *by, Name: byName}
	}
	if uploaded != nil {
		c.CI = &CIResult{Suite: suite, Name: name, Commit: commit, UploadedAt: *uploaded}
	}
	return c, json.Unmarshal(values, &c.Values)
}

// caseRuns reads the case iterations a condition on c keeps, in the order the execution runs them.
func caseRuns(ctx context.Context, tx db.DBTX, where string, args ...any) ([]CaseRun, error) {
	rows, err := tx.Query(ctx, selectCaseRun+` WHERE `+where+` ORDER BY c.position, c.iteration`, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CaseRun, error) { return scanCaseRun(row) })
	if out == nil {
		out = []CaseRun{}
	}
	return out, err
}

// caseIn finds the requirement a key names among an execution's cases, by a key it has or had.
func caseIn(ctx context.Context, tx db.DBTX, reader uuid.UUID, execution uuid.UUID, key string) (uuid.UUID, error) {
	e, err := requirement.FindEndpoint(ctx, tx, reader, strings.TrimSpace(key))
	if errors.Is(err, requirement.ErrNotFound) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	var held bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM test_execution_case WHERE execution_id = $1 AND requirement_id = $2)`,
		execution, e.ID).Scan(&held); err != nil {
		return uuid.Nil, err
	}
	if !held {
		return uuid.Nil, ErrNotFound
	}
	return e.ID, nil
}

// readCase reads one case iteration with its steps at the pinned revision and what was recorded of it.
func readCase(ctx context.Context, tx db.DBTX, execution, requirementID uuid.UUID, iteration int) (*Case, error) {
	run, err := scanCaseRun(tx.QueryRow(ctx, selectCaseRun+` WHERE c.execution_id = $1 AND c.requirement_id = $2 AND c.iteration = $3`,
		execution, requirementID, iteration))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out := &Case{Run: run, Results: []StepResult{}, Files: []File{}, Issues: []Issue{}}
	var pre, steps, params []byte
	if err := tx.QueryRow(ctx, `SELECT preconditions, steps, parameters FROM requirement_revision WHERE requirement_id = $1 AND number = $2`,
		requirementID, run.Revision).Scan(&pre, &steps, &params); err != nil {
		return nil, err
	}
	test, err := requirement.ReadTestCase(pre, steps, params)
	if err != nil {
		return nil, err
	}
	out.Preconditions, out.Steps, out.Parameters, out.Iterations = test.Preconditions, test.Steps, test.Parameters, test.Iterations()
	rows, err := tx.Query(ctx, `
		SELECT s.step, s.outcome, s.note, s.recorded_by, COALESCE(u.name, ''), s.recorded_at FROM test_execution_step s
		LEFT JOIN app_user u ON u.id = s.recorded_by
		WHERE s.execution_id = $1 AND s.requirement_id = $2 AND s.iteration = $3 ORDER BY s.step`, execution, requirementID, iteration)
	if err != nil {
		return nil, err
	}
	out.Results, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (StepResult, error) {
		var (
			r      StepResult
			by     *uuid.UUID
			byName string
			at     time.Time
		)
		err := row.Scan(&r.Step, &r.Outcome, &r.Note, &by, &byName, &at)
		r.RecordedAt = &at
		if by != nil {
			r.RecordedBy = &requirement.PersonRef{ID: *by, Name: byName}
		}
		return r, err
	})
	if err != nil {
		return nil, err
	}
	if out.Results == nil {
		out.Results = []StepResult{}
	}
	if out.Files, err = files(ctx, tx, execution, requirementID, iteration); err != nil {
		return nil, err
	}
	out.Issues, err = issues(ctx, tx, execution, requirementID, iteration)
	return out, err
}

// Case answers one case iteration of an execution to run or to read.
func (s *Service) Case(ctx context.Context, reader, execution uuid.UUID, key string, iteration int) (*Case, error) {
	var out *Case
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := readExecution(ctx, tx, reader, execution, false); err != nil {
			return err
		}
		id, err := caseIn(ctx, tx, reader, execution, key)
		if err != nil {
			return err
		}
		out, err = readCase(ctx, tx, execution, id, max(iteration, 1))
		return err
	})
	return out, err
}

// recording reads an open execution the caller records in, and the case a key names in it.
func recording(ctx context.Context, tx db.DBTX, actor, execution uuid.UUID, key string) (*executionRow, uuid.UUID, error) {
	e, err := readExecution(ctx, tx, actor, execution, false)
	if err != nil {
		return nil, uuid.Nil, err
	}
	if e.State != Open {
		return nil, uuid.Nil, ErrClosed
	}
	if err := stillOpen(ctx, tx, e.projectID); err != nil {
		return nil, uuid.Nil, err
	}
	if !e.Can.Record {
		return nil, uuid.Nil, ErrForbidden
	}
	id, err := caseIn(ctx, tx, actor, execution, key)
	return e, id, err
}

// Record records a case iteration: each step's outcome and actual result, and the case's own outcome, which follows
// from its steps when none is given. Whoever records it last is its recorder.
func (s *Service) Record(ctx context.Context, actor, execution uuid.UUID, key string, in RecordInput) (*Case, db.LSN, error) {
	iteration := max(in.Iteration, 1)
	note, err := checkText("note", in.Note, MaxNoteLength, false, fmt.Sprintf("Keep the note to %d characters.", MaxNoteLength))
	if err != nil {
		return nil, 0, err
	}
	if in.Outcome != nil && !validOutcome(*in.Outcome) {
		return nil, 0, &FieldError{"outcome", "Record passed, failed, blocked or skipped."}
	}
	var out *Case
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, id, err := recording(ctx, tx, actor, execution, key)
		if err != nil {
			return err
		}
		before, err := readCase(ctx, tx, execution, id, iteration)
		if err != nil {
			return err
		}
		for i, step := range in.Steps {
			field := fmt.Sprintf("steps[%d]", i)
			if step.Step < 1 || step.Step > len(before.Steps) {
				return &FieldError{field, fmt.Sprintf("The case has steps 1 to %d at the revision it runs. Record one of those.", len(before.Steps))}
			}
			if !validOutcome(step.Outcome) {
				return &FieldError{field, "Record passed, failed, blocked or skipped."}
			}
			stepNote, err := checkText(field, step.Note, MaxNoteLength, false, fmt.Sprintf("Keep the actual result to %d characters.", MaxNoteLength))
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO test_execution_step (execution_id, requirement_id, iteration, step, outcome, note, recorded_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (execution_id, requirement_id, iteration, step)
				DO UPDATE SET outcome = EXCLUDED.outcome, note = EXCLUDED.note, recorded_by = EXCLUDED.recorded_by, recorded_at = now()`,
				execution, id, iteration, step.Step, step.Outcome, stepNote, actor); err != nil {
				return refusal(err)
			}
		}
		outcome := in.Outcome
		if outcome == nil {
			var recorded []Outcome
			if err := pgxOutcomes(ctx, tx, &recorded, execution, id, iteration); err != nil {
				return err
			}
			outcome = Rollup(recorded, len(before.Steps))
		}
		var source, by any
		if outcome != nil {
			source, by = SourceTester, actor
		}
		if _, err := tx.Exec(ctx, `
			UPDATE test_execution_case SET outcome = $4, note = $5, source = $6, recorded_by = $7,
			    recorded_at = CASE WHEN $4::text IS NULL THEN NULL ELSE now() END
			WHERE execution_id = $1 AND requirement_id = $2 AND iteration = $3`,
			execution, id, iteration, outcome, note, source, by); err != nil {
			return refusal(err)
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTestExecutionRecorded, TargetType: "test_execution", TargetID: &execution, Actor: actor,
			Data: map[string]any{"project": e.ProjectKey, "case": before.Run.Case.Key, "iteration": iteration, "outcome": outcome, "steps": len(in.Steps)}}); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicRecorded, map[string]any{"actorId": actor, "executionId": execution, "projectId": e.projectID, "requirementId": id}); err != nil {
			return err
		}
		out, err = readCase(ctx, tx, execution, id, iteration)
		return err
	})
	return out, lsn, err
}

func pgxOutcomes(ctx context.Context, tx db.DBTX, out *[]Outcome, execution, id uuid.UUID, iteration int) error {
	rows, err := tx.Query(ctx, `SELECT outcome FROM test_execution_step WHERE execution_id = $1 AND requirement_id = $2 AND iteration = $3`,
		execution, id, iteration)
	if err != nil {
		return err
	}
	*out, err = pgx.CollectRows(rows, pgx.RowTo[Outcome])
	return err
}

// CaseResults answers a test case's results in every execution, newest first.
func (s *Service) CaseResults(ctx context.Context, reader uuid.UUID, key string) ([]CaseResult, error) {
	out := []CaseResult{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := requirement.FindEndpoint(ctx, tx, reader, strings.TrimSpace(key))
		if errors.Is(err, requirement.ErrNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT e.id, e.title, e.state, e.version, e.configuration, c.iteration, c.revision, c.outcome, c.source, c.recorded_at
			FROM test_execution_case c JOIN test_execution e ON e.id = c.execution_id
			WHERE c.requirement_id = $1 ORDER BY e.opened_at DESC, c.iteration`, r.ID)
		if err != nil {
			return err
		}
		found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CaseResult, error) {
			var c CaseResult
			return c, row.Scan(&c.ExecutionID, &c.ExecutionTitle, &c.State, &c.Version, &c.Configuration, &c.Iteration, &c.Revision,
				&c.Outcome, &c.Source, &c.RecordedAt)
		})
		if found != nil {
			out = found
		}
		return err
	})
	return out, err
}
