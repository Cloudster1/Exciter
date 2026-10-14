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
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// MCP over one endpoint: a tool call is the HTTP call the browser would make,
// run in this process as the caller, so nothing is stored between requests.
const (
	MCPProtocolVersion = "2025-06-18"
	MCPServerName      = "exciter"
	mcpServerVersion   = "1"
	// MCPMaxToolResultBytes cuts a tool result a model would drown in; the
	// text says how to ask for less.
	MCPMaxToolResultBytes = 64 << 10
	mcpPath               = APIPrefix + "/mcp"
	mcpOpenAPIResource    = "exciter://openapi.json"
)

const (
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

var truncatedNote = []byte("\n... The result was cut here. Narrow the query, or page with limit and offset.")

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
		return map[string]any{
			"protocolVersion": MCPProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}, "resources": map[string]any{}},
			"serverInfo":      map[string]any{"name": MCPServerName, "version": mcpServerVersion},
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
		return map[string]any{"resources": []map[string]any{{
			"uri": mcpOpenAPIResource, "name": "openapi.json", "mimeType": "application/json",
			"description": "The whole HTTP API, of which the tools are a part.",
		}}}, nil
	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || params.URI != mcpOpenAPIResource {
			return nil, &rpcError{Code: rpcInvalidParams, Message: "The one resource is " + mcpOpenAPIResource + "."}
		}
		text, err := Spec().MarshalIndent()
		if err != nil {
			return nil, &rpcError{Code: rpcInternalError, Message: "The document could not be written."}
		}
		return map[string]any{"contents": []map[string]any{{"uri": mcpOpenAPIResource, "mimeType": "application/json", "text": string(text)}}}, nil
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
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.Input,
			"annotations": map[string]any{"title": t.Name, "readOnlyHint": t.ReadOnly},
		})
	}
	return out
}

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
	contentType := "application/json"
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

	// A fresh route context, or chi answers the outer route's parameters.
	ctx := context.WithValue(outer.Context(), chi.RouteCtxKey, chi.NewRouteContext())
	inner := outer.Clone(ctx)
	inner.Method = t.Method
	inner.URL = &url.URL{Path: APIPrefix + path, RawQuery: query.Encode()}
	inner.RequestURI = ""
	inner.Body = io.NopCloser(bytes.NewReader(body))
	inner.ContentLength = int64(len(body))
	inner.Header.Del("Content-Length")
	inner.Header.Set("Content-Type", contentType)
	return inner, nil
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
	out := toolResult{}
	var structured map[string]any
	if json.Unmarshal(body, &structured) == nil {
		out.StructuredContent = structured
	}
	if len(body) > MCPMaxToolResultBytes {
		body = append(body[:MCPMaxToolResultBytes:MCPMaxToolResultBytes], truncatedNote...)
		out.StructuredContent = nil
	}
	out.Content = []toolContent{{Type: "text", Text: string(body)}}
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
