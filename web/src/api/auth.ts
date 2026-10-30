import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { API_BASE, ME_STALE_MS } from "@/config";
import { applyLanguage, type LanguageChoice } from "@/i18n";
import { applyTimeZone } from "@/lib/format";
import { HANDLES_UNAUTHORIZED } from "@/lib/session";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** Who is signed in, the installation and their role in it. */
export type Me = Wire["MeResponse"];
export type OrgRole = Me["role"];
export type Membership = Wire["Membership"];
export type Member = Wire["Member"];
export type CreatedMember = Wire["CreatedMember"];
/** The roles an administrator hands out; nobody is made an owner from here. */
export type AssignableRole = Exclude<OrgRole, "owner">;
export const ASSIGNABLE_ROLES: readonly AssignableRole[] = ["admin", "member", "guest"];
export type Provider = Wire["Provider"];
export type ProviderView = Wire["ProviderView"];
export type SaveProviderInput = Wire["SaveOIDCProviderRequest"];
export type JoinRequest = Wire["JoinRequest"];
export type GroupRole = Wire["GroupRole"];
/** The roles a mapped group or an answered request gives; ownership is never handed out. */
export type GrantedRole = GroupRole["role"];

export const meQueryKey = ["auth", "me"] as const;
export const membersQueryKey = ["users", "members"] as const;
export const providerQueryKey = ["oidc-provider"] as const;
export const groupRolesQueryKey = ["oidc-provider", "group-roles"] as const;
export const joinRequestsQueryKey = ["users", "requests"] as const;

/** The session query, never retried: a 401 is a definite answer, which the route guards act on themselves. */
export const meQuery = {
  queryKey: meQueryKey,
  queryFn: async (): Promise<Me> => (await api.GET("/auth/me")).data!,
  staleTime: ME_STALE_MS,
  retry: false,
  meta: HANDLES_UNAUTHORIZED,
} as const;

export function useMe() {
  return useQuery(meQuery);
}

/** Follows the signed-in person's language and time zone whenever the app has asked who they are; it never asks itself. */
export function useProfileLanguage(): void {
  const { data } = useQuery({ ...meQuery, enabled: false });
  const choice = data?.user.locale;
  const zone = data?.user.timezone;
  useEffect(() => {
    if (choice !== undefined) applyLanguage(choice);
  }, [choice]);
  useEffect(() => {
    if (zone !== undefined) applyTimeZone(zone);
  }, [zone]);
}

/** How the caller reads the interface: its language and the zone of its times; empty follows the browser. */
export interface ReadingChoice {
  locale: LanguageChoice;
  timezone: string;
}

/** Saves the caller's language and time zone together, as the profile's Save does. */
export function useSaveReading() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (choice: ReadingChoice): Promise<Me> => (await api.PATCH("/auth/me", { body: choice })).data!,
    onSuccess: (me) => queryClient.setQueryData(meQueryKey, me),
  });
}

/** Whether a role may change the installation's settings. */
export function administers(role: OrgRole | undefined): boolean {
  return role === "owner" || role === "admin";
}

/** Where the browser goes to sign in through the installation's provider; a navigation, since it follows redirects. */
export function ssoStartURL(next?: string): string {
  const start = `${API_BASE}/auth/oidc/start`;
  return next ? `${start}?next=${encodeURIComponent(next)}` : start;
}

/** Where a signed-in person signs in again at the identity provider, for one signature, coming back to next (#46). */
export function reauthStartURL(next: string): string {
  return `${API_BASE}/auth/oidc/reauthenticate?next=${encodeURIComponent(next)}`;
}

export function useLogin() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: { email: string; password: string }): Promise<Me> => (await api.POST("/auth/login", { body })).data!,
    onSuccess: (me) => queryClient.setQueryData(meQueryKey, me),
    // A 401 here is a wrong password, which the form shows.
    meta: HANDLES_UNAUTHORIZED,
  });
}

export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.POST("/auth/logout");
    },
    // Whatever was cached belonged to the person leaving.
    onSettled: () => queryClient.clear(),
  });
}

/** The installation's people and their roles; asked only of administrators. */
export function useMembers() {
  return useQuery({
    queryKey: membersQueryKey,
    queryFn: async (): Promise<Member[]> => (await api.GET("/users")).data!.members,
  });
}

/** Adds a person with a password; a password left out is made and comes back once. */
export function useCreateMember() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: { email: string; name: string; role: AssignableRole; password: string }): Promise<CreatedMember> =>
      (await api.POST("/users", { body })).data!.member,
    onSettled: () => queryClient.invalidateQueries({ queryKey: membersQueryKey }),
  });
}

export function useSetMemberRole() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ userId, role }: { userId: string; role: AssignableRole }): Promise<Member> =>
      (await api.PATCH("/users/{userID}", { params: { path: { userID: userId } }, body: { role } })).data!.member,
    onSettled: () => queryClient.invalidateQueries({ queryKey: membersQueryKey }),
  });
}

/** Switches a member's account off or on; off signs them out everywhere and ends their tokens. */
export function useSetMemberActive() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ userId, active }: { userId: string; active: boolean }): Promise<Member> =>
      (await api.PATCH("/users/{userID}", { params: { path: { userID: userId } }, body: { active } })).data!.member,
    onSettled: () => queryClient.invalidateQueries({ queryKey: membersQueryKey }),
  });
}

/** The installation's identity provider and the callback address to register with it; asked only of administrators. */
export function useProvider() {
  return useQuery({
    queryKey: providerQueryKey,
    queryFn: async (): Promise<ProviderView> => (await api.GET("/oidc-provider")).data!,
  });
}

export function useSaveProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: SaveProviderInput): Promise<ProviderView> => (await api.PUT("/oidc-provider", { body })).data!,
    onSuccess: (view) => queryClient.setQueryData(providerQueryKey, view),
  });
}

/** Which provider groups grant which role here. */
export function useGroupRoles() {
  return useQuery({
    queryKey: groupRolesQueryKey,
    queryFn: async (): Promise<GroupRole[]> => (await api.GET("/oidc-provider/group-roles")).data!.groupRoles,
  });
}

export function useSetGroupRole() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: { group: string; role: GrantedRole }): Promise<GroupRole> =>
      (await api.POST("/oidc-provider/group-roles", { body })).data!.groupRole,
    onSettled: () => queryClient.invalidateQueries({ queryKey: groupRolesQueryKey }),
  });
}

export function useRemoveGroupRole() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id }: { id: string }) => {
      await api.DELETE("/oidc-provider/group-roles/{groupRoleID}", { params: { path: { groupRoleID: id } } });
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: groupRolesQueryKey }),
  });
}

/** Who signed in through the provider and waits to be let in; asked only of administrators. */
export function useJoinRequests() {
  return useQuery({
    queryKey: joinRequestsQueryKey,
    queryFn: async (): Promise<JoinRequest[]> => (await api.GET("/users/requests")).data!.requests,
  });
}

export function useAdmitJoinRequest() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ userId, role }: { userId: string; role: GrantedRole }) =>
      (await api.POST("/users/requests/{userID}/admit", { params: { path: { userID: userId } }, body: { role } })).data!.membership,
    onSettled: () =>
      Promise.all([queryClient.invalidateQueries({ queryKey: joinRequestsQueryKey }), queryClient.invalidateQueries({ queryKey: membersQueryKey })]),
  });
}

export function useDeclineJoinRequest() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ userId }: { userId: string }) => {
      await api.DELETE("/users/requests/{userID}", { params: { path: { userID: userId } } });
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: joinRequestsQueryKey }),
  });
}
