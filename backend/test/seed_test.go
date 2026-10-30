//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/seed"
)

// The bootstrap points the organization at its provider once, in its administrator's name, and lets the named
// people in ahead of their first sign-in; a provider an administrator changed since is theirs to keep.
func TestTheBootstrapConfiguresTheProviderAndItsMembersOnce(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	ctx := context.Background()
	h.singleSignOn(t)
	adminID := h.userID(t, bootstrapAdmin(t, h, a))
	first, second := "first-"+h.suffix()+"@suite.test", "second-"+h.suffix()+"@suite.test"
	h.forgetPerson(t, first)
	h.forgetPerson(t, second)
	people := seed.Bootstrapped(config.Bootstrap{
		OIDCIssuer: "http://localhost:8180/realms/dynamo", OIDCClientID: exciterClient, OIDCClientSecret: exciterSecret,
		Members: []config.BootstrapMember{{Email: first, Role: "admin"}, {Email: second, Role: "member"}},
	})

	t.Run("nobody's name configures nothing", func(t *testing.T) {
		if _, err := seed.Populate(ctx, a.accounts, a.sso, seed.People{Provider: people.Provider}); err == nil {
			t.Fatal("a provider was configured in nobody's name")
		}
	})

	asAdmin := db.WithUser(ctx, adminID)
	done, err := seed.Populate(asAdmin, a.accounts, a.sso, people)
	if err != nil || len(done.Added) != 2 || !done.ProviderCreated {
		t.Fatalf("the first run = %+v, %v; want both let in and the provider made", done, err)
	}
	for email, administers := range map[string]bool{first: true, second: false} {
		if n := h.countRows(t, `
			SELECT count(*) FROM app_user u WHERE email = $1 AND role = 'member' AND perm_is_administrator(u.id) = $2`, email, administers); n != 1 {
			t.Errorf("%s is not let in as a member who administers: %v", email, administers)
		}
	}
	if n := h.countRows(t, `
		SELECT count(*) FROM oidc_provider WHERE issuer = $1 AND client_secret IS NOT NULL
		  AND position(convert_to($2, 'UTF8') in client_secret) = 0 AND create_groups AND enabled`,
		people.Provider.Issuer, exciterSecret); n != 1 {
		t.Fatal("the provider is not configured with its secret sealed")
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'sso.provider_saved' AND actor_user_id = $1`, adminID); n != 1 {
		t.Errorf("the provider's configuration is recorded %d times in the administrator's name, want once", n)
	}

	if _, err := h.super.Exec(ctx, `UPDATE oidc_provider SET client_id = 'changed'`); err != nil {
		t.Fatal(err)
	}
	again, err := seed.Populate(asAdmin, a.accounts, a.sso, people)
	if err != nil || len(again.Added) != 0 || again.ProviderCreated || !again.ProviderExisting {
		t.Fatalf("the second run = %+v, %v; want nothing made", again, err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM oidc_provider WHERE client_id = 'changed'`); n != 1 {
		t.Error("the second run overwrote what an administrator changed")
	}
}
