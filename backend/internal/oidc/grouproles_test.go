package oidc

import (
	"slices"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/auth"
)

func TestTheGroupsThatHoldAGrantAreTheOnesThatAdmit(t *testing.T) {
	granted := map[string]bool{"engineering": true, "marketing": true, "exciter-administrators": true}
	for _, tc := range []struct {
		name    string
		claimed []string
		from    []string
	}{
		{"no groups", nil, []string{}},
		{"only groups without a grant", []string{"sales", "Engineering"}, []string{}},
		{"one group with a grant", []string{"engineering"}, []string{"engineering"}},
		{"two groups, in order", []string{"marketing", "engineering"}, []string{"engineering", "marketing"}},
		{"a group claimed twice counts once", []string{"engineering", "engineering"}, []string{"engineering"}},
		{"beside others", []string{"sales", "exciter-administrators"}, []string{"exciter-administrators"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := admittingGroups(granted, tc.claimed); !slices.Equal(got, tc.from) {
				t.Fatalf("admittingGroups(%q) = %q, want %q", tc.claimed, got, tc.from)
			}
		})
	}
}

func TestNoGrantAdmitsNobody(t *testing.T) {
	if from := admittingGroups(nil, []string{"engineering"}); len(from) != 0 {
		t.Fatalf("no group with a grant admitted through %q", from)
	}
}

func TestASignInLetsInByAGroupWithAGrant(t *testing.T) {
	role := func(r auth.OrgRole) *auth.OrgRole { return &r }
	for _, tc := range []struct {
		name      string
		current   *auth.OrgRole
		admitting []string
		want      *auth.OrgRole
	}{
		{"a stranger in no group with a grant waits", nil, nil, nil},
		{"a stranger in a group with a grant joins as a member", nil, []string{"engineering"}, role(auth.RoleMember)},
		{"a member stays a member, whatever the groups", role(auth.RoleMember), []string{"engineering"}, role(auth.RoleMember)},
		{"a member in no group with a grant stays a member", role(auth.RoleMember), nil, role(auth.RoleMember)},
		{"a guest in a group with a grant stays a guest", role(auth.RoleGuest), []string{"engineering"}, role(auth.RoleGuest)},
		{"a guest in no group stays a guest", role(auth.RoleGuest), nil, role(auth.RoleGuest)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, admit := range []bool{true, false} {
				want := tc.want
				// Off, a group with a grant lets nobody new in.
				if !admit && tc.current == nil {
					want = nil
				}
				got := nextStanding(tc.current, tc.admitting, admit)
				switch {
				case got == nil && want == nil:
				case got == nil || want == nil || *got != *want:
					t.Fatalf("nextStanding(%v, %q, admit %v) = %v, want %v", tc.current, tc.admitting, admit, got, want)
				}
			}
		})
	}
}
