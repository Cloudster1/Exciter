package testresult

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/via"
)

// The events of test runs: an upload handed to the worker, a run done, and a counting branch named, the last two
// for coverage to be judged again.
const (
	TopicRequested     = "test_run.requested"
	TopicFinished      = "test_run.finished"
	TopicSettingsSaved = "evidence_settings.saved"
)

var (
	// ErrNotFound is also the answer for a run the caller may not read.
	ErrNotFound = errors.New("test run not found")
	// ErrForbidden is an upload to a project the caller does not edit, or a counting branch they do not keep.
	ErrForbidden = errors.New("the project's editors upload test results")
)

// FieldError is a form field or a setting that does not hold, with the sentence that says how to fix it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

var commitShape = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// Service keeps the test results CI uploads; the worker reads each upload as its uploader.
type Service struct {
	db      *db.Cluster
	commits CommitFinder
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// CheckForm trims a run's form fields and says which does not hold.
func CheckForm(f *RunForm) error {
	f.Repository, f.Commit, f.Branch = strings.TrimSpace(f.Repository), strings.ToLower(strings.TrimSpace(f.Commit)), strings.TrimSpace(f.Branch)
	f.Job, f.RunURL = strings.TrimSpace(f.Job), strings.TrimSpace(f.RunURL)
	switch {
	case f.Repository == "" || utf8.RuneCountInString(f.Repository) > MaxNameLength:
		return &FieldError{"repository", "Name the repository, such as acme/checkout, in at most 200 characters."}
	case !commitShape.MatchString(f.Commit):
		return &FieldError{"commit", "Give the commit's SHA, 7 to 64 hex digits."}
	case f.Branch == "" || utf8.RuneCountInString(f.Branch) > MaxNameLength:
		return &FieldError{"branch", "Name the branch the run tested, in at most 200 characters."}
	case utf8.RuneCountInString(f.Job) > MaxNameLength:
		return &FieldError{"job", "Name the job in at most 200 characters."}
	case f.RunURL != "" && !WebAddress(f.RunURL):
		return &FieldError{"runUrl", "Give the run's address as an http or https link."}
	}
	return nil
}

// WebAddress says an address is an http or https link of at most MaxURLLength, the only ones a page links to.
func WebAddress(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && len(s) <= MaxURLLength &&
		!strings.ContainsAny(s, " \t\r\n")
}

// editing reads the project a run goes into as the actor, who must edit it.
func editing(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectKey string) (*project.Project, error) {
	p, err := project.Find(ctx, tx, actor, projectKey)
	if err != nil {
		return nil, err
	}
	if p.ArchivedAt != nil {
		return nil, project.ErrArchived
	}
	if !p.Can.Edit {
		return nil, ErrForbidden
	}
	return p, nil
}

// requested is what the worker is handed: the run, and who it acts as, with the projects their token reached.
type requested struct {
	ActorID  uuid.UUID   `json:"actorId"`
	RunID    uuid.UUID   `json:"runId"`
	Projects []uuid.UUID `json:"projects,omitempty"`
}

// Upload reads the reports of a run and hands them to the worker; nothing lands on a requirement until it is done.
func (s *Service) Upload(ctx context.Context, actor uuid.UUID, projectKey string, form RunForm, files []File) (*Run, db.LSN, error) {
	if err := CheckForm(&form); err != nil {
		return nil, 0, err
	}
	if len(files) == 0 {
		return nil, 0, &FieldError{"file", "Upload one JUnit XML file or more as parts named file."}
	}
	if len(files) > MaxRunFiles {
		return nil, 0, &FieldError{"file", fmt.Sprintf("Upload at most %d files in one run.", MaxRunFiles)}
	}
	cases, err := ReadFiles(files, DefaultVerifyWords)
	switch {
	case errors.Is(err, ErrNotJUnit):
		return nil, 0, &FieldError{"file", FileName(err) + " is not a JUnit XML file. Upload the XML report your test runner writes."}
	case errors.Is(err, ErrTooManyCases):
		return nil, 0, &FieldError{"file", fmt.Sprintf("A run holds at most %d test cases. Upload the reports in more than one run, one per job.", MaxRunCases)}
	case err != nil:
		return nil, 0, err
	}
	if cases == nil {
		cases = []Case{}
	}
	body, err := json.Marshal(cases)
	if err != nil {
		return nil, 0, err
	}
	var out *Run
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := editing(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO test_run (project_id, repository, commit_sha, branch, job, run_url, cases, uploaded_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			p.ID, form.Repository, form.Commit, form.Branch, form.Job, form.RunURL, body, actor).Scan(&id); err != nil {
			return fmt.Errorf("keep the run: %w", err)
		}
		projects, _ := db.ProjectsFrom(ctx)
		if err := events.Emit(ctx, tx, TopicRequested, requested{ActorID: actor, RunID: id, Projects: projects}); err != nil {
			return err
		}
		out, err = findRun(ctx, tx, id)
		return err
	})
	return out, lsn, err
}

const selectRun = `
SELECT t.id, p.key, t.repository, t.commit_sha, t.branch, t.job, t.run_url, t.state, t.cases_read, t.matched,
       t.passed, t.failed, t.skipped, t.errored, t.reported, t.unreported, t.error, t.uploaded_by, COALESCE(u.name, ''),
       ` + `CASE WHEN t.via_kind IS NULL THEN NULL ELSE jsonb_build_object('kind', t.via_kind, 'name', t.via_name) END` + `,
       t.uploaded_at, t.finished_at, t.project_id
FROM test_run t
JOIN project p ON p.id = t.project_id
LEFT JOIN app_user u ON u.id = t.uploaded_by`

// run is a run as the table keeps it, with whose and which project's it is.
type run struct {
	Run
	projectID uuid.UUID
	uploader  *uuid.UUID
}

func findRun(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Run, error) {
	r, err := scanRun(tx.QueryRow(ctx, selectRun+` WHERE t.id = $1`, id))
	if err != nil {
		return nil, err
	}
	return &r.Run, nil
}

func scanRun(row pgx.Row) (*run, error) {
	var (
		r        run
		reported []byte
		by       *uuid.UUID
		v        *via.Via
	)
	err := row.Scan(&r.ID, &r.ProjectKey, &r.Repository, &r.Commit, &r.Branch, &r.Job, &r.RunURL, &r.State, &r.Cases, &r.Matched,
		&r.Counts.Passed, &r.Counts.Failed, &r.Counts.Skipped, &r.Counts.Errored, &reported, &r.Unreported, &r.Error, &by,
		&r.UploadedBy.Name, &v, &r.UploadedAt, &r.FinishedAt, &r.projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if by != nil {
		r.UploadedBy.ID = *by
	}
	r.uploader, r.Via = by, v
	if err := json.Unmarshal(reported, &r.Reported); err != nil {
		return nil, fmt.Errorf("read what the run reported: %w", err)
	}
	if r.Reported == nil {
		r.Reported = []Reported{}
	}
	return &r, nil
}

// Get is one run, its uploader's and its project's administrators' to read; from the primary, so a pipeline
// waiting on it sees what the worker wrote.
func (s *Service) Get(ctx context.Context, actor, id uuid.UUID) (*Run, error) {
	var out *Run
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := scanRun(tx.QueryRow(ctx, selectRun+` WHERE t.id = $1`, id))
		if err != nil {
			return err
		}
		if r.uploader == nil || *r.uploader != actor {
			var administer bool
			if err := tx.QueryRow(ctx, `SELECT perm_project_holds($1, $2, 'administer')`, r.projectID, actor).Scan(&administer); err != nil {
				return err
			}
			if !administer {
				return ErrNotFound
			}
		}
		out = &r.Run
		return nil
	})
	return out, err
}

// Settings is a project's counting branch, as whoever views it reads it.
func (s *Service) Settings(ctx context.Context, actor uuid.UUID, projectKey string) (*Settings, error) {
	var out Settings
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		out, err = settingsOf(ctx, tx, p.ID)
		return err
	})
	return &out, err
}

func settingsOf(ctx context.Context, tx db.DBTX, projectID uuid.UUID) (Settings, error) {
	out := Settings{CountingBranch: DefaultCountingBranch}
	err := tx.QueryRow(ctx, `SELECT counting_branch, updated_at FROM evidence_settings WHERE project_id = $1`, projectID).
		Scan(&out.CountingBranch, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return out, err
}

// SaveSettings names a project's counting branch; its administrators'.
func (s *Service) SaveSettings(ctx context.Context, actor uuid.UUID, projectKey string, in SettingsInput) (*Settings, db.LSN, error) {
	branch := strings.TrimSpace(in.CountingBranch)
	if branch == "" || utf8.RuneCountInString(branch) > MaxNameLength {
		return nil, 0, &FieldError{"countingBranch", "Name the branch whose evidence counts, such as main, in at most 200 characters."}
	}
	var out Settings
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		switch {
		case p.ArchivedAt != nil:
			return project.ErrArchived
		case !p.Can.Administer:
			return ErrForbidden
		}
		before, err := settingsOf(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO evidence_settings (project_id, counting_branch, updated_by) VALUES ($1, $2, $3)
			ON CONFLICT (project_id) DO UPDATE SET counting_branch = EXCLUDED.counting_branch, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			p.ID, branch, actor); err != nil {
			return fmt.Errorf("name the counting branch: %w", err)
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionEvidenceSettingsSaved, TargetType: "project", TargetID: &p.ID, Actor: actor,
			Data: map[string]any{"from": before.CountingBranch, "to": branch}}); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicSettingsSaved, map[string]any{"actorId": actor, "projectId": p.ID}); err != nil {
			return err
		}
		out, err = settingsOf(ctx, tx, p.ID)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	return &out, lsn, nil
}
