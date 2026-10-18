package exporttemplate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/docx"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/project"
)

var (
	ErrNotFound = errors.New("template not found")
	// ErrForbidden is somebody who does not administer a template's project changing it.
	ErrForbidden = errors.New("only the project's administrators upload and change its templates")
	// ErrTooMany is past MaxTemplates in one project.
	ErrTooMany = errors.New("too many templates")
	// ErrTooLarge is a file past MaxFileMB, or one unpacking past MaxUnpackedMB.
	ErrTooLarge = errors.New("the template is too large")
	// ErrNotUsable is a template an export of the project may not go through.
	ErrNotUsable = errors.New("the project may not use this template")
)

// MaxFileBytes is MaxFileMB in bytes.
const MaxFileBytes = MaxFileMB << 20

// FieldError is input refused on one field, with the sentence to show beside it.
type FieldError struct{ Field, Message string }

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Service keeps a project's Word templates and their files.
type Service struct {
	db    *db.Cluster
	store objectstore.Store
}

func NewService(cluster *db.Cluster, store objectstore.Store) *Service {
	return &Service{db: cluster, store: store}
}

const selectTemplate = `
SELECT t.id, export_template_project_key(t.id), t.name, t.description, t.shared, t.file_name, t.size, t.placeholders, t.fields, t.include,
       t.created_by, COALESCE(u.name, ''), t.created_at, t.updated_at, perm_project_holds(t.project_id, current_actor_id(), 'administer'),
       t.object_key, t.project_id
FROM export_template t
LEFT JOIN app_user u ON u.id = t.created_by`

type found struct {
	Template
	key       string
	projectID uuid.UUID
}

func scan(row pgx.Row) (*found, error) {
	var (
		f               found
		fields, include []byte
		by              *uuid.UUID
		placeholders    []string
	)
	err := row.Scan(&f.ID, &f.ProjectKey, &f.Name, &f.Description, &f.Shared, &f.FileName, &f.Size, &placeholders, &fields, &include,
		&by, &f.CreatedBy.Name, &f.CreatedAt, &f.UpdatedAt, &f.Can.Edit, &f.key, &f.projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if by != nil {
		f.CreatedBy.ID = *by
	}
	for _, p := range placeholders {
		f.Placeholders = append(f.Placeholders, Placeholder(p))
	}
	if f.Placeholders == nil {
		f.Placeholders = []Placeholder{}
	}
	if err := json.Unmarshal(fields, &f.Fields); err != nil {
		return nil, err
	}
	if f.Fields == nil {
		f.Fields = []FieldRef{}
	}
	return &f, json.Unmarshal(include, &f.Include)
}

func get(ctx context.Context, tx db.DBTX, id uuid.UUID) (*found, error) {
	return scan(tx.QueryRow(ctx, selectTemplate+` WHERE t.id = $1`, id))
}

// Templates answers the templates a project may export through: its own, then those other projects share.
func (s *Service) Templates(ctx context.Context, reader uuid.UUID, projectKey string) ([]Template, error) {
	out := []Template{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectTemplate+` WHERE t.project_id = $1 OR t.shared ORDER BY t.project_id <> $1, lower(t.name)`, p.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, f.Template)
		}
		return rows.Err()
	})
	return out, err
}

// Get answers one template the reader may read.
func (s *Service) Get(ctx context.Context, reader, id uuid.UUID) (*Template, error) {
	var out *Template
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := get(ctx, tx, id)
		if f != nil {
			out = &f.Template
		}
		return err
	})
	return out, err
}

func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", &FieldError{"name", "Give the template a name."}
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", &FieldError{"name", fmt.Sprintf("Keep the name to %d characters.", MaxNameLength)}
	}
	return name, nil
}

func checkDescription(d string) (string, error) {
	d = strings.TrimSpace(d)
	if utf8.RuneCountInString(d) > MaxDescriptionLength {
		return "", &FieldError{"description", fmt.Sprintf("Keep the description to %d characters.", MaxDescriptionLength)}
	}
	return d, nil
}

// read checks a file as a Word template, refusing one that cannot frame an export with what is wrong.
func read(fileName string, data []byte) (*docx.Template, error) {
	t, err := docx.ReadTemplate(data)
	var (
		refusal *docx.TemplateError
		large   *docx.TooLargeError
	)
	switch {
	case err == nil:
		return t, nil
	case errors.As(err, &refusal):
		return nil, &FieldError{"file", refusal.Message}
	case errors.As(err, &large):
		return nil, ErrTooLarge
	case errors.Is(err, docx.ErrNotWord):
		return nil, &FieldError{"file", fmt.Sprintf("%s is not a Word document. Save it as .docx or .dotx and upload it again.", fileName)}
	}
	return nil, err
}

func refusal(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return &FieldError{"name", "Another template of this project is called that. Pick another name."}
	}
	if errors.As(err, &pgErr) && pgErr.Code == "42501" {
		return ErrForbidden
	}
	return err
}

// Upload checks a Word file as a template and keeps it for the project, its file in the object store.
func (s *Service) Upload(ctx context.Context, actor uuid.UUID, projectKey string, form Form, fileName string, data []byte) (*Template, db.LSN, error) {
	if objectstore.IsUnavailable(s.store) {
		return nil, 0, objectstore.ErrUnavailable
	}
	if int64(len(data)) > MaxFileBytes {
		return nil, 0, ErrTooLarge
	}
	name, err := checkName(form.Name)
	if err != nil {
		return nil, 0, err
	}
	description, err := checkDescription(form.Description)
	if err != nil {
		return nil, 0, err
	}
	t, err := read(fileName, data)
	if err != nil {
		return nil, 0, err
	}
	sum := sha256.Sum256(data)
	var out *Template
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if p.ArchivedAt != nil {
			return project.ErrArchived
		}
		if !p.Can.Administer {
			return ErrForbidden
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM export_template WHERE project_id = $1`, p.ID).Scan(&count); err != nil {
			return err
		}
		if count >= MaxTemplates {
			return ErrTooMany
		}
		var id uuid.UUID
		var key string
		if err := tx.QueryRow(ctx, `INSERT INTO export_template (project_id, name, description, file_name, size, sha256, placeholders, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, object_key`,
			p.ID, name, description, objectstore.CleanName(fileName), len(data), hex.EncodeToString(sum[:]), t.Placeholders, actor).Scan(&id, &key); err != nil {
			return refusal(err)
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionExportTemplateUploaded, TargetType: "export_template", TargetID: &id, Actor: actor,
			Data: map[string]any{"project": p.Key, "name": name, "file": fileName}}); err != nil {
			return err
		}
		if err := s.store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), docx.ContentType); err != nil {
			return fmt.Errorf("keep the template's file: %w", err)
		}
		f, err := get(ctx, tx, id)
		if f != nil {
			out = &f.Template
		}
		return err
	})
	return out, lsn, err
}

func checkFields(fields []FieldRef) ([]FieldRef, error) {
	if len(fields) > MaxFields {
		return nil, &FieldError{"fields", fmt.Sprintf("A template shows at most %d fields.", MaxFields)}
	}
	out := make([]FieldRef, 0, len(fields))
	for _, f := range fields {
		f.Attribute = strings.TrimSpace(f.Attribute)
		switch {
		case !slices.Contains(Fields, f.Field):
			return nil, &FieldError{"fields", fmt.Sprintf("%q is not a field a template shows. Pick type, status, owner, priority, labels, revision, rationale or an attribute.", f.Field)}
		case f.Field == FieldAttribute && f.Attribute == "":
			return nil, &FieldError{"fields", "Name the attribute a field shows."}
		case f.Field != FieldAttribute:
			f.Attribute = ""
		}
		if slices.Contains(out, f) {
			return nil, &FieldError{"fields", "Show each field once."}
		}
		out = append(out, f)
	}
	return out, nil
}

// changing reads a template to change, refusing whoever does not administer its project.
func changing(ctx context.Context, tx db.DBTX, id uuid.UUID) (*found, error) {
	f, err := get(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if !f.Can.Edit {
		return nil, ErrForbidden
	}
	var archived bool
	if err := tx.QueryRow(ctx, `SELECT archived_at IS NOT NULL FROM project WHERE id = $1`, f.projectID).Scan(&archived); err != nil {
		return nil, err
	}
	if archived {
		return nil, project.ErrArchived
	}
	return f, nil
}

// Update renames a template, chooses its fields and inclusions, or shares it.
func (s *Service) Update(ctx context.Context, actor, id uuid.UUID, in Patch) (*Template, db.LSN, error) {
	var out *Template
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := changing(ctx, tx, id)
		if err != nil {
			return err
		}
		t := f.Template
		if in.Name != nil {
			if t.Name, err = checkName(*in.Name); err != nil {
				return err
			}
		}
		if in.Description != nil {
			if t.Description, err = checkDescription(*in.Description); err != nil {
				return err
			}
		}
		if in.Shared != nil {
			t.Shared = *in.Shared
		}
		if in.Fields != nil {
			if t.Fields, err = checkFields(*in.Fields); err != nil {
				return err
			}
		}
		if in.Include != nil {
			t.Include = *in.Include
		}
		fields, err := json.Marshal(t.Fields)
		if err != nil {
			return err
		}
		include, err := json.Marshal(t.Include)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE export_template SET name = $2, description = $3, shared = $4, fields = $5, include = $6 WHERE id = $1`,
			id, t.Name, t.Description, t.Shared, fields, include); err != nil {
			return refusal(err)
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionExportTemplateChanged, TargetType: "export_template", TargetID: &id, Actor: actor,
			Data: map[string]any{"project": f.ProjectKey, "name": t.Name, "shared": t.Shared}}); err != nil {
			return err
		}
		g, err := get(ctx, tx, id)
		if g != nil {
			out = &g.Template
		}
		return err
	})
	return out, lsn, err
}

// Delete removes a template; the worker's reaper removes its file.
func (s *Service) Delete(ctx context.Context, actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := changing(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM export_template WHERE id = $1`, id); err != nil {
			return refusal(err)
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionExportTemplateDeleted, TargetType: "export_template", TargetID: &id, Actor: actor,
			Data: map[string]any{"project": f.ProjectKey, "name": f.Name}})
	})
}

// File answers a template the reader may read, with its file's bytes.
func (s *Service) File(ctx context.Context, reader, id uuid.UUID) (*Template, []byte, error) {
	var f *found
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) (err error) {
		f, err = get(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	if objectstore.IsUnavailable(s.store) {
		return nil, nil, objectstore.ErrUnavailable
	}
	body, err := s.store.Get(ctx, f.key)
	if err != nil {
		return nil, nil, fmt.Errorf("read the template's file: %w", err)
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, MaxFileBytes+1))
	return &f.Template, data, err
}

// Usable reads a template an export of the project may go through, read and ready to frame it: the project's own,
// or one another project shares.
func (s *Service) Usable(ctx context.Context, reader uuid.UUID, projectKey string, id uuid.UUID) (*Template, *docx.Template, error) {
	var allowed bool
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM export_template WHERE id = $1 AND (project_id = $2 OR shared))`, id, p.ID).Scan(&allowed)
	})
	if err != nil {
		return nil, nil, err
	}
	if !allowed {
		return nil, nil, ErrNotUsable
	}
	t, data, err := s.File(ctx, reader, id)
	if err != nil {
		return nil, nil, err
	}
	frame, err := docx.ReadTemplate(data)
	if err != nil {
		return nil, nil, fmt.Errorf("read the template %s: %w", t.Name, err)
	}
	return t, frame, nil
}
