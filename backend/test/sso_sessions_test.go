//go:build integration

package test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/oidc"
)

// The same subject is the same person whatever address it arrives with, and
// groups kept by hand are the provider's to leave alone.
func TestSignInUpsertsBySubjectAndSyncsOnlyProviderGroups(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	ctx := context.Background()
	const issuer = "https://id.upsert.test"
	if _, _, err := a.sso.Save(withActor(h.addPerson(t, "admin")), oidc.Provider{Issuer: issuer, ClientID: exciterClient, CreateGroups: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	email := "carol-" + h.suffix() + "@suite.test"
	renamed := "carol-renamed-" + h.suffix() + "@suite.test"
	h.forgetPerson(t, email)
	h.forgetPerson(t, renamed)
	identity := &oidc.Identity{Issuer: issuer, Subject: "carol-" + uuid.NewString(), Email: email, Name: "Carol", Groups: []string{"a", "b"}}

	// Not a member yet: refused, with a request noted and nothing else given.
	if _, err := a.sso.SignIn(ctx, identity, time.Hour, "", ""); !errors.Is(err, oidc.ErrNotAMember) {
		t.Fatalf("a first sign-in = %v, want oidc.ErrNotAMember", err)
	}
	var waiting uuid.UUID
	if err := h.super.QueryRow(ctx, `
		SELECT r.user_id FROM join_request r JOIN app_user u ON u.id = r.user_id
		WHERE u.email = $1 AND u.role IS NULL
		  AND NOT EXISTS (SELECT 1 FROM user_session s WHERE s.user_id = r.user_id)
		  AND NOT EXISTS (SELECT 1 FROM group_member g WHERE g.user_id = r.user_id)`, email).Scan(&waiting); err != nil {
		t.Fatalf("no bare request for carol: %v", err)
	}
	if _, _, err := a.accounts.AdmitJoinRequest(ctx, waiting, auth.RoleMember, uuid.Nil, ""); err != nil {
		t.Fatal(err)
	}

	first, err := a.sso.SignIn(ctx, identity, time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.UserID != waiting {
		t.Fatal("the admitted person signed in as somebody else")
	}
	var local uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO groups (name) VALUES ($1) RETURNING id`, "hand-made-"+h.suffix()).Scan(&local); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, local, first.UserID); err != nil {
		t.Fatal(err)
	}

	identity.Email, identity.Name, identity.Groups = renamed, "Carol Renamed", []string{"b", "c"}
	second, err := a.sso.SignIn(ctx, identity, time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.UserID != first.UserID {
		t.Fatal("the same subject became a second person")
	}
	if !slices.Equal(second.Joined, []string{"c"}) || !slices.Equal(second.Left, []string{"a"}) {
		t.Errorf("joined %q left %q, want c and a", second.Joined, second.Left)
	}
	var gotEmail, gotName string
	var memberships int
	if err := h.super.QueryRow(ctx, `
		SELECT u.email, u.name, (SELECT count(*) FROM group_member m JOIN groups g ON g.id = m.group_id
		                         WHERE m.user_id = u.id AND g.builtin IS NULL)
		FROM app_user u WHERE u.id = $1`, first.UserID).Scan(&gotEmail, &gotName, &memberships); err != nil {
		t.Fatal(err)
	}
	if gotEmail != renamed || gotName != "Carol Renamed" || memberships != 3 {
		t.Errorf("account = %s %q in %d groups, want the new address and name in b, c and the local group besides Everyone", gotEmail, gotName, memberships)
	}

	t.Run("a first sign-in finds the member account using the address", func(t *testing.T) {
		admin := bootstrapAdmin(t, h, a)
		adminID := h.userID(t, admin)
		s, err := a.sso.SignIn(ctx, &oidc.Identity{Issuer: issuer, Subject: "admin-" + uuid.NewString(), Email: admin, Name: "Admin"}, time.Hour, "", "")
		if err != nil || s.UserID != adminID {
			t.Fatalf("the administrator signed in as %v (%v), want %v", s, err, adminID)
		}
	})

	t.Run("a deactivated account is refused however the provider vouches", func(t *testing.T) {
		if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, first.UserID); err != nil {
			t.Fatal(err)
		}
		if _, err := a.sso.SignIn(ctx, identity, time.Hour, "", ""); !errors.Is(err, auth.ErrSwitchedOff) {
			t.Fatalf("a deactivated sign-in = %v, want auth.ErrSwitchedOff", err)
		}
	})
}
