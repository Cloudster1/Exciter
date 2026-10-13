package attachment

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// PlanCopy names a new id for each file a copy of original takes: those on its list, and those its current
// content shows although they were taken off it, which the copy shows too.
func (s *Service) PlanCopy(ctx context.Context, tx db.DBTX, original uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT a.id FROM attachment a JOIN requirement r ON r.org_id = a.org_id AND r.id = a.requirement_id
		WHERE a.org_id = current_org_id() AND a.requirement_id = $1
		  AND (a.deleted_at IS NULL OR jsonb_path_exists(jsonb_build_array(r.statement, r.rationale, r.attributes),
		        'lax $.** ? ((@.type == "image" || @.type == "attachment") && @.attrs.attachmentId == $id)',
		        jsonb_build_object('id', a.id::text)))
		ORDER BY a.created_at, a.id`, original)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	planned := make(map[uuid.UUID]uuid.UUID, len(ids))
	for _, id := range ids {
		if planned[id], err = uuid.NewV7(); err != nil {
			return nil, err
		}
	}
	return planned, nil
}

// CopyFiles writes the planned files on the copy, objects included, as uploaded by whoever copied them.
func (s *Service) CopyFiles(ctx context.Context, tx db.DBTX, actor, copy uuid.UUID, ids map[uuid.UUID]uuid.UUID) error {
	for from, to := range ids {
		var fromKey, toKey, contentType string
		if err := tx.QueryRow(ctx, `
			INSERT INTO attachment (id, org_id, requirement_id, uploaded_by, file_name, content_type, size_bytes, width, height, created_at)
			SELECT $2, org_id, $3, $4, file_name, content_type, size_bytes, width, height, created_at
			FROM attachment WHERE org_id = current_org_id() AND id = $1
			RETURNING (SELECT object_key FROM attachment WHERE id = $1), object_key, content_type`,
			from, to, copy, actor).Scan(&fromKey, &toKey, &contentType); err != nil {
			return fmt.Errorf("copy the file: %w", err)
		}
		if err := s.copyObject(ctx, fromKey, toKey, contentType); err != nil {
			return err
		}
	}
	return nil
}

// copyObject reads the whole object before writing, which the store needs to sign the upload; a file is at
// most the upload limit.
func (s *Service) copyObject(ctx context.Context, from, to, contentType string) error {
	body, err := s.store.Get(ctx, from)
	if err != nil {
		return fmt.Errorf("copy %s: %w", from, err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("copy %s: %w", from, err)
	}
	return s.store.Put(ctx, to, bytes.NewReader(data), int64(len(data)), contentType)
}
