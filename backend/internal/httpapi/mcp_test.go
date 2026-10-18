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
	"github.com/cloudster1/exciter/backend/internal/openapi"
)

// The tools docs/api-contract-m1.md names and the three the foundation marked before it, with what
// docs/api-contract-m7.md adds for the document, its structure and baselines (#184), and for review, comments and
// trace (#185), and the reads of review sessions docs/api-contract-m8.md adds (#204).
var contractTools = []string{
	"check_requirement_text", "clear_armature_link", "clear_stator_link", "clear_suspect_link", "comment_on_requirement", "compare_baseline",
	"create_armature_issue", "create_requirement", "diff_revisions", "get_activity", "get_approval", "get_baseline",
	"get_baseline_spec", "get_code_links", "get_comment", "get_coverage", "get_matrix", "get_project", "get_quality_findings", "get_requirement", "get_review",
	"get_review_session", "get_revision", "get_spec", "get_test_results", "get_tree", "label_requirement", "link_armature_issue", "link_requirements",
	"link_stator_page", "list_armature_links", "list_armature_projects", "list_attributes", "list_audit_log",
	"list_baselines", "list_bulk_transitions", "list_comments", "list_labels", "list_links", "list_people",
	"list_project_link_types", "list_projects", "list_requirements", "list_review_sessions", "list_reviews", "list_revisions",
	"list_stator_links", "list_statuses", "list_suspect_links", "list_terms", "list_trace_chains", "list_transitions", "list_types",
	"list_waiting_reviews", "move_requirement", "reply_to_comment", "request_review", "request_reviews",
	"resolve_comment_thread", "run_trace_check", "search_armature_issues", "search_requirements", "search_stator_pages", "server_info",
	"take_baseline", "transition_requirement", "transition_requirements", "update_comment", "update_requirement",
	"whoami", "withdraw_review",
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
	if status != http.StatusOK || resp.Error != nil || !strings.Contains(string(resp.Result), MCPProtocolVersions[0]) || !strings.Contains(string(resp.Result), `"exciter"`) ||
		!strings.Contains(string(resp.Result), `"instructions"`) {
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

func TestAResultTooLargeIsRefusedAndSaysHowToAskForLess(t *testing.T) {
	rec := &capture{header: http.Header{}}
	rec.WriteHeader(http.StatusOK)
	_, _ = rec.Write([]byte(`{"text":"` + strings.Repeat("a", MCPMaxToolResultBytes) + `"}`))
	if got := rec.toolResult(); !got.IsError || got.StructuredContent != nil || got.Content[0].Text != tooLarge {
		t.Errorf("a long result answered %+v", got.Content)
	}
	fits := &capture{header: http.Header{}}
	_, _ = fits.Write([]byte(`{"requirement":{"key":"CHK-R1"}}`))
	if got := fits.toolResult(); got.IsError || got.StructuredContent == nil || got.Content[0].Text != `{"requirement":{"key":"CHK-R1"}}` {
		t.Errorf("an answer that fits is structured and its text the same JSON: %+v", got)
	}
	refused := &capture{header: http.Header{}}
	refused.WriteHeader(http.StatusNotFound)
	_, _ = refused.Write([]byte(`{"error":{"code":"not_found","message":"There is no such requirement."}}`))
	if r := refused.toolResult(); !r.IsError || r.Content[0].Text != "There is no such requirement." {
		t.Errorf("a refusal reads %+v", r)
	}
}

// A client asks in the newest version it speaks and goes on in whatever is answered, so the answer is its version
// when it is spoken here and the newest otherwise; a header naming a version neither side can speak is refused.
func TestTheVersionIsNegotiatedAndTheHeaderHeldToIt(t *testing.T) {
	s := &Server{}
	call := func(asked, header string) (int, string) {
		body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + asked + `"}}`
		req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(body))
		if header != "" {
			req.Header.Set(mcpVersionHeader, header)
		}
		rec := httptest.NewRecorder()
		s.handleMCP(rec, req)
		var resp rpcResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		var result struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(resp.Result, &result)
		return rec.Code, result.ProtocolVersion
	}
	for asked, answered := range map[string]string{
		"2025-06-18": "2025-06-18", "2025-11-25": "2025-11-25", "2024-11-05": MCPProtocolVersions[0], "": MCPProtocolVersions[0],
		"2026-07-28": MCPProtocolVersions[0],
	} {
		if _, got := call(asked, ""); got != answered {
			t.Errorf("asked for %q, answered %q, want %q", asked, got, answered)
		}
	}
	for header, status := range map[string]int{
		"2025-06-18": http.StatusOK, "2025-11-25": http.StatusOK, "2026-07-28": http.StatusOK, "2024-11-05": http.StatusBadRequest,
		"2025-03-26": http.StatusBadRequest, "nonsense": http.StatusBadRequest,
	} {
		if got, _ := call("2025-06-18", header); got != status {
			t.Errorf("the header %q answered %d, want %d", header, got, status)
		}
	}
}

// What a tool answers is described as the protocol asks, an object standing alone, and every hint is said so that
// a client never takes a write for destructive by default.
func TestEveryToolDescribesWhatItAnswersAndHowItActs(t *testing.T) {
	rows := map[string]operation{}
	for _, op := range operations {
		if op.tool != "" {
			rows[op.tool] = op
		}
	}
	for _, tool := range listTools(false, false) {
		name := tool["name"].(string)
		op := rows[name]
		if tool["title"] == "" || tool["title"] == name {
			t.Errorf("%s has no title in words: %v", name, tool["title"])
		}
		hints := tool["annotations"].(map[string]any)
		for _, hint := range []string{"readOnlyHint", "destructiveHint", "idempotentHint", "openWorldHint"} {
			if _, ok := hints[hint].(bool); !ok {
				t.Errorf("%s does not say %s", name, hint)
			}
		}
		if hints["idempotentHint"] != (op.method == http.MethodPut || op.method == http.MethodPatch) || hints["destructiveHint"] != op.destructive ||
			hints["openWorldHint"] != op.openWorld {
			t.Errorf("%s hints %v for %s", name, hints, op.method)
		}
		output, described := tool["outputSchema"].(*openapi.Schema)
		if answersNothing := op.responses[http.StatusOK] == nil && op.responses[http.StatusCreated] == nil; answersNothing {
			if described {
				t.Errorf("%s answers nothing but describes an output", name)
			}
			continue
		}
		if !described || output.Type != "object" || len(output.Properties) == 0 {
			t.Errorf("%s: the output is not an object: %+v", name, output)
			continue
		}
		encoded, _ := json.Marshal(output)
		if strings.Contains(string(encoded), "#/components/") {
			t.Errorf("%s: the output schema refers outside itself", name)
		}
	}
	if get, _ := toolByName("get_requirement"); get.Title != "Get requirement" || get.Output.Properties["requirement"] == nil {
		t.Errorf("get_requirement is %q with output %+v", get.Title, get.Output)
	}
	if link, _ := toolByName("link_armature_issue"); !link.OpenWorld {
		t.Error("linking an Armature issue does not say it reaches Armature")
	}
}

// A page of another site that rebinds a name to a local server reaches MCP with a bearer as well as with a cookie.
func TestMCPRefusesAForeignOriginWhateverTheCredential(t *testing.T) {
	h := newServer(t).Routes(nil)
	for origin, refused := range map[string]bool{"http://evil.test": true, "http://app.test": false, "": false} {
		req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Authorization", "Bearer x")
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, body := serve(t, h, req)
		if got := resp.StatusCode == http.StatusForbidden && errorOf(t, body)["code"] == "cross_site"; got != refused {
			t.Errorf("origin %q: %d %v", origin, resp.StatusCode, body)
		}
	}
	resp, body := serve(t, h, httptest.NewRequest(http.MethodGet, mcpPath, nil))
	if resp.StatusCode != http.StatusMethodNotAllowed || errorOf(t, body)["code"] != "method_not_allowed" {
		t.Errorf("GET %s = %d %v, want 405: there is no stream to open", mcpPath, resp.StatusCode, body)
	}
}

// An approval is a person's own judgement, and a signed one proves the person to the identity provider, so no tool
// stands for deciding a review however the table grows (docs/decisions.md, 2026-10-17).
func TestNoToolDecidesAReview(t *testing.T) {
	found := false
	for _, op := range operations {
		if op.handler != "handleDecideReview" {
			continue
		}
		found = true
		if op.tool != "" {
			t.Errorf("%s %s is the tool %s; a review is decided by a person in the browser", op.method, op.path, op.tool)
		}
	}
	if !found {
		t.Fatal("the decisions operation is not in the table; hold whatever decides a review now instead")
	}
	for _, tool := range toolCatalog() {
		if strings.HasSuffix(tool.Path, "/decisions") {
			t.Errorf("%s stands for %s", tool.Name, tool.Path)
		}
	}
}

// An assistant reads Markdown unless it asks for the document alone, and a tool whose read offers no format is
// called as it was.
func TestAReadingToolAsksForMarkdownUnlessToldOtherwise(t *testing.T) {
	outer := httptest.NewRequest(http.MethodPost, mcpPath, nil)
	get, _ := toolByName("get_requirement")
	inner, err := get.request(outer, map[string]json.RawMessage{"reqKey": json.RawMessage(`"CHK-R1"`)})
	if err != nil || inner.URL.Query().Get("format") != "markdown" {
		t.Errorf("get_requirement reads %v: %v", inner.URL, err)
	}
	inner, _ = get.request(outer, map[string]json.RawMessage{"reqKey": json.RawMessage(`"CHK-R1"`), "format": json.RawMessage(`"document"`)})
	if inner.URL.Query().Get("format") != "document" {
		t.Errorf("a format asked for is not kept: %v", inner.URL)
	}
	tree, _ := toolByName("get_tree")
	if inner, _ = tree.request(outer, map[string]json.RawMessage{"projectKey": json.RawMessage(`"CHK"`)}); inner.URL.RawQuery != "" {
		t.Errorf("get_tree, which offers no format, reads %v", inner.URL)
	}
}
