package requirement

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/quality"
)

// Seed makes what a template holds in a project just made, in the transaction that made it (#24): types,
// attributes, the installation's link types it leaves off, and sections.
func Seed(ctx context.Context, tx db.DBTX, actor, projectID uuid.UUID, b project.Blueprint) error {
	for i, t := range b.TypeSpecs {
		sql := `INSERT INTO requirement_type (project_id, name, icon, colour, position) VALUES ($1, $2, $3, $4, $5)`
		if i == 0 {
			// The type the project was made with is its default; renaming it keeps that.
			sql = `UPDATE requirement_type SET name = $2, icon = $3, colour = $4 WHERE project_id = $1 AND position = $5`
		}
		if _, err := tx.Exec(ctx, sql, projectID, t.Name, t.Icon, t.Colour, i); err != nil {
			return fmt.Errorf("make the type %s: %w", t.Name, err)
		}
	}
	for i, a := range b.AttributeSpecs {
		options := make([]map[string]any, len(a.Options))
		for n, name := range a.Options {
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			options[n] = map[string]any{"id": id, "name": name, "retired": false}
		}
		encoded, err := json.Marshal(options)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO attribute_def (project_id, name, kind, options, position) VALUES ($1, $2, $3, $4, $5)`,
			projectID, a.Name, a.Kind, encoded, i); err != nil {
			return fmt.Errorf("make the attribute %s: %w", a.Name, err)
		}
	}
	if len(b.LinkTypeKeys) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO link_type_project (project_id, link_type_id, enabled)
			SELECT $1, lt.id, false FROM link_type lt
			WHERE lt.project_id IS NULL AND lt.key <> ALL ($2)`, projectID, b.LinkTypeKeys); err != nil {
			return fmt.Errorf("leave link types off: %w", err)
		}
	}
	for _, title := range b.Sections {
		if err := seedSection(ctx, tx, actor, projectID, title, "From the template "+b.Name); err != nil {
			return err
		}
	}
	return nil
}

// seedSection writes one empty section at the end of the project's top level, as revision 1.
func seedSection(ctx context.Context, tx db.DBTX, actor, projectID uuid.UUID, title, comment string) error {
	at, err := lastAtTop(ctx, tx, projectID)
	if err != nil {
		return err
	}
	content := Normative{Title: title, Statement: document.Empty, Attributes: map[string]json.RawMessage{}}
	hash := content.Hash()
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO requirement (project_id, kind, title, statement, attributes, normative_hash, rank, created_by, updated_by)
		VALUES ($1, $2, $3, $4, '{}', $5, $6, $7, $7) RETURNING id`,
		projectID, KindSection, title, document.Empty, hash, at, actor).Scan(&id); err != nil {
		return fmt.Errorf("write the section %s: %w", title, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO requirement_revision (requirement_id, number, title, statement, attributes, hash, comment, created_by)
		VALUES ($1, 1, $2, $3, '{}', $4, $5, $6)`, id, title, document.Empty, hash, comment, actor); err != nil {
		return fmt.Errorf("write revision 1 of %s: %w", title, err)
	}
	if err := quality.Record(ctx, tx, id, 1); err != nil {
		return err
	}
	return events.Emit(ctx, tx, TopicCreated, map[string]any{"actorId": actor, "projectId": projectID, "requirementId": id, "revision": 1})
}
