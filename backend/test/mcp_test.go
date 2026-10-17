//go:build integration

package test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

// bearer sends a personal access token on every call, as an assistant's client configured with one does.
type bearer string

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}

// The protocol's own Go client holds the endpoint to the protocol as others read it, not as Exciter does (#183):
// it asks in a newer revision first and falls back, opens no stream, and reads what each tool says it answers.
func TestAnMCPClientWorksWithRequirements(t *testing.T) {
	key := projectKey(t, "MCP")
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	_, secret := makeToken(t, f.owner, map[string]any{"name": "assistant"})
	f.h.settle(t)

	a := connectAssistant(t, f, secret)
	ctx, session := a.ctx, a.session

	started := session.InitializeResult()
	if started.ProtocolVersion != httpapi.MCPProtocolVersions[0] || started.ServerInfo.Name != httpapi.MCPServerName ||
		!strings.Contains(started.Instructions, "baseRevision") {
		t.Errorf("initialize: version %s, server %+v, instructions %q", started.ProtocolVersion, started.ServerInfo, started.Instructions)
	}
	create, get := a.tools["create_requirement"], a.tools["get_requirement"]
	if create == nil || get == nil {
		t.Fatalf("the tools listed are %v", slices.Sorted(maps.Keys(a.tools)))
	}
	if get.Title != "Get requirement" || get.Annotations == nil || !get.Annotations.ReadOnlyHint ||
		create.Annotations.DestructiveHint == nil || *create.Annotations.DestructiveHint {
		t.Errorf("get_requirement is %q with %+v; create_requirement %+v", get.Title, get.Annotations, create.Annotations)
	}

	made := callTool(ctx, t, session, create, map[string]any{"projectKey": key, "title": "Written by an assistant"})
	reqKey, _ := made["requirement"].(map[string]any)["key"].(string)
	if !strings.HasPrefix(reqKey, key+"-R") {
		t.Fatalf("create_requirement answered %v", made)
	}
	read := callTool(ctx, t, session, get, map[string]any{"reqKey": reqKey})
	if read["requirement"].(map[string]any)["title"] != "Written by an assistant" {
		t.Errorf("get_requirement answered %v", read)
	}

	refused, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_requirement", Arguments: map[string]any{"reqKey": key + "-R999"}})
	if err != nil || !refused.IsError || refused.StructuredContent != nil {
		t.Errorf("a requirement nobody has is a refusal in the result: %v %+v", err, refused)
	}

	document, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "exciter://openapi.json"})
	if err != nil || len(document.Contents) != 1 || !strings.Contains(document.Contents[0].Text, `"/mcp"`) {
		t.Errorf("the API document as a resource: %v", err)
	}
}

// callTool calls a tool and holds what it answers to the schema it said it answers in.
func callTool(ctx context.Context, t *testing.T, session *mcp.ClientSession, tool *mcp.Tool, args map[string]any) map[string]any {
	t.Helper()
	got, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool.Name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool.Name, err)
	}
	if got.IsError {
		t.Fatalf("%s refused: %+v", tool.Name, got.Content)
	}
	encoded, _ := json.Marshal(tool.OutputSchema)
	var schema jsonschema.Schema
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("%s: the output schema does not read as JSON Schema: %v", tool.Name, err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("%s: the output schema does not resolve: %v", tool.Name, err)
	}
	structured, ok := got.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("%s answered no structured content: %+v", tool.Name, got)
	}
	if err := resolved.Validate(structured); err != nil {
		t.Errorf("%s answered outside its output schema: %v", tool.Name, err)
	}
	return structured
}

// The edges of the endpoint as a client meets them: no stream to open, and a header in a revision nobody speaks.
func TestTheMCPEndpointRefusesWhatItDoesNotSpeak(t *testing.T) {
	f := newProjectFixture(t)
	_, secret := makeToken(t, f.plain, map[string]any{"name": "assistant"})
	script := f.api.withToken(f.plain.user, secret)
	refuse(t, "a stream", script.get(t, "/api/v1/mcp"), http.StatusMethodNotAllowed, "method_not_allowed")

	call := func(version string) response {
		req, _ := http.NewRequest(http.MethodPost, f.api.srv.URL+"/api/v1/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("MCP-Protocol-Version", version)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return response{Status: resp.StatusCode, Body: body}
	}
	refuse(t, "an old revision", call("2024-11-05"), http.StatusBadRequest, "unsupported_protocol_version")
	for _, version := range httpapi.MCPProtocolVersions {
		if got := call(version); got.Status != http.StatusOK || got.Body["error"] != nil {
			t.Errorf("%s: %d %v", version, got.Status, got.Body)
		}
	}
}

// assistant is an MCP client connected with a token, its tools by name, each answer held to its tool's schema.
type assistant struct {
	ctx     context.Context
	session *mcp.ClientSession
	tools   map[string]*mcp.Tool
}

func connectAssistant(t *testing.T, f *projectFixture, secret string) *assistant {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "exciter-integration", Version: "1"}, nil).Connect(ctx,
		&mcp.StreamableClientTransport{Endpoint: f.api.srv.URL + "/api/v1/mcp", HTTPClient: &http.Client{Transport: bearer(secret)}}, nil)
	if err != nil {
		t.Fatalf("the client does not connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &assistant{ctx: ctx, session: session, tools: map[string]*mcp.Tool{}}
	for _, tool := range listed.Tools {
		a.tools[tool.Name] = tool
	}
	return a
}

func (a *assistant) call(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	tool := a.tools[name]
	if tool == nil {
		t.Fatalf("%s is not offered", name)
	}
	return callTool(a.ctx, t, a.session, tool, args)
}

// refused calls a tool expecting a refusal, and answers its sentence.
func (a *assistant) refused(t *testing.T, name string, args map[string]any) string {
	t.Helper()
	got, err := a.session.CallTool(a.ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if !got.IsError {
		t.Fatalf("%s was not refused: %+v", name, got.StructuredContent)
	}
	return got.Content[0].(*mcp.TextContent).Text
}

// An assistant writes a specification from what the project's structure says it may hold, moves it through the
// workflow in one step, baselines it and reads what changed since, as the tools of #184 let it.
func TestAnAssistantWritesAndBaselinesASpecification(t *testing.T) {
	key := projectKey(t, "MCS")
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	_, secret := makeToken(t, f.owner, map[string]any{"name": "assistant"})
	f.h.settle(t)
	a := connectAssistant(t, f, secret)

	types := list(t, response{Body: a.call(t, "list_types", map[string]any{"projectKey": key})}, "types")
	var typeID string
	for _, each := range types {
		if each := each.(map[string]any); each["retired"] != true {
			typeID = each["id"].(string)
			break
		}
	}
	if typeID == "" {
		t.Fatalf("%s has no type to write with: %v", key, types)
	}
	a.call(t, "list_attributes", map[string]any{"projectKey": key})
	statuses := list(t, response{Body: a.call(t, "list_statuses", map[string]any{"projectKey": key})}, "statuses")
	if len(statuses) == 0 {
		t.Fatal("a project's workflow has no statuses")
	}
	if kinds := list(t, response{Body: a.call(t, "list_project_link_types", map[string]any{"projectKey": key})}, "linkTypes"); len(kinds) == 0 {
		t.Error("a project offers no link type")
	}

	section := a.call(t, "create_requirement", map[string]any{"projectKey": key, "title": "Checkout", "kind": "section"})["requirement"].(map[string]any)
	var keys []string
	for _, title := range []string{"Pay by card", "Pay by invoice"} {
		made := a.call(t, "create_requirement", map[string]any{"projectKey": key, "title": title, "typeId": typeID})["requirement"].(map[string]any)
		keys = append(keys, made["key"].(string))
	}
	moved := a.call(t, "move_requirement", map[string]any{"reqKey": keys[0], "place": map[string]any{"parentId": section["id"], "afterId": nil}})
	if moved["requirement"].(map[string]any)["key"] != keys[0] {
		t.Errorf("a move within the project keeps the key: %v", moved)
	}
	labelled := a.call(t, "label_requirement", map[string]any{"reqKey": keys[0], "name": "assistant"})
	if !slices.Contains(labelled["labels"].([]any), any("assistant")) {
		t.Errorf("label_requirement answered %v", labelled)
	}
	if found := list(t, response{Body: a.call(t, "list_labels", map[string]any{"project": key, "q": "assist"})}, "labels"); len(found) != 1 {
		t.Errorf("list_labels found %v", found)
	}
	if entries := list(t, response{Body: a.call(t, "get_activity", map[string]any{"reqKey": keys[0]})}, "entries"); len(entries) < 2 {
		t.Errorf("the activity of a requirement written, moved and labelled holds %d entries", len(entries))
	}

	offered := list(t, response{Body: a.call(t, "list_bulk_transitions", map[string]any{"projectKey": key, "key": keys})}, "requirements")
	if len(offered) != 2 {
		t.Fatalf("list_bulk_transitions answered %v", offered)
	}
	transitions := offered[0].(map[string]any)["transitions"].([]any)
	if len(transitions) == 0 {
		t.Fatal("a new requirement offers no transition")
	}
	first := transitions[0].(map[string]any)
	results := list(t, response{Body: a.call(t, "transition_requirements", map[string]any{"projectKey": key, "transitionId": first["id"],
		"comment": "Ready for review", "keys": append(slices.Clone(keys), key+"-R999")})}, "results")
	outcomes := map[string]string{}
	for _, each := range results {
		outcomes[each.(map[string]any)["key"].(string)] = each.(map[string]any)["outcome"].(string)
	}
	if outcomes[keys[0]] != "taken" || outcomes[keys[1]] != "taken" || outcomes[key+"-R999"] != "refused" {
		t.Errorf("transition_requirements answered %v", outcomes)
	}

	baseline := a.call(t, "take_baseline", map[string]any{"projectKey": key, "name": "Before the change"})["baseline"].(map[string]any)
	id := baseline["id"].(string)
	if listed := list(t, response{Body: a.call(t, "list_baselines", map[string]any{"projectKey": key})}, "baselines"); len(listed) != 1 {
		t.Errorf("list_baselines answered %v", listed)
	}
	if got := a.call(t, "get_baseline", map[string]any{"baselineID": id})["baseline"].(map[string]any); got["name"] != "Before the change" {
		t.Errorf("get_baseline answered %v", got)
	}
	if held := list(t, response{Body: a.call(t, "get_baseline_spec", map[string]any{"baselineID": id})}, "entries"); len(held) != 3 {
		t.Errorf("the baseline holds %d entries, want the section and both requirements", len(held))
	}
	a.call(t, "update_requirement", map[string]any{"reqKey": keys[1], "title": "Pay by invoice within 30 days", "baseRevision": 1})

	items := a.call(t, "compare_baseline", map[string]any{"baselineID": id})["comparison"].(map[string]any)["items"].([]any)
	revised := false
	for _, each := range items {
		if item := each.(map[string]any); item["key"] == keys[1] && item["revised"] == true {
			revised = true
		}
	}
	if !revised {
		t.Errorf("compare_baseline does not find %s revised: %v", keys[1], items)
	}
	spec := a.call(t, "get_spec", map[string]any{"projectKey": key, "changes": "baseline", "baseline": id, "onlyChanged": true})
	var changed []string
	for _, each := range spec["entries"].([]any) {
		if entry := each.(map[string]any); entry["change"] != nil && entry["change"].(map[string]any)["kind"] != "unchanged" {
			changed = append(changed, entry["node"].(map[string]any)["key"].(string))
		}
	}
	if !slices.Equal(changed, keys[1:]) {
		t.Errorf("get_spec against the baseline marks %v changed, want %v", changed, keys[1:])
	}

	// What the tools leave to their person: reading alone keeps every write of them away, and another project is
	// out of a limited token's sight.
	_, reading := makeToken(t, f.owner, map[string]any{"name": "reader", "scopes": []string{"read"}})
	other := projectKey(t, "MCO")
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": other, "name": other}), http.StatusCreated, "make "+other)
	_, limited := makeToken(t, f.owner, map[string]any{"name": "elsewhere", "projects": []string{other}})
	f.h.settle(t)
	reader := connectAssistant(t, f, reading)
	for _, write := range []string{"move_requirement", "label_requirement", "transition_requirements", "take_baseline"} {
		if reader.tools[write] != nil {
			t.Errorf("a read-only token is offered %s", write)
		}
	}
	if entries := reader.call(t, "get_spec", map[string]any{"projectKey": key})["entries"].([]any); len(entries) != 3 {
		t.Errorf("a read-only token reads %d entries of the specification", len(entries))
	}
	if sentence := connectAssistant(t, f, limited).refused(t, "get_spec", map[string]any{"projectKey": key}); sentence == "" {
		t.Error("a token limited to another project was refused without a sentence")
	}
}
