package httpapi

import (
	"net/http"
	"strconv"

	"github.com/cloudster1/exciter/backend/internal/audit"
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
	// public routes need no session; binary ones answer with a file.
	public      bool
	binary      bool
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

	// The audit log, as Armature's and Stator's administrators read theirs. Agreed here; served with #5.
	{method: "GET", path: "/audit", handler: "handleListAudit", orgWide: true, pending: true, tag: "audit",
		tool: "list_audit_log", toolHelp: "Who did what in the organization, newest first. For administrators.",
		summary: "Who did what to the organization, newest first; next is the cursor for the window after, null at the end. For administrators.",
		query:   append(auditQuery, keysetQuery(audit.DefaultLimit, audit.MaxLimit)...), responses: ok(env{"entries": []audit.AuditEntry{}, "next": (*string)(nil)})},
	{method: "GET", path: "/audit/facets", handler: "handleAuditFacets", orgWide: true, pending: true, tag: "audit",
		summary:   "The actions, people and kinds of target the log holds, to narrow it by, and how many days an entry is kept, 0 for ever. For administrators.",
		responses: ok(env{"facets": audit.AuditFacets{}})},
	{method: "GET", path: "/audit/export", handler: "handleExportAudit", orgWide: true, pending: true, tag: "audit",
		summary: "The log as a CSV file, newest first, narrowed like the list; the export is itself recorded. Refused with export_too_large past " + strconv.Itoa(audit.MaxExport) + " entries. For administrators.",
		binary:  true, query: auditQuery, responses: map[int]any{http.StatusOK: nil, http.StatusUnprocessableEntity: errorEnvelope{}}},
}

var intParam = &openapi.Schema{Type: "integer"}

// keysetQuery is a window of a list read newest first: how many, and where the window before it ended.
func keysetQuery(def, max int) []param {
	return []param{
		{name: "limit", schema: intParam, description: "1 to " + strconv.Itoa(max) + "; " + strconv.Itoa(def) + " when absent."},
		{name: "cursor", description: "The next of the window before; the first window when absent."},
	}
}

// auditQuery narrows the audit log; every part is optional.
var auditQuery = []param{
	{name: "action", schema: &openapi.Schema{Type: "string", Enum: audit.Actions}, description: "One action."},
	{name: "actor", schema: &openapi.Schema{Type: "string", Format: "uuid"}, description: "The person who acted."},
	{name: "targetType", description: "What kind of thing the entries are about, such as requirement or user."},
	{name: "target", schema: &openapi.Schema{Type: "string", Format: "uuid"}, description: "The id of the thing the entries are about."},
	{name: "from", description: "The first day, YYYY-MM-DD in UTC, or the first instant with its zone."},
	{name: "to", description: "The last day, YYYY-MM-DD in UTC and inclusive, or the instant the range ends before."},
}

// metaResponse names the product and its build, so a client connecting to an
// address can tell it reached Exciter and which release.
type metaResponse struct {
	Product string       `json:"product"`
	Build   version.Info `json:"build"`
}
