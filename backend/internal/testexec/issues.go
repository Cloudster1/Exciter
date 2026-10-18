package testexec

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// IssueFiler files an issue in Armature with its own words as the caller; armature.Service is one.
type IssueFiler interface {
	FileDraft(ctx context.Context, d armature.IssueDraft) (string, []string, error)
}

var (
	// ErrNoIssues is a server that files no issues, having no Armature.
	ErrNoIssues = errors.New("Armature is not set up")
	// ErrNotFailed refuses filing an issue for a case iteration that did not fail.
	ErrNotFailed = errors.New("the case iteration did not fail")
)

func issues(ctx context.Context, tx db.DBTX, execution, id uuid.UUID, iteration int) ([]Issue, error) {
	rows, err := tx.Query(ctx, `
		SELECT i.issue_key, i.step, i.created_by, COALESCE(u.name, ''), i.created_at FROM test_execution_issue i
		LEFT JOIN app_user u ON u.id = i.created_by
		WHERE i.execution_id = $1 AND i.requirement_id = $2 AND i.iteration = $3 ORDER BY i.created_at`, execution, id, iteration)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Issue, error) {
		var (
			i  Issue
			by *uuid.UUID
		)
		err := row.Scan(&i.Key, &i.Step, &by, &i.CreatedBy.Name, &i.CreatedAt)
		if by != nil {
			i.CreatedBy.ID = *by
		}
		return i, err
	})
	if out == nil {
		out = []Issue{}
	}
	return out, err
}

// verified are the keys of the live requirements a case verifies.
func verified(ctx context.Context, tx db.DBTX, id uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT p.key, r.number FROM trace_link l
		JOIN link_type lt ON lt.id = l.type_id AND lt.counts = 'verification'
		JOIN requirement r ON r.id = l.target_id AND r.trashed_at IS NULL
		JOIN project p ON p.id = r.project_id
		WHERE l.source_id = $1 AND l.deleted_at IS NULL ORDER BY p.key, r.number`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (string, error) {
		var (
			key    string
			number int
		)
		err := row.Scan(&key, &number)
		return requirement.Key(key, number), err
	})
}

func plain(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	root, err := document.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(document.PlainText(root)), " ")
}

// failureDraft is the issue a failed case iteration is filed as: which case failed where, the step, what was expected
// and what happened, and a link back to the run.
func failureDraft(e *executionRow, c *Case, in FailureIssueInput, keys []string, link string) armature.IssueDraft {
	summary := strings.TrimSpace(in.Summary)
	if summary == "" {
		summary = fmt.Sprintf("%s %s failed in %s", c.Run.Case.Key, c.Run.Case.Title, e.Title)
	}
	var paragraphs []string
	paragraphs = append(paragraphs, fmt.Sprintf("%s failed in the test execution %s, at revision %d, iteration %d.", c.Run.Case.Key, e.Title, c.Run.Revision, c.Run.Iteration))
	if e.Version != "" || e.Configuration != "" {
		paragraphs = append(paragraphs, fmt.Sprintf("Version: %s. Configuration: %s.", e.Version, e.Configuration))
	}
	if in.Step != nil && *in.Step >= 1 && *in.Step <= len(c.Steps) {
		s := c.Steps[*in.Step-1]
		paragraphs = append(paragraphs, fmt.Sprintf("Step %d: %s", *in.Step, plain(s.Action)))
		if expected := plain(s.Expected); expected != "" {
			paragraphs = append(paragraphs, "Expected: "+expected)
		}
		for _, r := range c.Results {
			if r.Step == *in.Step && r.Note != "" {
				paragraphs = append(paragraphs, "Actual: "+r.Note)
			}
		}
	}
	if c.Run.Note != "" {
		paragraphs = append(paragraphs, c.Run.Note)
	}
	return armature.IssueDraft{
		ArmatureProject: in.ArmatureProject, Summary: summary, Paragraphs: paragraphs,
		Link: link, LinkText: "The run in Exciter", RequirementKeys: append([]string{c.Run.Case.Key}, keys...), Relation: armature.Relates,
	}
}

// FileIssue files a failed case iteration as an issue in Armature with the caller's token, linked with relates to the
// case and the requirements it verifies that the caller may link.
func (s *Service) FileIssue(ctx context.Context, actor, execution uuid.UUID, key string, in FailureIssueInput) (*Issue, db.LSN, error) {
	if s.issues == nil {
		return nil, 0, ErrNoIssues
	}
	iteration := max(in.Iteration, 1)
	var (
		draft armature.IssueDraft
		id    uuid.UUID
		e     *executionRow
	)
	if err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if e, id, err = recording(ctx, tx, actor, execution, key); err != nil {
			return err
		}
		c, err := readCase(ctx, tx, execution, id, iteration)
		if err != nil {
			return err
		}
		if c.Run.Outcome == nil || *c.Run.Outcome != Failed {
			return ErrNotFailed
		}
		keys, err := verified(ctx, tx, id)
		if err != nil {
			return err
		}
		draft = failureDraft(e, c, in, keys, fmt.Sprintf("%s/p/%s/tests/executions/%s/run/%s", s.appURL, e.ProjectKey, execution, c.Run.Case.Key))
		return nil
	}); err != nil {
		return nil, 0, err
	}
	if in.TypeID != nil && strings.TrimSpace(*in.TypeID) != "" {
		typeID, err := uuid.Parse(strings.TrimSpace(*in.TypeID))
		if err != nil {
			return nil, 0, &FieldError{"typeId", "Pick the issue's type from the list Armature offers."}
		}
		draft.TypeID = &typeID
	}
	issueKey, linked, err := s.issues.FileDraft(ctx, draft)
	if err != nil {
		return nil, 0, err
	}
	var out *Issue
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `INSERT INTO test_execution_issue (execution_id, requirement_id, iteration, step, issue_key, created_by)
			VALUES ($1, $2, $3, $4, $5, $6)`, execution, id, iteration, in.Step, issueKey, actor); err != nil {
			return refusal(err)
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTestExecutionIssueFiled, TargetType: "test_execution", TargetID: &execution, Actor: actor,
			Data: map[string]any{"project": e.ProjectKey, "case": key, "iteration": iteration, "issue": issueKey, "linked": linked}}); err != nil {
			return err
		}
		found, err := issues(ctx, tx, execution, id, iteration)
		for i := range found {
			if found[i].Key == issueKey {
				out = &found[i]
			}
		}
		return err
	})
	return out, lsn, err
}
