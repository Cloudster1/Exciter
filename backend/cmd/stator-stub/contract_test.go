package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
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

func (c *contract) route(method, path string) *route {
	for i, rt := range routes {
		if rt.Method == method && rt.Path == path {
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

	rt := c.route(method, path)
	if rt == nil {
		c.t.Fatalf("%s %s is no route of the stub", method, path)
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
