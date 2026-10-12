package perm

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
)

func grantTo(t SubjectType, role Role, reaches bool) InspectGrant {
	sub := Subject{Type: t, Name: string(t)}
	if t != SubjectEveryone {
		id := uuid.New()
		sub.ID = &id
	}
	return InspectGrant{Grant: Grant{Subject: sub, Role: role}, Reaches: reaches}
}

// reason is the kind of the first step not passed, which is what a no is explained by.
func reason(a AccessRight) StepKind {
	for _, s := range a.Steps {
		if !s.Passed {
			return s.Kind
		}
	}
	return ""
}

func rightOf(rights []AccessRight, r Right) AccessRight {
	return rights[slices.IndexFunc(rights, func(a AccessRight) bool { return a.Right == r })]
}

func TestRolesAddUp(t *testing.T) {
	f := InspectFacts{OrgRole: auth.RoleMember, Grants: []InspectGrant{
		grantTo(SubjectGroup, RoleEditor, true), grantTo(SubjectUser, RoleReviewer, true), grantTo(SubjectUser, RoleAdmin, false),
	}}
	for _, r := range []Right{RightView, RightComment, RightEdit, RightApprove} {
		if got := reason(rightOf(Explain(f), r)); got != "" {
			t.Errorf("an editor through a group and a reviewer on their own is refused %s by %s", r, got)
		}
	}
	if got := reason(rightOf(Explain(f), RightAdminister)); got != StepGrant {
		t.Errorf("administer comes from a grant that does not reach them: %q", got)
	}
	if got := rightOf(Explain(f), RightApprove).Steps[1].Grants; len(got) != 1 || got[0].Role != RoleReviewer {
		t.Errorf("approve names only the reviewer grant: %v", got)
	}
}

func TestAGuestIsReachedOnlyByGrantsNamingThem(t *testing.T) {
	everyone := grantTo(SubjectEveryone, RoleEditor, false)
	f := InspectFacts{OrgRole: auth.RoleGuest, Grants: []InspectGrant{everyone}}
	if got := reason(rightOf(Explain(f), RightEdit)); got != StepGuest {
		t.Errorf("a guest whom only everyone's grant would let edit is refused by %q, want guest", got)
	}
	if got := reason(rightOf(Explain(f), RightApprove)); got != StepGrant {
		t.Errorf("approve, which everyone's grant does not give either, is refused by %q, want grant", got)
	}
	f.Grants = append(f.Grants, grantTo(SubjectGroup, RoleAdmin, true))
	if got := reason(rightOf(Explain(f), RightEdit)); got != "" {
		t.Errorf("a guest's group grant lets them edit, but %q refused", got)
	}
	if got := reason(rightOf(Explain(f), RightAdminister)); got != StepGrant {
		t.Errorf("an admin grant reaching a guest counts as editor, but administer was refused by %q", got)
	}
}

func TestOrgAdministratorsHoldEverythingAndArchivingFreezesChanges(t *testing.T) {
	f := InspectFacts{OrgRole: auth.RoleAdmin, Archived: true}
	for _, a := range Explain(f) {
		frozen := a.Right == RightComment || a.Right == RightEdit || a.Right == RightApprove
		if got := reason(a); (got == StepArchived) != frozen || (!frozen && got != "") {
			t.Errorf("%s on an archived project is refused by %q", a.Right, got)
		}
		if a.Steps[0].Kind != StepOrgAdmin || !a.Steps[0].Passed {
			t.Errorf("%s does not start with the organization's administrators: %v", a.Right, a.Steps[0])
		}
	}
}

func TestCappedRoleAndRightsMatchTheDatabase(t *testing.T) {
	want := map[Role][]Right{
		RoleViewer:   {RightView},
		RoleReviewer: {RightView, RightComment, RightApprove},
		RoleEditor:   {RightView, RightComment, RightEdit},
		RoleAdmin:    Rights,
	}
	for role, rights := range want {
		if got := rightsOf(role); !slices.Equal(got, rights) {
			t.Errorf("%s gives %v, want %v", role, got, rights)
		}
	}
	if cappedRole(auth.RoleGuest, RoleAdmin) != RoleEditor || cappedRole(auth.RoleMember, RoleAdmin) != RoleAdmin {
		t.Error("only a guest's admin grants count as editor")
	}
}
