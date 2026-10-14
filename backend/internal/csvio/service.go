package csvio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// TopicRequested hands a started import to the worker.
const TopicRequested = "import.requested"

var (
	// ErrNotFound is also the answer for an import the caller may not see.
	ErrNotFound = errors.New("import not found")
	// ErrForbidden is an import the caller sees but is not theirs to map or start, or a project they do not run.
	ErrForbidden = errors.New("only the import's maker maps and starts it")
	// ErrStarted is a change to the mapping of an import that already started.
	ErrStarted = errors.New("the import already started")
	// ErrProblems is a start while the file still has problems.
	ErrProblems = errors.New("the import has problems")
)

// Service moves requirements in and out of CSV files; the worker runs the imports it starts.
type Service struct {
	db           *db.Cluster
	requirements *requirement.Service
	trace        *trace.Service
	search       *search.Service
}

func NewService(cluster *db.Cluster, requirements *requirement.Service, links *trace.Service, finder *search.Service) *Service {
	return &Service{db: cluster, requirements: requirements, trace: links, search: finder}
}

// job is an import as the table keeps it, with what a client is never shown.
type job struct {
	Import
	projectID uuid.UUID
	createdBy uuid.UUID
	headers   []string
	rows      []Row
	choices   Choices
}

const selectJob = `
SELECT j.id, p.key, j.file_name, j.state, j.mapping, j.choices, j.preview, j.done, j.total, j.imported, j.updated, j.refused,
       j.error, COALESCE(u.name, ''), j.created_at, j.finished_at, j.project_id, j.created_by, j.headers, j.rows
FROM import_job j
JOIN project p ON p.id = j.project_id
LEFT JOIN app_user u ON u.id = j.created_by`

func scanJob(row pgx.Row) (*job, error) {
	var (
		j                                                 job
		mapping, choices, preview, refused, headers, rows []byte
	)
	err := row.Scan(&j.ID, &j.ProjectKey, &j.FileName, &j.State, &mapping, &choices, &preview, &j.Done, &j.Total, &j.Imported, &j.Updated, &refused,
		&j.Error, &j.ByName, &j.Created, &j.Finished, &j.projectID, &j.createdBy, &headers, &rows)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	for _, part := range []struct {
		raw  []byte
		into any
	}{{mapping, &j.Mapping}, {choices, &j.choices}, {preview, &j.Preview}, {refused, &j.Refused}, {headers, &j.headers}, {rows, &j.rows}} {
		if err := json.Unmarshal(part.raw, part.into); err != nil {
			return nil, fmt.Errorf("read the import: %w", err)
		}
	}
	if j.Mapping == nil {
		j.Mapping = Mapping{}
	}
	if j.Refused == nil {
		j.Refused = []Refusal{}
	}
	return &j, nil
}

func findJob(ctx context.Context, tx db.DBTX, id uuid.UUID) (*job, error) {
	return scanJob(tx.QueryRow(ctx, selectJob+` WHERE j.id = $1`, id))
}

// running reads the project an import goes into as the actor, who must run it.
func running(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectKey string) (*project.Project, error) {
	p, err := project.Find(ctx, tx, actor, projectKey)
	if err != nil {
		return nil, err
	}
	if p.ArchivedAt != nil {
		return nil, project.ErrArchived
	}
	if !p.Can.Administer {
		return nil, ErrForbidden
	}
	return p, nil
}

// withTargets fills what a client is told besides the row: the targets a column may fill.
func withTargets(ctx context.Context, tx db.DBTX, j *job) (*Import, error) {
	defs, err := attribute.Defs(ctx, tx, j.projectID)
	if err != nil {
		return nil, err
	}
	var live []attribute.Def
	for _, d := range defs {
		if !d.Retired {
			live = append(live, d)
		}
	}
	out := j.Import
	out.Targets = targetsOf(live)
	return &out, nil
}

// Upload reads a file into an import of a project, with a mapping guessed from its headers. Nothing is written
// to the project yet.
func (s *Service) Upload(ctx context.Context, actor uuid.UUID, projectKey, fileName string, data []byte) (*Import, db.LSN, error) {
	headers, rows, err := Read(data)
	if err != nil {
		return nil, 0, err
	}
	var out *Import
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := running(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		w, err := loadWorld(ctx, tx, actor, p)
		if err != nil {
			return err
		}
		mapping := Guess(headers, w.defs)
		preview := w.analyze(headers, rows, mapping, Choices{}).preview
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO import_job (project_id, created_by, file_name, headers, rows, mapping, preview, total)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			p.ID, actor, fileName, headers, rows, mapping, preview, len(rows)).Scan(&id); err != nil {
			return fmt.Errorf("keep the import: %w", err)
		}
		j, err := findJob(ctx, tx, id)
		if err != nil {
			return err
		}
		out, err = withTargets(ctx, tx, j)
		return err
	})
	return out, lsn, err
}

// Get answers an import its maker, or an administrator of its project, may see.
func (s *Service) Get(ctx context.Context, actor, id uuid.UUID) (*Import, error) {
	var out *Import
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		j, err := findJob(ctx, tx, id)
		if err != nil {
			return err
		}
		out, err = withTargets(ctx, tx, j)
		return err
	})
	return out, err
}

// mapping reads an import its maker changes, held so two tabs do not map it at once.
func mapping(ctx context.Context, tx db.DBTX, actor, id uuid.UUID) (*job, *world, error) {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM import_job WHERE id = $1 FOR UPDATE`, id); err != nil {
		return nil, nil, err
	}
	j, err := findJob(ctx, tx, id)
	if err != nil {
		return nil, nil, err
	}
	if j.createdBy != actor {
		return nil, nil, ErrForbidden
	}
	if j.State != StateUploaded {
		return nil, nil, ErrStarted
	}
	p, err := running(ctx, tx, actor, j.ProjectKey)
	if err != nil {
		return nil, nil, err
	}
	w, err := loadWorld(ctx, tx, actor, p)
	return j, w, err
}

// SetMapping sets how the file's columns, words and people are read, and answers the problems that leaves.
func (s *Service) SetMapping(ctx context.Context, actor, id uuid.UUID, in MappingInput) (*Import, db.LSN, error) {
	if in.Mapping == nil {
		in.Mapping = Mapping{}
	}
	choices := Choices{Values: in.Values, People: in.People}
	var out *Import
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		j, w, err := mapping(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		preview := w.analyze(j.headers, j.rows, in.Mapping, choices).preview
		if _, err := tx.Exec(ctx, `UPDATE import_job SET mapping = $2, choices = $3, preview = $4 WHERE id = $1`,
			id, in.Mapping, choices, preview); err != nil {
			return fmt.Errorf("map the import: %w", err)
		}
		if j, err = findJob(ctx, tx, id); err != nil {
			return err
		}
		out, err = withTargets(ctx, tx, j)
		return err
	})
	return out, lsn, err
}

// Start hands an import without problems to the worker, reading the project again first, since it may have
// changed since the mapping.
func (s *Service) Start(ctx context.Context, actor, id uuid.UUID) (*Import, db.LSN, error) {
	var (
		out      *Import
		problems bool
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		j, w, err := mapping(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		preview := w.analyze(j.headers, j.rows, j.Mapping, j.choices).preview
		if problems = len(preview.Problems) > 0; problems {
			// The problems found now are kept, so the next read shows why the start was refused.
			_, err := tx.Exec(ctx, `UPDATE import_job SET preview = $2 WHERE id = $1`, id, preview)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE import_job SET state = 'queued', preview = $2 WHERE id = $1`, id, preview); err != nil {
			return fmt.Errorf("start the import: %w", err)
		}
		if err := events.Emit(ctx, tx, TopicRequested, map[string]any{"actorId": actor, "importId": id, "projectId": j.projectID}); err != nil {
			return err
		}
		if j, err = findJob(ctx, tx, id); err != nil {
			return err
		}
		out, err = withTargets(ctx, tx, j)
		return err
	})
	if err == nil && problems {
		return nil, lsn, ErrProblems
	}
	return out, lsn, err
}

// Cancel discards an import that has not started or has ended, and stops one that is queued or running after
// the row it is on; what it made stays.
func (s *Service) Cancel(ctx context.Context, actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		j, err := findJob(ctx, tx, id)
		if err != nil {
			return err
		}
		switch j.State {
		case StateQueued, StateRunning:
			_, err = tx.Exec(ctx, `UPDATE import_job SET state = 'cancelled', finished_at = $2 WHERE id = $1`, id, time.Now())
		default:
			var tag interface{ RowsAffected() int64 }
			tag, err = tx.Exec(ctx, `DELETE FROM import_job WHERE id = $1`, id)
			if err == nil && tag.RowsAffected() == 0 {
				return ErrNotFound
			}
		}
		return err
	})
}
