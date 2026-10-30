package perm

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
)

// builtin are what the built-in roles hold as the migration makes them.
var builtin = map[string][]string{
	RoleViewer:               {"view"},
	RoleReviewer:             {"view", "comment", "approve"},
	RoleEditor:               {"view", "comment", "edit"},
	RoleProjectAdministrator: {"view", "comment", "edit", "approve", "administer"},
}

func grantOf(role string, permissions []string, group bool) InspectGrant {
	id := uuid.New()
	a := Assignment{ID: uuid.New(), Role: role, RoleName: role}
	if group {
		a.GroupID, a.GroupName = &id, "a group"
	} else {
		a.UserID, a.UserName = &id, "somebody"
	}
	return InspectGrant{Assignment: a, Permissions: permissions}
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
		grantOf(RoleEditor, builtin[RoleEditor], true), grantOf(RoleReviewer, builtin[RoleReviewer], false),
	}}
	for _, r := range []Right{RightView, RightComment, RightEdit, RightApprove} {
		if got := reason(rightOf(Explain(f), r)); got != "" {
			t.Errorf("an editor through a group and a reviewer on their own is refused %s by %s", r, got)
		}
	}
	if got := reason(rightOf(Explain(f), RightAdminister)); got != StepGrant {
		t.Errorf("administer, which no grant reaching them gives, is refused by %q", got)
	}
	if got := rightOf(Explain(f), RightApprove).Steps[1].Grants; len(got) != 1 || got[0].Role != RoleReviewer {
		t.Errorf("approve names only the reviewer grant: %v", got)
	}
}

func TestARoleOfTheInstallationsOwnGivesWhatItHolds(t *testing.T) {
	f := InspectFacts{OrgRole: auth.RoleMember, Grants: []InspectGrant{grantOf("signer", []string{"view", "approve"}, false)}}
	if got := reason(rightOf(Explain(f), RightApprove)); got != "" {
		t.Errorf("a role holding approve is refused approve by %q", got)
	}
	if got := reason(rightOf(Explain(f), RightComment)); got != StepGrant {
		t.Errorf("a role without comment lets them comment: %q", got)
	}
}

func TestAGuestNeverRunsAProject(t *testing.T) {
	f := InspectFacts{OrgRole: auth.RoleGuest, Grants: []InspectGrant{grantOf(RoleProjectAdministrator, builtin[RoleProjectAdministrator], true)}}
	if got := reason(rightOf(Explain(f), RightEdit)); got != "" {
		t.Errorf("a guest's group grant lets them edit, but %q refused", got)
	}
	for _, r := range []Right{RightAdminister, RightApprove} {
		if got := reason(rightOf(Explain(f), r)); got != StepGuest {
			t.Errorf("a role to run the project reaches a guest as an editor, but %s was refused by %q", r, got)
		}
	}
	f.Grants = append(f.Grants, grantOf(RoleReviewer, builtin[RoleReviewer], false))
	if got := reason(rightOf(Explain(f), RightApprove)); got != "" {
		t.Errorf("a guest named reviewer may approve, but %q refused", got)
	}
}

func TestAdministratorsHoldEverythingAndArchivingFreezesChanges(t *testing.T) {
	f := InspectFacts{OrgRole: auth.RoleMember, Administers: true, Archived: true}
	for _, a := range Explain(f) {
		frozen := a.Right == RightComment || a.Right == RightEdit || a.Right == RightApprove
		if got := reason(a); (got == StepArchived) != frozen || (!frozen && got != "") {
			t.Errorf("%s on an archived project is refused by %q", a.Right, got)
		}
		if a.Steps[0].Kind != StepOrgAdmin || !a.Steps[0].Passed {
			t.Errorf("%s does not start with the installation's administrators: %v", a.Right, a.Steps[0])
		}
	}
}

func TestHeldMatchesTheDatabase(t *testing.T) {
	admin := builtin[RoleProjectAdministrator]
	if got := held(auth.RoleMember, admin); !slices.Equal(got, admin) {
		t.Errorf("a member holds %v of the project administrator, want all of it", got)
	}
	if got := held(auth.RoleGuest, admin); !slices.Equal(got, builtin[RoleEditor]) {
		t.Errorf("a guest holds %v of the project administrator, want what an editor holds", got)
	}
	if got := held(auth.RoleGuest, builtin[RoleReviewer]); !slices.Equal(got, builtin[RoleReviewer]) {
		t.Errorf("a guest named reviewer holds %v", got)
	}
	if len(admin) != 5 {
		t.Error("held changed the role it was given")
	}
}

func TestKeysAreMadeFromNames(t *testing.T) {
	for name, want := range map[string]string{
		"Signer":           "signer",
		"  Lead reviewer ": "lead_reviewer",
		"QA / Safety":      "qa_safety",
		"2nd line":         "r_2nd_line",
		"Ünïcode only":     "n_code_only",
		"a very long name that goes on and on and on": "a_very_long_name_that_goes_on_and_on_and",
	} {
		if got := KeyFromName(name); got != want {
			t.Errorf("KeyFromName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestPermissionsAreCleanedIntoTheMatrixOrder(t *testing.T) {
	got, err := cleanPermissions([]Permission{PermAdministerInstallation, Permission(RightView), Permission(RightAdminister)})
	if err != nil || !slices.Equal(got, []string{"view", "administer", "administerInstallation"}) {
		t.Fatalf("cleanPermissions = %v, %v", got, err)
	}
	if _, err := cleanPermissions([]Permission{"fly"}); err == nil {
		t.Error("a permission nobody checks was taken")
	}
}

func TestEachPermissionSaysWhereItCounts(t *testing.T) {
	for _, p := range Permissions() {
		installation := p.Permission == PermCreateProjects || p.Permission == PermAdministerInstallation
		if (p.Scope == ScopeInstallation) != installation || p.Words == "" {
			t.Errorf("%s counts in %s and reads %q", p.Permission, p.Scope, p.Words)
		}
	}
}

func TestMineFollowsTheGrantsAcrossTheInstallation(t *testing.T) {
	for _, tc := range []struct {
		p    *auth.Principal
		want OrgCan
	}{
		{nil, OrgCan{}},
		{&auth.Principal{Role: auth.RoleMember}, OrgCan{}},
		{&auth.Principal{Role: auth.RoleMember, Granted: []string{"createProjects"}}, OrgCan{CreateProject: true}},
		{&auth.Principal{Role: auth.RoleMember, Granted: []string{"createProjects", "administerInstallation"}}, OrgCan{CreateProject: true, Administer: true}},
		{&auth.Principal{Role: auth.RoleGuest, Granted: []string{"createProjects", "administerInstallation"}}, OrgCan{}},
	} {
		if got := Mine(tc.p); got != tc.want {
			t.Errorf("Mine(%+v) = %+v, want %+v", tc.p, got, tc.want)
		}
	}
}
