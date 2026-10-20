package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/exchangestub"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/theme"
)

// The contract, as docs/api-contract-m2.md sets it: the stub serves nothing
// Armature does not, and answers in Armature's shapes. Armature's document is
// vendored under api/armature; updating it is how a change there reaches here.

const armatureDocument = "../../../api/armature/openapi.json"

func loadArmature(t *testing.T) (*openapi.Document, []byte) {
	t.Helper()
	raw, err := os.ReadFile(armatureDocument)
	if err != nil {
		t.Fatalf("read Armature's document; vendor it with make armature-openapi: %v", err)
	}
	var doc openapi.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Armature's document does not read as OpenAPI: %v", err)
	}
	return &doc, raw
}

func TestEveryRouteIsAnArmatureOperation(t *testing.T) {
	doc, _ := loadArmature(t)
	for _, rt := range routes {
		item, ok := doc.Paths[rt.Path]
		if !ok || item[strings.ToLower(rt.Method)] == nil {
			t.Errorf("the stub serves %s %s, which Armature's document does not have", rt.Method, rt.Path)
			continue
		}
	}
}

// contract drives the stub and checks every request and answer against
// Armature's document, counting which routes answered with a success.
type contract struct {
	t      *testing.T
	doc    *openapi.Document
	base   string
	served map[string]bool
}

// route finds the stub's route for a concrete path; a {name} segment of the
// template matches any one segment. Literal templates win over templated ones.
func (c *contract) route(method, path string) *route {
	var found *route
	for i, rt := range routes {
		want, got := strings.Split(rt.Path, "/"), strings.Split(path, "/")
		if rt.Method != method || len(want) != len(got) {
			continue
		}
		matches := true
		for j := range want {
			if !strings.HasPrefix(want[j], "{") && !strings.EqualFold(want[j], got[j]) {
				matches = false
			}
		}
		if matches && (found == nil || strings.Count(rt.Path, "{") < strings.Count(found.Path, "{")) {
			found = &routes[i]
		}
	}
	return found
}

// call sends a request as token (empty for none) and returns the status and
// the body; for an Armature route both are checked against the document.
func (c *contract) call(method, path, token string, body any) (int, []byte) {
	c.t.Helper()
	var reader io.Reader
	var sent []byte
	if body != nil {
		sent, _ = json.Marshal(body)
		reader = bytes.NewReader(sent)
	}
	full := path
	if !strings.HasPrefix(path, stubPrefix) {
		full = apiPrefix + path
	}
	req, _ := http.NewRequest(method, c.base+full, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if strings.HasPrefix(path, stubPrefix) {
		return resp.StatusCode, raw
	}

	bare, _, _ := strings.Cut(path, "?")
	rt := c.route(method, bare)
	if rt == nil {
		c.t.Fatalf("%s %s is no route of the stub", method, bare)
	}
	op := c.doc.Paths[rt.Path][strings.ToLower(method)]
	if sent != nil {
		if op.RequestBody == nil {
			c.t.Errorf("%s %s takes no body in Armature, and was sent one", method, rt.Path)
		} else if err := c.doc.ValidateJSON(op.RequestBody.Content["application/json"].Schema, sent); err != nil {
			c.t.Errorf("%s %s: Armature would refuse the body %s: %v", method, rt.Path, sent, err)
		}
	}
	answer, ok := op.Responses[strconv.Itoa(resp.StatusCode)]
	if !ok {
		if resp.StatusCode < http.StatusBadRequest {
			c.t.Errorf("%s %s answered %d, which Armature never does", method, rt.Path, resp.StatusCode)
			return resp.StatusCode, raw
		}
		answer = op.Responses["default"]
	}
	if media, isJSON := answer.Content["application/json"]; isJSON {
		if err := c.doc.ValidateJSON(media.Schema, raw); err != nil {
			c.t.Errorf("%s %s answered %d in a shape Armature does not: %v\n%s", method, rt.Path, resp.StatusCode, err, raw)
		}
	} else if len(answer.Content) == 0 && len(raw) > 0 {
		c.t.Errorf("%s %s answered %d with a body, where Armature has none", method, rt.Path, resp.StatusCode)
	}
	if resp.StatusCode < http.StatusBadRequest {
		c.served[rt.Method+" "+rt.Path] = true
	}
	return resp.StatusCode, raw
}

func (c *contract) expect(method, path, token string, body any, want int) map[string]any {
	c.t.Helper()
	status, raw := c.call(method, path, token, body)
	if status != want {
		c.t.Fatalf("%s %s = %d, want %d: %s", method, path, status, want, raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func patFor(tenant, person string) string { return tokenPrefix + tenant + "_" + person }

func keysOf(t *testing.T, answer map[string]any) []string {
	t.Helper()
	out := []string{}
	for _, is := range answer["issues"].([]any) {
		out = append(out, is.(map[string]any)["key"].(string))
	}
	return out
}

func TestEveryAnswerFitsArmaturesDocument(t *testing.T) {
	doc, raw := loadArmature(t)
	server := httptest.NewServer(newStub(raw, fakeKeycloak).handler())
	defer server.Close()
	c := &contract{t: t, doc: doc, base: server.URL, served: map[string]bool{}}
	alice, admin, reader := patFor("acme", "alice"), patFor("acme", adminName), patFor("acme", readerName)

	if got := c.expect("GET", "/openapi.json", "", nil, 200); got["info"].(map[string]any)["title"] != "Armature" {
		t.Errorf("the document served is not Armature's: %v", got["info"])
	}

	meta := c.expect("GET", "/meta", "", nil, 200)
	if meta["product"] != armature.Product || len(meta["capabilities"].([]any)) != len(armatureCapabilities) {
		t.Errorf("the stub's /meta is not Armature's: %v", meta)
	}

	me := c.expect("GET", "/auth/me", alice, nil, 200)
	principal := me["principal"].(map[string]any)
	if principal["org"].(map[string]any)["slug"] != "acme" || principal["user"].(map[string]any)["name"] != "Alice" {
		t.Errorf("a token names its tenant and person: %v", principal)
	}
	if scopes := fmt.Sprint(me["token"].(map[string]any)["scopes"]); scopes != "[]" {
		t.Errorf("alice's token has scopes %s, want none", scopes)
	}
	if role := c.expect("GET", "/auth/me", admin, nil, 200)["principal"].(map[string]any)["role"]; role != "admin" {
		t.Errorf("the admin's role is %v", role)
	}
	if scopes := fmt.Sprint(c.expect("GET", "/auth/me", reader, nil, 200)["token"].(map[string]any)["scopes"]); scopes != "[read]" {
		t.Errorf("the reader's token has scopes %s, want [read]", scopes)
	}
	other := c.expect("GET", "/auth/me", patFor("globex", "alice"), nil, 200)["principal"].(map[string]any)
	if other["org"].(map[string]any)["id"] == principal["org"].(map[string]any)["id"] {
		t.Error("two tenants share an organization id")
	}
	c.expect("GET", "/auth/me", revokedToken, nil, 401)
	c.expect("GET", "/auth/me", "", nil, 401)
	c.expect("GET", "/auth/me", "exciter_pat_nothing", nil, 401)

	for _, tc := range []struct{ query, as, want string }{
		{"text=sign-in", alice, "[CP-2]"},
		{"text=PATCH", admin, "[SEC-1]"},
		{"text=patch", alice, "[]"},
		{"project=cp&limit=100", alice, "[CP-1 CP-2 CP-3 CP-4 CP-5]"},
		{"project=CP&limit=2&offset=1", alice, "[CP-2 CP-3]"},
	} {
		if keys := fmt.Sprint(keysOf(t, c.expect("GET", "/issues?"+tc.query, tc.as, nil, 200))); keys != tc.want {
			t.Errorf("%s found %s, want %s", tc.query, keys, tc.want)
		}
	}
	c.expect("GET", "/issues?q=key%20%3D%20CP-1", alice, nil, 400)
	c.expect("GET", "/issues?limit=0", alice, nil, 422)

	c.expect("GET", "/issues/cp-1", alice, nil, 200)
	c.expect("GET", "/issues/SEC-1", alice, nil, 404)
	c.expect("GET", "/issues/SEC-1", admin, nil, 200)
	if moved := c.expect("GET", "/issues/SEC-2", alice, nil, 200)["issue"].(map[string]any); moved["key"] != "CP-5" {
		t.Errorf("an old key finds %v, want CP-5", moved["key"])
	}

	// A link as link sync sends it (#27), sent again harmlessly, refused where the person may only read, and an
	// Armature that does not answer for a while.
	sent := armature.RemoteLinkRequest{URL: "https://exciter.example" + armature.RequirementPath + uuid.NewString(),
		Title: armature.LinkTitle("CHK-R1", "Brakes engage within 100 ms", armature.Implements), Source: armature.LinkSource,
		IconURL: "https://exciter.example" + armature.IconPath}
	put := c.expect("POST", "/issues/CP-1/remote-links", alice, sent, 201)["remoteLink"].(map[string]any)
	sent.Title = armature.LinkTitle("CHK-R1", "Brakes engage within 80 ms", armature.Implements)
	if again := c.expect("POST", "/issues/CP-1/remote-links", alice, sent, 200)["remoteLink"].(map[string]any); again["id"] != put["id"] {
		t.Error("the same url made a second link")
	}
	c.expect("POST", "/issues/CP-1/remote-links", reader, sent, 403)
	c.expect("POST", "/issues/SEC-1/remote-links", alice, sent, 404)
	if n := len(c.expect("GET", "/issues/CP-1/remote-links", alice, nil, 200)["remoteLinks"].([]any)); n != 1 {
		t.Errorf("CP-1 has %d links, want 1", n)
	}
	// A commit a repository told Armature of shows on the issue's development panel (#200).
	c.expect("POST", stubPrefix+"/acme/issues/CP-1/commits", "", map[string]any{"repository": "acme/checkout", "sha": "4f2c9a1e7b3d",
		"url": "https://git.example/acme/checkout/commit/4f2c9a1e7b3d", "message": "Brake within 100 ms"}, 204)
	dev := c.expect("GET", "/issues/CP-1/development", alice, nil, 200)
	if commits := dev["commits"].([]any); len(commits) != 1 || commits[0].(map[string]any)["sha"] != "4f2c9a1e7b3d" {
		t.Errorf("CP-1's development reads %v", dev)
	}
	c.expect("GET", "/issues/SEC-1/development", alice, nil, 404)
	held := c.expect("GET", stubPrefix+"/acme/remote-links", "", nil, 200)["remoteLinks"].([]any)
	if len(held) != 1 || held[0].(map[string]any)["issueKey"] != "CP-1" || held[0].(map[string]any)["source"] != armature.LinkSource {
		t.Errorf("the stub holds %v", held)
	}
	c.expect("PUT", stubPrefix+"/acme/people/alice/read-only-projects", "", map[string]any{"projects": []string{"CP"}}, 204)
	c.expect("POST", "/issues/CP-2/remote-links", alice, sent, 403)
	c.expect("DELETE", "/issues/CP-1/remote-links/"+put["id"].(string), alice, nil, 403)
	c.expect("PUT", stubPrefix+"/acme/people/alice/read-only-projects", "", map[string]any{"projects": []string{}}, 204)
	c.expect("PUT", stubPrefix+"/acme/remote-links/outage", "", map[string]any{"status": 503, "count": 1}, 204)
	c.expect("DELETE", "/issues/CP-1/remote-links/"+put["id"].(string), alice, nil, 503)
	c.expect("DELETE", "/issues/CP-1/remote-links/"+put["id"].(string), alice, nil, 204)
	c.expect("DELETE", "/issues/CP-1/remote-links/"+put["id"].(string), alice, nil, 404)
	c.expect("PUT", stubPrefix+"/acme/remote-links/outage", "", map[string]any{"status": 404, "count": 1}, 422)
	if other := c.expect("GET", "/issues/CP-1", patFor("globex", "alice"), nil, 200)["issue"].(map[string]any); other["id"] == put["id"] {
		t.Error("tenants share ids")
	}

	// What the create dialog reads (#28), and an issue filed as a create sends it.
	if keys := fmt.Sprint(c.expect("GET", "/access/me", alice, nil, 200)["projects"]); keys != "[CP]" {
		t.Errorf("alice has access to %s, want CP alone", keys)
	}
	if keys := fmt.Sprint(c.expect("GET", "/access/me", admin, nil, 200)["projects"]); keys != "[CP SEC]" {
		t.Errorf("the admin has access to %s, want CP and SEC", keys)
	}
	if n := len(c.expect("GET", "/projects", alice, nil, 200)["projects"].([]any)); n != 1 {
		t.Errorf("alice lists %d projects, want 1", n)
	}
	types := c.expect("GET", "/issue-types", alice, nil, 200)["issueTypes"].([]any)
	if len(types) != 5 {
		t.Errorf("the stub has %d issue types, want 5", len(types))
	}
	filed := armature.CreateRequest{ProjectKey: "CP", Summary: armature.IssueSummary("CHK-R1", "Brakes engage within 100 ms"),
		Description: armature.Description("CHK-R1", "https://exciter.example"+armature.RequirementPath+uuid.NewString())}
	made := c.expect("POST", "/issues", alice, filed, 201)["issue"].(map[string]any)
	if made["key"] != "CP-6" || made["type"].(map[string]any)["name"] != "Task" || made["description"] == nil {
		t.Errorf("a create made %v %v", made["key"], made["type"])
	}
	bug := uuid.MustParse(types[1].(map[string]any)["id"].(string))
	filed.TypeID = &bug
	if kind := c.expect("POST", "/issues", alice, filed, 201)["issue"].(map[string]any)["type"].(map[string]any)["name"]; kind != "Bug" {
		t.Errorf("a create naming Bug made a %v", kind)
	}
	filed.TypeID = nil
	c.expect("POST", "/issues", reader, filed, 403)
	c.expect("POST", "/issues", alice, armature.CreateRequest{ProjectKey: "SEC", Summary: "Not visible"}, 404)
	c.expect("POST", "/issues", alice, armature.CreateRequest{ProjectKey: "CP", Summary: " "}, 422)
	c.expect("PUT", stubPrefix+"/acme/people/alice/read-only-projects", "", map[string]any{"projects": []string{"CP"}}, 204)
	c.expect("POST", "/issues", alice, filed, 403)
	if perms := c.expect("GET", "/access/me", alice, nil, 200)["permissions"].(map[string]any)["projects"].(map[string]any)["CP"]; fmt.Sprint(perms) != "[issue.read]" {
		t.Errorf("alice reading CP alone holds %v", perms)
	}
	c.expect("PUT", stubPrefix+"/acme/people/alice/read-only-projects", "", map[string]any{"projects": []string{}}, 204)
	c.expect("PUT", stubPrefix+"/acme/refused-summary", "", map[string]any{"summary": filed.Summary}, 204)
	c.expect("POST", "/issues", alice, filed, 422)
	c.expect("PUT", stubPrefix+"/acme/refused-summary", "", map[string]any{"summary": ""}, 204)

	// The theme Armature shows a person, and its file (#30).
	if got := c.expect("GET", "/themes/active", alice, nil, 200); got["theme"] != nil {
		t.Errorf("alice has a theme before the stub is told: %v", got)
	}
	c.expect("PUT", stubPrefix+"/acme/people/alice/theme", "", map[string]any{"theme": "deep-tech"}, 204)
	active := c.expect("GET", "/themes/active", alice, nil, 200)["theme"].(map[string]any)
	status, pkg := c.call("GET", "/themes/"+active["id"].(string)+"/export", alice, nil)
	var exported theme.Package
	if err := json.Unmarshal(pkg, &exported); status != 200 || err != nil || exported.Format != theme.PackageFormat {
		t.Errorf("the export is %d %s", status, pkg)
	}
	c.expect("GET", "/themes/"+active["id"].(string)+"/export", patFor("acme", "bob"), nil, 404)
	c.expect("PUT", stubPrefix+"/acme/people/alice/theme-delay", "", map[string]any{"ms": 20000}, 422)

	// Webhooks as Armature posts them (#29): every envelope in Armature's shape, signed as Exciter checks.
	got := webhookBin(t, doc)
	c.expect("PUT", stubPrefix+"/acme/webhook", "", map[string]any{"url": got.url + "/hook", "secret": "armature_whs_contract"}, 204)
	summary, done := "Set up the build pipeline again", "done"
	c.expect("PATCH", stubPrefix+"/acme/issues/CP-3", "", map[string]any{"summary": summary, "statusCategory": done}, 200)
	c.expect("POST", stubPrefix+"/acme/issues/CP-3/move", "", map[string]any{"projectKey": "SEC"}, 200)
	c.expect("DELETE", stubPrefix+"/acme/issues/SEC-3", "", nil, 204)
	linked := c.expect("POST", "/issues/CP-2/remote-links", alice, sent, 201)["remoteLink"].(map[string]any)
	c.expect("DELETE", stubPrefix+"/acme/issues/CP-2/remote-links?url="+sent.URL, "", nil, 204)
	put = c.expect("POST", "/issues/CP-2/remote-links", alice, sent, 201)["remoteLink"].(map[string]any)
	c.expect("DELETE", "/issues/CP-2/remote-links/"+put["id"].(string), alice, nil, 204)
	c.expect("POST", "/issues", alice, armature.CreateRequest{ProjectKey: "CP", Summary: "Announced"}, 201)
	replayed := c.expect("POST", stubPrefix+"/acme/webhooks", "", map[string]any{"topic": "ping", "payload": map[string]any{"message": "Armature can reach this address."}}, 200)
	if replayed["status"] != float64(http.StatusNoContent) {
		t.Errorf("a sent event answers %v", replayed)
	}
	wantTopics := "issue.updated issue.transitioned issue.updated issue.deleted issue.remote_link.removed issue.remote_link.removed issue.created ping"
	if topics := got.topics(); topics != wantTopics {
		t.Errorf("the stub announced %s, want %s", topics, wantTopics)
	}
	moved, removed := got.payload(2), got.payload(4)
	if moved["movedFrom"] != "CP-3" || moved["key"] != "SEC-3" || got.payload(1)["toStatusCategory"] != done {
		t.Errorf("the move reads %v", moved)
	}
	if removed["linkId"] != linked["id"] || removed["url"] != sent.URL || removed["relation"] != "page" {
		t.Errorf("the removal reads %v", removed)
	}
	if n := len(c.expect("GET", stubPrefix+"/acme/deliveries", "", nil, 200)["deliveries"].([]any)); n != 8 {
		t.Errorf("the stub noted %d deliveries, want 8", n)
	}
	c.expect("PUT", stubPrefix+"/acme/webhook", "", map[string]any{"url": ""}, 204)
	c.expect("POST", stubPrefix+"/acme/webhooks", "", map[string]any{"topic": "ping"}, 409)

	if status, _ := c.call("DELETE", stubPrefix+"/acme", "", nil); status != http.StatusNoContent {
		t.Errorf("forgetting a tenant answered %d", status)
	}
	if status, _ := c.call("GET", stubPrefix+"/healthz", "", nil); status != http.StatusNoContent {
		t.Errorf("the health check answered %d", status)
	}

	c.exchange()

	for _, rt := range routes {
		if !c.served[rt.Method+" "+rt.Path] {
			t.Errorf("the scenario never had %s %s answer with a success", rt.Method, rt.Path)
		}
	}
}

// fakeKeycloak reads "kc|email|azp|aud" as a verified token, so the contract needs no identity provider.
func fakeKeycloak(_ context.Context, raw string) (*exchangestub.Claims, error) {
	parts := strings.Split(raw, "|")
	if len(parts) != 4 || parts[0] != "kc" {
		return nil, errors.New("it is not a current token signed by the identity provider")
	}
	return &exchangestub.Claims{Email: parts[1], AuthorizedBy: parts[2], Audience: []string{parts[3]}}, nil
}

// form posts to the token endpoint as a partner app does, checking the answer against Armature's document.
func (c *contract) form(id, secret string, values url.Values, want int) map[string]any {
	c.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, c.base+apiPrefix+"/oauth/token", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(id), url.QueryEscape(secret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		c.t.Fatalf("POST /oauth/token %v = %d, want %d: %s", values, resp.StatusCode, want, raw)
	}
	op := c.doc.Paths["/oauth/token"]["post"]
	if op.RequestBody == nil || op.RequestBody.Content["application/x-www-form-urlencoded"].Schema == nil {
		c.t.Fatal("Armature's token endpoint takes no form")
	}
	sent, _ := json.Marshal(flat(values))
	if err := c.doc.ValidateJSON(op.RequestBody.Content["application/x-www-form-urlencoded"].Schema, sent); err != nil {
		c.t.Errorf("Armature would refuse the form %s: %v", sent, err)
	}
	if err := c.doc.ValidateJSON(op.Responses[strconv.Itoa(want)].Content["application/json"].Schema, raw); err != nil {
		c.t.Errorf("POST /oauth/token answered %d in a shape Armature does not: %v\n%s", want, err, raw)
	}
	if want == http.StatusOK {
		c.served["POST /oauth/token"] = true
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func flat(values url.Values) map[string]string {
	out := map[string]string{}
	for k := range values {
		out[k] = values.Get(k)
	}
	return out
}

// exchange is the token exchange as docs/oauth.md has it (#274): Exciter's app trades alice's realm token for her
// tokens, which act as her, refreshes them, and a rotated refresh token presented again ends the grant.
func (c *contract) exchange() {
	c.t.Helper()
	c.expect("PUT", stubPrefix+"/acme/oauth-app", "", map[string]any{"clientId": "exciter-app", "clientSecret": "s3cret", "keycloakClientId": "exciter"}, http.StatusNoContent)
	subject := func(token string) url.Values {
		return url.Values{"grant_type": {exchangestub.GrantExchange}, "subject_token": {token}, "subject_token_type": {exchangestub.TokenTypeAccess}}
	}
	c.form("exciter-app", "wrong", subject("kc|alice@suite.test|exciter|armature"), http.StatusUnauthorized)
	c.form("exciter-app", "s3cret", subject("kc|alice@suite.test|stator|armature"), http.StatusBadRequest)
	c.form("exciter-app", "s3cret", subject("kc|alice@suite.test|exciter|stator"), http.StatusBadRequest)
	got := c.form("exciter-app", "s3cret", subject("kc|alice@suite.test|exciter|armature"), http.StatusOK)
	access, refresh := got["access_token"].(string), got["refresh_token"].(string)
	if !strings.HasPrefix(access, accessTokenPrefix) || !strings.HasPrefix(refresh, refreshTokenPrefix) || got["scope"] != "read issue.write" {
		c.t.Errorf("the exchange answered %v", got)
	}
	me := c.expect("GET", "/auth/me", access, nil, 200)
	if me["principal"].(map[string]any)["user"].(map[string]any)["name"] != "Alice" || fmt.Sprint(me["token"].(map[string]any)["scopes"]) != "[read issue.write]" {
		c.t.Errorf("an exchanged token is not alice's with the app's scopes: %v", me)
	}
	renewed := c.form("exciter-app", "s3cret", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}, http.StatusOK)
	if renewed["refresh_token"] == refresh {
		c.t.Error("a refresh did not rotate the refresh token")
	}
	if status, _ := c.call("GET", "/auth/me", access, nil); status != http.StatusUnauthorized {
		c.t.Errorf("the access token before the refresh still answers %d", status)
	}
	c.form("exciter-app", "s3cret", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}, http.StatusBadRequest)
	if status, _ := c.call("GET", "/auth/me", renewed["access_token"].(string), nil); status != http.StatusUnauthorized {
		c.t.Errorf("a replayed refresh token left the grant standing: %d", status)
	}
}

// bin receives the stub's webhooks, checking each against Armature's EnvelopeV1 and its signatures.
type bin struct {
	t      *testing.T
	url    string
	mu     sync.Mutex
	bodies []map[string]any
}

func webhookBin(t *testing.T, doc *openapi.Document) *bin {
	b := &bin{t: t}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		headers := armature.WebhookHeaders{Signature: r.Header.Get(armature.SignatureHeader), Timestamp: r.Header.Get(armature.TimestampHeader),
			TimestampedSignature: r.Header.Get(armature.TimestampedSignatureHeader)}
		if !headers.Verify(raw, "armature_whs_contract") || headers.Timestamp == "" {
			t.Errorf("a delivery is not signed as Armature signs: %v", r.Header)
		}
		if err := doc.ValidateJSON(&openapi.Schema{Ref: "#/components/schemas/EnvelopeV1"}, raw); err != nil {
			t.Errorf("a delivery is not Armature's envelope: %v\n%s", err, raw)
		}
		if r.Header.Get("X-Armature-Webhook-Version") != "1" || r.Header.Get("X-Armature-Event") == "" {
			t.Errorf("a delivery's headers are %v", r.Header)
		}
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		b.mu.Lock()
		b.bodies = append(b.bodies, body)
		b.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	b.url = server.URL
	return b
}

func (b *bin) topics() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, body := range b.bodies {
		out = append(out, body["topic"].(string))
	}
	return strings.Join(out, " ")
}

func (b *bin) payload(i int) map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	if i >= len(b.bodies) {
		b.t.Fatalf("only %d deliveries came", len(b.bodies))
	}
	return b.bodies[i]["payload"].(map[string]any)
}
