package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
)

// The tools docs/api-contract-m1.md names, and the three the foundation marked before it.
var contractTools = []string{
	"create_requirement", "diff_revisions", "get_project", "get_requirement", "get_revision", "get_tree",
	"link_requirements", "list_audit_log", "list_links", "list_people", "list_projects", "list_requirements",
	"list_revisions", "list_transitions", "search_requirements", "server_info", "transition_requirement",
	"update_requirement", "whoami",
}

func TestTheToolsAreTheContractsEachNamedOnceAndExplained(t *testing.T) {
	var names []string
	seen := map[string]string{}
	for _, op := range operations {
		if op.tool == "" {
			continue
		}
		key := op.method + " " + op.path
		if prior, ok := seen[op.tool]; ok {
			t.Errorf("%s and %s share the tool name %s", prior, key, op.tool)
		}
		seen[op.tool] = key
		names = append(names, op.tool)
		switch {
		case !strings.HasSuffix(op.toolHelp, "."):
			t.Errorf("%s: the tool sentence %q does not end", key, op.toolHelp)
		case strings.ToLower(op.tool) != op.tool || strings.ContainsAny(op.tool, "- "):
			t.Errorf("%s: %q is not a snake_case tool name", key, op.tool)
		case op.pending || op.binary || op.multipart:
			t.Errorf("%s cannot be a tool: it is pending, or carries a file", key)
		case op.method == http.MethodDelete:
			t.Errorf("%s: no tool removes anything", key)
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, contractTools) {
		t.Errorf("the tools are %v, want the contract's %v", names, contractTools)
	}
}

func TestAToolRequiresItsPathAndReadsWhenItGets(t *testing.T) {
	for _, tool := range toolCatalog() {
		if tool.ReadOnly != (tool.Method == http.MethodGet) {
			t.Errorf("%s: readOnly %v for %s", tool.Name, tool.ReadOnly, tool.Method)
		}
		for _, name := range pathParams(tool.Path) {
			if _, ok := tool.Input.Properties[name]; !ok || !slices.Contains(tool.Input.Required, name) {
				t.Errorf("%s: the path value %s is not a required argument", tool.Name, name)
			}
		}
		encoded, err := json.Marshal(tool.Input)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "#/components/") {
			t.Errorf("%s: the schema refers outside itself: %s", tool.Name, encoded)
		}
	}
	create, _ := toolByName("create_requirement")
	for _, field := range []string{"projectKey", "title", "statement"} {
		if _, ok := create.Input.Properties[field]; !ok {
			t.Errorf("create_requirement does not take %s", field)
		}
	}
	search, _ := toolByName("search_requirements")
	if q := search.Input.Properties["q"]; q == nil || q.Type != "string" {
		t.Error("search_requirements does not take q as a string")
	}
}

func TestATokenIsOfferedOnlyWhatItMayCall(t *testing.T) {
	all, reading, limited := listTools(false, false), listTools(true, false), listTools(false, true)
	if len(reading) == 0 || len(reading) >= len(all) {
		t.Fatalf("%d tools for a read token of %d", len(reading), len(all))
	}
	for _, tool := range reading {
		if tool["annotations"].(map[string]any)["readOnlyHint"] != true {
			t.Errorf("a read token is offered %s", tool["name"])
		}
	}
	for _, tool := range limited {
		if tool["name"] == "list_audit_log" {
			t.Error("a token limited to projects is offered the organization's audit log")
		}
	}
	if len(limited) != len(all)-1 {
		t.Errorf("a limited token is offered %d tools of %d, want all but the audit log", len(limited), len(all))
	}
}

func TestTheEndpointSpeaksJSONRPC(t *testing.T) {
	s := &Server{}
	call := func(body string, p *auth.Principal) (int, rpcResponse) {
		req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(body))
		if p != nil {
			req = req.WithContext(context.WithValue(req.Context(), ctxPrincipal, p))
		}
		rec := httptest.NewRecorder()
		s.handleMCP(rec, req)
		var resp rpcResponse
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("not a JSON-RPC answer: %s", rec.Body.String())
			}
		}
		return rec.Code, resp
	}

	status, resp := call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, nil)
	if status != http.StatusOK || resp.Error != nil || !strings.Contains(string(resp.Result), MCPProtocolVersion) || !strings.Contains(string(resp.Result), `"exciter"`) {
		t.Fatalf("initialize: %d %+v", status, resp)
	}
	if string(resp.ID) != "1" {
		t.Errorf("the id %s was not echoed", resp.ID)
	}
	_, resp = call(`{"jsonrpc":"2.0","id":"a","method":"tools/list"}`, nil)
	if !strings.Contains(string(resp.Result), `"get_requirement"`) || !strings.Contains(string(resp.Result), `"create_requirement"`) {
		t.Fatalf("tools/list: %s", resp.Result)
	}
	reader := &auth.Principal{UserID: uuid.New(), Token: &auth.CallingToken{Scopes: []string{auth.ScopeRead}, AllProjects: true}}
	_, resp = call(`{"jsonrpc":"2.0","id":"b","method":"tools/list"}`, reader)
	if !strings.Contains(string(resp.Result), `"get_requirement"`) || strings.Contains(string(resp.Result), `"create_requirement"`) {
		t.Errorf("a read token is offered: %s", resp.Result)
	}
	_, resp = call(`{"jsonrpc":"2.0","id":"c","method":"tools/call","params":{"name":"create_requirement","arguments":{}}}`, reader)
	if resp.Error != nil || !strings.Contains(string(resp.Result), `"isError":true`) || !strings.Contains(string(resp.Result), "can only read") {
		t.Errorf("a read token calling a writing tool: %+v %s", resp.Error, resp.Result)
	}
	limited := &auth.Principal{UserID: uuid.New(), Token: &auth.CallingToken{Scopes: []string{}, Projects: []string{"CHK"}}}
	_, resp = call(`{"jsonrpc":"2.0","id":"d","method":"tools/call","params":{"name":"list_audit_log","arguments":{}}}`, limited)
	if !strings.Contains(string(resp.Result), "limited to some projects") {
		t.Errorf("a limited token calling an organization-wide tool: %s", resp.Result)
	}

	if status, _ = call(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil); status != http.StatusAccepted {
		t.Errorf("a notification answers %d", status)
	}
	if _, resp = call(`{"jsonrpc":"2.0","id":2,"method":"nothing/here"}`, nil); resp.Error == nil || resp.Error.Code != rpcMethodNotFound {
		t.Errorf("an unknown method answers %+v", resp.Error)
	}
	if _, resp = call(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"no_such_tool"}}`, nil); resp.Error == nil || resp.Error.Code != rpcInvalidParams {
		t.Errorf("an unknown tool answers %+v", resp.Error)
	}
	if _, resp = call(`[{"jsonrpc":"2.0","id":4,"method":"ping"}]`, nil); resp.Error == nil || resp.Error.Code != rpcInvalidRequest {
		t.Errorf("a batch answers %+v", resp.Error)
	}
	if _, resp = call(`{"id":5,"method":"ping"}`, nil); resp.Error == nil || resp.Error.Code != rpcInvalidRequest {
		t.Errorf("a request without jsonrpc 2.0 answers %+v", resp.Error)
	}
	if status, _ = call(`not json`, nil); status != http.StatusBadRequest {
		t.Errorf("malformed JSON answers %d", status)
	}
	_, resp = call(`{"jsonrpc":"2.0","id":6,"method":"resources/read","params":{"uri":"`+mcpOpenAPIResource+`"}}`, nil)
	if resp.Error != nil || !strings.Contains(string(resp.Result), `/mcp`) {
		t.Errorf("the document is not a resource: %+v", resp.Error)
	}
}

func TestAToolCallBecomesTheHTTPCallItStandsFor(t *testing.T) {
	tool, _ := toolByName("search_requirements")
	outer := httptest.NewRequest(http.MethodPost, mcpPath, nil)
	outer.Header.Set("Authorization", "Bearer exciter_pat_x")
	inner, err := tool.request(outer, map[string]json.RawMessage{
		"q": json.RawMessage(`"basket"`), "limit": json.RawMessage(`5`), "project": json.RawMessage(`["CHK","SYS"]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inner.Method != http.MethodGet || inner.URL.Path != APIPrefix+"/search" {
		t.Errorf("inner call is %s %s", inner.Method, inner.URL.Path)
	}
	if q := inner.URL.Query(); q.Get("q") != "basket" || q.Get("limit") != "5" || len(q["project"]) != 2 {
		t.Errorf("query = %s", inner.URL.RawQuery)
	}
	if inner.Header.Get("Authorization") != "Bearer exciter_pat_x" {
		t.Error("the credential did not travel")
	}

	get, _ := toolByName("get_requirement")
	if _, err := get.request(outer, nil); err == nil {
		t.Error("a missing path value was accepted")
	}
	if _, err := get.request(outer, map[string]json.RawMessage{"reqKey": json.RawMessage(`"CHK-R1"`), "extra": json.RawMessage(`1`)}); err == nil {
		t.Error("an argument a GET cannot carry was accepted")
	}

	create, _ := toolByName("create_requirement")
	inner, err = create.request(outer, map[string]json.RawMessage{"projectKey": json.RawMessage(`"CHK"`), "title": json.RawMessage(`"Pay"`)})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(inner.Body)
	if inner.Method != http.MethodPost || inner.URL.Path != APIPrefix+"/projects/CHK/requirements" || string(body) != `{"title":"Pay"}` {
		t.Errorf("create call: %s %s %s", inner.Method, inner.URL.Path, body)
	}
}

func TestAResultTooLargeIsCutAndSaysSo(t *testing.T) {
	rec := &capture{header: http.Header{}}
	rec.WriteHeader(http.StatusOK)
	_, _ = rec.Write([]byte(`{"text":"` + strings.Repeat("a", MCPMaxToolResultBytes) + `"}`))
	got := rec.toolResult()
	if got.StructuredContent != nil || !strings.HasSuffix(got.Content[0].Text, string(truncatedNote)) {
		t.Errorf("a long result was not cut: %d bytes", len(got.Content[0].Text))
	}
	refused := &capture{header: http.Header{}}
	refused.WriteHeader(http.StatusNotFound)
	_, _ = refused.Write([]byte(`{"error":{"code":"not_found","message":"There is no such requirement."}}`))
	if r := refused.toolResult(); !r.IsError || r.Content[0].Text != "There is no such requirement." {
		t.Errorf("a refusal reads %+v", r)
	}
}
