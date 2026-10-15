import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { STATOR_LOOKUP_MAX_PAGES, STATOR_SEARCH_LIMIT } from "@/config";
import { api } from "./client";
import { refreshSuspects } from "./links";
import type { components } from "./schema";

// The Stator client, as the Armature one; each issue of M4 adds what it reads.

type Wire = components["schemas"];

/** The installation's Stator instance, as its administrators see it; the webhook secret never comes back. */
export type StatorConnection = Wire["StatorConnection"];
export type StatorConnectionInput = Wire["StatorConnectionInput"];
/** The caller's own link to Stator; the token never comes back. */
export type StatorAccount = Wire["StatorAccount"];
export type StatorStatus = StatorAccount["status"];
/** A page as the caller may read it in Stator, and one the picker found. */
export type StatorPage = Wire["StatorPage"];
export type StatorPageHit = Wire["StatorPageHit"];
/** A requirement's trace to a page, with the page's latest version stored for everyone (#41). */
export type StatorLink = Wire["StatorLink"];
export type StatorRelation = StatorLink["relation"];
export type StatorLinkInput = Wire["StatorLinkInput"];
/** Confirms a trace to a page against the page's version as read. */
export type StatorClearInput = Wire["StatorClearInput"];

/** What a lookup found: why Stator was not asked, or each page, null when the caller may not read it. */
export interface StatorPages {
  status: StatorStatus | undefined;
  pages: Map<string, StatorPage | null>;
}

export const statorQueryKey = ["stator"] as const;
export const statorConnectionQueryKey = [...statorQueryKey, "connection"] as const;
export const statorAccountQueryKey = [...statorQueryKey, "account"] as const;

export function useStatorConnection() {
  return useQuery({
    queryKey: statorConnectionQueryKey,
    queryFn: async (): Promise<StatorConnection | null> => (await api.GET("/stator/connection")).data!.connection,
  });
}

// A new address forgets every member's token, so the caller's own account is asked again after any change to the
// connection.
export function useSaveStatorConnection() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: StatorConnectionInput): Promise<StatorConnection> => (await api.PUT("/stator/connection", { body })).data!.connection,
    onSuccess: (connection) => {
      queryClient.setQueryData(statorConnectionQueryKey, connection);
      void queryClient.invalidateQueries({ queryKey: statorAccountQueryKey });
    },
  });
}

export function useRemoveStatorConnection() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/stator/connection");
    },
    onSuccess: () => {
      queryClient.setQueryData(statorConnectionQueryKey, null);
      void queryClient.invalidateQueries({ queryKey: statorAccountQueryKey });
    },
  });
}

export function useStatorAccount() {
  return useQuery({
    queryKey: statorAccountQueryKey,
    queryFn: async (): Promise<StatorAccount> => (await api.GET("/stator/account")).data!.account,
  });
}

export function useConnectStator() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (token: string): Promise<StatorAccount> => (await api.PUT("/stator/account/token", { body: { token } })).data!.account,
    onSuccess: (account) => {
      queryClient.setQueryData(statorAccountQueryKey, account);
      forgetPages(queryClient);
    },
  });
}

// A check reads what Stator offers again, which the connection shows its administrators.
export function useCheckStator() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<StatorAccount> => (await api.POST("/stator/account/check")).data!.account,
    onSuccess: (account) => {
      queryClient.setQueryData(statorAccountQueryKey, account);
      void queryClient.invalidateQueries({ queryKey: statorConnectionQueryKey });
      forgetPages(queryClient);
    },
  });
}

export function useDisconnectStator() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/stator/account/token");
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: statorAccountQueryKey });
      forgetPages(queryClient);
    },
  });
}

// A token changed answers every page differently.
function forgetPages(queryClient: ReturnType<typeof useQueryClient>) {
  void queryClient.invalidateQueries({ queryKey: [...statorQueryKey, "pages"] });
  void queryClient.invalidateQueries({ queryKey: [...statorQueryKey, "search"] });
}

/** Looks pages up as the caller, a batch per query; the first status other than ok says why none were asked. */
export function useStatorPages(ids: readonly string[], enabled: boolean): StatorPages {
  const distinct = [...new Set(ids)];
  const batches: string[][] = [];
  for (let i = 0; i < distinct.length; i += STATOR_LOOKUP_MAX_PAGES) batches.push(distinct.slice(i, i + STATOR_LOOKUP_MAX_PAGES));
  return useQueries({
    queries: batches.map((batch) => ({
      queryKey: [...statorQueryKey, "pages", batch],
      queryFn: async () => (await api.GET("/stator/pages", { params: { query: { id: batch } } })).data!,
      enabled,
    })),
    combine: (results): StatorPages => {
      const pages = new Map<string, StatorPage | null>();
      let status: StatorStatus | undefined;
      for (const result of results) {
        if (!result.data) continue;
        if (result.data.status !== "ok") return { status: result.data.status, pages: new Map() };
        status = "ok";
        for (const each of result.data.pages) pages.set(each.id, each.page);
      }
      return { status, pages };
    },
  });
}

/** The picker's pages for the first words of a title. */
export function useStatorSearch(q: string, enabled: boolean) {
  return useQuery({
    queryKey: [...statorQueryKey, "search", q],
    queryFn: async () => (await api.GET("/stator/search", { params: { query: { q, limit: STATOR_SEARCH_LIMIT } } })).data!,
    enabled: enabled && q.trim() !== "",
  });
}

const linksKey = (reqKey: string) => [...statorQueryKey, "links", reqKey] as const;

/** A requirement's traces to pages. */
export function useStatorLinks(reqKey: string, enabled: boolean) {
  return useQuery({
    queryKey: linksKey(reqKey),
    queryFn: async (): Promise<StatorLink[]> => (await api.GET("/requirements/{reqKey}/stator-links", { params: { path: { reqKey } } })).data!.links,
    enabled,
  });
}

export function useStatorLinkActions(reqKey: string) {
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: linksKey(reqKey) });
  const create = useMutation({
    mutationFn: async (body: StatorLinkInput): Promise<StatorLink> =>
      (await api.POST("/requirements/{reqKey}/stator-links", { params: { path: { reqKey } }, body })).data!.link,
    onSuccess: refresh,
  });
  const remove = useMutation({
    mutationFn: async (link: StatorLink) => {
      await api.DELETE("/stator-links/{linkID}", { params: { path: { linkID: link.id } } });
    },
    onSuccess: refresh,
  });
  const clear = useMutation({
    mutationFn: async ({ id, ...body }: StatorClearInput & { id: string }): Promise<StatorLink> =>
      (await api.POST("/stator-links/{linkID}/clear", { params: { path: { linkID: id } }, body })).data!.link,
    onSuccess: () => refreshSuspects(queryClient),
  });
  return { create, remove, clear };
}
