package httpapi

import (
	"encoding/json"
	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/comment"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/attachment"
	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/installation"
	"github.com/cloudster1/exciter/backend/internal/label"
	"github.com/cloudster1/exciter/backend/internal/notify"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/perm"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/review"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/theme"
	"github.com/cloudster1/exciter/backend/internal/trace"
	"github.com/cloudster1/exciter/backend/internal/version"
	"github.com/cloudster1/exciter/backend/internal/watch"
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
	// orgWide routes concern the installation as a whole, which a token
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

	// The installation itself: what everybody signed in may read about it, and its name.
	{method: "GET", path: "/installation", handler: "handleGetInstallation", tag: "installation", summary: "The installation's name, as everybody signed in sees it.",
		responses: ok(env{"installation": installation.Installation{}})},
	{method: "PUT", path: "/installation", handler: "handleRenameInstallation", orgWide: true, tag: "installation",
		summary: "Rename the installation; written to the audit log. For administrators.",
		request: renameInstallationRequest{}, responses: ok(env{"installation": installation.Installation{}})},

	// Signing in with a password or through the installation's identity provider, and the session's own settings.
	{method: "POST", path: "/auth/login", handler: "handleLogin", tag: "auth", summary: "Sign in a local account with email and password, and set the session cookie.", public: true,
		request: loginRequest{}, responses: ok(meResponse{})},
	{method: "GET", path: "/auth/oidc/start", handler: "handleOIDCStart", tag: "auth", summary: "Begin signing in through the installation's identity provider.", public: true, redirect: true,
		query: []param{{name: "next", description: "A path of this application to land on afterwards."}}, responses: map[int]any{}},
	{method: "GET", path: "/auth/oidc/callback", handler: "handleOIDCCallback", tag: "auth", summary: "Where the identity provider sends the browser back; sets the session cookie.", public: true, redirect: true,
		query: []param{{name: "state"}, {name: "code"}, {name: "error"}}, responses: map[int]any{}},
	{method: "POST", path: "/auth/logout", handler: "handleLogout", tag: "auth", summary: "End the session.", responses: none()},
	{method: "GET", path: "/auth/me", handler: "handleMe", tag: "auth", summary: "Who is signed in, and how they stand in the installation.",
		tool: "whoami", toolHelp: "Who the caller is and the role they have.", responses: ok(meResponse{})},
	{method: "PATCH", path: "/auth/me", handler: "handleUpdateMe", tag: "auth", summary: "Change the caller's own settings, such as the interface language.",
		request: updateMeRequest{}, responses: ok(meResponse{})},

	// The installation's people, for its administrators.
	{method: "GET", path: "/users", handler: "handleListMembers", orgWide: true, tag: "members",
		summary:   "The installation's people, those switched on first, then owners first, with whether the identity provider's groups decide their roles. For administrators.",
		responses: ok(env{"members": []auth.Member{}})},
	{method: "POST", path: "/users", handler: "handleCreateMember", orgWide: true, tag: "members",
		summary: "Add a person with a password, made for them when none is given and shown once in the answer. For administrators.",
		request: createMemberRequest{}, responses: map[int]any{http.StatusCreated: env{"member": auth.CreatedMember{}}}},
	{method: "PATCH", path: "/users/{userID}", handler: "handleUpdateMember", orgWide: true, tag: "members",
		summary: "Give a member another role, or switch their account off or on; an owner's role and the caller's own stay. For administrators.",
		request: updateMemberRequest{}, responses: ok(env{"member": auth.Member{}})},
	{method: "DELETE", path: "/users/{userID}", handler: "handleSwitchOffMember", orgWide: true, tag: "members",
		summary: "Switch a member's account off: their sessions and tokens end at once, their account and work stay. For administrators.", responses: none()},
	{method: "GET", path: "/users/requests", handler: "handleListJoinRequests", orgWide: true, tag: "members",
		summary:   "Who signed in through the identity provider and is waiting to be let in. For administrators.",
		responses: ok(env{"requests": []auth.JoinRequest{}})},
	{method: "POST", path: "/users/requests/{userID}/admit", handler: "handleAdmitJoinRequest", orgWide: true, tag: "members",
		summary: "Let a waiting person in with the standing given. For administrators.",
		request: admitRequest{}, responses: ok(env{"membership": auth.Membership{}})},
	{method: "DELETE", path: "/users/requests/{userID}", handler: "handleDeclineJoinRequest", orgWide: true, tag: "members",
		summary: "Turn a waiting person away; they may ask again. For administrators.", responses: none()},

	// The installation's identity provider and the roles its groups grant, for its administrators.
	{method: "GET", path: "/oidc-provider", handler: "handleGetOIDCProvider", orgWide: true, tag: "sso",
		summary:   "The identity provider, if one is configured, and the callback address to register with it. For administrators.",
		responses: ok(providerView{})},
	{method: "PUT", path: "/oidc-provider", handler: "handleSaveOIDCProvider", orgWide: true, tag: "sso",
		summary: "Configure the identity provider; a client secret left out keeps the stored one. For administrators.",
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
		tool: "list_audit_log", toolHelp: "Who did what in Exciter, newest first. For administrators.",
		summary: "Who did what in Exciter, newest first; next is the cursor for the window after, null at the end. For administrators.",
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
		summary:   "The theme the caller sees: chosen, followed from Armature, the default for everyone, or null for the built-in one; source says which.",
		responses: ok(env{"theme": (*theme.Theme)(nil), "source": theme.Source("")})},
	{method: "PUT", path: "/themes/active", handler: "handleChooseTheme", tag: "themes",
		summary: "Use a theme; null returns to the default for everyone, null with builtIn keeps the built-in one.",
		request: chooseThemeRequest{}, responses: ok(env{"theme": (*theme.Theme)(nil)})},
	{method: "GET", path: "/themes/default", handler: "handleDefaultTheme", tag: "themes",
		summary:   "The default theme for everyone, whatever the caller chose for themselves, or null for the built-in one.",
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
	{method: "GET", path: "/project-templates", handler: "handleListProjectTemplates", tag: "projects",
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
	{method: "GET", path: "/access/me", handler: "handleMyAccess", tag: "permissions",
		summary:   "What the caller may do across Exciter, which decides which buttons to draw.",
		responses: ok(env{"can": perm.OrgCan{}})},
	{method: "GET", path: "/people", handler: "handleListPeople", tag: "permissions",
		summary: "The people of Exciter, to pick whom to grant something or who owns a requirement.",
		tool:    "list_people", toolHelp: "People whose name or email starts with q, with the ids other tools take.",
		query: pickerQuery, responses: ok(env{"people": []perm.Person{}})},
	{method: "GET", path: "/groups", handler: "handleListGroups", tag: "permissions",
		summary: "Groups, to pick whom to grant something.",
		query:   pickerQuery, responses: ok(env{"groups": []perm.Group{}})},
	{method: "GET", path: "/projects/{projectKey}/grants", handler: "handleListProjectGrants", tag: "permissions",
		summary:   "Who holds which role in a project. For the project's administrators.",
		responses: ok(env{"grants": []perm.Grant{}})},
	{method: "PUT", path: "/projects/{projectKey}/grants", handler: "handleSetProjectGrants", tag: "permissions",
		summary: "Replace a project's whole table of grants; refused when it would leave the project without an administrator. For the project's administrators.",
		request: perm.GrantsInput{}, responses: map[int]any{http.StatusOK: env{"grants": []perm.Grant{}}, http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/access/{userID}", handler: "handleInspectProjectAccess", tag: "permissions",
		summary:   "What a person may do in a project and which grant decides each right, as the database answers it. For the project's administrators.",
		responses: ok(env{"access": perm.AccessReport{}})},

	// Requirements, their trash (#13) and their place in the tree (#15).
	{method: "POST", path: "/projects/{projectKey}/requirements", handler: "handleCreateRequirement", tag: "requirements",
		summary: "Write a requirement or a section as revision 1, in the status the workflow starts in.",
		tool:    "create_requirement", toolHelp: "Write a requirement or a section in a project, by its key, as revision 1.",
		request: requirement.CreateInput{}, responses: map[int]any{http.StatusCreated: env{"requirement": requirement.Requirement{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/requirements/by-id/{requirementID}", handler: "handleGetRequirementByID", tag: "requirements",
		summary:   "One requirement by its id, which never changes, for permalinks.",
		responses: ok(env{"requirement": requirement.Requirement{}})},
	{method: "GET", path: "/requirements/{reqKey}", handler: "handleGetRequirement", tag: "requirements",
		summary: "One requirement by its key or a key it had; the answer carries its current key.",
		tool:    "get_requirement", toolHelp: "One requirement by its key, such as CHK-R42, with its statement, rationale and latest revision number.",
		responses: ok(env{"requirement": requirement.Requirement{}})},
	{method: "PATCH", path: "/requirements/{reqKey}", handler: "handleUpdateRequirement", tag: "requirements",
		summary: "Change a requirement; a change to its normative content writes the next revision over baseRevision, else 409 revision_conflict. The status moves through transitions.",
		tool:    "update_requirement", toolHelp: "Change a requirement; a change to its title, statement or rationale needs baseRevision, the revision it was made from.",
		request: requirement.UpdateInput{}, responses: map[int]any{http.StatusOK: env{"requirement": requirement.Requirement{}}, http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/requirements/{reqKey}", handler: "handleTrashRequirement", tag: "requirements",
		summary:   "Move a requirement and everything below it to its project's trash.",
		responses: none()},
	{method: "GET", path: "/projects/{projectKey}/trash", handler: "handleListRequirementTrash", tag: "requirements",
		summary:   "The project's trash, the latest first.",
		responses: ok(env{"items": []requirement.TrashItem{}})},
	{method: "DELETE", path: "/projects/{projectKey}/trash", handler: "handleEmptyRequirementTrash", tag: "requirements",
		summary:   "Delete everything in the project's trash for good. For the project's administrators.",
		responses: none()},
	{method: "POST", path: "/projects/{projectKey}/trash/{requirementID}/restore", handler: "handleRestoreRequirement", tag: "requirements",
		summary:   "Put a trashed requirement back where it was, or at the top when its parent is gone.",
		responses: ok(env{"requirement": requirement.Requirement{}})},
	{method: "DELETE", path: "/projects/{projectKey}/trash/{requirementID}", handler: "handlePurgeRequirement", tag: "requirements",
		summary:   "Delete a trashed requirement, what went with it and their revisions for good. For the project's administrators.",
		responses: none()},
	{method: "GET", path: "/projects/{projectKey}/tree", handler: "handleGetTree", tag: "tree",
		summary: "Every requirement and section of a project, out of the trash, in reading order with outline numbers.",
		tool:    "get_tree", toolHelp: "A project's requirements and sections in reading order with their outline numbers and keys, without their text.",
		responses: ok(env{"nodes": []requirement.TreeNode{}})},
	{method: "GET", path: "/projects/{projectKey}/spec", handler: "handleGetSpecification", tag: "tree",
		summary: "The project read as one numbered document: a window of the tree in reading order, with content.",
		query:   windowQuery(requirement.DefaultLimit, requirement.MaxSpecLimit), responses: ok(env{"entries": []requirement.SpecEntry{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "POST", path: "/requirements/{reqKey}/move", handler: "handleMoveRequirement", tag: "tree",
		summary: "Put a requirement, with everything below it, elsewhere in its project or in another, which gives new keys.",
		request: requirement.MoveInput{}, responses: map[int]any{http.StatusOK: env{"requirement": requirement.Requirement{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "POST", path: "/requirements/{reqKey}/copy", handler: "handleCopyRequirement", tag: "tree",
		summary: "Copy a requirement, and what is below it when asked, as new requirements at their current revision.",
		request: requirement.CopyInput{}, responses: map[int]any{http.StatusCreated: env{"requirement": requirement.Requirement{}, "copied": 0}, http.StatusUnprocessableEntity: errorEnvelope{}}},

	// Types and attributes (#14).
	{method: "GET", path: "/attribute-kinds", handler: "handleListAttributeKinds", tag: "types",
		summary:   "What an attribute can be, for the form that defines one.",
		responses: ok(env{"kinds": []attribute.KindInfo{}})},
	{method: "GET", path: "/projects/{projectKey}/types", handler: "handleListRequirementTypes", tag: "types",
		summary:   "A project's requirement types in order, retired ones included.",
		responses: ok(env{"types": []attribute.Type{}})},
	{method: "POST", path: "/projects/{projectKey}/types", handler: "handleCreateRequirementType", tag: "types",
		summary: "Make a requirement type. For the project's administrators.",
		request: attribute.TypeInput{}, responses: map[int]any{http.StatusCreated: env{"type": attribute.Type{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PATCH", path: "/projects/{projectKey}/types/{typeID}", handler: "handleUpdateRequirementType", tag: "types",
		summary: "Change, move or retire a requirement type. For the project's administrators.",
		request: attribute.TypeInput{}, responses: ok(env{"type": attribute.Type{}})},
	{method: "DELETE", path: "/projects/{projectKey}/types/{typeID}", handler: "handleDeleteRequirementType", tag: "types",
		summary:   "Delete a requirement type no requirement or revision ever had; retire one that was used. For the project's administrators.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/attributes", handler: "handleListAttributes", tag: "types",
		summary:   "A project's attributes in order, retired ones included.",
		responses: ok(env{"attributes": []attribute.Def{}})},
	{method: "POST", path: "/projects/{projectKey}/attributes", handler: "handleCreateAttribute", tag: "types",
		summary: "Define an attribute. For the project's administrators.",
		request: attribute.DefInput{}, responses: map[int]any{http.StatusCreated: env{"attribute": attribute.Def{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PATCH", path: "/projects/{projectKey}/attributes/{attributeID}", handler: "handleUpdateAttribute", tag: "types",
		summary: "Change an attribute's name, options, types, order or whether it is required or retired; never its kind. For the project's administrators.",
		request: attribute.DefInput{}, responses: map[int]any{http.StatusOK: env{"attribute": attribute.Def{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/projects/{projectKey}/attributes/{attributeID}", handler: "handleDeleteAttribute", tag: "types",
		summary:   "Delete an attribute no requirement or revision ever had a value for; retire one that was used. For the project's administrators.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusConflict: errorEnvelope{}}},

	// History (#16).
	{method: "GET", path: "/requirements/{reqKey}/revisions", handler: "handleListRevisions", tag: "history",
		summary: "A requirement's revisions, the latest first.",
		tool:    "list_revisions", toolHelp: "A requirement's revisions, the latest first, with author, time and comment.",
		query: windowQuery(requirement.DefaultLimit, requirement.MaxLimit), responses: ok(env{"revisions": []requirement.RevisionEntry{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "GET", path: "/requirements/{reqKey}/revisions/{revisionNumber}", handler: "handleGetRevision", tag: "history",
		summary: "One revision with its normative content, as it was.",
		tool:    "get_revision", toolHelp: "One revision of a requirement with what it said then.",
		responses: ok(env{"revision": requirement.Revision{}})},
	{method: "POST", path: "/requirements/{reqKey}/revisions/{revisionNumber}/restore", handler: "handleRestoreRevision", tag: "history",
		summary: "Write an older revision's content again as the next revision, which records the one it restored.",
		request: requirement.RestoreInput{}, responses: map[int]any{http.StatusOK: env{"requirement": requirement.Requirement{}, "revision": requirement.RevisionEntry{}}, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/requirements/{reqKey}/diff", handler: "handleDiffRevisions", tag: "history",
		summary: "What changed between two revisions of a requirement: statement, rationale, type and attributes.",
		tool:    "diff_revisions", toolHelp: "What changed between two revisions of a requirement: statement, rationale, type and attributes.",
		query: []param{
			{name: "from", schema: intParam, description: "A revision number, 0 for the empty requirement; the one before to when absent."},
			{name: "to", schema: intParam, description: "A revision number; the latest when absent."},
		}, responses: ok(env{"comparison": requirement.Comparison{}})},
	{method: "GET", path: "/requirements/{reqKey}/activity", handler: "handleListActivity", tag: "history",
		summary: "Revisions and logged changes of a requirement in one list, newest first; next is the cursor for the window after, null at the end.",
		query:   keysetQuery(requirement.DefaultLimit, requirement.MaxLimit), responses: ok(env{"entries": []requirement.ActivityEntry{}, "next": (*string)(nil)})},

	// Statuses and workflow (#17).
	{method: "GET", path: "/projects/{projectKey}/statuses", handler: "handleListStatuses", tag: "workflow",
		summary:   "A project's statuses in order, with how many requirements are in each.",
		responses: ok(env{"statuses": []workflow.Status{}})},
	{method: "POST", path: "/projects/{projectKey}/statuses", handler: "handleCreateStatus", tag: "workflow",
		summary: "Make a status for the project's workflows to use. For the project's administrators.",
		request: workflow.StatusInput{}, responses: map[int]any{http.StatusCreated: env{"status": workflow.Status{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PATCH", path: "/projects/{projectKey}/statuses/{statusID}", handler: "handleUpdateStatus", tag: "workflow",
		summary: "Rename, describe, move or recategorize a status. For the project's administrators.",
		request: workflow.StatusInput{}, responses: ok(env{"status": workflow.Status{}})},
	{method: "DELETE", path: "/projects/{projectKey}/statuses/{statusID}", handler: "handleDeleteStatus", tag: "workflow",
		summary:   "Delete a status no workflow and no requirement uses. For the project's administrators.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/workflows", handler: "handleListWorkflows", tag: "workflow",
		summary:   "A project's workflows, and which the project and each type follow.",
		responses: ok(env{"workflows": []workflow.Workflow{}})},
	{method: "POST", path: "/projects/{projectKey}/workflows", handler: "handleCreateWorkflow", tag: "workflow",
		summary: "Make a workflow from the designer's graph. For the project's administrators.",
		request: workflow.GraphInput{}, responses: map[int]any{http.StatusCreated: env{"workflow": workflow.Workflow{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PUT", path: "/projects/{projectKey}/workflow", handler: "handleAssignWorkflow", tag: "workflow",
		summary: "Make a workflow the project's or a type's, moving requirements whose status it lacks as the mapping says. For the project's administrators.",
		request: workflow.AssignInput{}, responses: map[int]any{http.StatusOK: env{"workflow": workflow.Workflow{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/workflows/{workflowID}", handler: "handleGetWorkflow", tag: "workflow",
		summary:   "One workflow with its steps, transitions and rules.",
		responses: ok(env{"workflow": workflow.Workflow{}})},
	{method: "PUT", path: "/projects/{projectKey}/workflows/{workflowID}", handler: "handleSaveWorkflow", tag: "workflow",
		summary: "Save a workflow's whole graph; transitions keep their ids, and a status it drops must not be in use under it. For the project's administrators.",
		request: workflow.GraphInput{}, responses: map[int]any{http.StatusOK: env{"workflow": workflow.Workflow{}}, http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/projects/{projectKey}/workflows/{workflowID}", handler: "handleDeleteWorkflow", tag: "workflow",
		summary:   "Delete a workflow neither the project nor a type follows. For the project's administrators.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/workflow-rule-types", handler: "handleListRuleTypes", tag: "workflow",
		summary:   "The conditions, validators and post-functions a transition can carry, with their settings.",
		responses: ok(env{"ruleTypes": []workflow.RuleType{}})},
	{method: "GET", path: "/requirements/{reqKey}/transitions", handler: "handleListTransitions", tag: "workflow",
		summary: "The transitions the caller may take on a requirement now.",
		tool:    "list_transitions", toolHelp: "The transitions the caller may take on a requirement now, with the status each leads to.",
		responses: ok(env{"transitions": []workflow.Available{}})},
	{method: "POST", path: "/requirements/{reqKey}/transitions", handler: "handleTransitionRequirement", tag: "workflow",
		summary: "Take a transition; a validator that refuses it answers 422 with its sentence.",
		tool:    "transition_requirement", toolHelp: "Take one of a requirement's transitions by its id, with a comment when it asks for one.",
		request: workflow.TransitionRequest{}, responses: map[int]any{http.StatusOK: env{"requirement": requirement.Requirement{}}, http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},

	// Trace links (#18).
	{method: "GET", path: "/link-types", handler: "handleListOrgLinkTypes", tag: "links",
		summary:   "The link types for all projects.",
		responses: ok(env{"linkTypes": []trace.LinkType{}})},
	{method: "POST", path: "/link-types", handler: "handleCreateOrgLinkType", orgWide: true, tag: "links",
		summary: "Make a link type for every project. For administrators.",
		request: trace.LinkTypeInput{}, responses: map[int]any{http.StatusCreated: env{"linkType": trace.LinkType{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PATCH", path: "/link-types/{linkTypeID}", handler: "handleUpdateLinkType", tag: "links",
		summary: "Change or retire a link type: one for all projects for administrators, a project's own for its administrators.",
		request: trace.LinkTypeInput{}, responses: map[int]any{http.StatusOK: env{"linkType": trace.LinkType{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/link-types/{linkTypeID}", handler: "handleDeleteLinkType", tag: "links",
		summary:   "Delete a link type no link ever had; retire one that was used.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/link-types", handler: "handleListProjectLinkTypes", tag: "links",
		summary:   "The link types a project may use, those for all projects and its own, with whether each is enabled there and its rules.",
		responses: ok(env{"linkTypes": []trace.LinkType{}})},
	{method: "POST", path: "/projects/{projectKey}/link-types", handler: "handleCreateProjectLinkType", tag: "links",
		summary: "Make a link type of the project's own. For the project's administrators.",
		request: trace.LinkTypeInput{}, responses: map[int]any{http.StatusCreated: env{"linkType": trace.LinkType{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PUT", path: "/projects/{projectKey}/link-types/{linkTypeID}", handler: "handleSetProjectLinkType", tag: "links",
		summary: "Enable or disable a link type in a project, and which requirement types it may join. For the project's administrators.",
		request: trace.ProjectLinkTypeInput{}, responses: map[int]any{http.StatusOK: env{"linkType": trace.LinkType{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/requirements/{reqKey}/links", handler: "handleListLinks", tag: "links",
		summary: "A requirement's links in both directions, to requirements the caller may view.",
		tool:    "list_links", toolHelp: "A requirement's trace links in both directions, each with its type, the phrase read from this end, and the requirement at the other end.",
		responses: ok(env{"links": []trace.Link{}})},
	{method: "POST", path: "/requirements/{reqKey}/links", handler: "handleCreateLink", tag: "links",
		summary: "Link a requirement to another, recording the revisions of both; a self-link, a repeat or a cycle of an acyclic type is refused.",
		tool:    "link_requirements", toolHelp: "Link a requirement to another by a link type key such as derives, refines, satisfies, verifies, conflicts or relates; outgoing makes the requirement in the path the source.",
		request: trace.LinkInput{}, responses: map[int]any{http.StatusCreated: env{"link": trace.Link{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/links/{linkID}", handler: "handleDeleteLink", tag: "links",
		summary:   "Remove a link, from either end; it is kept as a tombstone for baselines and history.",
		responses: none()},

	// Labels (#19).
	{method: "GET", path: "/requirements/{reqKey}/labels", handler: "handleListRequirementLabels", tag: "labels",
		summary:   "A requirement's labels, by name.",
		responses: ok(env{"labels": []string{}})},
	{method: "POST", path: "/requirements/{reqKey}/labels", handler: "handleAddRequirementLabel", tag: "labels",
		summary: "Put a label on a requirement, coining it if nobody uses it yet; logged, never a revision.",
		request: label.Input{}, responses: map[int]any{http.StatusOK: env{"labels": []string{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/requirements/{reqKey}/labels/{labelName}", handler: "handleRemoveRequirementLabel", tag: "labels",
		summary:   "Take a label off a requirement; one it does not carry is no change.",
		responses: none()},
	{method: "GET", path: "/labels", handler: "handleListLabels", tag: "labels",
		summary: "Labels in use on requirements the caller may view, starting with what was typed, the most used first.",
		query: []param{
			{name: "q", description: "The start of the label."},
			{name: "project", description: "A project key to stay inside."},
			{name: "limit", schema: intParam, description: "1 to 50; 10 when absent."},
		}, responses: ok(env{"labels": []label.Suggestion{}})},
	{method: "GET", path: "/labels/{labelName}/requirements", handler: "handleListLabelledRequirements", tag: "labels",
		summary:   "Requirements the caller may view that carry a label, by key.",
		query:     append([]param{{name: "project", description: "A project key to stay inside."}}, windowQuery(label.DefaultLimit, label.MaxLimit)...),
		responses: ok(env{"requirements": []label.Labelled{}, "total": 0, "limit": 0, "offset": 0})},

	// Search, the grid and saved views (#20).
	{method: "GET", path: "/search", handler: "handleSearch", tag: "search",
		tool: "search_requirements", toolHelp: "Requirements across projects whose key, title, statement, rationale or text values match words, with snippets, narrowed by project, status category, label, owner or the day of their latest change.",
		summary: "Requirements whose key, title, statement or rationale match, among what the caller may view.",
		query:   searchQuery, responses: map[int]any{http.StatusOK: env{"hits": []search.Hit{}, "total": 0, "limit": 0, "offset": 0}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/search/quick", handler: "handleQuickSearch", tag: "search",
		summary: "Requirements whose key or title start with what was typed, for the command palette.",
		query: []param{
			{name: "q", description: "The words or the key typed so far."},
			{name: "project", description: "A project key to stay inside."},
			{name: "limit", schema: intParam, description: "1 to 20; 8 when absent."},
		}, responses: ok(env{"requirements": []search.QuickHit{}})},
	{method: "GET", path: "/projects/{projectKey}/requirements", handler: "handleListGrid", tag: "search",
		summary: "A window of a project's requirements as the grid shows them, narrowed and ordered.",
		tool:    "list_requirements", toolHelp: "A project's requirements in outline order, a window at a time, narrowed by words, kind, type, status, label, owner, priority or attribute conditions, and ordered by a column.",
		query:     append(gridQuery, windowQuery(search.DefaultLimit, search.MaxGridLimit)...),
		responses: map[int]any{http.StatusOK: env{"rows": []search.Row{}, "total": 0, "limit": 0, "offset": 0}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/views", handler: "handleListViews", tag: "search",
		summary:   "The caller's own views of a project's grid, then the shared ones.",
		responses: ok(env{"views": []search.View{}})},
	{method: "POST", path: "/projects/{projectKey}/views", handler: "handleCreateView", tag: "search",
		summary: "Save a view of a project's grid; sharing it is for its editors.",
		request: search.ViewInput{}, responses: map[int]any{http.StatusCreated: env{"view": search.View{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PATCH", path: "/views/{viewID}", handler: "handleUpdateView", tag: "search",
		summary: "Change a view: the owner's to do, or a project administrator's once shared.",
		request: search.ViewInput{}, responses: ok(env{"view": search.View{}})},
	{method: "DELETE", path: "/views/{viewID}", handler: "handleDeleteView", tag: "search",
		summary:   "Delete a view: the owner's to do, or a project administrator's once shared.",
		responses: none()},

	// Attachments (#21).
	{method: "GET", path: "/requirements/{reqKey}/attachments", handler: "handleListAttachments", tag: "attachments",
		summary:   "The files attached to a requirement, the latest first.",
		responses: ok(env{"attachments": []attachment.Attachment{}})},
	{method: "POST", path: "/requirements/{reqKey}/attachments", handler: "handleUploadAttachment", tag: "attachments",
		summary:   "Attach a file, sent as a multipart part named file; logged, never a revision.",
		multipart: true, responses: map[int]any{http.StatusCreated: env{"attachment": attachment.Attachment{}}, http.StatusRequestEntityTooLarge: errorEnvelope{}}},
	{method: "GET", path: "/attachments/{attachmentID}", handler: "handleDownloadAttachment", tag: "attachments",
		summary: "The bytes of an attached file, as a download, or in place with inline for the types a browser shows safely.",
		query:   []param{{name: "inline", schema: &openapi.Schema{Type: "boolean"}, description: "true to show PNG, JPEG, GIF, WebP, PDF and plain text in place."}},
		binary:  true, responses: ok(nil)},
	{method: "DELETE", path: "/attachments/{attachmentID}", handler: "handleDeleteAttachment", tag: "attachments",
		summary:   "Take a file off a requirement; its bytes stay while a revision shows it.",
		responses: none()},

	// Personal access tokens and the assistants' endpoint (#22).
	{method: "GET", path: "/tokens", handler: "handleListAPITokens", orgWide: true, tag: "tokens",
		summary:   "The caller's personal access tokens, without their secrets.",
		responses: ok(env{"tokens": []auth.APIToken{}})},
	{method: "POST", path: "/tokens", handler: "handleCreateAPIToken", orgWide: true, tag: "tokens",
		summary: "Make a personal access token; the secret is in this answer and never again. Needs a session.",
		request: auth.NewAPIToken{}, responses: map[int]any{http.StatusCreated: env{"token": auth.APIToken{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/tokens/{tokenID}", handler: "handleRevokeAPIToken", orgWide: true, tag: "tokens",
		summary:   "Revoke one of the caller's tokens; it stops working at once.",
		responses: none()},
	{method: "GET", path: "/tokens/everyone", handler: "handleListEveryonesAPITokens", orgWide: true, tag: "tokens",
		summary:   "Everybody's personal access tokens, with whose each is. For administrators.",
		responses: ok(env{"tokens": []auth.OrgAPIToken{}})},
	{method: "DELETE", path: "/tokens/everyone/{tokenID}", handler: "handleRevokeAnybodysAPIToken", orgWide: true, tag: "tokens",
		summary:   "Revoke anybody's token. For administrators.",
		responses: none()},
	{method: "POST", path: "/mcp", handler: "handleMCP", id: "mcp", tag: "mcp",
		summary: "The Model Context Protocol endpoint: the marked operations of this API as tools, run as the caller. The body is a JSON-RPC 2.0 request.",
		request: json.RawMessage{}, responses: map[int]any{http.StatusOK: json.RawMessage{}, http.StatusAccepted: nil}},

	// CSV import and export (#23).
	{method: "POST", path: "/projects/{projectKey}/import", handler: "handleUploadImport", tag: "import",
		summary:   "Upload a CSV file, sent as a multipart part named file, and see its columns with a mapping guessed from the headers. Nothing is written yet. For the project's administrators.",
		multipart: true, responses: map[int]any{http.StatusCreated: env{"import": csvio.Import{}}, http.StatusRequestEntityTooLarge: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/imports/{importID}", handler: "handleGetImport", tag: "import",
		summary:   "An import: its mapping and problems before it starts, its progress while it runs, and what it did after.",
		responses: ok(env{"import": csvio.Import{}})},
	{method: "PUT", path: "/imports/{importID}/mapping", handler: "handleMapImport", tag: "import",
		summary: "Set how the file's columns, words and people are read, and see the problems that would leave.",
		request: csvio.MappingInput{}, responses: map[int]any{http.StatusOK: env{"import": csvio.Import{}}, http.StatusConflict: errorEnvelope{}}},
	{method: "POST", path: "/imports/{importID}/start", handler: "handleStartImport", tag: "import",
		summary:   "Hand an import without problems to the worker; refused as import_problems while any are left.",
		responses: map[int]any{http.StatusAccepted: env{"import": csvio.Import{}}, http.StatusConflict: errorEnvelope{}}},
	{method: "DELETE", path: "/imports/{importID}", handler: "handleCancelImport", tag: "import",
		summary:   "Discard an import that has not started, or stop one that runs after the row it is on.",
		responses: none()},
	{method: "GET", path: "/projects/{projectKey}/export.csv", handler: "handleExportCSV", tag: "import",
		summary: "A project's requirements as a CSV file, narrowed like the grid, keys kept, with the columns asked for.",
		query:   append(gridQuery, param{name: "column", repeated: true, description: "Columns in order, as the grid names them; the grid's default when absent."}),
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusUnprocessableEntity: errorEnvelope{}}},

	// The Armature integration (M2, docs/api-contract-m2.md): the connection and each person's token (#26).
	{method: "GET", path: "/armature/connection", handler: "handleGetArmatureConnection", orgWide: true, tag: "armature",
		summary:   "The installation's Armature: where people open it and where its webhooks go; null when none. For administrators.",
		responses: ok(env{"connection": (*armature.Connection)(nil)})},
	{method: "PUT", path: "/armature/connection", handler: "handleSaveArmatureConnection", orgWide: true, tag: "armature",
		summary: "Connect the installation to an Armature, checked to answer as one; a new address forgets every stored token. For administrators.",
		request: armature.ConnectionInput{}, responses: map[int]any{http.StatusOK: env{"connection": armature.Connection{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/armature/connection", handler: "handleRemoveArmatureConnection", orgWide: true, tag: "armature",
		summary:   "Disconnect Exciter from Armature, forgetting every stored token and link record. For administrators.",
		responses: none()},
	{method: "GET", path: "/armature/account", handler: "handleGetArmatureAccount", tag: "armature",
		summary:   "The caller's own standing with Armature: whether Exciter is connected, and whom their token acts as.",
		responses: ok(env{"account": armature.Account{}})},
	{method: "PUT", path: "/armature/account/token", handler: "handleStoreArmatureToken", tag: "armature",
		summary: "Store the caller's Armature personal access token, checked with Armature first and never answered again. Needs a session.",
		request: armature.TokenInput{}, responses: map[int]any{http.StatusOK: env{"account": armature.Account{}}, http.StatusUnprocessableEntity: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusBadGateway: errorEnvelope{}}},
	{method: "POST", path: "/armature/account/check", handler: "handleCheckArmatureAccount", tag: "armature",
		summary:   "Ask Armature now whether the caller's token still works.",
		responses: ok(env{"account": armature.Account{}})},
	{method: "DELETE", path: "/armature/account/token", handler: "handleRemoveArmatureToken", tag: "armature",
		summary:   "Forget the caller's Armature token; done also when there was none.",
		responses: none()},

	// Requirements traced to Armature issues (#27), read as the viewer.
	{method: "GET", path: "/armature/issues", handler: "handleLookupArmatureIssues", tag: "armature",
		summary:   "Issues by their keys, as the caller may see them in Armature; status says why when Armature could not be asked.",
		query:     []param{{name: "key", repeated: true, description: "Issue keys, at most 50."}},
		responses: map[int]any{http.StatusOK: env{"status": armature.StatusOK, "issues": []armature.LookupResult{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/armature/issues/{issueKey}", handler: "handleGetArmatureIssue", tag: "armature",
		summary:   "One issue as the caller may see it in Armature, null when they may not.",
		responses: ok(env{"status": armature.StatusOK, "issue": (*armature.Issue)(nil)})},
	{method: "GET", path: "/armature/search", handler: "handleSearchArmatureIssues", tag: "armature",
		summary:   "Issues whose key or summary matches, as the caller may see them, for the picker.",
		query:     []param{{name: "q", description: "A key, or words of the summary."}, {name: "limit", schema: intParam, description: "1 to 20; 10 when absent."}},
		responses: map[int]any{http.StatusOK: env{"status": armature.StatusOK, "issues": []armature.Issue{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/requirements/{reqKey}/armature-links", handler: "handleListArmatureLinks", tag: "armature",
		summary: "The Armature issues a requirement is traced to, with the status category stored for everyone and how far each link's sync has come.",
		tool:    "list_armature_links", toolHelp: "The Armature issues a requirement is traced to, each with its relation, the status category stored for everyone and whether Armature shows the link yet.",
		responses: ok(env{"links": []armature.Link{}})},
	{method: "POST", path: "/requirements/{reqKey}/armature-links", handler: "handleCreateArmatureLink", tag: "armature",
		summary: "Trace a requirement to an Armature issue the caller may see; the worker puts a link to the requirement on the issue.",
		tool:    "link_armature_issue", toolHelp: "Trace a requirement to an Armature issue by its key, such as CP-12, as implements, verifies or relates; the caller's own Armature token must see the issue.",
		request: armature.LinkInput{}, responses: map[int]any{http.StatusCreated: env{"link": armature.Link{}}, http.StatusConflict: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}, http.StatusBadGateway: errorEnvelope{}}},
	{method: "DELETE", path: "/armature-links/{linkID}", handler: "handleDeleteArmatureLink", tag: "armature",
		summary:   "Remove a trace to an issue; the worker takes the link off the issue.",
		responses: none()},

	// Issues made from requirements (#28).
	{method: "GET", path: "/armature/projects", handler: "handleListArmatureProjects", tag: "armature",
		summary:   "The Armature projects the caller may see, and whether they may file issues in each.",
		responses: ok(env{"status": armature.StatusOK, "projects": []armature.Project{}})},
	{method: "GET", path: "/armature/issue-types", handler: "handleListArmatureIssueTypes", tag: "armature",
		summary:   "The kinds of issue Armature files, subtasks left out.",
		responses: ok(env{"status": armature.StatusOK, "issueTypes": []armature.IssueType{}})},
	{method: "POST", path: "/projects/{projectKey}/armature-issues", handler: "handleCreateArmatureIssues", tag: "armature",
		summary: "Make one Armature issue per requirement, each traced to its requirement; stops at the first Armature refuses.",
		request: armature.CreateInput{}, responses: map[int]any{http.StatusCreated: env{"created": []armature.Created{}, "failed": (*armature.CreateFailure)(nil)},
			http.StatusForbidden: errorEnvelope{}, http.StatusNotFound: errorEnvelope{}, http.StatusConflict: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}, http.StatusBadGateway: errorEnvelope{}}},

	// Armature's webhooks (#29): vouched for by their signature alone.
	{method: "POST", path: "/armature/webhook", handler: "handleArmatureWebhook", tag: "armature", public: true,
		summary: "Where Armature sends the installation's webhooks; the body is Armature's signed envelope.",
		request: json.RawMessage{}, responses: map[int]any{http.StatusNoContent: nil, http.StatusUnauthorized: errorEnvelope{},
			http.StatusRequestEntityTooLarge: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},

	// The followed Armature theme (#30).
	{method: "GET", path: "/armature/theme", handler: "handleGetArmatureTheme", tag: "armature",
		summary:   "Whether the caller follows their Armature theme, and why it is not shown when it is not.",
		responses: ok(env{"follow": armature.Follow{}})},
	{method: "PUT", path: "/armature/theme", handler: "handleFollowArmatureTheme", tag: "armature",
		summary:   "Follow the caller's active Armature theme, mirrored here and refreshed when it changes.",
		responses: map[int]any{http.StatusOK: env{"follow": armature.Follow{}}, http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/armature/theme", handler: "handleUnfollowArmatureTheme", tag: "armature",
		summary:   "Stop following the Armature theme and return to the default for everyone.",
		responses: none()},

	// Comments and mentions (#33), agreed in docs/api-contract-m3.md.
	{method: "GET", path: "/requirements/{reqKey}/comments", handler: "handleListComments", tag: "comments",
		summary: "A requirement's discussion: every thread, oldest first, each with its comments in order.",
		tool:    "list_comments", toolHelp: "The discussion on a requirement: each thread with its comments, their authors and the revision each was written on.",
		responses: ok(env{"threads": []comment.Thread{}})},
	{method: "POST", path: "/requirements/{reqKey}/comments", handler: "handleCreateComment", tag: "comments",
		summary: "Start a thread on a requirement; the comment records the revision it was written on and tells whoever it mentions.",
		tool:    "comment_on_requirement", toolHelp: "Start a thread on a requirement; the body is a rich text document of paragraphs, as the editor writes it.",
		request: comment.Input{}, responses: map[int]any{http.StatusCreated: env{"thread": comment.Thread{}}, http.StatusConflict: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/requirements/{reqKey}/mentionable", handler: "handleListMentionable", tag: "comments",
		summary: "The people a comment on the requirement may mention, and whether each may read it.",
		query: []param{
			{name: "q", description: "Words the name or email starts with."},
			{name: "limit", schema: intParam, description: "1 to " + strconv.Itoa(comment.MentionableMaxLimit) + "; " + strconv.Itoa(comment.MentionableDefaultLimit) + " when absent."},
		}, responses: ok(env{"people": []comment.Mentionable{}})},
	{method: "GET", path: "/comments/{commentID}", handler: "handleGetCommentThread", tag: "comments",
		summary:   "The whole thread a comment belongs to.",
		responses: ok(env{"thread": comment.Thread{}})},
	{method: "POST", path: "/comments/{commentID}/replies", handler: "handleReplyToComment", tag: "comments",
		summary: "Reply in the thread of a comment, at its end; a reply reopens a resolved thread.",
		request: comment.Input{}, responses: map[int]any{http.StatusCreated: env{"comment": comment.Comment{}, "thread": comment.Thread{}},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PATCH", path: "/comments/{commentID}", handler: "handleEditComment", tag: "comments",
		summary: "Rewrite a comment; its author's alone. People newly mentioned are told.",
		request: comment.Input{}, responses: map[int]any{http.StatusOK: env{"comment": comment.Comment{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/comments/{commentID}", handler: "handleDeleteComment", tag: "comments",
		summary:   "Delete a comment, leaving its place in the thread; its author's, or the project's administrators'.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusForbidden: errorEnvelope{}}},
	{method: "PUT", path: "/comment-threads/{threadID}/resolution", handler: "handleResolveThread", tag: "comments",
		summary:   "Mark a thread resolved; anybody who may comment on the requirement.",
		responses: map[int]any{http.StatusOK: env{"thread": comment.Thread{}}, http.StatusForbidden: errorEnvelope{}}},
	{method: "DELETE", path: "/comment-threads/{threadID}/resolution", handler: "handleReopenThread", tag: "comments",
		summary:   "Reopen a resolved thread.",
		responses: map[int]any{http.StatusOK: env{"thread": comment.Thread{}}, http.StatusForbidden: errorEnvelope{}}},

	// Watching and notifications (#34).
	{method: "PUT", path: "/requirements/{reqKey}/watch", handler: "handleWatchRequirement", tag: "watching",
		summary:   "Follow a requirement: its revisions, transitions, suspect links, reviews and comments.",
		responses: ok(env{"watching": watch.Watching{}})},
	{method: "DELETE", path: "/requirements/{reqKey}/watch", handler: "handleUnwatchRequirement", tag: "watching",
		summary:   "Stop following a requirement, and stop being made to follow it again by editing it.",
		responses: none()},
	{method: "GET", path: "/requirements/{reqKey}/watchers", handler: "handleListWatchers", tag: "watching",
		summary: "Who follows a requirement, by itself or through its project.",
		query:   windowQuery(watch.DefaultLimit, watch.MaxLimit), responses: ok(env{"watchers": []watch.Watcher{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "PUT", path: "/projects/{projectKey}/watch", handler: "handleWatchProject", tag: "watching",
		summary:   "Follow every requirement of a project.",
		responses: ok(env{"watch": watch.Watch{}})},
	{method: "DELETE", path: "/projects/{projectKey}/watch", handler: "handleUnwatchProject", tag: "watching",
		summary:   "Stop following a project.",
		responses: none()},
	{method: "GET", path: "/watches", handler: "handleListWatches", tag: "watching",
		summary: "What the caller follows, newest first.",
		query:   windowQuery(watch.DefaultLimit, watch.MaxLimit), responses: ok(env{"watches": []watch.Watch{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "GET", path: "/notifications", handler: "handleListNotifications", tag: "notifications",
		summary: "What the caller was told, newest first.",
		query: append([]param{{name: "unread", schema: &openapi.Schema{Type: "boolean"}, description: "true for the unread ones alone."}},
			windowQuery(notify.DefaultLimit, notify.MaxLimit)...),
		responses: ok(env{"notifications": []notify.Notification{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "GET", path: "/notifications/unread-count", handler: "handleCountUnreadNotifications", tag: "notifications",
		summary:   "How many of the caller's notifications are unread, which the bell asks every half minute.",
		responses: ok(env{"unread": 0})},
	{method: "POST", path: "/notifications/read", handler: "handleMarkNotificationsRead", tag: "notifications",
		summary: "Mark the caller's notifications read: those named, or all; ids of anybody else's are ignored.",
		request: notify.ReadInput{}, responses: none()},
	{method: "GET", path: "/notification-preferences", handler: "handleGetNotificationPreferences", tag: "notifications",
		summary:   "How the caller wants to be told: each kind in the app and by mail, the digest, and following what they edit.",
		responses: ok(env{"preferences": notify.Preferences{}})},
	{method: "PUT", path: "/notification-preferences", handler: "handleSaveNotificationPreferences", tag: "notifications",
		summary: "Change how the caller wants to be told.",
		request: notify.Preferences{}, responses: map[int]any{http.StatusOK: env{"preferences": notify.Preferences{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},

	// Review and approval of requirement revisions (#35).
	{method: "GET", path: "/projects/{projectKey}/approval-rules", handler: "handleListApprovalRules", tag: "reviews",
		summary:   "Who approves the requirements of each type in a project, and how many must.",
		responses: ok(env{"rules": []review.Rule{}})},
	{method: "PUT", path: "/projects/{projectKey}/approval-rules", handler: "handleSaveApprovalRule", tag: "reviews",
		summary: "Set the approval rule of one type, or of every type without its own; the project's administrators'. Reviews already asked for keep theirs.",
		request: review.RuleInput{}, responses: map[int]any{http.StatusOK: env{"rule": review.Rule{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/approval-rules/{ruleID}", handler: "handleDeleteApprovalRule", tag: "reviews",
		summary:   "Remove an approval rule; the project's administrators'.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusForbidden: errorEnvelope{}}},
	{method: "GET", path: "/requirements/{reqKey}/approval", handler: "handleGetApproval", tag: "reviews",
		summary:   "A requirement's review now open, and the latest revision approved, which counts only while it is the current one.",
		responses: ok(env{"approval": review.Approval{}})},
	{method: "GET", path: "/requirements/{reqKey}/reviews", handler: "handleListReviews", tag: "reviews",
		summary: "A requirement's reviews, newest first.",
		query:   windowQuery(review.DefaultLimit, review.MaxLimit), responses: ok(env{"reviews": []review.Review{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "POST", path: "/requirements/{reqKey}/reviews", handler: "handleRequestReview", tag: "reviews",
		summary: "Ask for the current revision to be approved: the review pins it and freezes the approvers its type's rule names.",
		request: review.RequestInput{}, responses: map[int]any{http.StatusCreated: env{"review": review.Review{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/reviews/waiting", handler: "handleListWaitingReviews", tag: "reviews",
		summary: "The reviews waiting for the caller's decision, oldest first, across every project.",
		query:   windowQuery(review.DefaultLimit, review.MaxLimit), responses: ok(env{"reviews": []review.Review{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "GET", path: "/reviews/{reviewID}", handler: "handleGetReview", tag: "reviews",
		summary:   "One review with every approver's decision.",
		responses: ok(env{"review": review.Review{}})},
	{method: "POST", path: "/reviews/{reviewID}/decisions", handler: "handleDecideReview", tag: "reviews",
		summary: "Approve or reject the revision under review, naming it as read; a decision on another revision is refused.",
		request: review.DecisionInput{}, responses: map[int]any{http.StatusOK: env{"review": review.Review{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "POST", path: "/reviews/{reviewID}/withdraw", handler: "handleWithdrawReview", tag: "reviews",
		summary:   "Withdraw a pending review; whoever asked for it, or the project's administrators.",
		responses: map[int]any{http.StatusOK: env{"review": review.Review{}}, http.StatusForbidden: errorEnvelope{}, http.StatusConflict: errorEnvelope{}}},

	// Suspect links (#36).
	{method: "GET", path: "/projects/{projectKey}/suspects", handler: "handleListSuspectLinks", tag: "links",
		summary:   "The suspect links touching a project's requirements, longest waiting first.",
		query:     windowQuery(trace.SuspectDefaultLimit, trace.SuspectMaxLimit),
		responses: ok(env{"links": []trace.SuspectLink{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "POST", path: "/links/{linkID}/clear", handler: "handleClearLink", tag: "links",
		summary: "Confirm a link against its ends as read, with a comment; whoever may edit either end.",
		request: trace.ClearInput{}, responses: map[int]any{http.StatusOK: env{"link": trace.Link{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/links/{linkID}/clearances", handler: "handleListClearances", tag: "links",
		summary:   "Every confirmation of a link, newest first.",
		responses: ok(env{"clearances": []trace.Clearance{}})},
	{method: "POST", path: "/armature-links/{linkID}/clear", handler: "handleClearArmatureLink", tag: "armature",
		summary: "Confirm a trace to an Armature issue against the requirement's revision as read, with a comment.",
		request: armature.ClearInput{}, responses: map[int]any{http.StatusOK: env{"link": armature.Link{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},

	// Baselines (#37).
	{method: "GET", path: "/projects/{projectKey}/baselines", handler: "handleListBaselines", tag: "baselines",
		summary:   "A project's baselines, newest first.",
		responses: ok(env{"baselines": []baseline.Baseline{}})},
	{method: "POST", path: "/projects/{projectKey}/baselines", handler: "handleTakeBaseline", tag: "baselines",
		summary: "Take a baseline of the project, or of one section's subtree, as one consistent snapshot; the project's administrators'.",
		request: baseline.Input{}, responses: map[int]any{http.StatusCreated: env{"baseline": baseline.Baseline{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/baselines/{baselineID}", handler: "handleGetBaseline", tag: "baselines",
		summary:   "One baseline.",
		responses: ok(env{"baseline": baseline.Baseline{}})},
	{method: "PATCH", path: "/baselines/{baselineID}", handler: "handleUpdateBaseline", tag: "baselines",
		summary: "Rename a baseline, or lock it for good; the project's administrators'. Its items never change.",
		request: baseline.UpdateInput{}, responses: map[int]any{http.StatusOK: env{"baseline": baseline.Baseline{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/baselines/{baselineID}", handler: "handleDeleteBaseline", tag: "baselines",
		summary:   "Delete a baseline that is not locked; the project's administrators'.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusForbidden: errorEnvelope{}, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/baselines/{baselineID}/spec", handler: "handleBaselineSpec", tag: "baselines",
		summary: "A baseline read as a specification, each requirement at the revision it holds, in its outline.",
		query:   windowQuery(baseline.DefaultLimit, baseline.MaxLimit), responses: ok(env{"entries": []baseline.Entry{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "GET", path: "/baselines/{baselineID}/compare", handler: "handleCompareBaseline", tag: "baselines",
		summary:   "What differs between a baseline and another of the project, or now: requirements added, removed, revised, moved and transitioned, and links added and removed.",
		query:     []param{{name: "with", description: "Another baseline's id, or now, the default."}},
		responses: map[int]any{http.StatusOK: env{"comparison": baseline.Comparison{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},

	// Coverage and the traceability matrix (#38).
	{method: "GET", path: "/projects/{projectKey}/coverage-rules", handler: "handleListCoverageRules", tag: "coverage",
		summary:   "Which coverage dimensions apply to the requirements of each type in a project.",
		responses: ok(env{"rules": []trace.CoverageRule{}})},
	{method: "PUT", path: "/projects/{projectKey}/coverage-rules", handler: "handleSaveCoverageRule", tag: "coverage",
		summary: "Set the coverage rule of one type, or of every type without its own; the project's administrators'.",
		request: trace.CoverageRuleInput{}, responses: map[int]any{http.StatusOK: env{"rule": trace.CoverageRule{}}, http.StatusForbidden: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/coverage-rules/{ruleID}", handler: "handleDeleteCoverageRule", tag: "coverage",
		summary:   "Remove a coverage rule; the project's administrators'.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusForbidden: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/coverage", handler: "handleProjectCoverage", tag: "coverage",
		summary: "How far a project's requirements, narrowed like the grid, are decomposed, implemented and verified: the totals, and each requirement.",
		query: append(append(append([]param{}, gridQuery...), levelQuery...),
			windowQuery(trace.CoverageDefaultLimit, trace.CoverageMaxLimit)...),
		responses: map[int]any{http.StatusOK: env{"summary": trace.CoverageSummary{}, "rows": []trace.CoverageRow{}, "total": 0, "limit": 0, "offset": 0},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/matrix", handler: "handleTraceMatrix", tag: "coverage",
		summary:   "A traceability matrix: a page of the project's requirements, narrowed like the grid, against the requirements or Armature issues they link to.",
		query:     append(append([]param{}, gridQuery...), append(matrixQuery, windowQuery(trace.MatrixDefaultLimit, trace.MatrixMaxLimit)...)...),
		responses: map[int]any{http.StatusOK: env{"matrix": trace.Matrix{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/projects/{projectKey}/matrix.csv", handler: "handleTraceMatrixCSV", tag: "coverage",
		summary: "The traceability matrix as a CSV file, every row the filter keeps.",
		query:   append(append([]param{}, gridQuery...), matrixQuery...),
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusUnprocessableEntity: errorEnvelope{}}},
}

// levelQuery keeps a coverage report's rows at a level in each dimension; the totals count every requirement.
var levelQuery = []param{
	{name: "decomposed", schema: &openapi.Schema{Type: "string", Enum: enumStrings(trace.Levels)}, description: "Only the requirements decomposed this far."},
	{name: "implemented", schema: &openapi.Schema{Type: "string", Enum: enumStrings(trace.Levels)}, description: "Only the requirements implemented this far."},
	{name: "verified", schema: &openapi.Schema{Type: "string", Enum: enumStrings(trace.Levels)}, description: "Only the requirements verified this far."},
}

// matrixQuery says what a traceability matrix's columns are.
var matrixQuery = []param{
	{name: "columns", schema: &openapi.Schema{Type: "string", Enum: enumStrings(trace.MatrixColumnKinds)}, description: "requirements, the default, or issues."},
	{name: "columnProject", description: "For requirement columns, the project they come from; the same project when absent."},
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
	{name: "suspect", schema: &openapi.Schema{Type: "boolean"}, description: "true to keep only requirements with a suspect link."},
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
