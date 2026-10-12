//go:build integration

package test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/seed"
	"github.com/cloudster1/exciter/backend/internal/testorg"
)

const suiteTestToken = "the integration suite's test token"

// squareSVG is a picture a theme may carry.
const squareSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M2 2h12v12H2z"/></svg>`

// startTestOrgAPI is the api as the compose stack runs it for the browser suite, with the test endpoints when on;
// the organizations it makes are owned by a fresh owner, as the seed's are by the bootstrap administrator.
func startTestOrgAPI(t *testing.T, h *harness, kc *keycloak, on bool) (*apiServer, string) {
	t.Helper()
	owner := "owner-" + h.suffix() + "@example.test"
	h.forgetPerson(t, owner)
	a := newAPIServerWith(t, h, func(a *apiServer, s *httpapi.Server) {
		if !on {
			return
		}
		s.TestOrgs = testorg.NewService(h.cluster, a.accounts, a.sso, a.store, owner, seed.People{
			Members: []seed.Member{{Email: aliceEmail, Role: auth.RoleAdmin}, {Email: bobEmail, Role: auth.RoleMember}},
			Provider: &oidc.Provider{Issuer: kc.issuer(), ClientID: exciterClient, ClientSecret: exciterSecret,
				CreateGroups: true, Enabled: true},
		})
		s.TestToken = suiteTestToken
	})
	return a, owner
}

// testCall sends one request to the test endpoints with the token given.
func testCall(t *testing.T, a *apiServer, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, a.URL+httpapi.APIPrefix+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set(httpapi.TestTokenHeader, token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// orgColumns names every column that points at an organization: each foreign
// key to org, and every column called org_id whether or not it has one.
func (h *harness) orgColumns(t *testing.T) [][2]string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT c.conrelid::regclass::text, a.attname
		FROM pg_constraint c
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)
		WHERE c.contype = 'f' AND c.confrelid = 'public.org'::regclass AND cardinality(c.conkey) = 1
		UNION
		SELECT table_name::text, column_name::text FROM information_schema.columns
		WHERE table_schema = 'public' AND column_name = 'org_id'
		ORDER BY 1, 2`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		out = append(out, [2]string{table, column})
	}
	if len(out) == 0 {
		t.Fatal("no column points at an organization; the query is wrong")
	}
	return out
}

// rowsNaming counts, per table and column, the rows that name the organization.
func (h *harness) rowsNaming(t *testing.T, org uuid.UUID) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, tc := range h.orgColumns(t) {
		var n int
		sql := `SELECT count(*) FROM ` + quoteIdent(tc[0]) + ` WHERE ` + quoteIdent(tc[1]) + ` = $1`
		if err := h.super.QueryRow(context.Background(), sql, org).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		out[tc[0]+"."+tc[1]] = n
	}
	var n int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM org WHERE id = $1`, org).Scan(&n); err != nil {
		t.Fatal(err)
	}
	out["org.id"] = n
	return out
}

func quoteIdent(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// membersOf lists an organization's people as address=role, in address order.
func (h *harness) membersOf(t *testing.T, org uuid.UUID) string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT u.email, m.org_role FROM org_member m JOIN app_user u ON u.id = m.user_id
		WHERE m.org_id = $1 ORDER BY u.email`, org)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var email, role string
		if err := rows.Scan(&email, &role); err != nil {
			t.Fatal(err)
		}
		got = append(got, email+"="+role)
	}
	return strings.Join(got, ",")
}

// An organization made by the endpoint is signed in to, used and removed, and
// nothing of it is left in any table or in the bucket.
func TestThrowawayOrganizations(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a, owner := startTestOrgAPI(t, h, kc, true)
	ctx := context.Background()
	for _, r := range apiContract.routes {
		if strings.HasPrefix(r.Path, "/test/") {
			t.Fatalf("the coverage check counts %s %s, which the document leaves out", r.Method, r.Path)
		}
	}

	t.Run("a call without the token is refused", func(t *testing.T) {
		for _, token := range []string{"", "not the token at all"} {
			if status, body := testCall(t, a, http.MethodPost, "/test/orgs", token, `{}`); status != http.StatusUnauthorized {
				t.Errorf("create with token %q = %d %v, want 401", token, status, body)
			}
			if status, _ := testCall(t, a, http.MethodDelete, "/test/orgs/"+seed.DemoOrgSlug, token, ""); status != http.StatusUnauthorized {
				t.Errorf("delete with token %q = %d, want 401", token, status)
			}
		}
	})

	status, made := testCall(t, a, http.MethodPost, "/test/orgs", suiteTestToken, `{"label":"Integration"}`)
	if status != http.StatusCreated {
		t.Fatalf("create = %d %v", status, made)
	}
	slug, _ := made["slug"].(string)
	orgID, err := uuid.Parse(made["id"].(string))
	if err != nil || !strings.HasPrefix(slug, "integration-") {
		t.Fatalf("made %v, want a slug starting with the label and an id", made)
	}
	// The endpoint may fail the test halfway; the organization goes anyway.
	t.Cleanup(func() { testCall(t, a, http.MethodDelete, "/test/orgs/"+slug, suiteTestToken, "") })

	t.Run("the owner, alice and bob are let in and the provider is set", func(t *testing.T) {
		if got := h.membersOf(t, orgID); got != aliceEmail+"=admin,"+bobEmail+"=member,"+owner+"=owner" {
			t.Errorf("members = %v", got)
		}
		var issuer string
		if err := h.super.QueryRow(ctx, `SELECT issuer FROM oidc_provider WHERE org_id = $1 AND enabled`, orgID).Scan(&issuer); err != nil || issuer != kc.issuer() {
			t.Errorf("provider issuer = %q, %v", issuer, err)
		}
	})

	alice, bob := newBrowser(t, a), newBrowser(t, a)
	t.Run("alice and bob sign in and alice leaves a shared default theme with a file", func(t *testing.T) {
		alice.signIn(t, slug, "alice", alicePassword, "")
		bob.signIn(t, slug, "bob", bobPassword, "")
		for who, b := range map[string]*browser{"admin": alice, "member": bob} {
			status, me := b.me(t)
			current, _ := me["organization"].(map[string]any)
			if status != http.StatusOK || current["slug"] != slug || current["role"] != who {
				t.Fatalf("/auth/me = %d %v, want %s as %s", status, me, slug, who)
			}
		}
		admin := a.as(t, h.userID(t, aliceEmail), orgID)
		member := a.as(t, h.userID(t, bobEmail), orgID)
		themeID := obj(t, want(t, admin.post(t, "/api/v1/themes", map[string]any{"name": "Throwaway", "shared": true}), http.StatusCreated, "make a theme"), "theme")["id"].(string)
		want(t, member.put(t, "/api/v1/themes/active", map[string]any{"themeId": themeID}), http.StatusOK, "bob chooses the theme")
		want(t, admin.put(t, "/api/v1/themes/default", map[string]any{"themeId": themeID}), http.StatusOK, "make it the default")
		assetID := obj(t, want(t, admin.upload(t, "/api/v1/themes/"+themeID+"/assets", "square.svg", []byte(squareSVG)), http.StatusCreated, "upload"), "asset")["id"].(string)
		if keys, err := a.store.List(ctx, objectstore.OrgPrefix(orgID)); err != nil || len(keys) != 1 || keys[0] != themeObject(orgID, themeID, assetID) {
			t.Fatalf("the bucket holds %v under the organization (%v), want the one file", keys, err)
		}
	})

	t.Run("only a throwaway organization can be deleted", func(t *testing.T) {
		if status, body := testCall(t, a, http.MethodDelete, "/test/orgs/"+seed.DemoOrgSlug, suiteTestToken, ""); status != http.StatusNotFound {
			t.Fatalf("delete demo = %d %v, want 404", status, body)
		}
		if n := h.countRows(t, `SELECT count(*) FROM org WHERE slug = $1`, seed.DemoOrgSlug); n != 1 {
			t.Fatalf("demo is gone: %d", n)
		}
	})

	before := h.rowsNaming(t, orgID)
	for _, table := range []string{"org.id", "org_member.org_id", "oidc_provider.org_id", "groups.org_id", "theme.org_id", "theme_asset.org_id",
		"user_theme.org_id", "audit_log.org_id", "user_session.proof_org_id"} {
		if before[table] == 0 {
			t.Errorf("before deleting, %s names the organization in no row, so the check after proves nothing there", table)
		}
	}

	t.Run("deleting leaves no row, no file and no session", func(t *testing.T) {
		if status, body := testCall(t, a, http.MethodDelete, "/test/orgs/"+slug, suiteTestToken, ""); status != http.StatusNoContent {
			t.Fatalf("delete = %d %v", status, body)
		}
		for column, n := range h.rowsNaming(t, orgID) {
			if n != 0 {
				t.Errorf("%s still names the deleted organization in %d rows", column, n)
			}
		}
		if keys, err := a.store.List(ctx, objectstore.OrgPrefix(orgID)); err != nil || len(keys) != 0 {
			t.Errorf("the bucket still holds %v under the organization (%v)", keys, err)
		}
		h.settle(t)
		for _, b := range []*browser{alice, bob} {
			if status, _ := b.me(t); status != http.StatusUnauthorized {
				t.Errorf("a session proven in the deleted organization still answers /auth/me with %d", status)
			}
		}
		if status, _ := testCall(t, a, http.MethodDelete, "/test/orgs/"+slug, suiteTestToken, ""); status != http.StatusNotFound {
			t.Errorf("deleting twice = %d, want 404", status)
		}
	})
}

// Off, the endpoints are not there at all, token or no token.
func TestTheTestEndpointsAreNotFoundWhenOff(t *testing.T) {
	h := newHarness(t)
	a, _ := startTestOrgAPI(t, h, newKeycloak(t), false)
	for _, token := range []string{"", suiteTestToken} {
		if status, body := testCall(t, a, http.MethodPost, "/test/orgs", token, `{}`); status != http.StatusNotFound {
			t.Errorf("create with the endpoints off = %d %v, want 404", status, body)
		}
		if status, _ := testCall(t, a, http.MethodDelete, "/test/orgs/"+seed.DemoOrgSlug, token, ""); status != http.StatusNotFound {
			t.Errorf("delete with the endpoints off = %d, want 404", status)
		}
	}
}
