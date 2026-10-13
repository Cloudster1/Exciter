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
	"testing"

	"github.com/cloudster1/exciter/backend/internal/openapi"
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

func TestEveryAnswerFitsArmaturesDocument(t *testing.T) {
	doc, raw := loadArmature(t)
	server := httptest.NewServer(newStub(raw).handler())
	defer server.Close()
	c := &contract{t: t, doc: doc, base: server.URL, served: map[string]bool{}}
	alice, admin, reader := patFor("acme", "alice"), patFor("acme", adminName), patFor("acme", readerName)

	if got := c.expect("GET", "/openapi.json", "", nil, 200); got["info"].(map[string]any)["title"] != "Armature" {
		t.Errorf("the document served is not Armature's: %v", got["info"])
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
