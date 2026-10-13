package attachment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Service keeps the rows and hands the bytes to the store. Whether the actor may see or change a file is
// decided on its requirement.
type Service struct {
	db    *db.Cluster
	store objectstore.Store
	log   *slog.Logger
	// MaxSize is the largest file an upload takes.
	MaxSize int64
}

// NewService makes the service and has it told of every requirement copy, so the copy's files come along.
func NewService(cluster *db.Cluster, store objectstore.Store, requirements *requirement.Service) *Service {
	s := &Service{db: cluster, store: store, log: slog.Default(), MaxSize: DefaultMaxSize}
	if requirements != nil {
		requirements.ObserveCopies(s)
	}
	return s
}

// WithMaxSize sets what an upload may weigh.
func (s *Service) WithMaxSize(limit int64) *Service {
	if limit > 0 {
		s.MaxSize = limit
	}
	return s
}

// WithLogger sets where objects left for the reaper are noted.
func (s *Service) WithLogger(log *slog.Logger) *Service {
	if log != nil {
		s.log = log
	}
	return s
}

const selectAttachment = `
SELECT a.id, a.requirement_id, a.file_name, a.content_type, a.size_bytes, a.width, a.height,
       COALESCE(u.name, ''), a.created_at, attachment_referenced(a.requirement_id, a.id), a.object_key
FROM attachment a
LEFT JOIN app_user u ON u.id = a.uploaded_by`

// stored is a row with where its bytes are, which a client is never shown.
type stored struct {
	Attachment
	objectKey string
}

func scan(row pgx.Row) (*stored, error) {
	var a stored
	err := row.Scan(&a.ID, &a.RequirementID, &a.FileName, &a.ContentType, &a.Size, &a.Width, &a.Height,
		&a.UploadedByName, &a.CreatedAt, &a.Referenced, &a.objectKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// changeable says why the reader may not change the files of a requirement, or nil when they may.
func changeable(e *requirement.Endpoint) error {
	switch {
	case e.Trashed:
		return requirement.ErrTrashed
	case e.Archived:
		return project.ErrArchived
	case !e.Linkable:
		return ErrForbidden
	}
	return nil
}

// List is the files on a requirement, the latest first, without those taken off the list.
func (s *Service) List(ctx context.Context, actor uuid.UUID, key string) ([]Attachment, error) {
	out := []Attachment{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := requirement.FindEndpoint(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectAttachment+`
			WHERE a.org_id = current_org_id() AND a.requirement_id = $1 AND a.deleted_at IS NULL
			ORDER BY a.created_at DESC, a.id DESC`, e.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, a.Attachment)
		}
		return rows.Err()
	})
	return out, err
}

// UploadInput is one file as it arrived.
type UploadInput struct {
	FileName    string
	ContentType string
	Body        io.Reader
}

// Upload stores a file on a requirement. The bytes go to the store inside the transaction that writes the row,
// so a store that refuses leaves no row.
func (s *Service) Upload(ctx context.Context, actor uuid.UUID, key string, in UploadInput) (*Attachment, db.LSN, error) {
	if objectstore.IsUnavailable(s.store) {
		return nil, 0, objectstore.ErrUnavailable
	}
	name := objectstore.CleanName(in.FileName)
	data, err := s.readWhole(in.Body)
	if err != nil {
		return nil, 0, err
	}
	contentType := contentTypeFor(in.ContentType, data)
	width, height := dimensions(contentType, data)

	var created *Attachment
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := requirement.FindEndpoint(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		if err := changeable(e); err != nil {
			return err
		}
		var id uuid.UUID
		var objectKey string
		if err := tx.QueryRow(ctx, `
			INSERT INTO attachment (org_id, requirement_id, uploaded_by, file_name, content_type, size_bytes, width, height)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7)
			RETURNING id, object_key`,
			e.ID, actor, name, contentType, len(data), width, height).Scan(&id, &objectKey); err != nil {
			return fmt.Errorf("record the file: %w", err)
		}
		if err := s.store.Put(ctx, objectKey, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
			return err
		}
		if err := requirement.LogChange(ctx, tx, actor, e.ID, requirement.ChangeAttachment, "", name); err != nil {
			return err
		}
		found, err := scan(tx.QueryRow(ctx, selectAttachment+` WHERE a.id = $1`, id))
		if err == nil {
			created = &found.Attachment
		}
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return created, lsn, nil
}

// readWhole reads an upload whole, as Stator does, so the limit holds before anything is written and the store
// is handed a body it can rewind.
func (s *Service) readWhole(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, s.MaxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(data)) > s.MaxSize {
		return nil, &TooLargeError{Limit: s.MaxSize}
	}
	if len(data) == 0 {
		return nil, ErrEmpty
	}
	return data, nil
}

// Located is a file its reader may read, found but not fetched yet.
type Located struct {
	Attachment
	key   string
	store objectstore.Store
}

// Bytes fetches the whole file. The caller closes the reader.
func (l *Located) Bytes(ctx context.Context) (io.ReadCloser, error) {
	return l.store.Get(ctx, l.key)
}

// Locate finds a file the actor may read, on a requirement they may view. One taken off the list is still
// found, since a revision that shows it still shows it.
func (s *Service) Locate(ctx context.Context, actor uuid.UUID, id uuid.UUID) (*Located, error) {
	var found *stored
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, err = scan(tx.QueryRow(ctx, selectAttachment+` WHERE a.org_id = current_org_id() AND a.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &Located{Attachment: found.Attachment, key: found.objectKey, store: s.store}, nil
}

// Delete takes a file off its requirement: off the list when a revision shows it, so history keeps it, else for
// good, the row leaving a tombstone and the object going after the commit or with the reaper.
func (s *Service) Delete(ctx context.Context, actor uuid.UUID, id uuid.UUID) (db.LSN, error) {
	var gone string
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var requirementID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT requirement_id FROM attachment WHERE org_id = current_org_id() AND id = $1 AND deleted_at IS NULL`,
			id).Scan(&requirementID); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		e, err := requirement.FindEndpointByID(ctx, tx, actor, requirementID)
		if errors.Is(err, requirement.ErrNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := changeable(e); err != nil {
			return err
		}
		// Every revision is written under this lock, so whether one shows the file holds until the commit.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM requirement WHERE id = $1 FOR UPDATE`, e.ID); err != nil {
			return fmt.Errorf("hold %s: %w", e.Key, err)
		}
		found, err := scan(tx.QueryRow(ctx, selectAttachment+` WHERE a.id = $1 AND a.deleted_at IS NULL`, id))
		if err != nil {
			return err
		}
		if found.Referenced {
			_, err = tx.Exec(ctx, `UPDATE attachment SET deleted_at = now(), deleted_by = $2 WHERE id = $1`, id, actor)
		} else {
			_, err = tx.Exec(ctx, `DELETE FROM attachment WHERE id = $1`, id)
			gone = found.objectKey
		}
		if err != nil {
			return fmt.Errorf("delete %s: %w", found.FileName, err)
		}
		return requirement.LogChange(ctx, tx, actor, e.ID, requirement.ChangeAttachment, found.FileName, "")
	})
	if err != nil {
		return 0, err
	}
	if gone != "" {
		if err := s.reap(ctx, gone, s.db.Write); err != nil {
			s.log.Warn("attachment object left for the reaper", "key", gone, "error", err)
		}
	}
	return lsn, nil
}
