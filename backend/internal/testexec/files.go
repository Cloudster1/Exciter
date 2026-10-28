package testexec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// bytesPerMB and MaxFileBytes, MaxFileMB in bytes, what an upload reads at most.
const (
	bytesPerMB   = 1 << 20
	MaxFileBytes = MaxFileMB * bytesPerMB
)

var (
	// ErrTooLarge refuses a file over MaxFileMB.
	ErrTooLarge = errors.New("the file is too large")
	// ErrEmpty refuses a file of nothing.
	ErrEmpty = errors.New("the file is empty")
)

const selectFile = `
SELECT f.id, f.step, f.file_name, f.content_type, f.size, f.uploaded_by, COALESCE(u.name, ''), f.created_at, f.object_key, f.execution_id
FROM test_execution_file f LEFT JOIN app_user u ON u.id = f.uploaded_by`

type fileRow struct {
	File
	key       string
	execution uuid.UUID
}

func scanFile(row pgx.Row) (*fileRow, error) {
	var (
		f  fileRow
		by *uuid.UUID
	)
	err := row.Scan(&f.ID, &f.Step, &f.FileName, &f.ContentType, &f.Size, &by, &f.UploadedBy.Name, &f.CreatedAt, &f.key, &f.execution)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if by != nil {
		f.UploadedBy = requirement.PersonRef{ID: *by, Name: f.UploadedBy.Name}
	}
	return &f, err
}

func files(ctx context.Context, tx db.DBTX, execution, id uuid.UUID, iteration int) ([]File, error) {
	rows, err := tx.Query(ctx, selectFile+` WHERE f.execution_id = $1 AND f.requirement_id = $2 AND f.iteration = $3 ORDER BY f.created_at, f.id`,
		execution, id, iteration)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []File{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f.File)
	}
	return out, rows.Err()
}

// contentTypeOf is what a file says it is, when it says so plainly, else what its bytes look like.
func contentTypeOf(declared string, data []byte) string {
	declared = strings.TrimSpace(declared)
	if declared != "" && declared != "application/octet-stream" && !strings.ContainsAny(declared, "\r\n") {
		if _, _, err := mime.ParseMediaType(declared); err == nil {
			return declared
		}
	}
	return http.DetectContentType(data)
}

// Upload records a file on a step or on a case iteration while the execution is open; its bytes go to the store in
// the transaction that writes its row.
func (s *Service) Upload(ctx context.Context, actor, execution uuid.UUID, key string, iteration int, step *int, name, contentType string,
	data []byte) (*File, db.LSN, error) {
	if objectstore.IsUnavailable(s.store) {
		return nil, 0, objectstore.ErrUnavailable
	}
	switch {
	case len(data) > MaxFileBytes:
		return nil, 0, ErrTooLarge
	case len(data) == 0:
		return nil, 0, ErrEmpty
	}
	iteration = max(iteration, 1)
	name = objectstore.CleanName(name)
	contentType = contentTypeOf(contentType, data)
	sum := sha256.Sum256(data)
	var out *File
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, id, err := recording(ctx, tx, actor, execution, key)
		if err != nil {
			return err
		}
		c, err := readCase(ctx, tx, execution, id, iteration)
		if err != nil {
			return err
		}
		if step != nil && (*step < 1 || *step > len(c.Steps)) {
			return &FieldError{"step", fmt.Sprintf("The case has steps 1 to %d at the revision it runs. Put the file on one of those, or on the case.", len(c.Steps))}
		}
		var held int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM test_execution_file WHERE execution_id = $1 AND requirement_id = $2 AND iteration = $3
			AND step IS NOT DISTINCT FROM $4`, execution, id, iteration, step).Scan(&held); err != nil {
			return err
		}
		if held >= MaxFilesPerStep {
			return &FieldError{"file", fmt.Sprintf("A step holds at most %d files. Delete one first.", MaxFilesPerStep)}
		}
		var fileID uuid.UUID
		var objectKey string
		if err := tx.QueryRow(ctx, `
			INSERT INTO test_execution_file (execution_id, requirement_id, iteration, step, file_name, content_type, size, sha256, uploaded_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id, object_key`,
			execution, id, iteration, step, name, contentType, len(data), hex.EncodeToString(sum[:]), actor).Scan(&fileID, &objectKey); err != nil {
			return refusal(err)
		}
		if err := s.store.Put(ctx, objectKey, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
			return fmt.Errorf("keep the file: %w", err)
		}
		f, err := scanFile(tx.QueryRow(ctx, selectFile+` WHERE f.id = $1`, fileID))
		if f != nil {
			out = &f.File
		}
		return err
	})
	return out, lsn, err
}

// FileBytes answers a file the reader may read, with its bytes.
func (s *Service) FileBytes(ctx context.Context, reader, id uuid.UUID) (*File, []byte, error) {
	var f *fileRow
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) (err error) {
		if f, err = scanFile(tx.QueryRow(ctx, selectFile+` WHERE f.id = $1`, id)); err != nil {
			return err
		}
		_, err = readExecution(ctx, tx, reader, f.execution, false)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	if objectstore.IsUnavailable(s.store) {
		return nil, nil, objectstore.ErrUnavailable
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	body, err := s.store.Get(callCtx, f.key)
	if err != nil {
		return nil, nil, fmt.Errorf("read the file: %w", err)
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, MaxFileBytes+1))
	return &f.File, data, err
}

// DeleteFile deletes a file its uploader recorded while the execution is open; the reaper removes its bytes.
func (s *Service) DeleteFile(ctx context.Context, actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := scanFile(tx.QueryRow(ctx, selectFile+` WHERE f.id = $1`, id))
		if err != nil {
			return err
		}
		e, err := readExecution(ctx, tx, actor, f.execution, false)
		if err != nil {
			return err
		}
		if e.State != Open {
			return ErrClosed
		}
		if err := stillOpen(ctx, tx, e.projectID); err != nil {
			return err
		}
		if f.UploadedBy.ID != actor {
			return ErrForbidden
		}
		_, err = tx.Exec(ctx, `DELETE FROM test_execution_file WHERE id = $1`, id)
		return refusal(err)
	})
}
