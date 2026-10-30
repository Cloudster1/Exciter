import type { Assignment, Group, PermissionDescription, RoleDescription } from "@/api/access";
import { aProject } from "./projects";

/** The ids the access fixtures share, so a grant, a group and its members name each other. */
export const accessIds = {
  everyone: "0195f000-0000-7000-8000-00000000e0e0",
  reviewers: "0195f000-0000-7000-8000-0000000000a1",
  staff: "0195f000-0000-7000-8000-0000000000a2",
  ada: "0195f000-0000-7000-8000-00000000000a",
  bob: "0195f000-0000-7000-8000-00000000b0b0",
};

/** Every permission Exciter checks, as the API lists them. */
export const permissionList: PermissionDescription[] = [
  { permission: "view", words: "View the project and its requirements", scope: "project" },
  { permission: "comment", words: "Comment on requirements", scope: "project" },
  { permission: "edit", words: "Write, change, link and move requirements", scope: "project" },
  { permission: "approve", words: "Approve what is up for review", scope: "project" },
  { permission: "administer", words: "Administer the project", scope: "project" },
  { permission: "createProjects", words: "Create projects", scope: "installation" },
  { permission: "administerInstallation", words: "Administer Exciter", scope: "installation" },
];

const builtIn = (role: string, name: string, held: RoleDescription["permissions"], orgWideOnly = false): RoleDescription => ({
  role,
  name,
  description: "",
  orgWideOnly,
  builtin: true,
  permissions: held,
  inUse: 1,
});

/** The built-in roles, and one of the installation's own. */
export const roleList: RoleDescription[] = [
  builtIn("administrator", "Administrator", ["view", "comment", "edit", "approve", "administer", "createProjects", "administerInstallation"], true),
  builtIn("admin", "Project administrator", ["view", "comment", "edit", "approve", "administer"]),
  builtIn("editor", "Editor", ["view", "comment", "edit"]),
  builtIn("reviewer", "Reviewer", ["view", "comment", "approve"]),
  builtIn("viewer", "Viewer", ["view"]),
  { role: "auditor", name: "Auditor", description: "Reads for the audit.", orgWideOnly: false, builtin: false, permissions: ["view"], inUse: 2 },
];

const checkout = aProject();

/** A grant across the installation to a person, one to Everyone, and one in a project to a group. */
export const grantList: Assignment[] = [
  { id: "g-1", role: "administrator", roleName: "Administrator", userId: accessIds.ada, userName: "Ada Lovelace", createdAt: "2026-10-01T08:00:00Z" },
  {
    id: "g-2",
    role: "viewer",
    roleName: "Viewer",
    groupId: accessIds.everyone,
    groupName: "Everyone",
    groupBuiltin: "everyone",
    createdAt: "2026-10-01T08:00:00Z",
  },
  {
    id: "g-3",
    role: "editor",
    roleName: "Editor",
    projectId: checkout.id,
    projectKey: checkout.key,
    projectName: checkout.name,
    groupId: accessIds.reviewers,
    groupName: "Reviewers",
    createdAt: "2026-10-01T08:00:00Z",
  },
];

/** Everyone, a group kept by hand and one the provider fills. */
export const groupList: Group[] = [
  {
    id: accessIds.everyone,
    name: "Everyone",
    description: "",
    source: "local",
    externalRef: "",
    builtin: "everyone",
    memberCount: 9,
    roleCount: 1,
    fromProvider: false,
  },
  { id: accessIds.reviewers, name: "Reviewers", description: "", source: "local", externalRef: "", memberCount: 1, roleCount: 1, fromProvider: false },
  { id: accessIds.staff, name: "Staff", description: "", source: "oidc", externalRef: "staff", memberCount: 3, roleCount: 0, fromProvider: true },
];

/** A page of grants as the API answers it. */
export const grantPage = (assignments: Assignment[]) => ({ assignments, total: assignments.length, limit: 50, offset: 0 });

/** What the Access page reads, answered from the fixtures. */
export const accessAnswers = {
  "GET /roles": { status: 200, body: { roles: roleList } },
  "GET /permissions": { status: 200, body: { permissions: permissionList } },
  "GET /role-assignments": { status: 200, body: grantPage(grantList) },
  "GET /groups": { status: 200, body: { groups: groupList } },
  [`GET /groups/${accessIds.reviewers}`]: {
    status: 200,
    body: { group: { ...groupList[1], members: [{ id: accessIds.ada, name: "Ada Lovelace", email: "ada@exciter.test" }] } },
  },
  [`GET /groups/${accessIds.staff}`]: { status: 200, body: { group: { ...groupList[2], members: [] } } },
};
