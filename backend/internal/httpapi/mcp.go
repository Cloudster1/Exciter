package httpapi

// The Model Context Protocol endpoint, adapted from Stator's (#22).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// MCP over one endpoint: a tool call is the HTTP call the browser would make,
// run in this process as the caller, so nothing is stored between requests.
const (
	MCPServerName    = "exciter"
	mcpServerVersion = "1"
	// MCPMaxToolResultBytes refuses a tool result a model would drown in; the
	// refusal says how to ask for less.
	MCPMaxToolResultBytes = 64 << 10
	mcpPath               = APIPrefix + "/mcp"
	mcpVersionHeader      = "MCP-Protocol-Version"
)

// MCPProtocolVersions are the revisions spoken here, newest first (#183).
var MCPProtocolVersions = []string{"2025-11-25", "2025-06-18"}

// mcpInstructions is what the model is told of Exciter before it calls anything.
const mcpInstructions = "Exciter manages requirements. A project is named by its key, such as CHK, and a requirement by its " +
	"key, such as CHK-R42; list_projects and search_requirements find them. A requirement's text changes in revisions: a change " +
	"to its title, statement or rationale sends baseRevision, the revision it was made from, and a newer one is refused so it " +
	"can be read and merged first. A project's specification numbers its sections and requirements by outline, such as 1.2.3, " +
	"and is read whole as Markdown from its resource, such as exciter://projects/CHK/spec. " +
	"Reviews can be asked for and read here; a person decides them in Exciter's browser pages, never through a tool."

const (
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

var tooLarge = fmt.Sprintf("The answer is larger than %d KiB. Narrow the query, or page with limit and offset.", MCPMaxToolResultBytes>>10)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// rpcResponse is what the endpoint answers; its members are untyped in the
// document because each method has a result of its own.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type toolCallParams struct {
	Name      string                     `json:"name"`
	Arguments map[string]json.RawMessage `json:"arguments"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content           []toolContent `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError,omitempty"`
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if v := r.Header.Get(mcpVersionHeader); v != "" && !spoken(v) {
		respondError(w, r, &APIError{Status: http.StatusBadRequest, Code: "unsupported_protocol_version",
			Message: "Exciter speaks MCP " + strings.Join(MCPProtocolVersions, " and ") + ". Update the client, or set it to one of them."})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		respondError(w, r, ErrBadRequest("The request body is too large. Send less at a time."))
		return
	}
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
		writeRPC(w, r, nil, nil, &rpcError{Code: rpcInvalidRequest, Message: "Send one request at a time; batches are not accepted."})
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		respondError(w, r, ErrBadRequest("The request body is not valid JSON. Send one JSON-RPC 2.0 request."))
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		writeRPC(w, r, req.ID, nil, &rpcError{Code: rpcInvalidRequest, Message: "A request names a method and says jsonrpc 2.0."})
		return
	}
	if strings.HasPrefix(req.Method, "notifications/") {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	result, rpcErr := s.dispatchMCP(r, req)
	writeRPC(w, r, req.ID, result, rpcErr)
}

func (s *Server) dispatchMCP(r *http.Request, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		return map[string]any{
			"protocolVersion": negotiated(params.ProtocolVersion),
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false}, "resources": map[string]any{"listChanged": false},
				"prompts": map[string]any{"listChanged": false},
			},
			"serverInfo":   map[string]any{"name": MCPServerName, "version": mcpServerVersion},
			"instructions": mcpInstructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		p := PrincipalFrom(r.Context())
		return map[string]any{"tools": listTools(p.ReadOnly(), p.InProjectsOnly())}, nil
	case "tools/call":
		var params toolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
			return nil, &rpcError{Code: rpcInvalidParams, Message: "A tool call names a tool and gives its arguments as an object."}
		}
		return s.callTool(r, params)
	case "resources/list":
		return s.listResources(r, req.Params)
	case "resources/templates/list":
		return map[string]any{"resourceTemplates": resourceTemplates}, nil
	case "resources/read":
		return s.readResource(r, req.Params)
	case "prompts/list":
		return map[string]any{"prompts": promptList()}, nil
	case "prompts/get":
		return s.getPrompt(r, req.Params)
	}
	return nil, &rpcError{Code: rpcMethodNotFound, Message: "There is no method " + req.Method + "."}
}

// listTools leaves the writing tools out for a token made to read, and the installation-wide ones for a token
// limited to projects, so an assistant is never offered what it would only be refused.
func listTools(readOnly, projectsOnly bool) []map[string]any {
	out := []map[string]any{}
	for _, t := range toolCatalog() {
		if (readOnly && !t.ReadOnly) || (projectsOnly && t.OrgWide) {
			continue
		}
		tool := map[string]any{
			"name":        t.Name,
			"title":       t.Title,
			"description": t.Description,
			"inputSchema": t.Input,
			// Every hint is sent, as the protocol takes a write left unmarked for destructive.
			"annotations": map[string]any{"title": t.Title, "readOnlyHint": t.ReadOnly, "destructiveHint": t.Destructive,
				"idempotentHint": t.Idempotent, "openWorldHint": t.OpenWorld},
		}
		if t.Output != nil {
			tool["outputSchema"] = t.Output
		}
		out = append(out, tool)
	}
	return out
}

// negotiated answers the version a client asks for when it is spoken here, and the newest otherwise, for the client
// to decide whether to go on.
func negotiated(asked string) string {
	if slices.Contains(MCPProtocolVersions, asked) {
		return asked
	}
	return MCPProtocolVersions[0]
}

// spoken says whether a request may be answered in the version its header names. One newer than any spoken here
// passes, as a newer client asks in its own version first and falls back on what the answer says.
func spoken(version string) bool {
	return slices.Contains(MCPProtocolVersions, version) || (protocolRevision.MatchString(version) && version > MCPProtocolVersions[0])
}

// protocolRevision is how the protocol names its revisions, by the day each was published.
var protocolRevision = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// callTool runs the tool as the HTTP call it stands for, through the whole
// middleware chain, so what a token may do here is what it may do anywhere.
func (s *Server) callTool(r *http.Request, params toolCallParams) (any, *rpcError) {
	tool, ok := toolByName(params.Name)
	if !ok {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "There is no tool " + params.Name + ". Ask tools/list for the names."}
	}
	if !tool.ReadOnly && PrincipalFrom(r.Context()).ReadOnly() {
		return refusal(errReadOnlyToken.Message), nil
	}
	if tool.OrgWide && PrincipalFrom(r.Context()).InProjectsOnly() {
		return refusal(errProjectsToken.Message), nil
	}
	inner, err := tool.request(r, params.Arguments)
	if err != nil {
		return nil, &rpcError{Code: rpcInvalidParams, Message: err.Error()}
	}
	if s.handler == nil {
		return nil, &rpcError{Code: rpcInternalError, Message: "The router is not ready. Try again in a moment."}
	}
	rec := &capture{header: http.Header{}}
	s.handler.ServeHTTP(rec, inner)
	return rec.toolResult(), nil
}

// request builds the HTTP call a tool call stands for: path values into the
// route, query keys onto the address, and what is left as the body.
func (t mcpTool) request(outer *http.Request, args map[string]json.RawMessage) (*http.Request, error) {
	rest := make(map[string]json.RawMessage, len(args))
	for k, v := range args {
		rest[k] = v
	}
	path := t.Path
	for _, name := range t.pathParams {
		raw, ok := rest[name]
		if !ok {
			return nil, fmt.Errorf("The argument %s is required.", name)
		}
		delete(rest, name)
		path = strings.Replace(path, "{"+name+"}", url.PathEscape(scalar(raw)), 1)
	}
	query := url.Values{}
	// An assistant reads and writes Markdown, so a read that offers it answers in it unless the call says otherwise.
	if slices.ContainsFunc(t.query, func(q param) bool { return q.name == formatQuery.name }) {
		if _, said := rest[formatQuery.name]; !said {
			query.Set(formatQuery.name, string(requirement.FormatMarkdown))
		}
	}
	for _, q := range t.query {
		raw, ok := rest[q.name]
		if !ok {
			continue
		}
		delete(rest, q.name)
		var many []json.RawMessage
		if q.repeated && json.Unmarshal(raw, &many) == nil {
			for _, each := range many {
				query.Add(q.name, scalar(each))
			}
			continue
		}
		query.Set(q.name, scalar(raw))
	}
	var body []byte
	switch {
	case t.hasBody:
		encoded, err := json.Marshal(rest)
		if err != nil {
			return nil, errors.New("The arguments could not be encoded.")
		}
		body = encoded
	case len(rest) > 0:
		names := make([]string, 0, len(rest))
		for k := range rest {
			names = append(names, k)
		}
		return nil, fmt.Errorf("The tool does not take %s.", strings.Join(names, ", "))
	}

	return inProcess(outer, t.Method, path, query, body), nil
}

// inProcess is a call made in this process with the outer request's credential, as the caller would make it.
func inProcess(outer *http.Request, method, path string, query url.Values, body []byte) *http.Request {
	// A fresh route context, or chi answers the outer route's parameters.
	ctx := context.WithValue(outer.Context(), chi.RouteCtxKey, chi.NewRouteContext())
	inner := outer.Clone(ctx)
	inner.Method = method
	inner.URL = &url.URL{Path: APIPrefix + path, RawQuery: query.Encode()}
	inner.RequestURI = ""
	inner.Body = io.NopCloser(bytes.NewReader(body))
	inner.ContentLength = int64(len(body))
	inner.Header.Del("Content-Length")
	inner.Header.Set("Content-Type", "application/json")
	return inner
}

// scalar writes a JSON value the way it goes into a path or a query string.
func scalar(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return ""
	}
	return string(raw)
}

func refusal(message string) toolResult {
	return toolResult{Content: []toolContent{{Type: "text", Text: message}}, IsError: true}
}

// capture is the response of the inner call, held in memory to be answered as
// a tool result.
type capture struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (c *capture) Header() http.Header         { return c.header }
func (c *capture) Write(p []byte) (int, error) { return c.body.Write(p) }
func (c *capture) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func (c *capture) toolResult() toolResult {
	status := c.status
	if status == 0 {
		status = http.StatusOK
	}
	body := c.body.Bytes()
	if status >= http.StatusBadRequest {
		var envelope errorEnvelope
		if json.Unmarshal(body, &envelope) == nil && envelope.Error.Message != "" {
			return refusal(envelope.Error.Message)
		}
		return refusal(http.StatusText(status) + ".")
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return toolResult{Content: []toolContent{{Type: "text", Text: "Done."}}}
	}
	// A cut answer would not fit the tool's output schema, so one too large is refused whole.
	if len(body) > MCPMaxToolResultBytes {
		return refusal(tooLarge)
	}
	out := toolResult{Content: []toolContent{{Type: "text", Text: string(body)}}}
	var structured map[string]any
	if json.Unmarshal(body, &structured) == nil {
		out.StructuredContent = structured
	}
	return out
}

func writeRPC(w http.ResponseWriter, r *http.Request, id json.RawMessage, result any, rpcErr *rpcError) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	resp := rpcResponse{JSONRPC: "2.0", ID: id, Error: rpcErr}
	if rpcErr == nil {
		encoded, err := json.Marshal(result)
		if err != nil {
			resp.Error = &rpcError{Code: rpcInternalError, Message: "The result could not be written."}
		} else {
			resp.Result = encoded
		}
	}
	if session := r.Header.Get("Mcp-Session-Id"); session != "" {
		w.Header().Set("Mcp-Session-Id", session)
	}
	respondJSON(w, r, http.StatusOK, resp)
}
