package httpapi

import (
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/version"
)

// The API described in terms of the code that serves it. Every route in Routes has a row in operations, and a
// test refuses a router and a table that disagree; the document is then derived from the rows by reflection.

// env is a response envelope: the one or two keys a handler wraps its result
// in. A nil value is an untyped JSON value; a typed nil pointer is nullable.
type env map[string]any

// param is a query parameter.
type param struct {
	name, description string
	schema            *openapi.Schema
	repeated          bool
}

// operation is one row of the table.
type operation struct {
	method, path string
	handler      string
	// id names the operation for clients when the handler's name would not
	// be unique, as when one handler serves two paths.
	id      string
	summary string
	tag     string
	query   []param
	// request is the JSON body type the handler decodes.
	request any
	// responses maps a status to its envelope or bare type; nil is no body.
	responses map[int]any
	// public routes need no session.
	public      bool
	description string
	// pending routes are agreed but not built yet: they answer 501, and the
	// integration suite does not expect them covered. See docs/architecture.md.
	pending bool
	// tool names the operation for an assistant and toolHelp is the sentence
	// the model reads; empty means it is not offered.
	tool, toolHelp string
	// orgWide routes concern the organization as a whole, which a token
	// limited to projects is refused.
	orgWide bool
}

// operations is the table. Order is by area, then by path; paths are relative
// to APIPrefix except the probes, which live at the root.
var operations = []operation{
	{method: "GET", path: "/healthz", handler: "handleLiveness", tag: "health", summary: "Whether the process is running.", public: true,
		responses: ok(statusResponse{})},
	{method: "GET", path: "/readyz", handler: "handleReadiness", tag: "health", summary: "Whether the process can serve traffic, and how reads are routed.", public: true,
		responses: map[int]any{http.StatusOK: readinessResponse{}, http.StatusServiceUnavailable: statusResponse{}}},
	{method: "GET", path: "/openapi.json", handler: "handleOpenAPI", tag: "health", summary: "This document.", public: true,
		responses: ok(openapi.Document{})},
	{method: "GET", path: "/meta", handler: "handleMeta", tag: "health", summary: "Which product and build answers here.", public: true,
		tool: "server_info", toolHelp: "Which product and build of Exciter the assistant is talking to.",
		responses: ok(metaResponse{})},
}

// metaResponse names the product and its build, so a client connecting to an
// address can tell it reached Exciter and which release.
type metaResponse struct {
	Product string       `json:"product"`
	Build   version.Info `json:"build"`
}
