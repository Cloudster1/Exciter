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

// InspectGrant is one of a project's grants, and whether it reaches the person inspected.
type InspectGrant struct {
	Grant
	Reaches bool
}

// InspectFacts are the database's answers for one person and one project.
type InspectFacts struct {
	OrgRole  auth.OrgRole
	Archived bool
	Grants   []InspectGrant
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
		if f.OrgRole.CanAdminister() {
			a.Steps = append(a.Steps, step(AccessStep{Kind: StepOrgAdmin, Passed: true}))
		} else {
			giving := f.giving(right)
			a.Steps = append(a.Steps,
				step(AccessStep{Kind: StepGuest, Passed: f.OrgRole != auth.RoleGuest || len(giving) > 0 || !f.everyoneGives(right)}),
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
		s.Grants = []Grant{}
	}
	return s
}

// giving are the grants that reach the person and give the right, a guest's capped at editor.
func (f InspectFacts) giving(right Right) []Grant {
	var out []Grant
	for _, g := range f.Grants {
		if g.Reaches && slices.Contains(rightsOf(cappedRole(f.OrgRole, g.Role)), right) {
			out = append(out, g.Grant)
		}
	}
	return out
}

// everyoneGives says everyone's grant would give the right, which is what a guest misses.
func (f InspectFacts) everyoneGives(right Right) bool {
	return slices.ContainsFunc(f.Grants, func(g InspectGrant) bool {
		return g.Subject.Type == SubjectEveryone && slices.Contains(rightsOf(g.Role), right)
	})
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
		SELECT u.id, u.name, u.email::text, m.org_role, m.role_source
		FROM org_member m JOIN app_user u ON u.id = m.user_id
		WHERE m.user_id = $1`, person).Scan(
		&report.Person.ID, &report.Person.Name, &report.Person.Email, &report.Role, &report.RoleSource)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrNoSuchMember
	}
	if err != nil {
		return nil, fmt.Errorf("read the person: %w", err)
	}
	f := InspectFacts{OrgRole: report.Role, Verdict: map[Right]bool{}}
	var held []bool
	names := make([]string, len(Rights))
	for i, right := range Rights {
		names[i] = string(right)
	}
	if err := tx.QueryRow(ctx, `
		SELECT p.archived_at IS NOT NULL, (SELECT array_agg(perm_project_holds(p.id, $2, r) ORDER BY n) FROM unnest($3::text[]) WITH ORDINALITY AS x (r, n))
		FROM project p WHERE p.id = $1`, project, person, names).Scan(&f.Archived, &held); err != nil {
		return nil, fmt.Errorf("ask the database: %w", err)
	}
	for i, right := range Rights {
		f.Verdict[right] = i < len(held) && held[i]
	}
	rows, err := tx.Query(ctx, `SELECT `+subjectColumns+`, g.role, perm_grant_reaches($2, $3, g.subject_type, g.user_id, g.group_id)
		FROM project_grant g`+subjectJoins+` WHERE g.project_id = $1 ORDER BY `+subjectOrder, project, person, report.Role)
	if err != nil {
		return nil, fmt.Errorf("read the grants: %w", err)
	}
	if f.Grants, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (InspectGrant, error) {
		var g InspectGrant
		err := row.Scan(&g.Subject.Type, &g.Subject.ID, &g.Subject.Name, &g.Role, &g.Reaches)
		return g, err
	}); err != nil {
		return nil, fmt.Errorf("read the grants: %w", err)
	}
	report.Rights = Explain(f)
	return report, nil
}
