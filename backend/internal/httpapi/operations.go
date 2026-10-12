package httpapi

import (
	"net/http"
	"strconv"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/oidc"
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
	// public routes need no session; binary ones answer with a file, and
	// redirect ones send the browser on with a Location header.
	public      bool
	binary      bool
	redirect    bool
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

	// Signing in with a password or through the organization's identity provider, and the session's own settings.
	{method: "POST", path: "/auth/login", handler: "handleLogin", tag: "auth", summary: "Sign in a local account with email and password, and set the session cookie.", public: true,
		request: loginRequest{}, responses: ok(meResponse{})},
	{method: "GET", path: "/auth/oidc/{orgSlug}/start", handler: "handleOIDCStart", tag: "auth", summary: "Begin signing in through the organization's identity provider.", public: true, redirect: true,
		query: []param{{name: "next", description: "A path of this application to land on afterwards."}}, responses: map[int]any{}},
	{method: "GET", path: "/auth/oidc/callback", handler: "handleOIDCCallback", tag: "auth", summary: "Where the identity provider sends the browser back; sets the session cookie.", public: true, redirect: true,
		query: []param{{name: "state"}, {name: "code"}, {name: "error"}}, responses: map[int]any{}},
	{method: "POST", path: "/auth/logout", handler: "handleLogout", tag: "auth", summary: "End the session.", responses: none()},
	{method: "GET", path: "/auth/me", handler: "handleMe", tag: "auth", summary: "Who is signed in, and the organizations they may act in.",
		tool: "whoami", toolHelp: "Who the caller is and which organization they act in.", responses: ok(meResponse{})},
	{method: "PATCH", path: "/auth/me", handler: "handleUpdateMe", tag: "auth", summary: "Change the caller's own settings, such as the interface language.",
		request: updateMeRequest{}, responses: ok(meResponse{})},
	{method: "POST", path: "/auth/switch-org", handler: "handleSwitchOrg", tag: "auth", summary: "Act in another of the caller's organizations from now on.",
		request: switchOrgRequest{}, responses: ok(env{"organization": auth.CurrentOrg{}})},

	// The organization's people, for its administrators.
	{method: "GET", path: "/users", handler: "handleListMembers", orgWide: true, tag: "members",
		summary:   "The organization's members, owners first, with whether the identity provider's groups decide their roles. For administrators.",
		responses: ok(env{"members": []auth.Member{}})},
	{method: "POST", path: "/users", handler: "handleCreateMember", orgWide: true, tag: "members",
		summary: "Add a person with a password, made for them when none is given and shown once in the answer. For administrators.",
		request: createMemberRequest{}, responses: map[int]any{http.StatusCreated: env{"member": auth.CreatedMember{}}}},
	{method: "PATCH", path: "/users/{userID}", handler: "handleSetMemberRole", orgWide: true, tag: "members",
		summary: "Give a member another role; the owner's and the caller's own stay. For administrators.",
		request: setRoleRequest{}, responses: ok(env{"member": auth.Member{}})},
	{method: "DELETE", path: "/users/{userID}", handler: "handleRemoveMember", orgWide: true, tag: "members",
		summary: "Take a member out of the organization; their sessions stop reaching it at once. For administrators.", responses: none()},
	{method: "GET", path: "/users/requests", handler: "handleListJoinRequests", orgWide: true, tag: "members",
		summary:   "Who signed in through the identity provider and is waiting to be let in. For administrators.",
		responses: ok(env{"requests": []auth.JoinRequest{}})},
	{method: "POST", path: "/users/requests/{userID}/admit", handler: "handleAdmitJoinRequest", orgWide: true, tag: "members",
		summary: "Let a waiting person in with the standing given. For administrators.",
		request: admitRequest{}, responses: ok(env{"membership": auth.Membership{}})},
	{method: "DELETE", path: "/users/requests/{userID}", handler: "handleDeclineJoinRequest", orgWide: true, tag: "members",
		summary: "Turn a waiting person away; they may ask again. For administrators.", responses: none()},

	// The organization's identity provider and the roles its groups grant, for its administrators.
	{method: "GET", path: "/oidc-provider", handler: "handleGetOIDCProvider", orgWide: true, tag: "sso",
		summary:   "The organization's identity provider, if one is configured, and the callback address to register with it. For administrators.",
		responses: ok(providerView{})},
	{method: "PUT", path: "/oidc-provider", handler: "handleSaveOIDCProvider", orgWide: true, tag: "sso",
		summary: "Configure the organization's identity provider; a client secret left out keeps the stored one. For administrators.",
		request: saveOIDCProviderRequest{}, responses: ok(providerView{})},
	{method: "GET", path: "/oidc-provider/group-roles", handler: "handleListGroupRoles", orgWide: true, tag: "sso",
		summary: "Which provider groups grant which role. For administrators.", responses: ok(env{"groupRoles": []oidc.GroupRole{}})},
	{method: "POST", path: "/oidc-provider/group-roles", handler: "handleSetGroupRole", orgWide: true, tag: "sso",
		summary: "Map a provider group to a role, or change the role it maps to; members follow at their next sign-in. For administrators.",
		request: setGroupRoleRequest{}, responses: ok(env{"groupRole": oidc.GroupRole{}})},
	{method: "DELETE", path: "/oidc-provider/group-roles/{groupRoleID}", handler: "handleRemoveGroupRole", orgWide: true, tag: "sso",
		summary: "Unmap a provider group; the roles it granted go at each person's next sign-in. For administrators.", responses: none()},

	// The audit log, as Armature's and Stator's administrators read theirs.
	{method: "GET", path: "/audit", handler: "handleListAudit", orgWide: true, tag: "audit",
		tool: "list_audit_log", toolHelp: "Who did what in the organization, newest first. For administrators.",
		summary: "Who did what to the organization, newest first; next is the cursor for the window after, null at the end. For administrators.",
		query:   append(auditQuery, keysetQuery(audit.DefaultLimit, audit.MaxLimit)...), responses: ok(env{"entries": []audit.AuditEntry{}, "next": (*string)(nil)})},
	{method: "GET", path: "/audit/facets", handler: "handleAuditFacets", orgWide: true, tag: "audit",
		summary:   "The actions, people and kinds of target the log holds, to narrow it by, and how many days an entry is kept, 0 for ever. For administrators.",
		responses: ok(env{"facets": audit.AuditFacets{}})},
	{method: "GET", path: "/audit/export", handler: "handleExportAudit", orgWide: true, tag: "audit",
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
