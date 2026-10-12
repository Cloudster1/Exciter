package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/attachment"
	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/label"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/perm"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/theme"
	"github.com/cloudster1/exciter/backend/internal/trace"
	"github.com/cloudster1/exciter/backend/internal/version"
	"github.com/cloudster1/exciter/backend/internal/workflow"
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
	// request is the JSON body type the handler decodes; multipart says the body is a form with a part named file.
	request   any
	multipart bool
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

	// Themes, as Armature and Stator serve them, so a theme file of either imports here.
	{method: "GET", path: "/themes", handler: "handleListThemes", tag: "themes", summary: "Themes the caller may use: theirs, then the shared ones.",
		responses: ok(env{"themes": []theme.Theme{}})},
	{method: "POST", path: "/themes", handler: "handleCreateTheme", tag: "themes", summary: "Make a theme.",
		request: theme.Input{}, responses: created(env{"theme": theme.Theme{}})},
	{method: "GET", path: "/themes/examples", handler: "handleThemeExamples", tag: "themes", summary: "The themes shipped with the product, to start a theme from.",
		responses: ok(env{"examples": []theme.Example{}})},
	{method: "GET", path: "/themes/active", handler: "handleActiveTheme", tag: "themes",
		summary:   "The theme the caller sees: chosen, the organization's default, or null for the built-in one; source says which.",
		responses: ok(env{"theme": (*theme.Theme)(nil), "source": theme.Source("")})},
	{method: "PUT", path: "/themes/active", handler: "handleChooseTheme", tag: "themes",
		summary: "Use a theme; null returns to the organization's default, null with builtIn keeps the built-in one.",
		request: chooseThemeRequest{}, responses: ok(env{"theme": (*theme.Theme)(nil)})},
	{method: "GET", path: "/themes/default", handler: "handleDefaultTheme", tag: "themes",
		summary:   "The organization's default theme, whatever the caller chose for themselves, or null for the built-in one.",
		responses: ok(env{"theme": (*theme.Theme)(nil)})},
	{method: "PUT", path: "/themes/default", handler: "handleSetDefaultTheme", orgWide: true, tag: "themes",
		summary: "Name the shared theme everybody sees until they choose, or null for the built-in one. For administrators.",
		request: defaultThemeRequest{}, responses: ok(env{"theme": (*theme.Theme)(nil)})},
	{method: "POST", path: "/themes/import", handler: "handleImportTheme", tag: "themes",
		summary:   "Make a theme of the caller's own from an exported theme file, sent as a multipart part named file.",
		multipart: true, responses: created(env{"theme": theme.Theme{}})},
	{method: "GET", path: "/themes/{themeID}/export", handler: "handleExportTheme", tag: "themes",
		summary: "The theme as one file in the " + theme.PackageFormat + " format, its pictures and fonts inside, as a download.", binary: true, responses: ok(nil)},
	{method: "GET", path: "/themes/{themeID}", handler: "handleGetTheme", tag: "themes", summary: "One theme.", responses: ok(env{"theme": theme.Theme{}})},
	{method: "PATCH", path: "/themes/{themeID}", handler: "handleUpdateTheme", tag: "themes", summary: "Change a theme; the owner's to do, or an administrator's once shared.",
		request: theme.Input{}, responses: ok(env{"theme": theme.Theme{}})},
	{method: "DELETE", path: "/themes/{themeID}", handler: "handleDeleteTheme", tag: "themes", summary: "Delete a theme; everybody using it returns to the built-in one.",
		responses: none()},
	{method: "POST", path: "/themes/{themeID}/assets", handler: "handleUploadThemeAsset", tag: "themes",
		summary:   "Put a picture or a font on a theme, as a multipart part named file.",
		multipart: true, responses: created(env{"asset": theme.Asset{}})},
	{method: "GET", path: "/themes/{themeID}/assets/{assetID}", handler: "handleThemeAsset", tag: "themes", summary: "The bytes of a theme's file, as a download.",
		binary: true, responses: ok(nil)},
	{method: "DELETE", path: "/themes/{themeID}/assets/{assetID}", handler: "handleDeleteThemeAsset", tag: "themes", summary: "Take a file off a theme it no longer uses.",
		responses: none()},

	// M1, the requirements core: agreed in docs/api-contract-m1.md, answering 501 until each is built.

	// Projects (#11) and the templates they start from (#24).
	{method: "GET", path: "/projects", handler: "handleListProjects", tag: "projects",
		tool: "list_projects", toolHelp: "The projects the caller may view, with the keys other tools take; archived true lists archived ones too.",
		summary:   "Every project the caller may view, by name; archived ones only when asked for.",
		query:     []param{{name: "archived", schema: &openapi.Schema{Type: "boolean"}, description: "true to list archived projects too; false when absent."}},
		responses: ok(env{"projects": []project.Project{}})},
	{method: "POST", path: "/projects", handler: "handleCreateProject", orgWide: true, tag: "projects",
		summary: "Make a project, blank or from a template; the caller administers it. For owners and administrators.",
		request: project.CreateInput{}, responses: map[int]any{http.StatusCreated: env{"project": project.Project{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/key-check", handler: "handleCheckProjectKey", tag: "projects",
		summary:   "Whether a key is free, or a free key suggested for a name.",
		query:     []param{{name: "key", description: "A key to check."}, {name: "name", description: "A project name to suggest a key for."}},
		responses: ok(env{"check": project.KeyCheck{}})},
	{method: "GET", path: "/project-templates", handler: "handleListProjectTemplates", tag: "projects", pending: true,
		summary:   "The structures a new project can start from. Send one's key as template with POST /projects.",
		responses: ok(env{"templates": []project.Template{}})},
	{method: "GET", path: "/projects/{projectKey}", handler: "handleGetProject", tag: "projects",
		tool: "get_project", toolHelp: "One project by its key, with its settings and what the caller may do in it.",
		summary:   "One project by its key or a key it had, and what the caller may do in it.",
		responses: ok(env{"project": project.Project{}})},
	{method: "PATCH", path: "/projects/{projectKey}", handler: "handleUpdateProject", tag: "projects",
		summary: "Rename or describe a project, or change its settings. For the project's administrators.",
		request: project.UpdateInput{}, responses: ok(env{"project": project.Project{}})},
	{method: "DELETE", path: "/projects/{projectKey}", handler: "handleDeleteProject", tag: "projects",
		summary:   "Delete an archived project and everything in it for good. For the project's administrators.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusConflict: errorEnvelope{}}},
	{method: "PUT", path: "/projects/{projectKey}/key", handler: "handleChangeProjectKey", tag: "projects",
		summary: "Give a project a new key; its old key and every requirement key under it keep resolving. For the project's administrators.",
		request: project.KeyInput{}, responses: map[int]any{http.StatusOK: env{"project": project.Project{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PUT", path: "/projects/{projectKey}/archive", handler: "handleArchiveProject", tag: "projects",
		summary:   "Archive a project: it stays readable and nothing in it changes. Archiving it again is no change. For the project's administrators.",
		responses: ok(env{"project": project.Project{}})},
	{method: "DELETE", path: "/projects/{projectKey}/archive", handler: "handleUnarchiveProject", tag: "projects",
		summary:   "Unarchive a project; one that is not archived is no change. For the project's administrators.",
		responses: ok(env{"project": project.Project{}})},

	// Permissions (#12).
	{method: "GET", path: "/access/me", handler: "handleMyAccess", tag: "permissions", pending: true,
		summary:   "What the caller may do across the organization, which decides which buttons to draw.",
		responses: ok(env{"can": perm.OrgCan{}})},
	{method: "GET", path: "/people", handler: "handleListPeople", tag: "permissions", pending: true,
		summary: "Members of the organization, to pick whom to grant something or who owns a requirement.",
		query:   pickerQuery, responses: ok(env{"people": []perm.Person{}})},
	{method: "GET", path: "/groups", handler: "handleListGroups", tag: "permissions", pending: true,
		summary: "Groups of the organization, to pick whom to grant something.",
		query:   pickerQuery, responses: ok(env{"groups": []perm.Group{}})},
	{method: "GET", path: "/projects/{projectKey}/grants", handler: "handleListProjectGrants", tag: "permissions", pending: true,
		summary:   "Who holds which role in a project. For the project's administrators.",
		responses: ok(env{"grants": []perm.Grant{}})},
	{method: "PUT", path: "/projects/{projectKey}/grants", handler: "handleSetProjectGrants", tag: "permissions", pending: true,
		summary: "Replace a project's whole table of grants; refused when it would leave the project without an administrator. For the project's administrators.",
		request: perm.GrantsInput{}, responses: map[int]any{http.StatusOK: env{"grants": []perm.Grant{}}, http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/access/{userID}", handler: "handleInspectProjectAccess", tag: "permissions", pending: true,
		summary:   "What a person may do in a project and which grant decides each right, as the database answers it. For the project's administrators.",
		responses: ok(env{"access": perm.AccessReport{}})},

	// Requirements, their trash (#13) and their place in the tree (#15).
	{method: "POST", path: "/projects/{projectKey}/requirements", handler: "handleCreateRequirement", tag: "requirements", pending: true,
		summary: "Write a requirement or a section as revision 1, in the status the workflow starts in.",
		request: requirement.CreateInput{}, responses: map[int]any{http.StatusCreated: env{"requirement": requirement.Requirement{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/requirements/by-id/{requirementID}", handler: "handleGetRequirementByID", tag: "requirements", pending: true,
		summary:   "One requirement by its id, which never changes, for permalinks.",
		responses: ok(env{"requirement": requirement.Requirement{}})},
	{method: "GET", path: "/requirements/{reqKey}", handler: "handleGetRequirement", tag: "requirements", pending: true,
		summary:   "One requirement by its key or a key it had; the answer carries its current key.",
		responses: ok(env{"requirement": requirement.Requirement{}})},
	{method: "PATCH", path: "/requirements/{reqKey}", handler: "handleUpdateRequirement", tag: "requirements", pending: true,
		summary: "Change a requirement; a change to its normative content writes the next revision over baseRevision, else 409 revision_conflict. The status moves through transitions.",
		request: requirement.UpdateInput{}, responses: map[int]any{http.StatusOK: env{"requirement": requirement.Requirement{}}, http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/requirements/{reqKey}", handler: "handleTrashRequirement", tag: "requirements", pending: true,
		summary:   "Move a requirement and everything below it to its project's trash.",
		responses: none()},
	{method: "GET", path: "/projects/{projectKey}/trash", handler: "handleListRequirementTrash", tag: "requirements", pending: true,
		summary:   "The project's trash, the latest first.",
		responses: ok(env{"items": []requirement.TrashItem{}})},
	{method: "DELETE", path: "/projects/{projectKey}/trash", handler: "handleEmptyRequirementTrash", tag: "requirements", pending: true,
		summary:   "Delete everything in the project's trash for good. For the project's administrators.",
		responses: none()},
	{method: "POST", path: "/projects/{projectKey}/trash/{requirementID}/restore", handler: "handleRestoreRequirement", tag: "requirements", pending: true,
		summary:   "Put a trashed requirement back where it was, or at the top when its parent is gone.",
		responses: ok(env{"requirement": requirement.Requirement{}})},
	{method: "DELETE", path: "/projects/{projectKey}/trash/{requirementID}", handler: "handlePurgeRequirement", tag: "requirements", pending: true,
		summary:   "Delete a trashed requirement, what went with it and their revisions for good. For the project's administrators.",
		responses: none()},
	{method: "GET", path: "/projects/{projectKey}/tree", handler: "handleGetTree", tag: "tree", pending: true,
		summary:   "Every requirement and section of a project, out of the trash, in reading order with outline numbers.",
		responses: ok(env{"nodes": []requirement.TreeNode{}})},
	{method: "GET", path: "/projects/{projectKey}/spec", handler: "handleGetSpecification", tag: "tree", pending: true,
		summary: "The project read as one numbered document: a window of the tree in reading order, with content.",
		query:   windowQuery(requirement.DefaultLimit, requirement.MaxSpecLimit), responses: ok(env{"entries": []requirement.SpecEntry{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "POST", path: "/requirements/{reqKey}/move", handler: "handleMoveRequirement", tag: "tree", pending: true,
		summary: "Put a requirement, with everything below it, elsewhere in its project or in another, which gives new keys.",
		request: requirement.MoveInput{}, responses: map[int]any{http.StatusOK: env{"requirement": requirement.Requirement{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "POST", path: "/requirements/{reqKey}/copy", handler: "handleCopyRequirement", tag: "tree", pending: true,
		summary: "Copy a requirement, and what is below it when asked, as new requirements at their current revision.",
		request: requirement.CopyInput{}, responses: map[int]any{http.StatusCreated: env{"requirement": requirement.Requirement{}, "copied": 0}, http.StatusUnprocessableEntity: errorEnvelope{}}},

	// Types and attributes (#14).
	{method: "GET", path: "/attribute-kinds", handler: "handleListAttributeKinds", tag: "types", pending: true,
		summary:   "What an attribute can be, for the form that defines one.",
		responses: ok(env{"kinds": []attribute.KindInfo{}})},
	{method: "GET", path: "/projects/{projectKey}/types", handler: "handleListRequirementTypes", tag: "types", pending: true,
		summary:   "A project's requirement types in order, retired ones included.",
		responses: ok(env{"types": []attribute.Type{}})},
	{method: "POST", path: "/projects/{projectKey}/types", handler: "handleCreateRequirementType", tag: "types", pending: true,
		summary: "Make a requirement type. For the project's administrators.",
		request: attribute.TypeInput{}, responses: map[int]any{http.StatusCreated: env{"type": attribute.Type{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PATCH", path: "/projects/{projectKey}/types/{typeID}", handler: "handleUpdateRequirementType", tag: "types", pending: true,
		summary: "Change, move or retire a requirement type. For the project's administrators.",
		request: attribute.TypeInput{}, responses: ok(env{"type": attribute.Type{}})},
	{method: "DELETE", path: "/projects/{projectKey}/types/{typeID}", handler: "handleDeleteRequirementType", tag: "types", pending: true,
		summary:   "Delete a requirement type no requirement or revision ever had; retire one that was used. For the project's administrators.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/attributes", handler: "handleListAttributes", tag: "types", pending: true,
		summary:   "A project's attributes in order, retired ones included.",
		responses: ok(env{"attributes": []attribute.Def{}})},
	{method: "POST", path: "/projects/{projectKey}/attributes", handler: "handleCreateAttribute", tag: "types", pending: true,
		summary: "Define an attribute. For the project's administrators.",
		request: attribute.DefInput{}, responses: map[int]any{http.StatusCreated: env{"attribute": attribute.Def{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PATCH", path: "/projects/{projectKey}/attributes/{attributeID}", handler: "handleUpdateAttribute", tag: "types", pending: true,
		summary: "Change an attribute's name, options, types, order or whether it is required or retired; never its kind. For the project's administrators.",
		request: attribute.DefInput{}, responses: map[int]any{http.StatusOK: env{"attribute": attribute.Def{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/projects/{projectKey}/attributes/{attributeID}", handler: "handleDeleteAttribute", tag: "types", pending: true,
		summary:   "Delete an attribute no requirement or revision ever had a value for; retire one that was used. For the project's administrators.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusConflict: errorEnvelope{}}},

	// History (#16).
	{method: "GET", path: "/requirements/{reqKey}/revisions", handler: "handleListRevisions", tag: "history", pending: true,
		summary: "A requirement's revisions, the latest first.",
		query:   windowQuery(requirement.DefaultLimit, requirement.MaxLimit), responses: ok(env{"revisions": []requirement.RevisionEntry{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "GET", path: "/requirements/{reqKey}/revisions/{revisionNumber}", handler: "handleGetRevision", tag: "history", pending: true,
		summary:   "One revision with its normative content, as it was.",
		responses: ok(env{"revision": requirement.Revision{}})},
	{method: "POST", path: "/requirements/{reqKey}/revisions/{revisionNumber}/restore", handler: "handleRestoreRevision", tag: "history", pending: true,
		summary: "Write an older revision's content again as the next revision, which records the one it restored.",
		request: requirement.RestoreInput{}, responses: map[int]any{http.StatusOK: env{"requirement": requirement.Requirement{}, "revision": requirement.RevisionEntry{}}, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/requirements/{reqKey}/diff", handler: "handleDiffRevisions", tag: "history", pending: true,
		summary: "What changed between two revisions of a requirement: statement, rationale, type and attributes.",
		query: []param{
			{name: "from", schema: intParam, description: "A revision number, 0 for the empty requirement; the one before to when absent."},
			{name: "to", schema: intParam, description: "A revision number; the latest when absent."},
		}, responses: ok(env{"comparison": requirement.Comparison{}})},
	{method: "GET", path: "/requirements/{reqKey}/activity", handler: "handleListActivity", tag: "history", pending: true,
		summary: "Revisions and logged changes of a requirement in one list, newest first; next is the cursor for the window after, null at the end.",
		query:   keysetQuery(requirement.DefaultLimit, requirement.MaxLimit), responses: ok(env{"entries": []requirement.ActivityEntry{}, "next": (*string)(nil)})},

	// Statuses and workflow (#17).
	{method: "GET", path: "/projects/{projectKey}/statuses", handler: "handleListStatuses", tag: "workflow", pending: true,
		summary:   "A project's statuses in order, with how many requirements are in each.",
		responses: ok(env{"statuses": []workflow.Status{}})},
	{method: "POST", path: "/projects/{projectKey}/statuses", handler: "handleCreateStatus", tag: "workflow", pending: true,
		summary: "Make a status for the project's workflows to use. For the project's administrators.",
		request: workflow.StatusInput{}, responses: map[int]any{http.StatusCreated: env{"status": workflow.Status{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PATCH", path: "/projects/{projectKey}/statuses/{statusID}", handler: "handleUpdateStatus", tag: "workflow", pending: true,
		summary: "Rename, describe, move or recategorize a status. For the project's administrators.",
		request: workflow.StatusInput{}, responses: ok(env{"status": workflow.Status{}})},
	{method: "DELETE", path: "/projects/{projectKey}/statuses/{statusID}", handler: "handleDeleteStatus", tag: "workflow", pending: true,
		summary:   "Delete a status no workflow and no requirement uses. For the project's administrators.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/workflows", handler: "handleListWorkflows", tag: "workflow", pending: true,
		summary:   "A project's workflows, and which the project and each type follow.",
		responses: ok(env{"workflows": []workflow.Workflow{}})},
	{method: "POST", path: "/projects/{projectKey}/workflows", handler: "handleCreateWorkflow", tag: "workflow", pending: true,
		summary: "Make a workflow from the designer's graph. For the project's administrators.",
		request: workflow.GraphInput{}, responses: map[int]any{http.StatusCreated: env{"workflow": workflow.Workflow{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PUT", path: "/projects/{projectKey}/workflow", handler: "handleAssignWorkflow", tag: "workflow", pending: true,
		summary: "Make a workflow the project's or a type's, moving requirements whose status it lacks as the mapping says. For the project's administrators.",
		request: workflow.AssignInput{}, responses: map[int]any{http.StatusOK: env{"workflow": workflow.Workflow{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/workflows/{workflowID}", handler: "handleGetWorkflow", tag: "workflow", pending: true,
		summary:   "One workflow with its steps, transitions and rules.",
		responses: ok(env{"workflow": workflow.Workflow{}})},
	{method: "PUT", path: "/projects/{projectKey}/workflows/{workflowID}", handler: "handleSaveWorkflow", tag: "workflow", pending: true,
		summary: "Save a workflow's whole graph; transitions keep their ids, and a status it drops must not be in use under it. For the project's administrators.",
		request: workflow.GraphInput{}, responses: map[int]any{http.StatusOK: env{"workflow": workflow.Workflow{}}, http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/projects/{projectKey}/workflows/{workflowID}", handler: "handleDeleteWorkflow", tag: "workflow", pending: true,
		summary:   "Delete a workflow neither the project nor a type follows. For the project's administrators.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/workflow-rule-types", handler: "handleListRuleTypes", tag: "workflow", pending: true,
		summary:   "The conditions, validators and post-functions a transition can carry, with their settings.",
		responses: ok(env{"ruleTypes": []workflow.RuleType{}})},
	{method: "GET", path: "/requirements/{reqKey}/transitions", handler: "handleListTransitions", tag: "workflow", pending: true,
		summary:   "The transitions the caller may take on a requirement now.",
		responses: ok(env{"transitions": []workflow.Available{}})},
	{method: "POST", path: "/requirements/{reqKey}/transitions", handler: "handleTransitionRequirement", tag: "workflow", pending: true,
		summary: "Take a transition; a validator that refuses it answers 422 with its sentence.",
		request: workflow.TransitionRequest{}, responses: map[int]any{http.StatusOK: env{"requirement": requirement.Requirement{}}, http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},

	// Trace links (#18).
	{method: "GET", path: "/link-types", handler: "handleListOrgLinkTypes", tag: "links", pending: true,
		summary:   "The organization's link types.",
		responses: ok(env{"linkTypes": []trace.LinkType{}})},
	{method: "POST", path: "/link-types", handler: "handleCreateOrgLinkType", orgWide: true, tag: "links", pending: true,
		summary: "Make a link type for every project. For administrators.",
		request: trace.LinkTypeInput{}, responses: map[int]any{http.StatusCreated: env{"linkType": trace.LinkType{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PATCH", path: "/link-types/{linkTypeID}", handler: "handleUpdateLinkType", tag: "links", pending: true,
		summary: "Change or retire a link type: the organization's for administrators, a project's own for its administrators.",
		request: trace.LinkTypeInput{}, responses: map[int]any{http.StatusOK: env{"linkType": trace.LinkType{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/link-types/{linkTypeID}", handler: "handleDeleteLinkType", tag: "links", pending: true,
		summary:   "Delete a link type no link ever had; retire one that was used.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/link-types", handler: "handleListProjectLinkTypes", tag: "links", pending: true,
		summary:   "The link types a project may use, the organization's and its own, with whether each is enabled there and its rules.",
		responses: ok(env{"linkTypes": []trace.LinkType{}})},
	{method: "POST", path: "/projects/{projectKey}/link-types", handler: "handleCreateProjectLinkType", tag: "links", pending: true,
		summary: "Make a link type of the project's own. For the project's administrators.",
		request: trace.LinkTypeInput{}, responses: map[int]any{http.StatusCreated: env{"linkType": trace.LinkType{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PUT", path: "/projects/{projectKey}/link-types/{linkTypeID}", handler: "handleSetProjectLinkType", tag: "links", pending: true,
		summary: "Enable or disable a link type in a project, and which requirement types it may join. For the project's administrators.",
		request: trace.ProjectLinkTypeInput{}, responses: map[int]any{http.StatusOK: env{"linkType": trace.LinkType{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/requirements/{reqKey}/links", handler: "handleListLinks", tag: "links", pending: true,
		summary:   "A requirement's links in both directions, to requirements the caller may view.",
		responses: ok(env{"links": []trace.Link{}})},
	{method: "POST", path: "/requirements/{reqKey}/links", handler: "handleCreateLink", tag: "links", pending: true,
		summary: "Link a requirement to another, recording the revisions of both; a self-link, a repeat or a cycle of an acyclic type is refused.",
		request: trace.LinkInput{}, responses: map[int]any{http.StatusCreated: env{"link": trace.Link{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/links/{linkID}", handler: "handleDeleteLink", tag: "links", pending: true,
		summary:   "Remove a link, from either end; it is kept as a tombstone for baselines and history.",
		responses: none()},

	// Labels (#19).
	{method: "GET", path: "/requirements/{reqKey}/labels", handler: "handleListRequirementLabels", tag: "labels", pending: true,
		summary:   "A requirement's labels, by name.",
		responses: ok(env{"labels": []string{}})},
	{method: "POST", path: "/requirements/{reqKey}/labels", handler: "handleAddRequirementLabel", tag: "labels", pending: true,
		summary: "Put a label on a requirement, coining it if nobody uses it yet; logged, never a revision.",
		request: label.Input{}, responses: map[int]any{http.StatusOK: env{"labels": []string{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/requirements/{reqKey}/labels/{labelName}", handler: "handleRemoveRequirementLabel", tag: "labels", pending: true,
		summary:   "Take a label off a requirement; one it does not carry is no change.",
		responses: none()},
	{method: "GET", path: "/labels", handler: "handleListLabels", tag: "labels", pending: true,
		summary: "Labels in use on requirements the caller may view, starting with what was typed, the most used first.",
		query: []param{
			{name: "q", description: "The start of the label."},
			{name: "project", description: "A project key to stay inside."},
			{name: "limit", schema: intParam, description: "1 to 50; 10 when absent."},
		}, responses: ok(env{"labels": []label.Suggestion{}})},
	{method: "GET", path: "/labels/{labelName}/requirements", handler: "handleListLabelledRequirements", tag: "labels", pending: true,
		summary:   "Requirements the caller may view that carry a label, by key.",
		query:     append([]param{{name: "project", description: "A project key to stay inside."}}, windowQuery(label.DefaultLimit, label.MaxLimit)...),
		responses: ok(env{"requirements": []label.Labelled{}, "total": 0, "limit": 0, "offset": 0})},

	// Search, the grid and saved views (#20).
	{method: "GET", path: "/search", handler: "handleSearch", tag: "search", pending: true,
		summary: "Requirements whose key, title, statement or rationale match, among what the caller may view.",
		query:   searchQuery, responses: map[int]any{http.StatusOK: env{"hits": []search.Hit{}, "total": 0, "limit": 0, "offset": 0}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/search/quick", handler: "handleQuickSearch", tag: "search", pending: true,
		summary: "Requirements whose key or title start with what was typed, for the command palette.",
		query: []param{
			{name: "q", description: "The words or the key typed so far."},
			{name: "project", description: "A project key to stay inside."},
			{name: "limit", schema: intParam, description: "1 to 20; 8 when absent."},
		}, responses: ok(env{"requirements": []search.QuickHit{}})},
	{method: "GET", path: "/projects/{projectKey}/requirements", handler: "handleListGrid", tag: "search", pending: true,
		summary:   "A window of a project's requirements as the grid shows them, narrowed and ordered.",
		query:     append(gridQuery, windowQuery(search.DefaultLimit, search.MaxGridLimit)...),
		responses: map[int]any{http.StatusOK: env{"rows": []search.Row{}, "total": 0, "limit": 0, "offset": 0}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/views", handler: "handleListViews", tag: "search", pending: true,
		summary:   "The caller's own views of a project's grid, then the shared ones.",
		responses: ok(env{"views": []search.View{}})},
	{method: "POST", path: "/projects/{projectKey}/views", handler: "handleCreateView", tag: "search", pending: true,
		summary: "Save a view of a project's grid; sharing it is for its editors.",
		request: search.ViewInput{}, responses: map[int]any{http.StatusCreated: env{"view": search.View{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PATCH", path: "/views/{viewID}", handler: "handleUpdateView", tag: "search", pending: true,
		summary: "Change a view: the owner's to do, or a project administrator's once shared.",
		request: search.ViewInput{}, responses: ok(env{"view": search.View{}})},
	{method: "DELETE", path: "/views/{viewID}", handler: "handleDeleteView", tag: "search", pending: true,
		summary:   "Delete a view: the owner's to do, or a project administrator's once shared.",
		responses: none()},

	// Attachments (#21).
	{method: "GET", path: "/requirements/{reqKey}/attachments", handler: "handleListAttachments", tag: "attachments", pending: true,
		summary:   "The files attached to a requirement, the latest first.",
		responses: ok(env{"attachments": []attachment.Attachment{}})},
	{method: "POST", path: "/requirements/{reqKey}/attachments", handler: "handleUploadAttachment", tag: "attachments", pending: true,
		summary:   "Attach a file, sent as a multipart part named file; logged, never a revision.",
		multipart: true, responses: map[int]any{http.StatusCreated: env{"attachment": attachment.Attachment{}}, http.StatusRequestEntityTooLarge: errorEnvelope{}}},
	{method: "GET", path: "/attachments/{attachmentID}", handler: "handleDownloadAttachment", tag: "attachments", pending: true,
		summary: "The bytes of an attached file, as a download, or in place with inline for the types a browser shows safely.",
		query:   []param{{name: "inline", schema: &openapi.Schema{Type: "boolean"}, description: "true to show PNG, JPEG, GIF, WebP, PDF and plain text in place."}},
		binary:  true, responses: ok(nil)},
	{method: "DELETE", path: "/attachments/{attachmentID}", handler: "handleDeleteAttachment", tag: "attachments", pending: true,
		summary:   "Take a file off a requirement; its bytes stay while a revision shows it.",
		responses: none()},

	// Personal access tokens and the assistants' endpoint (#22).
	{method: "GET", path: "/tokens", handler: "handleListAPITokens", orgWide: true, tag: "tokens", pending: true,
		summary:   "The caller's personal access tokens in this organization, without their secrets.",
		responses: ok(env{"tokens": []auth.APIToken{}})},
	{method: "POST", path: "/tokens", handler: "handleCreateAPIToken", orgWide: true, tag: "tokens", pending: true,
		summary: "Make a personal access token; the secret is in this answer and never again. Needs a session.",
		request: auth.NewAPIToken{}, responses: map[int]any{http.StatusCreated: env{"token": auth.APIToken{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/tokens/{tokenID}", handler: "handleRevokeAPIToken", orgWide: true, tag: "tokens", pending: true,
		summary:   "Revoke one of the caller's tokens; it stops working at once.",
		responses: none()},
	{method: "GET", path: "/org/tokens", handler: "handleListOrgAPITokens", orgWide: true, tag: "tokens", pending: true,
		summary:   "Every personal access token in the organization, with whose it is. For administrators.",
		responses: ok(env{"tokens": []auth.OrgAPIToken{}})},
	{method: "DELETE", path: "/org/tokens/{tokenID}", handler: "handleRevokeOrgAPIToken", orgWide: true, tag: "tokens", pending: true,
		summary:   "Revoke anybody's token in the organization. For administrators.",
		responses: none()},
	{method: "POST", path: "/mcp", handler: "handleMCP", id: "mcp", tag: "mcp", pending: true,
		summary: "The Model Context Protocol endpoint: the marked operations of this API as tools, run as the caller. The body is a JSON-RPC 2.0 request.",
		request: json.RawMessage{}, responses: map[int]any{http.StatusOK: json.RawMessage{}, http.StatusAccepted: nil}},

	// CSV import and export (#23).
	{method: "POST", path: "/projects/{projectKey}/import", handler: "handleUploadImport", tag: "import", pending: true,
		summary:   "Upload a CSV file, sent as a multipart part named file, and see its columns with a mapping guessed from the headers. Nothing is written yet. For the project's administrators.",
		multipart: true, responses: map[int]any{http.StatusCreated: env{"import": csvio.Import{}}, http.StatusRequestEntityTooLarge: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/imports/{importID}", handler: "handleGetImport", tag: "import", pending: true,
		summary:   "An import: its mapping and problems before it starts, its progress while it runs, and what it did after.",
		responses: ok(env{"import": csvio.Import{}})},
	{method: "PUT", path: "/imports/{importID}/mapping", handler: "handleMapImport", tag: "import", pending: true,
		summary: "Set how the file's columns, words and people are read, and see the problems that would leave.",
		request: csvio.MappingInput{}, responses: map[int]any{http.StatusOK: env{"import": csvio.Import{}}, http.StatusConflict: errorEnvelope{}}},
	{method: "POST", path: "/imports/{importID}/start", handler: "handleStartImport", tag: "import", pending: true,
		summary:   "Hand an import without problems to the worker; refused as import_problems while any are left.",
		responses: map[int]any{http.StatusAccepted: env{"import": csvio.Import{}}, http.StatusConflict: errorEnvelope{}}},
	{method: "DELETE", path: "/imports/{importID}", handler: "handleCancelImport", tag: "import", pending: true,
		summary:   "Discard an import that has not started, or stop one that runs after the row it is on.",
		responses: none()},
	{method: "GET", path: "/projects/{projectKey}/export.csv", handler: "handleExportCSV", tag: "import", pending: true,
		summary: "A project's requirements as a CSV file, narrowed like the grid, keys kept, with the columns asked for.",
		query:   append(gridQuery, param{name: "column", repeated: true, description: "Columns in order, as the grid names them; the grid's default when absent."}),
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusUnprocessableEntity: errorEnvelope{}}},
}

var intParam = &openapi.Schema{Type: "integer"}

// keysetQuery is a window of a list read newest first: how many, and where the window before it ended.
func keysetQuery(def, max int) []param {
	return []param{
		{name: "limit", schema: intParam, description: "1 to " + strconv.Itoa(max) + "; " + strconv.Itoa(def) + " when absent."},
		{name: "cursor", description: "The next of the window before; the first window when absent."},
	}
}

// windowQuery is a window of a list read by offset: how many, and from where.
func windowQuery(def, max int) []param {
	return []param{
		{name: "limit", schema: intParam, description: "1 to " + strconv.Itoa(max) + "; " + strconv.Itoa(def) + " when absent."},
		{name: "offset", schema: intParam, description: "0 or later; 0 when absent."},
	}
}

var uuidParam = &openapi.Schema{Type: "string", Format: "uuid"}

// pickerQuery narrows the people and group pickers.
var pickerQuery = []param{
	{name: "q", description: "Words the name, or a person's email, starts with."},
	{name: "limit", schema: intParam, description: "1 to " + strconv.Itoa(perm.MaxPickerLimit) + "; " + strconv.Itoa(perm.DefaultPickerLimit) + " when absent."},
}

// searchQuery narrows a search across projects; every part is optional.
var searchQuery = []param{
	{name: "q", description: "Words to find: each must match, \"quoted words\" match as a phrase, or matches either side, -word leaves out what has it. Empty lists by the filters alone."},
	{name: "project", repeated: true, description: "Project keys to stay inside."},
	{name: "category", repeated: true, schema: &openapi.Schema{Type: "string", Enum: enumStrings(workflow.Categories)}, description: "Status categories."},
	{name: "label", repeated: true, description: "Label names; a hit carries at least one."},
	{name: "owner", repeated: true, schema: uuidParam, description: "People who own the requirement."},
	{name: "updatedAfter", schema: &openapi.Schema{Type: "string", Format: "date"}, description: "Changed on or after this day."},
	{name: "updatedBefore", schema: &openapi.Schema{Type: "string", Format: "date"}, description: "Changed before this day."},
	{name: "sort", schema: &openapi.Schema{Type: "string", Enum: []string{"relevance", "updated"}}, description: "relevance unless said, and updated when q is empty."},
	{name: "limit", schema: intParam, description: "1 to " + strconv.Itoa(search.MaxLimit) + "; " + strconv.Itoa(search.DefaultLimit) + " when absent."},
	{name: "offset", schema: intParam, description: "0 or later; 0 when absent."},
}

// gridQuery narrows and orders one project's requirements, as a view's filter does; every part is optional.
var gridQuery = []param{
	{name: "q", description: "Words to find, as in search."},
	{name: "kind", repeated: true, schema: &openapi.Schema{Type: "string", Enum: enumStrings(requirement.Kinds)}, description: "Requirements, sections or both when absent."},
	{name: "type", repeated: true, schema: uuidParam, description: "Requirement types."},
	{name: "status", repeated: true, schema: uuidParam, description: "Statuses."},
	{name: "category", repeated: true, schema: &openapi.Schema{Type: "string", Enum: enumStrings(workflow.Categories)}, description: "Status categories."},
	{name: "label", repeated: true, description: "Label names."},
	{name: "owner", repeated: true, schema: uuidParam, description: "Owners."},
	{name: "priority", repeated: true, schema: &openapi.Schema{Type: "string", Enum: enumStrings(requirement.Priorities)}, description: "Priorities."},
	{name: "attr", repeated: true, description: "attributeId:op:value, all of which must hold; op is one of " + strings.Join(enumStrings(search.Ops), ", ") + "."},
	{name: "sort", description: "A column to order by; outline order when absent."},
	{name: "desc", schema: &openapi.Schema{Type: "boolean"}, description: "true to order the column backwards."},
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
