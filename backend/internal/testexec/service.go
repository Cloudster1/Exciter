package testexec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Topics an execution writes to the outbox: who is told, and what coverage hears of (#252).
const (
	TopicStarted  = "test_execution.started"
	TopicReminder = "test_execution.reminder"
	TopicClosed   = "test_execution.closed"
	TopicRecorded = "test_execution.recorded"
)

var (
	// ErrNotFound is a plan, an execution, a case or a file that is not there, or of a project the caller may not view.
	ErrNotFound = errors.New("not found")
	// ErrForbidden refuses a change to somebody whose right it is not.
	ErrForbidden = errors.New("not the caller's to change")
	// ErrClosed refuses any change of an execution that is closed or aborted.
	ErrClosed = errors.New("the test execution is closed")
)

// FieldError is input refused on one field, with the sentence to show beside it.
type FieldError struct{ Field, Message string }

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Service keeps test plans and executions; its object store keeps the files recorded in them.
type Service struct {
	db     *db.Cluster
	store  objectstore.Store
	issues IssueFiler
	// appURL is where the web client is served, for the link an issue gives back to its run.
	appURL string
}

func NewService(cluster *db.Cluster, store objectstore.Store) *Service {
	return &Service{db: cluster, store: store}
}

// WithIssues lets the service file failures in Armature, each linking back to its run at appURL.
func (s *Service) WithIssues(f IssueFiler, appURL string) *Service {
	s.issues, s.appURL = f, strings.TrimRight(appURL, "/")
	return s
}

func checkText(field, value string, max int, required bool, sentence string) (string, error) {
	value = strings.TrimSpace(value)
	if (required && value == "") || utf8.RuneCountInString(value) > max {
		return "", &FieldError{field, sentence}
	}
	return value, nil
}

// editing reads a project the caller edits while it is open.
func editing(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectKey string) (*project.Project, error) {
	p, err := project.Find(ctx, tx, actor, projectKey)
	switch {
	case err != nil:
		return nil, err
	case p.ArchivedAt != nil:
		return nil, project.ErrArchived
	case !p.Can.Edit:
		return nil, ErrForbidden
	}
	return p, nil
}

// caseIDs resolves the keys of test cases of a project, in order, each once; a key that names nothing of the
// project, or what is no live test case, is refused by name.
func caseIDs(ctx context.Context, tx db.DBTX, projectID uuid.UUID, projectKey string, keys []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(keys))
	seen := map[uuid.UUID]bool{}
	for _, key := range keys {
		pk, number, ok := requirement.ParseKey(strings.ToUpper(strings.TrimSpace(key)))
		if !ok || !strings.EqualFold(pk, projectKey) {
			return nil, &FieldError{"cases", fmt.Sprintf("%s is not a key of a requirement of %s. Pick test cases of this project.", key, projectKey)}
		}
		var (
			id   uuid.UUID
			test bool
		)
		err := tx.QueryRow(ctx, `
			SELECT r.id, r.kind = 'requirement' AND r.trashed_at IS NULL AND COALESCE(t.role, '') = 'testCase'
			FROM requirement r LEFT JOIN requirement_type t ON t.id = r.type_id
			WHERE r.project_id = $1 AND r.number = $2`, projectID, number).Scan(&id, &test)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !test) {
			return nil, &FieldError{"cases", fmt.Sprintf("%s is no test case of this project. Pick requirements of a type marked as test cases.", key)}
		}
		if err != nil {
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// refusal reads what the database refused into the service's errors.
func refusal(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "42501":
			return ErrForbidden
		case pgErr.ConstraintName == "test_execution_closed":
			return ErrClosed
		case pgErr.ConstraintName == "test_plan_case_kept" || pgErr.ConstraintName == "test_execution_start":
			return &FieldError{"cases", "A plan and an execution hold live test cases of their own project. Pick them again."}
		}
	}
	return err
}
