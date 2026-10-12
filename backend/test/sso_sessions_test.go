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
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// trustIssuer gives org a provider of that issuer behind the policies' back, turned on or off.
func (h *harness) trustIssuer(t *testing.T, org tenant.Org, issuer string, enabled bool) {
	t.Helper()
	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO oidc_provider (org_id, issuer, client_id, enabled) VALUES ($1, $2, 'exciter', $3)
		ON CONFLICT (org_id) DO UPDATE SET issuer = EXCLUDED.issuer, enabled = EXCLUDED.enabled`, org.ID, issuer, enabled); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
}

// reaches asks the database's own rule whether a provider's sign-in for home reaches target.
func (h *harness) reaches(t *testing.T, home, target tenant.Org, person uuid.UUID) bool {
	t.Helper()
	var ok bool
	if err := h.super.QueryRow(context.Background(), `SELECT session_reaches('oidc', $1, $2, $3)`, home.ID, person, target.ID).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

// moveSession points a session at another organization straight through SQL, as a code path that forgot to ask
// would, through the admin role that passes every policy; only the trigger stands in the way.
func (h *harness) moveSession(sessionID uuid.UUID, target tenant.Org) error {
	_, err := h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE user_session SET current_org_id = $2 WHERE id = $1`, sessionID, target.ID)
		return err
	})
	return err
}

// A provider's sign-in reaches the organizations that trust the same provider and the ones the person owns, and
// no other, whether the service is asked or the database is written to directly.
func TestAProvidersSignInReachesWhereTheSameProviderIsTrusted(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	home, away, elsewhere, owned := h.makeOrg(t, "reach-home"), h.makeOrg(t, "reach-away"), h.makeOrg(t, "reach-elsewhere"), h.makeOrg(t, "reach-owned")
	person := h.addPerson(t, home.ID, "member")
	ctx := context.Background()
	for org, role := range map[tenant.Org]string{away: "member", elsewhere: "member", owned: "owner"} {
		if _, err := h.super.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, $3)`, org.ID, person, role); err != nil {
			t.Fatal(err)
		}
	}
	var sessionID uuid.UUID
	if _, err := h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		sessionID, _, err = auth.OpenSession(ctx, tx, person, &home.ID, auth.ProofOIDC, time.Now().Add(time.Hour), "", "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	p := &auth.Principal{UserID: person, SessionID: &sessionID}
	reachable := func() []string {
		orgs, err := a.accounts.Organizations(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		var slugs []string
		for _, o := range orgs {
			slugs = append(slugs, o.OrgSlug)
		}
		slices.Sort(slugs)
		return slugs
	}
	sorted := func(orgs ...tenant.Org) []string {
		var slugs []string
		for _, o := range orgs {
			slugs = append(slugs, o.Slug)
		}
		slices.Sort(slugs)
		return slugs
	}

	t.Run("with no provider anywhere it reaches home and what the person owns", func(t *testing.T) {
		if got := reachable(); !slices.Equal(got, sorted(home, owned)) {
			t.Fatalf("reaches %v, want home and the owned organization", got)
		}
		if h.reaches(t, home, away, person) {
			t.Error("the database lets the sign-in reach an organization that trusts nothing")
		}
		if _, _, err := a.accounts.SwitchOrg(ctx, sessionID, person, away.Slug); !errors.Is(err, auth.ErrSessionStaysHome) {
			t.Fatalf("switching away = %v, want ErrSessionStaysHome", err)
		}
		if err := h.moveSession(sessionID, away); !isCheckViolation(err) {
			t.Fatalf("a direct update moved the session away: %v", err)
		}
	})

	h.trustIssuer(t, home, "https://id.shared.test/realms/suite", true)
	h.trustIssuer(t, away, "https://id.shared.test/realms/suite", true)
	h.trustIssuer(t, elsewhere, "https://id.other.test/realms/suite", true)

	t.Run("an organization trusting the same issuer is reached", func(t *testing.T) {
		if !h.reaches(t, home, away, person) {
			t.Fatal("the database does not let the sign-in reach an organization trusting the same issuer")
		}
		if got := reachable(); !slices.Equal(got, sorted(home, away, owned)) {
			t.Fatalf("reaches %v, want home, away and the owned organization", got)
		}
		if _, _, err := a.accounts.SwitchOrg(ctx, sessionID, person, away.Slug); err != nil {
			t.Fatalf("switching to an organization sharing the provider = %v", err)
		}
	})

	t.Run("one trusting another issuer is not, even written to directly", func(t *testing.T) {
		if h.reaches(t, home, elsewhere, person) {
			t.Fatal("the database lets the sign-in reach an organization with another provider")
		}
		if err := h.moveSession(sessionID, elsewhere); !isCheckViolation(err) {
			t.Fatalf("a direct update moved the session to another provider's organization: %v", err)
		}
	})

	t.Run("a provider turned off is trusted no longer", func(t *testing.T) {
		h.trustIssuer(t, away, "https://id.shared.test/realms/suite", false)
		if h.reaches(t, home, away, person) {
			t.Fatal("a provider turned off still vouches for the sign-in")
		}
		if err := h.moveSession(sessionID, home); err != nil {
			t.Fatalf("going back home = %v", err)
		}
		if err := h.moveSession(sessionID, away); !isCheckViolation(err) {
			t.Fatalf("a direct update moved the session where the provider is off: %v", err)
		}
	})

	t.Run("a password reaches every membership", func(t *testing.T) {
		var ok bool
		if err := h.super.QueryRow(ctx, `SELECT session_reaches('password', $1, $2, $3)`, home.ID, person, elsewhere.ID).Scan(&ok); err != nil || !ok {
			t.Fatalf("a password session reaches elsewhere = %v (%v)", ok, err)
		}
	})
}

// The same subject is the same person whatever address it arrives with, and
// groups kept by hand are the provider's to leave alone.
func TestSignInUpsertsBySubjectAndSyncsOnlyProviderGroups(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	org := h.makeOrg(t, "upsert")
	ctx := context.Background()
	const issuer = "https://id.upsert.test"
	if _, _, err := a.sso.Save(withActor(org, h.addPerson(t, org.ID, "admin")), oidc.Provider{Issuer: issuer, ClientID: exciterClient, CreateGroups: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	email := "carol-" + h.suffix() + "@suite.test"
	renamed := "carol-renamed-" + h.suffix() + "@suite.test"
	h.forgetPerson(t, email)
	h.forgetPerson(t, renamed)
	identity := &oidc.Identity{Issuer: issuer, Subject: "carol-" + uuid.NewString(), Email: email, Name: "Carol", Groups: []string{"a", "b"}}

	// Not a member yet: refused, with a request noted and nothing else given.
	if _, err := a.sso.SignIn(ctx, org.ID, identity, time.Hour, "", ""); !errors.Is(err, oidc.ErrNotAMember) {
		t.Fatalf("a first sign-in = %v, want oidc.ErrNotAMember", err)
	}
	var waiting uuid.UUID
	if err := h.super.QueryRow(ctx, `
		SELECT r.user_id FROM org_join_request r JOIN app_user u ON u.id = r.user_id
		WHERE r.org_id = $1 AND u.email = $2
		  AND NOT EXISTS (SELECT 1 FROM org_member m WHERE m.org_id = r.org_id AND m.user_id = r.user_id)
		  AND NOT EXISTS (SELECT 1 FROM user_session s WHERE s.user_id = r.user_id)
		  AND NOT EXISTS (SELECT 1 FROM group_member g WHERE g.user_id = r.user_id)`, org.ID, email).Scan(&waiting); err != nil {
		t.Fatalf("no bare request for carol: %v", err)
	}
	if _, _, err := a.accounts.AdmitJoinRequest(ctx, org.ID, waiting, auth.RoleMember, uuid.Nil, ""); err != nil {
		t.Fatal(err)
	}

	first, err := a.sso.SignIn(ctx, org.ID, identity, time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.UserID != waiting {
		t.Fatal("the admitted person signed in as somebody else")
	}
	var local uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO groups (org_id, name) VALUES ($1, $2) RETURNING id`, org.ID, "hand-made-"+h.suffix()).Scan(&local); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO group_member (org_id, group_id, user_id) VALUES ($1, $2, $3)`, org.ID, local, first.UserID); err != nil {
		t.Fatal(err)
	}

	identity.Email, identity.Name, identity.Groups = renamed, "Carol Renamed", []string{"b", "c"}
	second, err := a.sso.SignIn(ctx, org.ID, identity, time.Hour, "", "")
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
		SELECT u.email, u.name, (SELECT count(*) FROM group_member m WHERE m.user_id = u.id AND m.org_id = $2)
		FROM app_user u WHERE u.id = $1`, first.UserID, org.ID).Scan(&gotEmail, &gotName, &memberships); err != nil {
		t.Fatal(err)
	}
	if gotEmail != renamed || gotName != "Carol Renamed" || memberships != 3 {
		t.Errorf("account = %s %q in %d groups, want the new address and name in b, c and the local group", gotEmail, gotName, memberships)
	}

	t.Run("a first sign-in finds the member account using the address", func(t *testing.T) {
		admin := bootstrapAdmin(t, h, a, org.ID)
		adminID := h.userID(t, admin)
		s, err := a.sso.SignIn(ctx, org.ID, &oidc.Identity{Issuer: issuer, Subject: "admin-" + uuid.NewString(), Email: admin, Name: "Admin"}, time.Hour, "", "")
		if err != nil || s.UserID != adminID {
			t.Fatalf("the administrator signed in as %v (%v), want %v", s, err, adminID)
		}
	})

	t.Run("a deactivated account is refused however the provider vouches", func(t *testing.T) {
		if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, first.UserID); err != nil {
			t.Fatal(err)
		}
		if _, err := a.sso.SignIn(ctx, org.ID, identity, time.Hour, "", ""); !errors.Is(err, auth.ErrUserInactive) {
			t.Fatalf("a deactivated sign-in = %v, want auth.ErrUserInactive", err)
		}
	})
}
