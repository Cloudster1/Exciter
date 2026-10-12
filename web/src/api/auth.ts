import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ME_STALE_MS } from "@/config";
import { applyLanguage, type LanguageChoice } from "@/i18n";
import { HANDLES_UNAUTHORIZED } from "@/lib/session";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** Who is signed in, where they act now, and where else they may. */
export type Me = Wire["MeResponse"];
export type OrgRole = Wire["CurrentOrg"]["role"];
export type Membership = Wire["Membership"];
export type Member = Wire["Member"];
export type CreatedMember = Wire["CreatedMember"];
/** The roles an administrator hands out; nobody is made an owner from here. */
export type AssignableRole = Exclude<OrgRole, "owner">;
export const ASSIGNABLE_ROLES: readonly AssignableRole[] = ["admin", "member", "guest"];

export const meQueryKey = ["auth", "me"] as const;
export const membersQueryKey = ["users", "members"] as const;

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

/** Follows the signed-in person's language whenever the app has asked who they are; it never asks itself. */
export function useProfileLanguage(): void {
  const { data } = useQuery({ ...meQuery, enabled: false });
  const choice = data?.user.locale;
  useEffect(() => {
    if (choice !== undefined) applyLanguage(choice);
  }, [choice]);
}

/** Changes the caller's interface language; an empty choice follows the browser. */
export function useSetLanguage() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (locale: LanguageChoice): Promise<Me> => (await api.PATCH("/auth/me", { body: { locale } })).data!,
    onSuccess: (me) => queryClient.setQueryData(meQueryKey, me),
  });
}

/** Whether a role may change the organization's settings. */
export function administers(role: OrgRole | undefined): boolean {
  return role === "owner" || role === "admin";
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

/** Moves the session to another of the person's organizations; everything cached was the last one's. */
export function useSwitchOrg() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (slug: string) => (await api.POST("/auth/switch-org", { body: { slug } })).data!.organization,
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

/** The organization's members and their roles; asked only of administrators. */
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

export function useRemoveMember() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ userId }: { userId: string }) => {
      await api.DELETE("/users/{userID}", { params: { path: { userID: userId } } });
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: membersQueryKey }),
  });
}
