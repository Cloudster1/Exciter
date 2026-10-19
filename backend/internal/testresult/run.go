package testresult

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Handler runs an upload the worker was handed, as its uploader with the projects their token reached.
func Handler(s *Service) events.HandlerFunc {
	return func(ctx context.Context, e events.Event) error {
		var r requested
		if err := json.Unmarshal(e.Payload, &r); err != nil {
			return fmt.Errorf("read %s: %w", e.Topic, err)
		}
		if r.Projects != nil {
			ctx = db.WithUserInProjects(ctx, r.ActorID, r.Projects)
		} else {
			ctx = db.WithUser(ctx, r.ActorID)
		}
		return s.Run(ctx, r.RunID)
	}
}

// target is what a key named, as its uploader sees it.
type target struct {
	id      uuid.UUID
	problem Problem
}

// Run lands a queued run's cases on the requirements they name, in one transaction with its counts, so a run is
// evidence whole or not at all. A run that is not queued is left alone.
func (s *Service) Run(ctx context.Context, id uuid.UUID) error {
	var (
		projectID uuid.UUID
		raw       []byte
	)
	if _, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `UPDATE test_run SET state = 'running', started_at = now() WHERE id = $1 AND state = 'queued'
			RETURNING project_id, cases`, id).Scan(&projectID, &raw)
		if errors.Is(err, pgx.ErrNoRows) {
			raw = nil
			return nil
		}
		return err
	}); err != nil || raw == nil {
		return err
	}
	var cases []Case
	if err := json.Unmarshal(raw, &cases); err != nil {
		return s.fail(ctx, id, fmt.Errorf("read the run's cases: %w", err))
	}
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		return land(ctx, tx, id, projectID, cases)
	})
	if err != nil {
		return s.fail(ctx, id, err)
	}
	return nil
}

func land(ctx context.Context, tx db.DBTX, id, projectID uuid.UUID, cases []Case) error {
	actor, _ := db.UserFrom(ctx)
	targets, err := resolve(ctx, tx, projectID, cases)
	if err != nil {
		return err
	}
	var (
		counts                            Counts
		matched                           int
		reported                          []Reported
		unreported                        int
		runs, reqs                        []uuid.UUID
		suites, names, outcomes, messages []string
		durations                         []int
		seen                              = map[string]bool{}
	)
	for _, c := range cases {
		switch c.Outcome {
		case Passed:
			counts.Passed++
		case Failed:
			counts.Failed++
		case Skipped:
			counts.Skipped++
		case Errored:
			counts.Errored++
		}
		hit := false
		for _, key := range c.Keys {
			t := targets[key]
			if t.problem != "" {
				mark := key + "\x00" + c.Name
				if seen[mark] {
					continue
				}
				seen[mark] = true
				if len(reported) < MaxReported {
					reported = append(reported, Reported{Key: key, Test: c.Name, Problem: t.problem})
				} else {
					unreported++
				}
				continue
			}
			hit = true
			runs, reqs = append(runs, id), append(reqs, t.id)
			suites, names, outcomes = append(suites, c.Suite), append(names, c.Name), append(outcomes, string(c.Outcome))
			messages, durations = append(messages, c.Message), append(durations, c.DurationMs)
		}
		if hit {
			matched++
		}
	}
	if len(runs) > 0 {
		// A runner that writes one name twice, as a parameterised test may, keeps the first.
		if _, err := tx.Exec(ctx, `
			INSERT INTO test_result (run_id, project_id, requirement_id, suite, name, outcome, message, duration_ms)
			SELECT r, $2, q, s, n, o, m, d FROM unnest($1::uuid[], $3::uuid[], $4::text[], $5::text[], $6::text[], $7::text[], $8::int[])
			    AS x (r, q, s, n, o, m, d)
			ON CONFLICT DO NOTHING`, runs, projectID, reqs, suites, names, outcomes, messages, durations); err != nil {
			return fmt.Errorf("keep the results: %w", err)
		}
		if err := fillExecutions(ctx, tx, id, projectID, actor); err != nil {
			return err
		}
	}
	if reported == nil {
		reported = []Reported{}
	}
	body, err := json.Marshal(reported)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE test_run SET state = 'done', cases = '[]'::jsonb, cases_read = $2, matched = $3, passed = $4, failed = $5,
		    skipped = $6, errored = $7, reported = $8, unreported = $9, finished_at = now()
		WHERE id = $1`, id, len(cases), matched, counts.Passed, counts.Failed, counts.Skipped, counts.Errored, body, unreported); err != nil {
		return fmt.Errorf("finish the run: %w", err)
	}
	return events.Emit(ctx, tx, TopicFinished, map[string]any{"actorId": actor, "runId": id, "projectId": projectID})
}

// resolve reads what each key the cases name is, in the run's project as its uploader sees it; a key elsewhere is
// unknown, as one nobody holds is.
func resolve(ctx context.Context, tx db.DBTX, projectID uuid.UUID, cases []Case) (map[string]target, error) {
	var keys, projects []string
	var numbers []int
	for _, c := range cases {
		for _, k := range c.Keys {
			if slices.Contains(keys, k) {
				continue
			}
			p, n, ok := requirement.ParseKey(k)
			if !ok {
				continue
			}
			keys, projects, numbers = append(keys, k), append(projects, p), append(numbers, n)
		}
	}
	out := map[string]target{}
	for _, k := range keys {
		out[k] = target{problem: ProblemUnknown}
	}
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT x.k, r.id, r.kind, r.trashed_at IS NOT NULL
		FROM unnest($1::text[], $2::text[], $3::int[]) AS x (k, p, n)
		JOIN requirement r ON r.id = requirement_key_id(x.p, x.n) AND r.project_id = $4`, keys, projects, numbers, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the keys the run names: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			key     string
			id      uuid.UUID
			kind    requirement.Kind
			trashed bool
		)
		if err := rows.Scan(&key, &id, &kind, &trashed); err != nil {
			return nil, err
		}
		switch {
		case trashed:
			out[key] = target{problem: ProblemTrashed}
		case kind != requirement.KindRequirement:
			out[key] = target{problem: ProblemSection}
		default:
			out[key] = target{id: id}
		}
	}
	return out, rows.Err()
}

// errLanding is what a run that could not land says; what went wrong inside is the server's to log, not the
// pipeline's to print.
const errLanding = "The run could not be read onto its requirements. Upload it again, and tell an administrator if it fails twice."

// fail ends a run that could not land, so a pipeline waiting on it stops waiting; a retry finds it ended.
func (s *Service) fail(ctx context.Context, id uuid.UUID, cause error) error {
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE test_run SET state = 'failed', cases = '[]'::jsonb, error = $2, finished_at = now()
			WHERE id = $1 AND state IN ('queued', 'running')`, id, errLanding)
		return err
	})
	if err != nil {
		return errors.Join(cause, err)
	}
	return nil
}

// fillExecutions fills the open executions holding the test cases a landing run verifies, as test_execution_take_ci
// says (#252), and tells each execution's audit trail and coverage.
func fillExecutions(ctx context.Context, tx db.DBTX, id, projectID, actor uuid.UUID) error {
	rows, err := tx.Query(ctx, `
		SELECT filled_execution, count(DISTINCT filled_case)::int FROM test_execution_take_ci($1)
		GROUP BY filled_execution ORDER BY filled_execution`, id)
	if err != nil {
		return fmt.Errorf("fill the open test executions: %w", err)
	}
	type filled struct {
		execution uuid.UUID
		cases     int
	}
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (filled, error) {
		var f filled
		return f, row.Scan(&f.execution, &f.cases)
	})
	if err != nil {
		return fmt.Errorf("fill the open test executions: %w", err)
	}
	for _, f := range found {
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTestExecutionRecorded, TargetType: "test_execution", TargetID: &f.execution,
			Actor: actor, Data: map[string]any{"source": "ci", "runId": id, "cases": f.cases}}); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicExecutionRecorded, map[string]any{"actorId": actor, "executionId": f.execution, "projectId": projectID, "runId": id}); err != nil {
			return err
		}
	}
	return nil
}
