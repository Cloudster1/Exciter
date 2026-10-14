package perm

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

var (
	// ErrForbidden is somebody who may view a project asking for what only its administrators see or change.
	ErrForbidden = errors.New("only the project's administrators manage its grants")
	ErrArchived  = errors.New("the project is archived")
	// ErrNoAdministrator is a table of grants that leaves nobody but the installation's administrators to run the project.
	ErrNoAdministrator = errors.New("no person or group would administer the project")
)

// FieldError is input refused on one field, with the sentence to show beside it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Service answers who may do what in a project, and the pickers that name people and groups.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// Mine is what a person with role may do across the installation.
func Mine(role auth.OrgRole) OrgCan {
	return OrgCan{CreateProject: role.CanAdminister(), Administer: role.CanAdminister()}
}

// rightsOf are the rights a role gives, as perm_role_rights answers them.
func rightsOf(role Role) []Right {
	switch role {
	case RoleViewer:
		return []Right{RightView}
	case RoleReviewer:
		return []Right{RightView, RightComment, RightApprove}
	case RoleEditor:
		return []Right{RightView, RightComment, RightEdit}
	case RoleAdmin:
		return Rights
	}
	return nil
}

// cappedRole is the role a grant gives a person of orgRole: a guest holds at most editor, as perm_grant_role says.
func cappedRole(orgRole auth.OrgRole, role Role) Role {
	if orgRole == auth.RoleGuest && role == RoleAdmin {
		return RoleEditor
	}
	return role
}

// subjectColumns, subjectJoins and subjectOrder read a Subject from a grant aliased g: everyone first, then
// groups, then people, each by name.
const (
	subjectColumns = `g.subject_type, COALESCE(g.user_id, g.group_id), COALESCE(u.name, gr.name, '` + EveryoneName + `')`
	subjectJoins   = `
		LEFT JOIN app_user u ON u.id = g.user_id
		LEFT JOIN groups gr ON gr.id = g.group_id`
	subjectOrder = `CASE g.subject_type WHEN 'everyone' THEN 0 WHEN 'group' THEN 1 ELSE 2 END,
		lower(COALESCE(u.name, gr.name, '')), COALESCE(g.user_id, g.group_id)`
)

// subjectRank orders subjects as subjectOrder does.
func subjectRank(t SubjectType) int {
	return map[SubjectType]int{SubjectEveryone: 0, SubjectGroup: 1, SubjectUser: 2}[t]
}

func compareGrants(a, b Grant) int {
	return cmp.Or(
		cmp.Compare(subjectRank(a.Subject.Type), subjectRank(b.Subject.Type)),
		cmp.Compare(strings.ToLower(a.Subject.Name), strings.ToLower(b.Subject.Name)),
		cmp.Compare(idString(a.Subject.ID), idString(b.Subject.ID)))
}

func idString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
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

// grantsOf reads a project's grants as stored.
func grantsOf(ctx context.Context, tx db.DBTX, project uuid.UUID) ([]Grant, error) {
	rows, err := tx.Query(ctx, `SELECT `+subjectColumns+`, g.role FROM project_grant g`+subjectJoins+`
		WHERE g.project_id = $1 ORDER BY `+subjectOrder, project)
	if err != nil {
		return nil, fmt.Errorf("read the grants: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Grant, error) {
		var g Grant
		err := row.Scan(&g.Subject.Type, &g.Subject.ID, &g.Subject.Name, &g.Role)
		return g, err
	})
}

// Grants answers who holds which role in a project, for its administrators.
func (s *Service) Grants(ctx context.Context, actor, project uuid.UUID) ([]Grant, error) {
	out := []Grant{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := administering(ctx, tx, actor, project); err != nil {
			return err
		}
		grants, err := grantsOf(ctx, tx, project)
		if grants != nil {
			out = grants
		}
		return err
	})
	return out, err
}

// ReplaceGrants puts a whole new table of grants on a project, which keeps at least one person or group as
// its administrator.
func (s *Service) ReplaceGrants(ctx context.Context, actor, project uuid.UUID, in GrantsInput) ([]Grant, db.LSN, error) {
	var out []Grant
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		archived, err := administering(ctx, tx, actor, project)
		if err != nil {
			return err
		}
		if archived {
			return ErrArchived
		}
		if out, err = resolveGrants(ctx, tx, in.Grants); err != nil {
			return err
		}
		if !slices.ContainsFunc(out, func(g Grant) bool { return g.Role == RoleAdmin }) {
			return ErrNoAdministrator
		}
		before, err := grantsOf(ctx, tx, project)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM project_grant WHERE project_id = $1`, project); err != nil {
			return fmt.Errorf("clear the grants: %w", err)
		}
		for _, g := range out {
			var user, group *uuid.UUID
			switch g.Subject.Type {
			case SubjectUser:
				user = g.Subject.ID
			case SubjectGroup:
				group = g.Subject.ID
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO project_grant (project_id, subject_type, user_id, group_id, role)
				VALUES ($1, $2, $3, $4, $5)`, project, g.Subject.Type, user, group, g.Role); err != nil {
				return fmt.Errorf("grant %s to %s: %w", g.Role, g.Subject.Name, err)
			}
		}
		slices.SortFunc(out, compareGrants)
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionProjectGrantsChanged, TargetType: "project",
			TargetID: &project, Actor: actor, Data: map[string]any{"before": grantLog(before), "after": grantLog(out)}})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// grantLog is how the audit log keeps grants: ids and names as they were, since either may change later.
func grantLog(grants []Grant) []map[string]any {
	out := make([]map[string]any, 0, len(grants))
	for _, g := range grants {
		out = append(out, map[string]any{"type": g.Subject.Type, "id": g.Subject.ID, "name": g.Subject.Name, "role": g.Role})
	}
	return out
}

// resolveGrants checks every row of a new table and names its subject; a refusal names the row's field.
func resolveGrants(ctx context.Context, tx db.DBTX, rows []GrantInput) ([]Grant, error) {
	out := make([]Grant, 0, len(rows))
	seen := map[string]bool{}
	for i, row := range rows {
		field := fmt.Sprintf("grants.%d.", i)
		if !slices.Contains(Roles, row.Role) {
			return nil, &FieldError{field + "role", "Choose viewer, reviewer, editor or admin."}
		}
		sub, orgRole, err := resolveSubject(ctx, tx, field+"subject", row.Subject)
		if err != nil {
			return nil, err
		}
		key := string(sub.Type) + ":" + idString(sub.ID)
		if seen[key] {
			return nil, &FieldError{field + "subject", fmt.Sprintf(
				"%s is named twice. Give each person or group one role; the roles of their groups add up on their own.", sub.Name)}
		}
		seen[key] = true
		if row.Role == RoleAdmin && sub.Type == SubjectEveryone {
			return nil, &FieldError{field + "role", "Everyone can be given at most editor. Give admin to people or groups."}
		}
		if row.Role == RoleAdmin && orgRole == auth.RoleGuest {
			return nil, &FieldError{field + "role", fmt.Sprintf(
				"%s is a guest, who can be given at most editor. Make them a member first to give them admin.", sub.Name)}
		}
		out = append(out, Grant{Subject: sub, Role: row.Role})
	}
	return out, nil
}

// resolveSubject checks a subject exists in the installation and names it, with a person's role in the installation.
func resolveSubject(ctx context.Context, tx db.DBTX, field string, ref SubjectRef) (Subject, auth.OrgRole, error) {
	switch ref.Type {
	case SubjectEveryone:
		return Subject{Type: SubjectEveryone, Name: EveryoneName}, "", nil
	case SubjectUser, SubjectGroup:
	default:
		return Subject{}, "", &FieldError{field, "Name each subject as a user, a group or everyone."}
	}
	if ref.ID == nil {
		return Subject{}, "", &FieldError{field, "Give the id of each person or group you name."}
	}
	var (
		name    string
		orgRole auth.OrgRole
		err     error
	)
	if ref.Type == SubjectUser {
		err = tx.QueryRow(ctx, `
			SELECT name, role FROM app_user WHERE id = $1 AND role IS NOT NULL`, *ref.ID).Scan(&name, &orgRole)
	} else {
		err = tx.QueryRow(ctx, `SELECT name FROM groups WHERE id = $1`, *ref.ID).Scan(&name)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Subject{}, "", &FieldError{field, fmt.Sprintf("There is no %s with the id %s here. Pick one from the list.", ref.Type, *ref.ID)}
	}
	if err != nil {
		return Subject{}, "", err
	}
	id := *ref.ID
	return Subject{Type: ref.Type, ID: &id, Name: name}, orgRole, nil
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
		if people != nil {
			out = people
		}
		return err
	})
	return out, err
}

// Groups are the installation's groups whose name, or a word of it, starts with q.
func (s *Service) Groups(ctx context.Context, q string, limit int) ([]Group, error) {
	out := []Group{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT g.id, g.name, (SELECT count(*)::int FROM group_member gm WHERE gm.group_id = g.id), g.source = 'oidc'
			FROM groups g
			WHERE (g.name ILIKE $1 OR g.name ILIKE '% ' || $1)
			ORDER BY lower(g.name), g.id LIMIT $2`, likePrefix(q), limit)
		if err != nil {
			return err
		}
		groups, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Group])
		if groups != nil {
			out = groups
		}
		return err
	})
	return out, err
}

// ViewableRequirement is the SQL condition every query that lists requirements narrows itself with, so a count
// counts only what the actor, bound as parameter $actorParam, may read of the requirements aliased alias.
func ViewableRequirement(alias string, actorParam int) string {
	return fmt.Sprintf("perm_requirement_viewable(%s.id, $%d::uuid)", alias, actorParam)
}
