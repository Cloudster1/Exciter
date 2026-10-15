package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/stator"
)

// The contract, as docs/api-contract-m4.md sets it: the stub serves nothing Stator does not, and answers in
// Stator's shapes. Stator's document is vendored under api/stator; updating it is how a change there reaches here.

const statorDocument = "../../../api/stator/openapi.json"

func loadStator(t *testing.T) *openapi.Document {
	t.Helper()
	raw, err := os.ReadFile(statorDocument)
	if err != nil {
		t.Fatalf("read Stator's document; vendor it with make stator-openapi: %v", err)
	}
	var doc openapi.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Stator's document does not read as OpenAPI: %v", err)
	}
	return &doc
}

func TestEveryRouteIsAStatorOperation(t *testing.T) {
	doc := loadStator(t)
	for _, rt := range routes {
		item, ok := doc.Paths[rt.Path]
		if !ok || item[strings.ToLower(rt.Method)] == nil {
			t.Errorf("the stub serves %s %s, which Stator's document does not have", rt.Method, rt.Path)
		}
	}
}

// contract drives the stub and checks every answer against Stator's document, counting which routes answered
// with a success.
type contract struct {
	t      *testing.T
	doc    *openapi.Document
	base   string
	served map[string]bool
}

// route finds the stub's route for a concrete path; a {name} segment of the template matches any one segment.
func (c *contract) route(method, path string) *route {
	for i, rt := range routes {
		want, got := strings.Split(rt.Path, "/"), strings.Split(path, "/")
		if rt.Method != method || len(want) != len(got) {
			continue
		}
		matches := true
		for j := range want {
			if !strings.HasPrefix(want[j], "{") && want[j] != got[j] {
				matches = false
			}
		}
		if matches {
			return &routes[i]
		}
	}
	return nil
}

// call sends a request as token (empty for none) and returns the status and the body; for a Stator route both
// are checked against the document.
func (c *contract) call(method, path, token string, body any) (int, []byte) {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		sent, _ := json.Marshal(body)
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
	answer, ok := op.Responses[strconv.Itoa(resp.StatusCode)]
	if !ok {
		if resp.StatusCode < http.StatusBadRequest {
			c.t.Errorf("%s %s answered %d, which Stator never does", method, rt.Path, resp.StatusCode)
			return resp.StatusCode, raw
		}
		answer = op.Responses["default"]
	}
	if media, isJSON := answer.Content["application/json"]; isJSON {
		if err := c.doc.ValidateJSON(media.Schema, raw); err != nil {
			c.t.Errorf("%s %s answered %d in a shape Stator does not: %v\n%s", method, rt.Path, resp.StatusCode, err, raw)
		}
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

func TestEveryAnswerFitsStatorsDocument(t *testing.T) {
	doc := loadStator(t)
	server := httptest.NewServer(newStub().handler())
	defer server.Close()
	c := &contract{t: t, doc: doc, base: server.URL, served: map[string]bool{}}
	alice := patFor("acme", "alice")

	// What connecting reads (#40): which product answers, what it offers, and whom a token acts as.
	meta := c.expect("GET", "/meta", "", nil, 200)
	if meta["product"] != stator.Product || len(meta["capabilities"].([]any)) != 0 {
		t.Errorf("the stub describes itself as %v", meta)
	}
	c.expect("PUT", stubPrefix+"/capabilities", "", map[string]any{"capabilities": []string{stator.RemoteLinksCapability + "@1"}}, 204)
	offered := c.expect("GET", "/meta", "", nil, 200)["capabilities"].([]any)
	if len(offered) != 1 || offered[0] != stator.RemoteLinksCapability+"@1" {
		t.Errorf("the stub offers %v after it was told", offered)
	}
	c.expect("PUT", stubPrefix+"/capabilities", "", map[string]any{"capabilities": nil}, 204)

	me := c.expect("GET", "/auth/me", alice, nil, 200)["user"].(map[string]any)
	if me["name"] != "Alice" || me["email"] != "alice@acme.stator.test" {
		t.Errorf("a token names its tenant and person: %v", me)
	}
	if again := c.expect("GET", "/auth/me", alice, nil, 200)["user"].(map[string]any); again["id"] != me["id"] {
		t.Error("the same token acts as another person on the next call")
	}
	if other := c.expect("GET", "/auth/me", patFor("globex", "alice"), nil, 200)["user"].(map[string]any); other["id"] == me["id"] {
		t.Error("two tenants share a person")
	}
	c.expect("GET", "/auth/me", revokedToken, nil, 401)
	c.expect("GET", "/auth/me", "", nil, 401)
	c.expect("GET", "/auth/me", "armature_pat_acme_alice", nil, 401)

	pagesTenantScenario(t, c)

	if status, _ := c.call("DELETE", stubPrefix+"/acme", "", nil); status != http.StatusNoContent {
		t.Errorf("forgetting a tenant answered %d", status)
	}
	if status, _ := c.call("GET", stubPrefix+"/healthz", "", nil); status != http.StatusNoContent {
		t.Errorf("the health check answered %d", status)
	}

	for _, rt := range routes {
		if !c.served[rt.Method+" "+rt.Path] {
			t.Errorf("the scenario never had %s %s answer with a success", rt.Method, rt.Path)
		}
	}
}

// pagesTenantScenario reads pages and searches as Exciter does (#41), and changes them as a person in Stator would,
// each change announced to a registered webhook.
func pagesTenantScenario(t *testing.T, c *contract) {
	alice, admin := patFor("acme", "alice"), patFor("acme", adminName)
	listed := map[string]map[string]any{}
	for _, p := range c.expect("GET", stubPrefix+"/acme/pages", "", nil, 200)["pages"].([]any) {
		page := p.(map[string]any)
		listed[page["title"].(string)] = page
	}
	timing, threat := listed["Brake timing"]["id"].(string), listed["Threat model"]["id"].(string)

	got := c.expect("GET", "/pages/"+timing, alice, nil, 200)
	page := got["page"].(map[string]any)
	if page["version"] != float64(2) || fmt.Sprint(page["ancestors"].([]any)[1].(map[string]any)["title"]) != "Braking system" ||
		got["space"].(map[string]any)["name"] != "Engineering" {
		t.Errorf("Brake timing reads %v", got)
	}
	c.expect("GET", "/pages/"+threat, alice, nil, 404)
	c.expect("GET", "/pages/"+threat, admin, nil, 200)
	c.expect("GET", "/pages/not-an-id", alice, nil, 404)

	for _, tc := range []struct{ query, as, want string }{
		{"q=brak", alice, "[Braking system Brake timing]"},
		{"q=brake+tim", alice, "[Brake timing]"},
		{"q=threat", alice, "[]"},
		{"q=threat", admin, "[Threat model]"},
		{"q=home&space=sec", admin, "[Security home]"},
		{"q=brak&limit=1", alice, "[Braking system]"},
	} {
		var titles []string
		for _, h := range c.expect("GET", "/search/quick?"+tc.query, tc.as, nil, 200)["pages"].([]any) {
			titles = append(titles, h.(map[string]any)["title"].(string))
		}
		if fmt.Sprint(titles) != tc.want && !(tc.want == "[]" && titles == nil) {
			t.Errorf("%s found %v, want %s", tc.query, titles, tc.want)
		}
	}

	got2 := webhookBin(t)
	c.expect("PUT", stubPrefix+"/acme/webhook", "", map[string]any{"url": got2.url + "/hook", "secret": "stator_whs_contract"}, 204)
	if v := c.expect("POST", stubPrefix+"/acme/pages/"+timing+"/publish", "", map[string]any{"title": "Brake timing, revised"}, 200)["version"]; v != float64(3) {
		t.Errorf("publishing made version %v", v)
	}
	c.expect("POST", stubPrefix+"/acme/pages/"+timing+"/move", "", map[string]any{"spaceKey": "SEC"}, 204)
	c.expect("GET", "/pages/"+timing, alice, nil, 404)
	c.expect("DELETE", stubPrefix+"/acme/pages/"+listed["Onboarding guide"]["id"].(string), "", nil, 204)
	c.expect("GET", "/pages/"+listed["Onboarding guide"]["id"].(string), alice, nil, 404)
	c.expect("POST", stubPrefix+"/acme/webhooks", "", map[string]any{"topic": "ping", "payload": map[string]any{"message": "Stator can reach this address."}}, 200)
	if topics := got2.topics(); topics != "page.published page.moved page.deleted ping" {
		t.Errorf("the stub announced %s", topics)
	}
	published, moved := got2.payload(0)["page"].(map[string]any), got2.payload(1)
	if published["version"] != float64(3) || published["title"] != "Brake timing, revised" || moved["page"].(map[string]any)["space"].(map[string]any)["key"] != "SEC" {
		t.Errorf("the announcements read %v and %v", published, moved)
	}
	c.expect("PUT", stubPrefix+"/acme/webhook", "", map[string]any{"url": ""}, 204)
	c.expect("POST", stubPrefix+"/acme/webhooks", "", map[string]any{"topic": "ping"}, 409)
}

// bin receives the stub's webhooks, checking each is signed and shaped as Stator's webhook service sends it. Stator's
// document still requires orgId on WebhookEnvelope, which its service no longer sends, so the shape is checked here.
type bin struct {
	t      *testing.T
	url    string
	mu     sync.Mutex
	bodies []map[string]any
}

func webhookBin(t *testing.T) *bin {
	b := &bin{t: t}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.Header.Get(stator.SignatureHeader) != stator.Sign(raw, "stator_whs_contract") || r.Header.Get(stator.EventHeader) == "" {
			t.Errorf("a delivery is not signed as Stator signs: %v", r.Header)
		}
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		for _, field := range []string{"id", "topic", "occurredAt", "payload"} {
			if _, ok := body[field]; !ok {
				t.Errorf("a delivery has no %s: %s", field, raw)
			}
		}
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
