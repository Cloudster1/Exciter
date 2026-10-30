import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { api } from "./client";
import { membersQueryKey } from "./auth";
import { projectAccessQueryKey } from "./permissions";
import { projectsQueryKey } from "./projects";
import type { components } from "./schema";
import { ASSIGNMENT_PAGE_SIZE, MAX_PICKER_LIMIT } from "@/config";
import { t } from "@/i18n";

type Wire = components["schemas"];

/** One of the installation's roles: what it is called and the permissions it holds outright. */
export type RoleDescription = Wire["Role"];
/** A permission a role may hold, in words, and where it counts. */
export type PermissionDescription = Wire["PermissionView"];
export type Permission = PermissionDescription["permission"];
/** A group of people, kept by hand, filled by the provider, or Everyone. */
export type Group = Wire["Group"];
/** One role held by one person or group, in one project or across the installation. */
export type Assignment = Wire["Assignment"];
export type AssignmentPage = Wire["AssignmentPage"];

/** The built-in roles' keys, which never change, whatever an administrator names them. */
export const ADMINISTRATOR_ROLE = "administrator";
export const VIEWER_ROLE = "viewer";

const roles = ["access", "roles"] as const;
const permissions = ["access", "permissions"] as const;
const assignments = ["access", "assignments"] as const;
export const groupsQueryKey = ["groups"] as const;

/** A group's name as the page shows it; Everyone is named in the reader's language. */
export function groupLabel(group: { name?: string; groupName?: string; builtin?: string; groupBuiltin?: string | null }): string {
  return group.builtin === "everyone" || group.groupBuiltin === "everyone" ? t.permissions.everyone : (group.name ?? group.groupName ?? "");
}

/** The name a grant's subject goes by on the page: Everyone in the reader's language, a group's or a person's name. */
export function grantSubject(grant: Assignment): string {
  return grant.groupId ? groupLabel(grant) : grant.userName || grant.userEmail || "";
}

/** What a permission lets somebody do, in the reader's language, the server's words else. */
export function permissionWords(permission: PermissionDescription | Permission): string {
  const key = typeof permission === "string" ? permission : permission.permission;
  return t.accessAdmin.permissionWords[key] ?? (typeof permission === "string" ? permission : permission.words);
}

export function useRoles() {
  return useQuery({
    queryKey: roles,
    queryFn: async (): Promise<RoleDescription[]> => (await api.GET("/roles")).data!.roles,
  });
}

export function usePermissions() {
  return useQuery({
    queryKey: permissions,
    queryFn: async (): Promise<PermissionDescription[]> => (await api.GET("/permissions")).data!.permissions,
    staleTime: Infinity,
  });
}

// What a role holds decides what everybody holding it may do, so every
// answer about access may have changed.
function useAfterAccessChange() {
  const queryClient = useQueryClient();
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: ["access"] }),
      queryClient.invalidateQueries({ queryKey: projectAccessQueryKey }),
      queryClient.invalidateQueries({ queryKey: groupsQueryKey }),
      queryClient.invalidateQueries({ queryKey: membersQueryKey }),
      queryClient.invalidateQueries({ queryKey: projectsQueryKey }),
    ]);
}

export function useCreateRole() {
  const settled = useAfterAccessChange();
  return useMutation({
    mutationFn: async (body: Wire["RoleInput"]): Promise<RoleDescription> => (await api.POST("/roles", { body })).data!.role,
    onSettled: settled,
  });
}

export function useUpdateRole() {
  const settled = useAfterAccessChange();
  return useMutation({
    mutationFn: async ({ role, ...body }: Wire["RoleInput"] & { role: string }): Promise<RoleDescription> =>
      (await api.PATCH("/roles/{roleKey}", { params: { path: { roleKey: role } }, body })).data!.role,
    onSettled: settled,
  });
}

export function useDeleteRole() {
  const settled = useAfterAccessChange();
  return useMutation({
    mutationFn: async (role: string) => {
      await api.DELETE("/roles/{roleKey}", { params: { path: { roleKey: role } } });
    },
    onSettled: settled,
  });
}

/** Every group, Everyone first. */
export function useGroups() {
  return useQuery({
    queryKey: [...groupsQueryKey, "all"],
    queryFn: async (): Promise<Group[]> => (await api.GET("/groups", { params: { query: { limit: MAX_PICKER_LIMIT } } })).data!.groups,
  });
}

/** One group with its members; for administrators. */
export function useGroup(id: string) {
  return useQuery({
    queryKey: [...groupsQueryKey, "one", id],
    queryFn: async (): Promise<Group> => (await api.GET("/groups/{groupID}", { params: { path: { groupID: id } } })).data!.group,
    enabled: Boolean(id),
  });
}

export function useCreateGroup() {
  const settled = useAfterAccessChange();
  return useMutation({
    mutationFn: async (body: Wire["GroupInput"]): Promise<Group> => (await api.POST("/groups", { body })).data!.group,
    onSettled: settled,
  });
}

export function useUpdateGroup() {
  const settled = useAfterAccessChange();
  return useMutation({
    mutationFn: async ({ id, ...body }: Wire["GroupInput"] & { id: string }): Promise<Group> =>
      (await api.PATCH("/groups/{groupID}", { params: { path: { groupID: id } }, body })).data!.group,
    onSettled: settled,
  });
}

export function useDeleteGroup() {
  const settled = useAfterAccessChange();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/groups/{groupID}", { params: { path: { groupID: id } } });
    },
    onSettled: settled,
  });
}

export function useAddToGroup() {
  const settled = useAfterAccessChange();
  return useMutation({
    mutationFn: async ({ id, userId }: { id: string; userId: string }): Promise<Group> =>
      (await api.POST("/groups/{groupID}/members", { params: { path: { groupID: id } }, body: { userId } })).data!.group,
    onSettled: settled,
  });
}

export function useRemoveFromGroup() {
  const settled = useAfterAccessChange();
  return useMutation({
    mutationFn: async ({ id, userId }: { id: string; userId: string }): Promise<Group> =>
      (await api.DELETE("/groups/{groupID}/members/{userID}", { params: { path: { groupID: id, userID: userId } } })).data!.group,
    onSettled: settled,
  });
}

export interface AssignmentParams {
  /** Only the grants in this project, by key; else every grant the caller may read. */
  project?: string;
  q?: string;
  role?: string;
  offset?: number;
}

/** A page of who holds what; a page stays on screen while the next loads. */
export function useAssignments({ project = "", q = "", role = "", offset = 0 }: AssignmentParams = {}, enabled = true) {
  const text = q.trim();
  return useQuery({
    queryKey: [...assignments, { project: project.toUpperCase(), q: text, role, offset }],
    queryFn: async (): Promise<AssignmentPage> =>
      (
        await api.GET("/role-assignments", {
          params: {
            query: {
              limit: ASSIGNMENT_PAGE_SIZE,
              offset: offset || undefined,
              project: project || undefined,
              q: text || undefined,
              role: role || undefined,
            },
          },
        })
      ).data!,
    placeholderData: keepPreviousData,
    enabled,
  });
}

/** Grants a role to a person or a group, in a project by its key or, without one, across the installation. */
export function useGrantRole() {
  const settled = useAfterAccessChange();
  return useMutation({
    mutationFn: async (body: { role: string; projectKey?: string; userId?: string; groupId?: string }): Promise<Assignment> =>
      (
        await api.POST("/role-assignments", {
          body: { role: body.role, projectKey: body.projectKey ?? "", userId: body.userId ?? null, groupId: body.groupId ?? null },
        })
      ).data!.assignment,
    onSettled: settled,
  });
}

export function useRevokeRole() {
  const settled = useAfterAccessChange();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/role-assignments/{assignmentID}", { params: { path: { assignmentID: id } } });
    },
    onSettled: settled,
  });
}
