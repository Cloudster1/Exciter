package reqifio

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/docx"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// FieldError is a field of an upload beside its file that the import refuses, with the sentence to show.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// readWord reads an uploaded Word document, in the sentences its uploader reads.
func readWord(fileName string, data []byte) (*docx.Source, error) {
	src, err := docx.Read(data)
	var large *docx.TooLargeError
	switch {
	case errors.As(err, &large):
		return nil, invalid("The document unpacks into more than %d MB, more than one import takes. Split it, and import the parts one after the other.", large.Limit/megabyte)
	case err != nil:
		return nil, invalid("%s is not a Word document. Save it as .docx and upload it again.", fileName)
	}
	return src, nil
}

// UploadWord reads a Word document into an import of a project, below the section parentKey names or at the top,
// and what it would make with the choices left as they are. Nothing is written to the project yet.
func (s *Service) UploadWord(ctx context.Context, actor uuid.UUID, projectKey, parentKey, fileName string, data []byte) (*Import, db.LSN, error) {
	src, err := readWord(fileName, data)
	if err != nil {
		return nil, 0, err
	}
	media := src.Media
	if objectstore.IsUnavailable(s.store) {
		src.Media, media = []docx.Media{}, nil
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
		j := &job{Import: Import{FileName: fileName, Format: FormatDocx}, source: src}
		if parentKey != "" {
			id, ok := w.keys[parentKey]
			if e := w.existing[id]; !ok || e == nil || e.Kind != requirement.KindSection {
				return &FieldError{Field: "parentKey", Message: fmt.Sprintf("%s is no section of %s. Choose one of its sections to import below, or none to import at its top.", parentKey, p.Key)}
			}
			j.parentID = &id
		}
		a := j.analyze(w, map[string]string{})
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO reqif_import (project_id, created_by, file_name, format, content, preview, total, parent_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			p.ID, actor, fileName, FormatDocx, src, a.preview, a.objects(), j.parentID).Scan(&id); err != nil {
			return fmt.Errorf("keep the import: %w", err)
		}
		for _, m := range media {
			if _, err := tx.Exec(ctx, `INSERT INTO reqif_import_object (import_id, path, content_type, data) VALUES ($1, $2, $3, $4)`,
				id, m.Path, m.ContentType, m.Data); err != nil {
				return fmt.Errorf("keep the picture %s: %w", m.Path, err)
			}
		}
		made, err := findJob(ctx, tx, id)
		if err == nil {
			out = &made.Import
		}
		return err
	})
	return out, lsn, err
}
