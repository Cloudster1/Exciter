package testexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/review"
)

// MaxDueAhead bounds how far ahead an execution's due date lies.
const MaxDueAhead = 366 * 24 * time.Hour

// MineLimit is how many executions a person's own list holds at most.
const MineLimit = 100

const selectExecution = `
SELECT e.id, p.key, e.project_id, e.title, e.plan_id, e.plan_title, e.baseline_id, e.baseline_name, e.version, e.configuration,
       e.manager_id, COALESCE(u.name, ''), e.due_at, e.state, e.excluded, e.close_comment, e.opened_at, e.closed_at,
       test_execution_manages(e.id, $1::uuid) AND e.state = 'open' AND project_open(e.project_id), test_execution_records(e.id, $1::uuid),
       (SELECT jsonb_build_object('total', count(*), 'passed', count(*) FILTER (WHERE c.outcome = 'passed'),
               'failed', count(*) FILTER (WHERE c.outcome = 'failed'), 'blocked', count(*) FILTER (WHERE c.outcome = 'blocked'),
               'skipped', count(*) FILTER (WHERE c.outcome = 'skipped'), 'notRun', count(*) FILTER (WHERE c.outcome IS NULL))
        FROM test_execution_case c WHERE c.execution_id = e.id)
FROM test_execution e
JOIN project p ON p.id = e.project_id
LEFT JOIN app_user u ON u.id = e.manager_id`

type executionRow struct {
	Execution
	projectID uuid.UUID
}

func scanExecution(row pgx.Row) (*executionRow, error) {
	var (
		e                  executionRow
		baselineID         *uuid.UUID
		baselineName       string
		manager            *uuid.UUID
		excluded, progress []byte
	)
	err := row.Scan(&e.ID, &e.ProjectKey, &e.projectID, &e.Title, &e.PlanID, &e.PlanTitle, &baselineID, &baselineName, &e.Version,
		&e.Configuration, &manager, &e.Manager.Name, &e.DueAt, &e.State, &excluded, &e.CloseComment, &e.OpenedAt, &e.ClosedAt,
		&e.Can.Manage, &e.Can.Record, &progress)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if manager != nil {
		e.Manager.ID = *manager
	}
	if baselineID != nil || baselineName != "" {
		e.Baseline = &BaselineRef{ID: baselineID, Name: baselineName}
	}
	if err := json.Unmarshal(excluded, &e.Excluded); err != nil {
		return nil, err
	}
	if e.Excluded == nil {
		e.Excluded = []Excluded{}
	}
	e.Testers = []review.Subject{}
	return &e, json.Unmarshal(progress, &e.Progress)
}

func testers(ctx context.Context, tx db.DBTX, id uuid.UUID) ([]review.Subject, error) {
	rows, err := tx.Query(ctx, `
		SELECT t.subject_type, t.user_id, t.group_id, COALESCE(u.name, g.name, '')
		FROM test_execution_tester t LEFT JOIN app_user u ON u.id = t.user_id LEFT JOIN groups g ON g.id = t.group_id
		WHERE t.execution_id = $1 ORDER BY t.subject_type DESC, 4`, id)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (review.Subject, error) {
		var s review.Subject
		return s, row.Scan(&s.Type, &s.UserID, &s.GroupID, &s.Name)
	})
	if out == nil {
		out = []review.Subject{}
	}
	return out, err
}

// readExecution reads one execution as the reader sees it, its testers and, when asked, its cases.
func readExecution(ctx context.Context, tx db.DBTX, reader, id uuid.UUID, withCases bool) (*executionRow, error) {
	e, err := scanExecution(tx.QueryRow(ctx, selectExecution+` WHERE e.id = $2`, reader, id))
	if err != nil {
		return nil, err
	}
	if e.Testers, err = testers(ctx, tx, id); err != nil {
		return nil, err
	}
	if withCases {
		e.Cases, err = caseRuns(ctx, tx, `c.execution_id = $1`, id)
	}
	return e, err
}

// Execution answers one execution, its testers and its cases.
func (s *Service) Execution(ctx context.Context, reader, id uuid.UUID) (*Execution, error) {
	var out *Execution
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := readExecution(ctx, tx, reader, id, true)
		if e != nil {
			out = &e.Execution
		}
		return err
	})
	return out, err
}

func (s *Service) list(ctx context.Context, reader uuid.UUID, where string, args ...any) ([]Execution, error) {
	out := []Execution{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectExecution+` WHERE `+where+` ORDER BY e.state <> 'open', e.opened_at DESC, e.id LIMIT `+fmt.Sprint(MineLimit),
			append([]any{reader}, args...)...)
		if err != nil {
			return err
		}
		var found []*executionRow
		for rows.Next() {
			e, err := scanExecution(rows)
			if err != nil {
				rows.Close()
				return err
			}
			found = append(found, e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, e := range found {
			if e.Testers, err = testers(ctx, tx, e.ID); err != nil {
				return err
			}
			out = append(out, e.Execution)
		}
		return nil
	})
	return out, err
}

// Executions answers a project's executions, open ones first, narrowed to a state when one is given.
func (s *Service) Executions(ctx context.Context, reader uuid.UUID, projectKey string, state State) ([]Execution, error) {
	if state != "" && state != Open && state != Closed && state != Aborted {
		return nil, &FieldError{"state", "Ask for open, closed or aborted executions."}
	}
	var projectID uuid.UUID
	if err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if p != nil {
			projectID = p.ID
		}
		return err
	}); err != nil {
		return nil, err
	}
	return s.list(ctx, reader, `e.project_id = $2 AND ($3::text = '' OR e.state = $3::text)`, projectID, string(state))
}

// Mine answers the executions the reader manages or tests, across projects, open ones first.
func (s *Service) Mine(ctx context.Context, reader uuid.UUID) ([]Execution, error) {
	return s.list(ctx, reader, `(e.manager_id = $1 OR test_execution_tests(e.id, $1))`)
}

// checkTesters holds testers to people who may comment in the project and groups that exist, each once.
func checkTesters(ctx context.Context, tx db.DBTX, projectID uuid.UUID, in []review.Subject) error {
	if len(in) == 0 || len(in) > MaxTesters {
		return &FieldError{"testers", fmt.Sprintf("Name 1 to %d people or groups who test.", MaxTesters)}
	}
	seen := map[string]bool{}
	for _, t := range in {
		var (
			id    *uuid.UUID
			query string
		)
		switch t.Type {
		case review.SubjectUser:
			id, query = t.UserID, `SELECT EXISTS (SELECT 1 FROM app_user WHERE id = $1 AND role IS NOT NULL AND is_active) AND perm_project_holds($2, $1, 'comment')`
		case review.SubjectGroup:
			id, query = t.GroupID, `SELECT EXISTS (SELECT 1 FROM groups WHERE id = $1) AND $2::uuid IS NOT NULL`
		default:
			return &FieldError{"testers", "A tester is a person or a group."}
		}
		if id == nil {
			return &FieldError{"testers", "Name each tester by the id of the person or the group."}
		}
		if key := string(t.Type) + id.String(); seen[key] {
			return &FieldError{"testers", "Name each tester once."}
		} else {
			seen[key] = true
		}
		var ok bool
		if err := tx.QueryRow(ctx, query, *id, projectID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return &FieldError{"testers", "A tester records results only when they may comment in the project. Pick them again."}
		}
	}
	return nil
}

func checkDue(due *time.Time, now time.Time) error {
	if due != nil && (due.Before(now) || due.After(now.Add(MaxDueAhead))) {
		return &FieldError{"dueAt", "Set the due date ahead, within a year, or leave it out."}
	}
	return nil
}

// Start starts an execution of a plan: its cases are pinned at once, at the revisions the baseline holds or their
// current ones, and its testers are told. The caller manages it.
func (s *Service) Start(ctx context.Context, actor uuid.UUID, projectKey string, in ExecutionInput) (*Execution, db.LSN, error) {
	title, err := checkText("title", in.Title, MaxTitleLength, true, fmt.Sprintf("Name the execution in at most %d characters, such as Release 2.0 on Linux.", MaxTitleLength))
	if err != nil {
		return nil, 0, err
	}
	version, err := checkText("version", in.Version, MaxVersionLength, false, fmt.Sprintf("Keep the version to %d characters.", MaxVersionLength))
	if err != nil {
		return nil, 0, err
	}
	configuration, err := checkText("configuration", in.Configuration, MaxConfigurationLength, false,
		fmt.Sprintf("Keep the configuration to %d characters.", MaxConfigurationLength))
	if err != nil {
		return nil, 0, err
	}
	if err := checkDue(in.DueAt, time.Now()); err != nil {
		return nil, 0, err
	}
	var out *Execution
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := editing(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if err := checkTesters(ctx, tx, p.ID, in.Testers); err != nil {
			return err
		}
		plan, err := readPlan(ctx, tx, actor, in.PlanID)
		if errors.Is(err, ErrNotFound) || (err == nil && plan.projectID != p.ID) {
			return &FieldError{"planId", "Pick a test plan of this project."}
		}
		if err != nil {
			return err
		}
		baselineName := ""
		held := map[uuid.UUID]bool{}
		if in.BaselineID != nil {
			if err := tx.QueryRow(ctx, `SELECT name FROM baseline WHERE id = $1 AND project_id = $2`, *in.BaselineID, p.ID).Scan(&baselineName); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return &FieldError{"baselineId", "Pick a baseline of this project, or none to run the cases as they are now."}
				}
				return err
			}
			rows, err := tx.Query(ctx, `SELECT requirement_id FROM baseline_item WHERE baseline_id = $1`, *in.BaselineID)
			if err != nil {
				return err
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			if err != nil {
				return err
			}
			for _, id := range ids {
				held[id] = true
			}
		}
		var (
			run      []uuid.UUID
			excluded = []Excluded{}
		)
		for _, c := range plan.Cases {
			switch {
			case c.Trashed:
				excluded = append(excluded, Excluded{Key: c.Key, Reason: "It is in the trash."})
			case in.BaselineID != nil && !held[c.ID]:
				excluded = append(excluded, Excluded{Key: c.Key, Reason: "The baseline does not hold it."})
			default:
				run = append(run, c.ID)
			}
		}
		if len(run) == 0 {
			return &FieldError{"planId", "The plan holds no test case to run here. Add cases to it, or pick another baseline."}
		}
		left, err := json.Marshal(excluded)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO test_execution (project_id, plan_id, plan_title, title, baseline_id, baseline_name, version, configuration, manager_id, due_at, excluded)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
			p.ID, plan.ID, plan.Title, title, in.BaselineID, baselineName, version, configuration, actor, in.DueAt, left).Scan(&id); err != nil {
			return refusal(err)
		}
		for _, t := range in.Testers {
			if _, err := tx.Exec(ctx, `INSERT INTO test_execution_tester (execution_id, subject_type, user_id, group_id) VALUES ($1, $2, $3, $4)`,
				id, t.Type, t.UserID, t.GroupID); err != nil {
				return refusal(err)
			}
		}
		var pinned int
		if err := tx.QueryRow(ctx, `SELECT test_execution_start($1, $2)`, id, run).Scan(&pinned); err != nil {
			return refusal(err)
		}
		if pinned > MaxExecutionCases {
			return &FieldError{"planId", fmt.Sprintf("The plan's cases run %d times with their parameters; an execution runs at most %d. Split the plan.", pinned, MaxExecutionCases)}
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTestExecutionStarted, TargetType: "test_execution", TargetID: &id, Actor: actor,
			Data: map[string]any{"project": p.Key, "title": title, "plan": plan.Title, "cases": len(run), "excluded": len(excluded)}}); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicStarted, map[string]any{"actorId": actor, "executionId": id, "projectId": p.ID}); err != nil {
			return err
		}
		e, err := readExecution(ctx, tx, actor, id, true)
		if e != nil {
			out = &e.Execution
		}
		return err
	})
	return out, lsn, err
}

// managing reads an open execution the caller manages, holding its row.
func managing(ctx context.Context, tx db.DBTX, actor, id uuid.UUID) (*executionRow, error) {
	e, err := readExecution(ctx, tx, actor, id, false)
	if err != nil {
		return nil, err
	}
	if e.State != Open {
		return nil, ErrClosed
	}
	if err := stillOpen(ctx, tx, e.projectID); err != nil {
		return nil, err
	}
	if !e.Can.Manage {
		return nil, ErrForbidden
	}
	_, err = tx.Exec(ctx, `SELECT 1 FROM test_execution WHERE id = $1 FOR UPDATE`, id)
	return e, err
}

// Update changes an open execution's title, version, configuration or due date.
func (s *Service) Update(ctx context.Context, actor, id uuid.UUID, in ExecutionPatch) (*Execution, db.LSN, error) {
	var out *Execution
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := managing(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		title, version, configuration, due := e.Title, e.Version, e.Configuration, e.DueAt
		if in.Title != nil {
			if title, err = checkText("title", *in.Title, MaxTitleLength, true, fmt.Sprintf("Name the execution in at most %d characters.", MaxTitleLength)); err != nil {
				return err
			}
		}
		if in.Version != nil {
			if version, err = checkText("version", *in.Version, MaxVersionLength, false, fmt.Sprintf("Keep the version to %d characters.", MaxVersionLength)); err != nil {
				return err
			}
		}
		if in.Configuration != nil {
			if configuration, err = checkText("configuration", *in.Configuration, MaxConfigurationLength, false,
				fmt.Sprintf("Keep the configuration to %d characters.", MaxConfigurationLength)); err != nil {
				return err
			}
		}
		if in.DueAt != nil {
			if err := checkDue(in.DueAt, time.Now()); err != nil {
				return err
			}
			due = in.DueAt
		}
		if _, err := tx.Exec(ctx, `UPDATE test_execution SET title = $2, version = $3, configuration = $4, due_at = $5,
			reminded_at = CASE WHEN due_at IS DISTINCT FROM $5 THEN NULL ELSE reminded_at END WHERE id = $1`,
			id, title, version, configuration, due); err != nil {
			return refusal(err)
		}
		r, err := readExecution(ctx, tx, actor, id, true)
		if r != nil {
			out = &r.Execution
		}
		return err
	})
	return out, lsn, err
}

// Close closes an execution, or aborts it so it counts for nothing; it never changes again.
func (s *Service) Close(ctx context.Context, actor, id uuid.UUID, in CloseInput) (*Execution, db.LSN, error) {
	comment, err := checkText("comment", in.Comment, MaxDescriptionLength, false, fmt.Sprintf("Keep the comment to %d characters.", MaxDescriptionLength))
	if err != nil {
		return nil, 0, err
	}
	state := Closed
	if in.Abort {
		state = Aborted
	}
	var out *Execution
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := managing(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE test_execution SET state = $2, close_comment = $3, closed_at = now(), closed_by = $4 WHERE id = $1`,
			id, state, comment, actor); err != nil {
			return refusal(err)
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTestExecutionClosed, TargetType: "test_execution", TargetID: &id, Actor: actor,
			Data: map[string]any{"project": e.ProjectKey, "title": e.Title, "state": state, "progress": e.Progress}}); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicClosed, map[string]any{"actorId": actor, "executionId": id, "projectId": e.projectID}); err != nil {
			return err
		}
		r, err := readExecution(ctx, tx, actor, id, true)
		if r != nil {
			out = &r.Execution
		}
		return err
	})
	return out, lsn, err
}

// keyOf is a case's key in its project.
func keyOf(projectKey string, number int) string { return requirement.Key(projectKey, number) }
