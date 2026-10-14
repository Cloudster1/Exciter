package trace

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

var (
	ErrNotFound = errors.New("link type not found")
	// ErrForbidden is somebody changing a link type that is the organization's administrators', or a
	// project's administrators', to change.
	ErrForbidden = errors.New("only administrators change link types")
	// ErrLinkNotFound is a link that is gone, or one whose ends the reader does not both see.
	ErrLinkNotFound = errors.New("link not found")
	// ErrSection is a section asked for its links, which only requirements take.
	ErrSection = errors.New("sections take no links")
)

// InUseError refuses deleting a type that links have, which is retired instead.
type InUseError struct {
	Count int
}

func (e *InUseError) Error() string { return fmt.Sprintf("the link type has %d links", e.Count) }

// Service keeps link types and the links between requirements.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

const selectLinkType = `
SELECT t.id, t.key, t.name, t.forward, t.backward, t.acyclic, t.suspect_end, t.counts, COALESCE(p.key, ''),
       t.retired, t.updated_at, link_type_usage(t.id, true)
FROM link_type t
LEFT JOIN project p ON p.org_id = t.org_id AND p.id = t.project_id`

// typeOrder puts the organization's types first, each scope in the order they were made.
const typeOrder = ` ORDER BY t.project_id IS NOT NULL, t.id`

func scanLinkType(row pgx.CollectableRow) (LinkType, error) {
	var t LinkType
	err := row.Scan(&t.ID, &t.Key, &t.Name, &t.Forward, &t.Backward, &t.Acyclic, &t.SuspectEnd, &t.Counts, &t.ProjectKey,
		&t.Retired, &t.UpdatedAt, &t.Links)
	t.Scope, t.Enabled, t.Rules = ScopeOrg, true, []TypeRule{}
	if t.ProjectKey != "" {
		t.Scope = ScopeProject
	}
	return t, err
}

func linkTypes(ctx context.Context, tx db.DBTX, where string, args ...any) ([]LinkType, error) {
	rows, err := tx.Query(ctx, selectLinkType+` WHERE t.org_id = current_org_id() AND `+where+typeOrder, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanLinkType)
}

func linkTypeByID(ctx context.Context, tx db.DBTX, id uuid.UUID) (*LinkType, error) {
	types, err := linkTypes(ctx, tx, `t.id = $1`, id)
	if err != nil {
		return nil, err
	}
	if len(types) == 0 {
		return nil, ErrNotFound
	}
	return &types[0], nil
}

// usable answers the types a project may use, the organization's and its own, with whether it uses each and
// the rules it sets for them.
func usable(ctx context.Context, tx db.DBTX, projectID uuid.UUID) ([]LinkType, error) {
	types, err := linkTypes(ctx, tx, `(t.project_id IS NULL OR t.project_id = $1)`, projectID)
	if err != nil {
		return nil, err
	}
	disabled := map[uuid.UUID]bool{}
	rows, err := tx.Query(ctx, `SELECT link_type_id FROM link_type_project WHERE project_id = $1 AND NOT enabled`, projectID)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		disabled[id] = true
	}
	rules := map[uuid.UUID][]TypeRule{}
	rows, err = tx.Query(ctx, `SELECT link_type_id, source_type_id, target_type_id FROM link_type_rule WHERE project_id = $1 ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			id   uuid.UUID
			rule TypeRule
		)
		if err := rows.Scan(&id, &rule.SourceTypeID, &rule.TargetTypeID); err != nil {
			rows.Close()
			return nil, err
		}
		rules[id] = append(rules[id], rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range types {
		types[i].Enabled = !disabled[types[i].ID]
		if r := rules[types[i].ID]; r != nil {
			types[i].Rules = r
		}
	}
	return types, nil
}

// shaping reads a project for a change to its link types, which only its administrators make while it is open.
func shaping(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectKey string) (*project.Project, error) {
	p, err := project.Find(ctx, tx, actor, projectKey)
	switch {
	case err != nil:
		return nil, err
	case p.ArchivedAt != nil:
		return nil, project.ErrArchived
	case !p.Can.Administer:
		return nil, ErrForbidden
	}
	return p, nil
}

// managing reads a type for a change, refusing whoever does not shape it: the organization's administrators
// shape its own, a project's administrators the project's.
func managing(ctx context.Context, tx db.DBTX, actor, id uuid.UUID) (*LinkType, error) {
	t, err := linkTypeByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if t.Scope == ScopeProject {
		if _, err := shaping(ctx, tx, actor, t.ProjectKey); err != nil {
			return nil, err
		}
		return t, nil
	}
	var administers bool
	if err := tx.QueryRow(ctx, `SELECT current_actor_administers()`).Scan(&administers); err != nil {
		return nil, err
	}
	if !administers {
		return nil, ErrForbidden
	}
	return t, nil
}

func record(ctx context.Context, tx db.DBTX, actor uuid.UUID, action string, t *LinkType, data map[string]any) error {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return err
	}
	data["key"], data["name"] = t.Key, t.Name
	if t.ProjectKey != "" {
		data["project"] = t.ProjectKey
	}
	return audit.Write(ctx, tx, org.ID, audit.Entry{Action: action, TargetType: "link_type", TargetID: &t.ID, Actor: actor, Data: data})
}

// typeRefusal names the database's refusals of a type that the caller can do something about.
func typeRefusal(err error, key string) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch {
	case pgErr.Code == "23505" && (pgErr.ConstraintName == "link_type_key_taken" || pgErr.ConstraintName == "link_type_key_idx" ||
		pgErr.ConstraintName == "link_type_project_key_idx"):
		return &FieldError{"key", fmt.Sprintf("Another link type has the key %s. Pick another key.", key)}
	case pgErr.Code == "23505":
		return &FieldError{"name", "Another link type is called that. Pick another name."}
	case pgErr.Code == "42501":
		return ErrForbidden
	}
	return err
}
