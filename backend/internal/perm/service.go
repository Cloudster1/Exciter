package perm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

var (
	// ErrForbidden is somebody who may view a project asking for what only its administrators see or change.
	ErrForbidden = errors.New("only the project's administrators manage its grants")
	// ErrNotAdministrator is somebody asking for what only the installation's administrators see or change.
	ErrNotAdministrator = errors.New("only an administrator manages the installation's access")
	ErrArchived         = errors.New("the project is archived")
	// ErrNoAdministrator is taking away the last grant that lets somebody run a project.
	ErrNoAdministrator = errors.New("no person or group would administer the project")
	// ErrLastAdministrator is a change that would leave the installation without an active administrator.
	ErrLastAdministrator = errors.New("the installation keeps at least one active administrator")
)

// FieldError is input refused on one field, with the sentence to show beside it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Service answers who may do what, and keeps the roles, groups and grants that decide it.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// Mine is what the caller may do across the installation, as their grants across it say.
func Mine(p *auth.Principal) OrgCan {
	return OrgCan{CreateProject: p.CanCreateProjects(), Administer: p.CanAdminister()}
}

// requireAdmin refuses, in the transaction that acts, anybody a grant does not let administer the installation.
func requireAdmin(ctx context.Context, tx db.DBTX) error {
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT current_actor_administers()`).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotAdministrator
	}
	return nil
}

// administering checks, in the transaction that acts, that actor administers project, and says whether it is archived.
func administering(ctx context.Context, tx db.DBTX, actor, project uuid.UUID) (bool, error) {
	var administers, archived bool
	err := tx.QueryRow(ctx, `
		SELECT perm_project_holds(p.id, $2, 'administer'), p.archived_at IS NOT NULL
		FROM project p WHERE p.id = $1`, project, actor).Scan(&administers, &archived)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !administers) {
		return false, ErrForbidden
	}
	return archived, err
}

// Record writes an access change to the audit log in the caller's transaction.
func Record(ctx context.Context, tx db.DBTX, actor uuid.UUID, action, targetType string, target *uuid.UUID, data map[string]any) error {
	return audit.Write(ctx, tx, audit.Entry{Action: action, TargetType: targetType, TargetID: target, Actor: actor, Data: data})
}

// grantConstraints are the database's refusals of a grant, whose messages are sentences for whoever grants it.
var grantConstraints = []string{
	"role_assignment_installation_wide", "role_assignment_guest_in_a_project", "role_assignment_guest_not_administrator",
	"role_assignment_everyone_administers",
}

// refusal reads the database's refusal of an access change: a grant it will not hold as a field error on field,
// and a change that leaves nobody to administer the installation as ErrLastAdministrator.
func refusal(err error, field string) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch {
	case pgErr.ConstraintName == "installation_keeps_administrator":
		return ErrLastAdministrator
	case slices.Contains(grantConstraints, pgErr.ConstraintName):
		return &FieldError{Field: field, Message: pgErr.Message}
	case pgErr.ConstraintName == "role_administrator_administers":
		return &FieldError{Field: "permissions", Message: pgErr.Message}
	}
	return err
}

// Refusal reads a write's refusal at commit, when the installation would be left without an active administrator.
func Refusal(err error) error { return refusal(err, "") }

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// likePrefix is a LIKE pattern matching what starts with the text typed.
func likePrefix(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(strings.TrimSpace(q)) + "%"
}

// People are members whose name, a word of it, or email starts with q.
func (s *Service) People(ctx context.Context, q string, limit int) ([]Person, error) {
	out := []Person{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.name, u.email::text FROM app_user u
			WHERE u.role IS NOT NULL AND (u.name ILIKE $1 OR u.name ILIKE '% ' || $1 OR u.email::text ILIKE $1)
			ORDER BY lower(u.name), u.email LIMIT $2`, likePrefix(q), limit)
		if err != nil {
			return err
		}
		people, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Person])
		out = nonNil(people)
		return err
	})
	return out, err
}

const selectGroup = `
SELECT g.id, g.name, g.description, g.source, COALESCE(g.external_ref, ''), COALESCE(g.builtin, ''),
       (SELECT count(*)::int FROM group_member gm WHERE gm.group_id = g.id),
       (SELECT count(*)::int FROM role_assignment a WHERE a.group_id = g.id)
FROM groups g`

func scanGroupRow(row pgx.CollectableRow) (Group, error) {
	var g Group
	err := row.Scan(&g.ID, &g.Name, &g.Description, &g.Source, &g.ExternalRef, &g.Builtin, &g.MemberCount, &g.RoleCount)
	g.FromProvider = g.Source == "oidc"
	return g, err
}

// Groups are the installation's groups whose name, or a word of it, starts with q, Everyone first.
func (s *Service) Groups(ctx context.Context, q string, limit int) ([]Group, error) {
	out := []Group{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectGroup+`
			WHERE (g.name ILIKE $1 OR g.name ILIKE '% ' || $1)
			ORDER BY g.builtin IS NULL, lower(g.name), g.id LIMIT $2`, likePrefix(q), limit)
		if err != nil {
			return err
		}
		groups, err := pgx.CollectRows(rows, scanGroupRow)
		out = nonNil(groups)
		return err
	})
	return out, err
}

// EveryoneID is the id of the built-in group that holds every member.
func EveryoneID(ctx context.Context, tx db.DBTX) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM groups WHERE builtin = $1`, EveryoneGroup).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("find Everyone: %w", err)
	}
	return id, nil
}

// ViewableRequirement is the SQL condition every query that lists requirements narrows itself with, so a count
// counts only what the actor, bound as parameter $actorParam, may read of the requirements aliased alias.
func ViewableRequirement(alias string, actorParam int) string {
	return fmt.Sprintf("perm_requirement_viewable(%s.id, $%d::uuid)", alias, actorParam)
}
