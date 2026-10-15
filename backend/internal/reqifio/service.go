package reqifio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attachment"
	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// TopicRequested hands a started import to the worker.
const TopicRequested = "reqif.requested"

var (
	// ErrNotFound is also the answer for an import the caller may not see.
	ErrNotFound = errors.New("reqif import not found")
	// ErrForbidden is an import the caller sees but is not theirs to map or start, or a project they do not run.
	ErrForbidden = errors.New("only the import's maker maps and starts it")
	// ErrStarted is a change to the choices of an import that already started.
	ErrStarted = errors.New("the import already started")
	// ErrProblems is a start while the file still has problems.
	ErrProblems = errors.New("the import has problems")
)

// Service moves requirements in and out of ReqIF files; the worker runs the imports it starts.
type Service struct {
	db           *db.Cluster
	requirements *requirement.Service
	attributes   *attribute.Service
	trace        *trace.Service
	files        *attachment.Service
	store        objectstore.Store
}

func NewService(cluster *db.Cluster, requirements *requirement.Service, attributes *attribute.Service, links *trace.Service,
	files *attachment.Service, store objectstore.Store) *Service {
	return &Service{db: cluster, requirements: requirements, attributes: attributes, trace: links, files: files, store: store}
}

// job is an import as the table keeps it, with what a client is never shown.
type job struct {
	Import
	projectID uuid.UUID
	createdBy uuid.UUID
	file      *File
}

const selectJob = `
SELECT j.id, p.key, j.file_name, j.format, j.state, j.choices, j.preview, j.done, j.total, j.imported, j.updated, j.unchanged,
       j.refused, j.error, COALESCE(u.name, ''), j.created_at, j.finished_at, j.project_id, j.created_by, j.content
FROM reqif_import j
JOIN project p ON p.id = j.project_id
LEFT JOIN app_user u ON u.id = j.created_by`

func scanJob(row pgx.Row) (*job, error) {
	var (
		j                                  job
		choices, preview, refused, content []byte
	)
	err := row.Scan(&j.ID, &j.ProjectKey, &j.FileName, &j.Format, &j.State, &choices, &preview, &j.Done, &j.Total, &j.Imported, &j.Updated,
		&j.Unchanged, &refused, &j.Error, &j.ByName, &j.Created, &j.Finished, &j.projectID, &j.createdBy, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	for _, part := range []struct {
		raw  []byte
		into any
	}{{choices, &j.Choices}, {preview, &j.Preview}, {refused, &j.Refused}, {content, &j.file}} {
		if err := json.Unmarshal(part.raw, part.into); err != nil {
			return nil, fmt.Errorf("read the import: %w", err)
		}
	}
	if j.Choices == nil {
		j.Choices = map[string]string{}
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

// Upload reads a file into an import of a project, and what it would make with nothing mapped yet. Nothing is
// written to the project; the files the rich text shows wait with the import until it runs.
func (s *Service) Upload(ctx context.Context, actor uuid.UUID, projectKey, fileName string, data []byte) (*Import, db.LSN, error) {
	f, format, embedded, err := Read(data)
	if err != nil {
		return nil, 0, err
	}
	if objectstore.IsUnavailable(s.store) {
		f.Images, embedded = nil, nil
	}
	var out *Import
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := running(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		w, err := loadWorld(ctx, tx, p)
		if err != nil {
			return err
		}
		preview := analyze(f, w, map[string]string{}).preview
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO reqif_import (project_id, created_by, file_name, format, content, preview, total)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
			p.ID, actor, fileName, format, f, preview, len(f.Objects)).Scan(&id); err != nil {
			return fmt.Errorf("keep the import: %w", err)
		}
		for _, e := range embedded {
			if _, err := tx.Exec(ctx, `INSERT INTO reqif_import_object (import_id, path, content_type, data) VALUES ($1, $2, $3, $4)`,
				id, e.Path, e.ContentType, e.Data); err != nil {
				return fmt.Errorf("keep the file %s: %w", e.Path, err)
			}
		}
		j, err := findJob(ctx, tx, id)
		if err == nil {
			out = &j.Import
		}
		return err
	})
	return out, lsn, err
}

// Get answers an import its maker, or an administrator of its project, may see.
func (s *Service) Get(ctx context.Context, actor, id uuid.UUID) (*Import, error) {
	var out *Import
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		j, err := findJob(ctx, tx, id)
		if err == nil {
			out = &j.Import
		}
		return err
	})
	return out, err
}

// choosing reads an import its maker changes, held so two tabs do not map it at once.
func choosing(ctx context.Context, tx db.DBTX, actor, id uuid.UUID) (*job, *world, error) {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM reqif_import WHERE id = $1 FOR UPDATE`, id); err != nil {
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
	w, err := loadWorld(ctx, tx, p)
	return j, w, err
}

// SetChoices maps the file's types, attributes and link types, and answers what that leaves.
func (s *Service) SetChoices(ctx context.Context, actor, id uuid.UUID, in ChoicesInput) (*Import, db.LSN, error) {
	if in.Choices == nil {
		in.Choices = map[string]string{}
	}
	var out *Import
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		j, w, err := choosing(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		preview := analyze(j.file, w, in.Choices).preview
		if _, err := tx.Exec(ctx, `UPDATE reqif_import SET choices = $2, preview = $3 WHERE id = $1`, id, in.Choices, preview); err != nil {
			return fmt.Errorf("map the import: %w", err)
		}
		if j, err = findJob(ctx, tx, id); err == nil {
			out = &j.Import
		}
		return err
	})
	return out, lsn, err
}

// Start hands an import without problems to the worker, reading the project again first, since it may have
// changed since the choices were made.
func (s *Service) Start(ctx context.Context, actor, id uuid.UUID) (*Import, db.LSN, error) {
	var (
		out      *Import
		problems bool
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		j, w, err := choosing(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		preview := analyze(j.file, w, j.Choices).preview
		if problems = len(preview.Problems) > 0; problems {
			// The problems found now are kept, so the next read shows why the start was refused.
			_, err := tx.Exec(ctx, `UPDATE reqif_import SET preview = $2 WHERE id = $1`, id, preview)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE reqif_import SET state = 'queued', preview = $2 WHERE id = $1`, id, preview); err != nil {
			return fmt.Errorf("start the import: %w", err)
		}
		if err := events.Emit(ctx, tx, TopicRequested, map[string]any{"actorId": actor, "importId": id, "projectId": j.projectID}); err != nil {
			return err
		}
		if j, err = findJob(ctx, tx, id); err == nil {
			out = &j.Import
		}
		return err
	})
	if err == nil && problems {
		return nil, lsn, ErrProblems
	}
	return out, lsn, err
}

// Cancel discards an import that has not started or has ended, and stops one that is queued or running after
// the object it is on; what it made stays.
func (s *Service) Cancel(ctx context.Context, actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		j, err := findJob(ctx, tx, id)
		if err != nil {
			return err
		}
		switch j.State {
		case StateQueued, StateRunning:
			_, err = tx.Exec(ctx, `UPDATE reqif_import SET state = 'cancelled', finished_at = $2 WHERE id = $1`, id, time.Now())
		default:
			var tag interface{ RowsAffected() int64 }
			tag, err = tx.Exec(ctx, `DELETE FROM reqif_import WHERE id = $1`, id)
			if err == nil && tag.RowsAffected() == 0 {
				return ErrNotFound
			}
		}
		return err
	})
}
