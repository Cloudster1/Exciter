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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "exciter-integration", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: f.api.srv.URL + "/api/v1/mcp", HTTPClient: &http.Client{Transport: bearer(secret)},
	}, nil)
	if err != nil {
		t.Fatalf("the client does not connect: %v", err)
	}
	defer session.Close()

	started := session.InitializeResult()
	if started.ProtocolVersion != httpapi.MCPProtocolVersions[0] || started.ServerInfo.Name != httpapi.MCPServerName ||
		!strings.Contains(started.Instructions, "baseRevision") {
		t.Errorf("initialize: version %s, server %+v, instructions %q", started.ProtocolVersion, started.ServerInfo, started.Instructions)
	}

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range listed.Tools {
		tools[tool.Name] = tool
	}
	create, get := tools["create_requirement"], tools["get_requirement"]
	if create == nil || get == nil {
		t.Fatalf("the tools listed are %v", slices.Sorted(maps.Keys(tools)))
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
