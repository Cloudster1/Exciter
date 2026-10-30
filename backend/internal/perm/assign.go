package perm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// Limits of a page of grants.
const (
	DefaultAssignmentLimit = 50
	MaxAssignmentLimit     = 200
)

// Assignment is one role held by one subject in one scope, as a listing shows it: with the names filled in,
// because a page of identifiers explains nothing.
type Assignment struct {
	ID       uuid.UUID `json:"id"`
	Role     string    `json:"role"`
	RoleName string    `json:"roleName"`
	// ProjectID is nil for a role held across the whole installation.
	ProjectID   *uuid.UUID `json:"projectId,omitempty"`
	ProjectKey  string     `json:"projectKey,omitempty"`
	ProjectName string     `json:"projectName,omitempty"`
	// Exactly one of the person and the group is set.
	UserID    *uuid.UUID `json:"userId,omitempty"`
	UserName  string     `json:"userName,omitempty"`
	UserEmail string     `json:"userEmail,omitempty"`
	// Guest marks a person from outside, let into the projects they are granted.
	Guest     bool       `json:"guest,omitempty"`
	GroupID   *uuid.UUID `json:"groupId,omitempty"`
	GroupName string     `json:"groupName,omitempty"`
	// GroupBuiltin is the group's mark when the installation keeps it, "everyone".
	GroupBuiltin string    `json:"groupBuiltin,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// AssignmentPage is a page of grants and how many matched in all.
type AssignmentPage struct {
	Assignments []Assignment `json:"assignments"`
	Total       int          `json:"total"`
	Limit       int          `json:"limit"`
	Offset      int          `json:"offset"`
}

// AssignmentFilter narrows the grants listed.
type AssignmentFilter struct {
	// Q is text the person, the group, the role or the project holds.
	Q string
	// Project keeps the grants in this project, by key.
	Project string
	// Installation keeps the grants across the whole installation.
	Installation bool
	// Role keeps the grants of this role, by key.
	Role          string
	Limit, Offset int
}

// GrantInput grants a role to a person or a group, in a project or, with no project, across the whole
// installation.
type GrantInput struct {
	Role       string     `json:"role"`
	ProjectKey string     `json:"projectKey"`
	UserID     *uuid.UUID `json:"userId"`
	GroupID    *uuid.UUID `json:"groupId"`
}

// ErrNoSuchAssignment is returned for a grant that is not there.
var ErrNoSuchAssignment = errors.New("there is no such grant")

const selectAssignment = `
SELECT a.id, a.role, r.name, a.project_id, COALESCE(p.key, ''), COALESCE(p.name, ''),
       a.user_id, COALESCE(u.name, ''), COALESCE(u.email::text, ''), COALESCE(u.role = 'guest', false),
       a.group_id, COALESCE(g.name, ''), COALESCE(g.builtin, ''), a.created_at
FROM role_assignment a
JOIN role r ON r.key = a.role
LEFT JOIN project p ON p.id = a.project_id
LEFT JOIN app_user u ON u.id = a.user_id
LEFT JOIN groups g ON g.id = a.group_id`

// assignmentOrder lists the installation first, then projects by key; in each, the most powerful role first,
// Everyone and groups before people.
const assignmentOrder = `
ORDER BY a.project_id IS NOT NULL, p.key, r.sort_order, lower(r.name),
         g.builtin IS NULL, a.group_id IS NULL, lower(COALESCE(g.name, u.name, '')), a.id`

func scanAssignment(row pgx.CollectableRow) (Assignment, error) {
	var a Assignment
	err := row.Scan(&a.ID, &a.Role, &a.RoleName, &a.ProjectID, &a.ProjectKey, &a.ProjectName,
		&a.UserID, &a.UserName, &a.UserEmail, &a.Guest, &a.GroupID, &a.GroupName, &a.GroupBuiltin, &a.CreatedAt)
	return a, err
}

// Assignments lists who holds what, a page at a time. Grants across the installation are its administrators' to
// read; a project's, the project's administrators' too.
func (s *Service) Assignments(ctx context.Context, actor uuid.UUID, in AssignmentFilter) (*AssignmentPage, error) {
	switch {
	case in.Limit <= 0:
		in.Limit = DefaultAssignmentLimit
	case in.Limit > MaxAssignmentLimit:
		in.Limit = MaxAssignmentLimit
	}
	in.Offset = max(in.Offset, 0)
	out := AssignmentPage{Assignments: []Assignment{}, Limit: in.Limit, Offset: in.Offset}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var project *uuid.UUID
		if key := strings.TrimSpace(in.Project); key != "" {
			id, _, err := administeredProject(ctx, tx, actor, key, "project")
			if err != nil {
				return err
			}
			project = &id
		} else if err := requireAdmin(ctx, tx); err != nil {
			return err
		}
		where := `
			WHERE ($1::uuid IS NULL OR a.project_id = $1)
			  AND (NOT $2 OR a.project_id IS NULL)
			  AND ($3 = '' OR a.role = $3)
			  AND ($4 = '' OR r.name ILIKE $4 OR a.role ILIKE $4 OR u.name ILIKE $4 OR u.email::text ILIKE $4
			       OR g.name ILIKE $4 OR p.key ILIKE $4 OR p.name ILIKE $4)`
		q := ""
		if text := strings.TrimSpace(in.Q); text != "" {
			q = "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text) + "%"
		}
		args := []any{project, in.Installation, strings.TrimSpace(in.Role), q}
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM (`+selectAssignment+where+`) AS counted`, args...).Scan(&out.Total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectAssignment+where+assignmentOrder+` LIMIT $5 OFFSET $6`, append(args, in.Limit, in.Offset)...)
		if err != nil {
			return err
		}
		list, err := pgx.CollectRows(rows, scanAssignment)
		out.Assignments = nonNil(list)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// administeredProject finds a project the actor administers by key, or a key it had, refusing one they do not, and
// saying it is there only to whoever may see it; it says whether the project is archived.
func administeredProject(ctx context.Context, tx db.DBTX, actor uuid.UUID, key, field string) (uuid.UUID, bool, error) {
	key = strings.ToUpper(strings.TrimSpace(key))
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT p.id FROM project p
		WHERE p.key = $1 OR p.id = (SELECT k.project_id FROM project_key_alias k WHERE k.key = $1)`, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, &FieldError{Field: field, Message: fmt.Sprintf("There is no project %s that you may see. Pick one from the list.", key)}
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	archived, err := administering(ctx, tx, actor, id)
	return id, archived, err
}

// Grant gives a role to a person or a group, in a project or across the installation. Granting the same twice is
// the one grant.
func (s *Service) Grant(ctx context.Context, actor uuid.UUID, in GrantInput) (*Assignment, db.LSN, error) {
	role := strings.TrimSpace(in.Role)
	if role == "" {
		return nil, 0, &FieldError{Field: "role", Message: "Choose the role to grant."}
	}
	if (in.UserID == nil) == (in.GroupID == nil) {
		return nil, 0, &FieldError{Field: "userId", Message: "Grant the role to one person or one group."}
	}
	field := "userId"
	if in.GroupID != nil {
		field = "groupId"
	}
	var out Assignment
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var project *uuid.UUID
		if key := strings.TrimSpace(in.ProjectKey); key != "" {
			id, archived, err := administeredProject(ctx, tx, actor, key, "projectKey")
			if err != nil {
				return err
			}
			if archived {
				return ErrArchived
			}
			project = &id
		} else if err := requireAdmin(ctx, tx); err != nil {
			return err
		}
		var whole bool
		err := tx.QueryRow(ctx, `SELECT installation_wide_only FROM role WHERE key = $1`, role).Scan(&whole)
		if errors.Is(err, pgx.ErrNoRows) {
			return &FieldError{Field: "role", Message: fmt.Sprintf("There is no role %s. Pick one from the list.", role)}
		}
		if err != nil {
			return err
		}
		if whole && project != nil {
			return &FieldError{Field: "role", Message: "That role answers for the whole installation and cannot be granted in one project. Pick another role."}
		}
		if err := subjectExists(ctx, tx, field, in); err != nil {
			return err
		}
		var id uuid.UUID
		var made bool
		// xmax is zero on a row this statement inserted rather than found.
		err = tx.QueryRow(ctx, `
			INSERT INTO role_assignment (role, project_id, user_id, group_id, created_by) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (role, project_id, user_id, group_id) DO UPDATE SET role = EXCLUDED.role
			RETURNING id, xmax = 0`, role, project, in.UserID, in.GroupID, actor).Scan(&id, &made)
		if err != nil {
			return refusal(err, field)
		}
		rows, err := tx.Query(ctx, selectAssignment+` WHERE a.id = $1`, id)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectExactlyOneRow(rows, scanAssignment); err != nil {
			return err
		}
		if !made {
			return nil
		}
		return Record(ctx, tx, actor, audit.ActionGrantAdded, "grant", &id, assignmentLog(out))
	})
	if err != nil {
		return nil, lsn, err
	}
	return &out, lsn, nil
}

// subjectExists checks the person or the group a grant names is one of the installation's.
func subjectExists(ctx context.Context, tx db.DBTX, field string, in GrantInput) error {
	var exists bool
	var err error
	if in.UserID != nil {
		err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM app_user WHERE id = $1 AND role IS NOT NULL)`, *in.UserID).Scan(&exists)
	} else {
		err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM groups WHERE id = $1)`, *in.GroupID).Scan(&exists)
	}
	if err != nil {
		return err
	}
	if !exists {
		what := "person"
		if in.GroupID != nil {
			what = "group"
		}
		return &FieldError{Field: field, Message: fmt.Sprintf("There is no %s with that id here. Pick one from the list.", what)}
	}
	return nil
}

// Revoke takes a grant away. The last that lets somebody run a project, and the last that leaves the installation
// an active administrator, stay.
func (s *Service) Revoke(ctx context.Context, actor, id uuid.UUID) (db.LSN, error) {
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectAssignment+` WHERE a.id = $1 FOR UPDATE OF a`, id)
		if err != nil {
			return err
		}
		a, err := pgx.CollectExactlyOneRow(rows, scanAssignment)
		if errors.Is(err, pgx.ErrNoRows) {
			// Row level security hides the grants across the installation, and those of projects somebody does
			// not administer, from anybody but its administrators, who alone are told one is gone.
			if err := requireAdmin(ctx, tx); err != nil {
				return err
			}
			return ErrNoSuchAssignment
		}
		if err != nil {
			return err
		}
		if a.ProjectID != nil {
			archived, err := administering(ctx, tx, actor, *a.ProjectID)
			if err != nil {
				return err
			}
			if archived {
				return ErrArchived
			}
			if err := keepsProjectAdministrator(ctx, tx, a); err != nil {
				return err
			}
		} else if err := requireAdmin(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM role_assignment WHERE id = $1`, id); err != nil {
			return err
		}
		return Record(ctx, tx, actor, audit.ActionGrantRemoved, "grant", &id, assignmentLog(a))
	})
	return lsn, Refusal(err)
}

// keepsProjectAdministrator refuses taking away the last grant in a project that lets somebody run it, so the
// project has somebody besides the installation's administrators to manage it.
func keepsProjectAdministrator(ctx context.Context, tx db.DBTX, a Assignment) error {
	var administers, others bool
	if err := tx.QueryRow(ctx, `
		SELECT 'administer' = ANY (r.permissions),
		       EXISTS (SELECT 1 FROM role_assignment o JOIN role ro ON ro.key = o.role
		               WHERE o.project_id = $2 AND o.id <> $1 AND 'administer' = ANY (ro.permissions))
		FROM role r WHERE r.key = $3`, a.ID, *a.ProjectID, a.Role).Scan(&administers, &others); err != nil {
		return err
	}
	if administers && !others {
		return ErrNoAdministrator
	}
	return nil
}

// assignmentLog is what the audit log keeps of a grant: ids and names as they were, since either may change later.
func assignmentLog(a Assignment) map[string]any {
	data := map[string]any{"role": a.Role, "roleName": a.RoleName}
	if a.ProjectID != nil {
		data["projectId"], data["project"] = *a.ProjectID, a.ProjectKey
	}
	if a.UserID != nil {
		data["userId"], data["userName"] = *a.UserID, a.UserName
	}
	if a.GroupID != nil {
		data["groupId"], data["groupName"] = *a.GroupID, a.GroupName
	}
	return data
}
