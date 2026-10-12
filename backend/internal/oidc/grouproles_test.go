package oidc

import (
	"slices"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/auth"
)

func TestTheHighestMappedRoleWins(t *testing.T) {
	mapping := map[string]auth.OrgRole{
		"engineering":            auth.RoleMember,
		"marketing":              auth.RoleMember,
		"exciter-administrators": auth.RoleAdmin,
		"ops":                    auth.RoleAdmin,
		// The database refuses to store it; the resolver does not lean on that.
		"founders": auth.RoleOwner,
	}
	for _, tc := range []struct {
		name    string
		claimed []string
		role    auth.OrgRole
		from    []string
	}{
		{"no groups", nil, "", nil},
		{"only unmapped groups", []string{"sales", "Engineering"}, "", nil},
		{"one member group", []string{"engineering"}, auth.RoleMember, []string{"engineering"}},
		{"two groups granting the same role", []string{"marketing", "engineering"}, auth.RoleMember, []string{"engineering", "marketing"}},
		{"admin beats member whatever the order", []string{"engineering", "exciter-administrators"}, auth.RoleAdmin, []string{"exciter-administrators"}},
		{"admin first", []string{"exciter-administrators", "engineering"}, auth.RoleAdmin, []string{"exciter-administrators"}},
		{"two admin groups", []string{"ops", "engineering", "exciter-administrators"}, auth.RoleAdmin, []string{"exciter-administrators", "ops"}},
		{"owner is never granted", []string{"founders"}, "", nil},
		{"owner is ignored beside a real grant", []string{"founders", "engineering"}, auth.RoleMember, []string{"engineering"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			role, from := grantedRole(mapping, tc.claimed)
			if role != tc.role || !slices.Equal(from, tc.from) {
				t.Fatalf("grantedRole(%q) = %q from %q, want %q from %q", tc.claimed, role, from, tc.role, tc.from)
			}
		})
	}
}

func TestAnEmptyMappingGrantsNothing(t *testing.T) {
	if role, from := grantedRole(nil, []string{"engineering"}); role != "" || len(from) != 0 {
		t.Fatalf("an empty mapping granted %q from %q", role, from)
	}
}

func TestASignInFollowsTheMapping(t *testing.T) {
	manual := func(r auth.OrgRole) *standing { return &standing{Role: r, Source: auth.RoleSourceManual} }
	provider := func(r auth.OrgRole) *standing { return &standing{Role: r, Source: auth.RoleSourceProvider} }
	for _, tc := range []struct {
		name    string
		current *standing
		granted auth.OrgRole
		want    *standing
	}{
		{"a stranger in no mapped group waits", nil, "", nil},
		{"a stranger in a mapped group joins with its role", nil, auth.RoleMember, provider(auth.RoleMember)},
		{"a stranger in an admin group joins as admin", nil, auth.RoleAdmin, provider(auth.RoleAdmin)},
		{"a hand-made member in an admin group is promoted", manual(auth.RoleMember), auth.RoleAdmin, provider(auth.RoleAdmin)},
		{"a hand-made admin in a member group follows the group", manual(auth.RoleAdmin), auth.RoleMember, provider(auth.RoleMember)},
		{"a hand-made admin in no mapped group is left alone", manual(auth.RoleAdmin), "", manual(auth.RoleAdmin)},
		{"a hand-made member in no mapped group is left alone", manual(auth.RoleMember), "", manual(auth.RoleMember)},
		{"a mapped admin whose group went is a member again", provider(auth.RoleAdmin), "", manual(auth.RoleMember)},
		{"a mapped member whose group went stays a member", provider(auth.RoleMember), "", manual(auth.RoleMember)},
		{"a mapped admin still in the group stays", provider(auth.RoleAdmin), auth.RoleAdmin, provider(auth.RoleAdmin)},
		{"the owner in a member group stays owner", manual(auth.RoleOwner), auth.RoleMember, manual(auth.RoleOwner)},
		{"the owner in an admin group stays owner", manual(auth.RoleOwner), auth.RoleAdmin, manual(auth.RoleOwner)},
		{"the owner in no group stays owner", manual(auth.RoleOwner), "", manual(auth.RoleOwner)},
		{"a guest in an admin group stays a guest", manual(auth.RoleGuest), auth.RoleAdmin, manual(auth.RoleGuest)},
		{"a guest in a member group stays a guest", manual(auth.RoleGuest), auth.RoleMember, manual(auth.RoleGuest)},
		{"a guest in no group stays a guest", manual(auth.RoleGuest), "", manual(auth.RoleGuest)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := nextStanding(tc.current, tc.granted)
			switch {
			case got == nil && tc.want == nil:
			case got == nil || tc.want == nil || *got != *tc.want:
				t.Fatalf("nextStanding(%v, %q) = %v, want %v", tc.current, tc.granted, got, tc.want)
			}
		})
	}
}
