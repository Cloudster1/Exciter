package perm

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// The access inspector decides nothing itself: every verdict is what perm_project_holds answers, and the steps
// lay out the facts it reads, so the explanation cannot drift from what the database enforces.

// InspectGrant is a grant that reaches the person inspected, with what its role holds.
type InspectGrant struct {
	Assignment
	Permissions []string
}

// InspectFacts are the database's answers for one person and one project.
type InspectFacts struct {
	OrgRole     auth.OrgRole
	Administers bool
	Archived    bool
	Grants      []InspectGrant
	// Verdict is perm_project_holds for each right.
	Verdict map[Right]bool
}

// frozen are the rights an archived project takes away.
var frozen = []Right{RightComment, RightEdit, RightApprove}

// Explain lays the facts out as the steps of each right; the first step not passed is the reason for a no.
func Explain(f InspectFacts) []AccessRight {
	out := make([]AccessRight, 0, len(Rights))
	for _, right := range Rights {
		a := AccessRight{Right: right, Allowed: f.Verdict[right]}
		if f.Administers {
			a.Steps = append(a.Steps, step(AccessStep{Kind: StepOrgAdmin, Passed: true}))
		} else {
			giving := f.giving(right)
			a.Steps = append(a.Steps,
				step(AccessStep{Kind: StepGuest, Passed: f.OrgRole != auth.RoleGuest || len(giving) > 0 || !f.cappedAway(right)}),
				step(AccessStep{Kind: StepGrant, Passed: len(giving) > 0, Grants: giving}))
		}
		if f.Archived && slices.Contains(frozen, right) {
			a.Steps = append(a.Steps, step(AccessStep{Kind: StepArchived}))
		}
		out = append(out, a)
	}
	return out
}

func step(s AccessStep) AccessStep {
	if s.Grants == nil {
		s.Grants = []Assignment{}
	}
	return s
}

// held are the permissions a role gives somebody of a standing, as perm_role_held answers them: a guest a role to
// run the project reaches works in it as an editor does.
func held(standing auth.OrgRole, permissions []string) []string {
	if standing != auth.RoleGuest || !slices.Contains(permissions, string(RightAdminister)) {
		return permissions
	}
	return slices.DeleteFunc(slices.Clone(permissions), func(p string) bool {
		return p == string(RightAdminister) || p == string(RightApprove)
	})
}

// giving are the grants that reach the person and give the right.
func (f InspectFacts) giving(right Right) []Assignment {
	var out []Assignment
	for _, g := range f.Grants {
		if slices.Contains(held(f.OrgRole, g.Permissions), string(right)) {
			out = append(out, g.Assignment)
		}
	}
	return out
}

// cappedAway says a grant reaching a guest would give the right to a member, which is what being a guest takes.
func (f InspectFacts) cappedAway(right Right) bool {
	return slices.ContainsFunc(f.Grants, func(g InspectGrant) bool { return slices.Contains(g.Permissions, string(right)) })
}

// Access explains what person may do in project, for one of its administrators; a non-member is
// auth.ErrNoSuchMember.
func (s *Service) Access(ctx context.Context, actor, project, person uuid.UUID) (*AccessReport, error) {
	var out *AccessReport
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := administering(ctx, tx, actor, project); err != nil {
			return err
		}
		report, err := inspect(ctx, tx, project, person)
		out = report
		return err
	})
	return out, err
}

func inspect(ctx context.Context, tx db.DBTX, project, person uuid.UUID) (*AccessReport, error) {
	report := &AccessReport{}
	err := tx.QueryRow(ctx, `
		SELECT u.id, u.name, u.email::text, u.role, perm_is_administrator(u.id)
		FROM app_user u WHERE u.id = $1 AND u.role IS NOT NULL`, person).Scan(
		&report.Person.ID, &report.Person.Name, &report.Person.Email, &report.Role, &report.Administers)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrNoSuchMember
	}
	if err != nil {
		return nil, fmt.Errorf("read the person: %w", err)
	}
	f := InspectFacts{OrgRole: report.Role, Administers: report.Administers, Verdict: map[Right]bool{}}
	var verdict []bool
	names := make([]string, len(Rights))
	for i, right := range Rights {
		names[i] = string(right)
	}
	if err := tx.QueryRow(ctx, `
		SELECT p.archived_at IS NOT NULL, (SELECT array_agg(perm_project_holds(p.id, $2, r) ORDER BY n) FROM unnest($3::text[]) WITH ORDINALITY AS x (r, n))
		FROM project p WHERE p.id = $1`, project, person, names).Scan(&f.Archived, &verdict); err != nil {
		return nil, fmt.Errorf("ask the database: %w", err)
	}
	for i, right := range Rights {
		f.Verdict[right] = i < len(verdict) && verdict[i]
	}
	// The grants come through perm_grants_reaching, which a project's administrator may read for its people even
	// where the grant is across the installation and row level security hides it.
	rows, err := tx.Query(ctx, `
		SELECT f.id, f.role, r.name, f.project_id, COALESCE(p.key, ''), COALESCE(p.name, ''),
		       f.user_id, COALESCE(u.name, ''), COALESCE(u.email::text, ''), COALESCE(u.role = 'guest', false),
		       f.group_id, COALESCE(g.name, ''), COALESCE(g.builtin, ''), f.created_at, r.permissions
		FROM perm_grants_reaching($2, $1) f
		JOIN role r ON r.key = f.role
		LEFT JOIN project p ON p.id = f.project_id
		LEFT JOIN app_user u ON u.id = f.user_id
		LEFT JOIN groups g ON g.id = f.group_id
		ORDER BY f.project_id IS NOT NULL, r.sort_order, lower(r.name), g.builtin IS NULL, f.group_id IS NULL,
		         lower(COALESCE(g.name, u.name, '')), f.id`, project, person)
	if err != nil {
		return nil, fmt.Errorf("read the grants: %w", err)
	}
	if f.Grants, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (InspectGrant, error) {
		var g InspectGrant
		a := &g.Assignment
		err := row.Scan(&a.ID, &a.Role, &a.RoleName, &a.ProjectID, &a.ProjectKey, &a.ProjectName,
			&a.UserID, &a.UserName, &a.UserEmail, &a.Guest, &a.GroupID, &a.GroupName, &a.GroupBuiltin, &a.CreatedAt, &g.Permissions)
		return g, err
	}); err != nil {
		return nil, fmt.Errorf("read the grants: %w", err)
	}
	report.Rights = Explain(f)
	return report, nil
}
