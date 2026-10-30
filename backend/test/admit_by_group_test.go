//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/oauth"
)

// The migration that brings admit_by_group, and the one before it.
const (
	beforeAdmitByGroup = 1519
	addsAdmitByGroup   = 1520
)

// admitByGroup turns letting people in by their groups on or off, as an
// administrator does on the settings page, keeping the rest as it is.
func admitByGroup(t *testing.T, admin *client, on bool) {
	t.Helper()
	was := obj(t, want(t, admin.get(t, providerPath), http.StatusOK, "read the provider"), "provider")
	saved := obj(t, want(t, admin.put(t, providerPath, map[string]any{
		"issuer": was["issuer"], "clientId": was["clientId"], "groupsClaim": was["groupsClaim"], "scopes": was["scopes"],
		"createGroups": was["createGroups"], "admitByGroup": on, "enabled": was["enabled"],
	}), http.StatusOK, "turn admitting by group on or off"), "provider")
	if saved["admitByGroup"] != on {
		t.Fatalf("admitting by group %v saved as %v", on, saved)
	}
	admin.api.h.settle(t)
}

// switchedOff makes the account an address belongs to, switched off. The
// address is passed twice, since Postgres cannot type one parameter as both
// citext and text.
func (h *harness) switchedOff(t *testing.T, email string) {
	t.Helper()
	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO app_user (email, name, is_active) VALUES ($1, $2, false)
		ON CONFLICT (email) DO UPDATE SET is_active = false`, email, email); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
}

// admittedBy is the group the audit log says let somebody in, and where.
func (h *harness) admittedBy(t *testing.T, email string) (group, via string) {
	t.Helper()
	joined := h.auditRows(t, "member.joined", h.userID(t, email))
	if len(joined) != 1 {
		t.Fatalf("%s joined %d times on the record, want once: %v", email, len(joined), joined)
	}
	group, _ = joined[0]["group"].(string)
	via, _ = joined[0]["via"].(string)
	return group, via
}

// A mapped group lets somebody new in at sign-in only once an administrator
// turns it on; it sets the role of somebody already in either way.
func TestAMappedGroupLetsSomebodyNewInAtSignInOnlyWhenTurnedOn(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, a, kc.issuer(), true)
	h.letIn(t, aliceEmail, "member")
	admin, _ := adminOf(t, h, a)
	group := "admit-" + h.suffix()
	kc.newGroup(t, group)
	mapGroup(t, admin, group, "member")
	mapGroup(t, admin, "exciter-administrators", "admin")
	const password = "admitted password"
	newcomer := "newcomer-" + h.suffix()
	newcomerEmail := kc.newPerson(t, h, newcomer, password, group)
	notAMember := "http://" + appHost + "/login?sso=not_a_member"

	if provider := obj(t, want(t, admin.get(t, providerPath), http.StatusOK, "read the provider"), "provider"); provider["admitByGroup"] != false {
		t.Fatalf("a provider set up without the setting lets people in by group: %v", provider)
	}

	t.Run("off, the default, somebody new in a mapped group waits like anybody", func(t *testing.T) {
		if landing := newBrowser(t, a).signIn(t, newcomer, password, "/"); landing != notAMember {
			t.Fatalf("%s landed on %q", newcomer, landing)
		}
		if role, _ := h.standing(t, newcomerEmail); role != "" {
			t.Fatalf("%s became %s with the setting off", newcomer, role)
		}
		if n := h.countRows(t, `SELECT count(*) FROM join_request r JOIN app_user u ON u.id = r.user_id WHERE u.email = $1`, newcomerEmail); n != 1 {
			t.Fatalf("%s has %d requests, want 1", newcomer, n)
		}
	})

	t.Run("off, a mapped group still sets the role of somebody already in", func(t *testing.T) {
		if role := signedIn(t, a, "alice", alicePassword).roleIn(t); role != "admin" {
			t.Fatalf("alice is %q, want the admin her group maps to", role)
		}
	})

	admitByGroup(t, admin, true)

	t.Run("on, somebody new in a mapped group is let in with its role and stops waiting", func(t *testing.T) {
		b := newBrowser(t, a)
		if landing := b.signIn(t, newcomer, password, "/settings"); landing != "http://"+appHost+"/settings" {
			t.Fatalf("%s landed on %q, want where they were going", newcomer, landing)
		}
		if role, source := h.standing(t, newcomerEmail); role != "member" || source != "oidc" {
			t.Fatalf("%s's membership is %s from %s", newcomer, role, source)
		}
		if n := h.countRows(t, `SELECT count(*) FROM join_request r JOIN app_user u ON u.id = r.user_id WHERE u.email = $1`, newcomerEmail); n != 0 {
			t.Fatalf("%s still has %d requests", newcomer, n)
		}
		if got, via := h.admittedBy(t, newcomerEmail); got != group || via != "sign_in" {
			t.Errorf("the audit log says admitted by %q via %q, want %s at sign-in", got, via, group)
		}
	})

	t.Run("on, somebody in no mapped group still waits", func(t *testing.T) {
		stray := "stray-" + h.suffix()
		email := kc.newPerson(t, h, stray, password)
		if landing := newBrowser(t, a).signIn(t, stray, password, "/"); landing != notAMember {
			t.Fatalf("%s landed on %q", stray, landing)
		}
		if role, _ := h.standing(t, email); role != "" {
			t.Fatalf("%s became %s", stray, role)
		}
	})

	t.Run("on, somebody switched off is refused whatever their groups", func(t *testing.T) {
		off := "off-" + h.suffix()
		email := kc.newPerson(t, h, off, password, group)
		h.switchedOff(t, email)
		if landing := newBrowser(t, a).signIn(t, off, password, "/"); landing != "http://"+appHost+"/login?sso=inactive" {
			t.Fatalf("%s landed on %q", off, landing)
		}
		if role, _ := h.standing(t, email); role != "" {
			t.Fatalf("a switched off account became %s", role)
		}
	})
}

// A partner's token exchange lets somebody Exciter has never seen in when
// their group is mapped, only once an administrator turns it on, and keeps no
// account for anybody it refuses (#273).
func TestAMappedGroupLetsAPartnerConnectSomebodyNewOnlyWhenTurnedOn(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, a, kc.issuer(), false)
	admin, _ := adminOf(t, h, a)
	stator := registerPartner(t, admin, map[string]any{"name": "Stator", "keycloakClientId": statorClient})
	group := "exchange-" + h.suffix()
	kc.newGroup(t, group)
	mapGroup(t, admin, group, "member")
	const password = "exchanged password"
	exchange := func(username string) (int, map[string]any) {
		t.Helper()
		return stator.exchange(t, a, kc.realmToken(t, statorClient, statorSecret, username, password))
	}
	refused := func(t *testing.T, who string, status int, got map[string]any, says string) {
		t.Helper()
		description, _ := got["error_description"].(string)
		if status != http.StatusBadRequest || got["error"] != oauth.ErrInvalidGrant || !strings.Contains(description, says) {
			t.Fatalf("the exchange for %s answered %d %v", who, status, got)
		}
	}
	accounts := func(email string) int {
		return h.countRows(t, `SELECT count(*) FROM app_user WHERE email = $1`, email)
	}
	mapped := "mapped-" + h.suffix()
	mappedEmail := kc.newPerson(t, h, mapped, password, group)

	t.Run("off, the default, somebody new in a mapped group is refused and no account is kept", func(t *testing.T) {
		status, got := exchange(mapped)
		refused(t, mapped, status, got, "no Exciter account yet")
		if n := accounts(mappedEmail); n != 0 {
			t.Errorf("a refused exchange kept %s's account", mapped)
		}
	})

	admitByGroup(t, admin, true)

	t.Run("on, somebody new in a mapped group is let in with its role, and the record names the group", func(t *testing.T) {
		if status, got := exchange(mapped); status != http.StatusOK {
			t.Fatalf("the exchange for %s answered %d %v", mapped, status, got)
		}
		if role, source := h.standing(t, mappedEmail); role != "member" || source != "oidc" {
			t.Fatalf("%s's membership is %s from %s", mapped, role, source)
		}
		if got, via := h.admittedBy(t, mappedEmail); got != group || via != "token_exchange" {
			t.Errorf("the audit log says admitted by %q via %q, want %s at a token exchange", got, via, group)
		}
	})

	t.Run("on, somebody in no mapped group is refused and no account is kept", func(t *testing.T) {
		stray := "stray-" + h.suffix()
		email := kc.newPerson(t, h, stray, password)
		status, got := exchange(stray)
		refused(t, stray, status, got, "no Exciter account yet")
		if n := accounts(email); n != 0 {
			t.Errorf("a refused exchange kept %s's account", stray)
		}
	})

	t.Run("on, somebody switched off is refused whatever their groups", func(t *testing.T) {
		off := "off-" + h.suffix()
		email := kc.newPerson(t, h, off, password, group)
		h.switchedOff(t, email)
		status, got := exchange(off)
		refused(t, off, status, got, "switched off")
		if role, _ := h.standing(t, email); role != "" {
			t.Fatalf("a switched off account became %s", role)
		}
	})
}

// An installation that already maps a group let people in by it from the
// moment it was mapped, so the upgrade turns the setting on there, and leaves
// it off where nothing is mapped.
func TestTheUpgradeKeepsLettingPeopleInWhereAGroupIsMapped(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	provider, super, _ := scratchDatabase(t, h, "exciter_admit_by_group_test")
	if _, err := provider.UpTo(ctx, beforeAdmitByGroup); err != nil {
		t.Fatalf("migrate to %d: %v", beforeAdmitByGroup, err)
	}
	if _, err := super.Exec(ctx, `INSERT INTO oidc_provider (issuer, client_id) VALUES ('https://id.test', 'exciter')`); err != nil {
		t.Fatal(err)
	}
	admits := func() bool {
		t.Helper()
		var on bool
		if err := super.QueryRow(ctx, `SELECT admit_by_group FROM oidc_provider`).Scan(&on); err != nil {
			t.Fatal(err)
		}
		return on
	}

	if _, err := provider.UpTo(ctx, addsAdmitByGroup); err != nil {
		t.Fatalf("migrate to %d: %v", addsAdmitByGroup, err)
	}
	if admits() {
		t.Error("the upgrade let people in by their groups where no group is mapped")
	}

	if _, err := provider.DownTo(ctx, beforeAdmitByGroup); err != nil {
		t.Fatalf("migrate back to %d: %v", beforeAdmitByGroup, err)
	}
	if _, err := super.Exec(ctx, `INSERT INTO oidc_group_role (group_ref, role) VALUES ('staff', 'member')`); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, addsAdmitByGroup); err != nil {
		t.Fatalf("migrate to %d again: %v", addsAdmitByGroup, err)
	}
	if !admits() {
		t.Error("the upgrade stopped letting people in by the group an installation maps")
	}
}
