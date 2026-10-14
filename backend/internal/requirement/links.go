package requirement

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
)

// Topics of trace links (#18), here so a copy that carries links emits them as internal/trace does.
const (
	TopicLinkCreated = "link.created"
	TopicLinkRemoved = "link.removed"
)

// Endpoint is a requirement as a link to it needs it: what it is, where, and whether the reader changes it.
type Endpoint struct {
	Ref
	ProjectID uuid.UUID
	// TypeID and TypeName are its type, nil and empty for one written before types.
	TypeID   *uuid.UUID
	TypeName string
	// Linkable is the reader editing it while it is out of the trash and its project open.
	Linkable bool
	// Archived says its project is archived, which leaves it as it is.
	Archived bool
}

// FindEndpoint reads the requirement a key, or a key it had, names as the reader sees it.
func FindEndpoint(ctx context.Context, tx db.DBTX, reader uuid.UUID, key string) (*Endpoint, error) {
	l, err := find(ctx, tx, reader, key)
	if err != nil {
		return nil, err
	}
	e := &Endpoint{
		Ref: Ref{ID: l.ID, Key: l.Key, ProjectKey: l.ProjectKey, Kind: l.Kind, Title: l.Title, Revision: l.Revision,
			Status: l.Status, Trashed: l.TrashedAt != nil},
		ProjectID: l.projectID,
		TypeID:    l.typeID(),
		Linkable:  l.Can.Link && !l.archived,
		Archived:  l.archived,
	}
	if l.Type != nil {
		e.TypeName = l.Type.Name
	}
	return e, nil
}

// EmitLink tells what follows links that one was made or removed, and each end that it changed.
func EmitLink(ctx context.Context, tx db.DBTX, topic string, actor, link, source, target uuid.UUID) error {
	if err := events.Emit(ctx, tx, topic, map[string]any{"actorId": actor, "linkId": link, "sourceId": source, "targetId": target}); err != nil {
		return err
	}
	for _, end := range []uuid.UUID{source, target} {
		if err := events.Emit(ctx, tx, TopicChanged, map[string]any{"actorId": actor, "requirementId": end, "field": ChangeLink}); err != nil {
			return err
		}
	}
	return nil
}

// copyLinks gives the copies the live links of their originals the actor sees: among the copies between the
// copies, and to the rest from the copy's end. A link whose type the copy's project may not use stays behind.
func copyLinks(ctx context.Context, tx db.DBTX, actor, to uuid.UUID, copies map[uuid.UUID]uuid.UUID) error {
	originals := make([]uuid.UUID, 0, len(copies))
	made := make([]uuid.UUID, 0, len(copies))
	for original, copy := range copies {
		originals = append(originals, original)
		made = append(made, copy)
	}
	rows, err := tx.Query(ctx, `
		WITH copied AS (SELECT * FROM unnest($1::uuid[], $2::uuid[]) AS c(original, copy))
		INSERT INTO trace_link (type_id, source_id, target_id, created_by)
		SELECT l.type_id, COALESCE(sc.copy, l.source_id), COALESCE(tc.copy, l.target_id), $3
		FROM trace_link l
		JOIN link_type lt ON lt.id = l.type_id
		JOIN requirement s ON s.id = l.source_id
		JOIN requirement t ON t.id = l.target_id
		LEFT JOIN copied sc ON sc.original = l.source_id
		LEFT JOIN copied tc ON tc.original = l.target_id
		WHERE l.deleted_at IS NULL AND (sc.original IS NOT NULL OR tc.original IS NOT NULL)
		  AND NOT lt.retired
		  AND (lt.project_id IS NULL OR lt.project_id = CASE WHEN sc.original IS NOT NULL THEN $4 ELSE s.project_id END)
		  AND (sc.original IS NOT NULL OR s.trashed_at IS NULL) AND (tc.original IS NOT NULL OR t.trashed_at IS NULL)
		ORDER BY l.created_at
		RETURNING id, source_id, target_id`, originals, made, actor, to)
	if err != nil {
		return fmt.Errorf("copy the links: %w", err)
	}
	type link struct{ id, source, target uuid.UUID }
	var carried []link
	for rows.Next() {
		var l link
		if err := rows.Scan(&l.id, &l.source, &l.target); err != nil {
			rows.Close()
			return err
		}
		carried = append(carried, l)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("copy the links: %w", linkRefusal(err))
	}
	for _, l := range carried {
		if err := EmitLink(ctx, tx, TopicLinkCreated, actor, l.id, l.source, l.target); err != nil {
			return err
		}
	}
	return nil
}

// linkRefusal names the one refusal a copy's links meet that the copy can say something about.
func linkRefusal(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "trace_link_limit" {
		return &FieldError{"links", "A requirement the copy links to already holds as many links as a requirement may. Copy without links, or remove some first."}
	}
	return err
}
