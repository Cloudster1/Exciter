import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { NOTIFICATION_PANEL_SIZE, NOTIFICATIONS_PAGE_SIZE, UNREAD_POLL_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

// What the caller was told and how they want to be told (#34), after Stator's api/notifications.ts.

type Wire = components["schemas"];

/** One thing the caller was told; the client words it from its kind, actor and requirement. */
export type Notification = Wire["Notification"];
export type NotificationKind = Notification["kind"];
/** How the caller wants to be told; a kind a switch map leaves out is on. */
export type Preferences = Wire["NotificationPreferences"];
export type Digest = Preferences["digest"];

// Keyed by the schema's kinds, so a kind the API adds and this leaves out fails the type check.
const everyKind = {
  mentioned: true,
  review_requested: true,
  reviewed: true,
  suspect: true,
  source_revised: true,
  replied: true,
  commented: true,
  resolved: true,
  revised: true,
  transitioned: true,
  session_invited: true,
  session_round: true,
  session_reminder: true,
  session_closed: true,
  code_orphaned: true,
  code_outdated: true,
  term_changed: true,
  execution_assigned: true,
  execution_reminder: true,
  execution_closed: true,
} satisfies Record<NotificationKind, true>;

/** Every kind, in the order the preferences list them, as the API's notify.Kinds. */
export const NOTIFICATION_KINDS = Object.keys(everyKind) as NotificationKind[];
export const DIGESTS: Digest[] = ["off", "hourly", "daily"];

/** Whether a switch map has a kind on; a kind it leaves out is on. */
export function switchedOn(switches: Preferences["inApp"], kind: NotificationKind): boolean {
  return switches[kind] !== false;
}

export const notificationsQueryKey = ["notifications"] as const;
const unreadQueryKey = [...notificationsQueryKey, "unread"] as const;
const preferencesQueryKey = ["notification-preferences"] as const;

/** How many are unread, asked every UNREAD_POLL_MS and whenever the window regains focus. */
export function useUnreadCount() {
  return useQuery({
    queryKey: unreadQueryKey,
    queryFn: async (): Promise<number> => (await api.GET("/notifications/unread-count")).data!.unread,
    refetchInterval: UNREAD_POLL_MS,
    refetchOnWindowFocus: "always",
  });
}

/** The latest notifications, read when the bell's panel opens. */
export function useLatestNotifications(enabled: boolean) {
  return useQuery({
    queryKey: [...notificationsQueryKey, "latest"],
    queryFn: async () => (await api.GET("/notifications", { params: { query: { limit: NOTIFICATION_PANEL_SIZE } } })).data!,
    enabled,
    staleTime: 0,
  });
}

/** A page of everything the caller was told, or of the unread alone, the latest first. */
export function useNotifications(unread: boolean, offset: number) {
  return useQuery({
    queryKey: [...notificationsQueryKey, "page", { unread, offset }],
    queryFn: async () =>
      (await api.GET("/notifications", { params: { query: { unread: unread || undefined, limit: NOTIFICATIONS_PAGE_SIZE, offset: offset || undefined } } }))
        .data!,
    placeholderData: keepPreviousData,
  });
}

/** Marks the named notifications read, or all of them, and the badge with them. */
export function useMarkRead() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (which: { ids: string[] } | { all: true }) => {
      await api.POST("/notifications/read", { body: which });
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: notificationsQueryKey }),
  });
}

export function usePreferences() {
  return useQuery({
    queryKey: preferencesQueryKey,
    queryFn: async (): Promise<Preferences> => (await api.GET("/notification-preferences")).data!.preferences,
  });
}

export function useSavePreferences() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (preferences: Preferences): Promise<Preferences> => (await api.PUT("/notification-preferences", { body: preferences })).data!.preferences,
    onSuccess: (saved) => queryClient.setQueryData(preferencesQueryKey, saved),
  });
}
