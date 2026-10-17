import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { ARMATURE_LINKS_POLL_MS, ARMATURE_LOOKUP_MAX_KEYS, ARMATURE_SEARCH_LIMIT } from "@/config";
import { api } from "./client";
import { refreshSuspects } from "./links";
import { armatureThemeQueryKey, themesQueryKey } from "./themes";
import type { components } from "./schema";

// Stator's Armature client; each issue of M2 adds what it reads.

type Wire = components["schemas"];

/** The installation's Armature instance, as its administrators see it; the webhook secret never comes back. */
export type ArmatureConnection = Wire["ArmatureConnection"];
export type ArmatureConnectionInput = Wire["ArmatureConnectionInput"];
/** The caller's own link to Armature; the token never comes back. */
export type ArmatureAccount = Wire["ArmatureAccount"];
export type ArmatureStatus = ArmatureAccount["status"];
/** An issue as the caller may see it in Armature. */
export type ArmatureIssue = Wire["ArmatureIssue"];
/** A requirement's trace to an issue, with the status category stored for everyone and its sync. */
export type ArmatureLink = Wire["ArmatureLink"];
export type ArmatureRelation = ArmatureLink["relation"];
export type ArmatureLinkInput = Wire["ArmatureLinkInput"];

/** What a lookup found: why Armature was not asked, or each key's issue, null when the caller may not see it. */
export interface ArmatureIssues {
  status: ArmatureStatus | undefined;
  issues: Map<string, ArmatureIssue | null>;
}

export const armatureQueryKey = ["armature"] as const;
export const armatureConnectionQueryKey = [...armatureQueryKey, "connection"] as const;
export const armatureAccountQueryKey = [...armatureQueryKey, "account"] as const;

export function useArmatureConnection() {
  return useQuery({
    queryKey: armatureConnectionQueryKey,
    queryFn: async (): Promise<ArmatureConnection | null> => (await api.GET("/armature/connection")).data!.connection,
  });
}

// A new address forgets every member's token, so the caller's own account is asked again after any change to the
// connection.
export function useSaveArmatureConnection() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: ArmatureConnectionInput): Promise<ArmatureConnection> => (await api.PUT("/armature/connection", { body })).data!.connection,
    onSuccess: (connection) => {
      queryClient.setQueryData(armatureConnectionQueryKey, connection);
      void queryClient.invalidateQueries({ queryKey: armatureAccountQueryKey });
      forgetIssues(queryClient);
    },
  });
}

export function useRemoveArmatureConnection() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/armature/connection");
    },
    onSuccess: () => {
      queryClient.setQueryData(armatureConnectionQueryKey, null);
      void queryClient.invalidateQueries({ queryKey: armatureAccountQueryKey });
      forgetIssues(queryClient);
    },
  });
}

export function useArmatureAccount() {
  return useQuery({
    queryKey: armatureAccountQueryKey,
    queryFn: async (): Promise<ArmatureAccount> => (await api.GET("/armature/account")).data!.account,
  });
}

export function useConnectArmature() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (token: string): Promise<ArmatureAccount> => (await api.PUT("/armature/account/token", { body: { token } })).data!.account,
    onSuccess: (account) => {
      queryClient.setQueryData(armatureAccountQueryKey, account);
      forgetIssues(queryClient);
    },
  });
}

export function useCheckArmature() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<ArmatureAccount> => (await api.POST("/armature/account/check")).data!.account,
    onSuccess: (account) => {
      queryClient.setQueryData(armatureAccountQueryKey, account);
      forgetIssues(queryClient);
    },
  });
}

// A connection or a token changed answers every issue differently.
function forgetIssues(queryClient: ReturnType<typeof useQueryClient>) {
  void queryClient.invalidateQueries({ queryKey: [...armatureQueryKey, "issues"] });
  void queryClient.invalidateQueries({ queryKey: [...armatureQueryKey, "search"] });
}

export function useDisconnectArmature() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/armature/account/token");
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: armatureAccountQueryKey });
      forgetIssues(queryClient);
    },
  });
}

/** The keys a lookup asks for, split into batches the API takes. */
export function lookupBatches(keys: readonly string[]): string[][] {
  const distinct = [...new Set(keys)];
  const out: string[][] = [];
  for (let i = 0; i < distinct.length; i += ARMATURE_LOOKUP_MAX_KEYS) out.push(distinct.slice(i, i + ARMATURE_LOOKUP_MAX_KEYS));
  return out;
}

/** Looks keys up as the caller, a batch per query; the first status other than ok says why none were asked. */
export function useArmatureIssues(keys: readonly string[], enabled: boolean): ArmatureIssues {
  const batches = lookupBatches(keys);
  return useQueries({
    queries: batches.map((batch) => ({
      queryKey: [...armatureQueryKey, "issues", batch],
      queryFn: async () => (await api.GET("/armature/issues", { params: { query: { key: batch } } })).data!,
      enabled,
    })),
    combine: (results): ArmatureIssues => {
      const issues = new Map<string, ArmatureIssue | null>();
      let status: ArmatureStatus | undefined;
      for (const result of results) {
        if (!result.data) continue;
        if (result.data.status !== "ok") return { status: result.data.status, issues: new Map() };
        status = "ok";
        for (const each of result.data.issues) issues.set(each.key, each.issue);
      }
      return { status, issues };
    },
  });
}

/** The picker's issues for what was typed: a key, or words of a summary. */
export function useArmatureSearch(q: string, enabled: boolean) {
  return useQuery({
    queryKey: [...armatureQueryKey, "search", q],
    queryFn: async () => (await api.GET("/armature/search", { params: { query: { q, limit: ARMATURE_SEARCH_LIMIT } } })).data!,
    enabled: enabled && q.trim() !== "",
  });
}

const linksKey = (reqKey: string) => [...armatureQueryKey, "links", reqKey] as const;

/** A requirement's traces to issues, asked again while the worker still has one to put on Armature. */
export function useArmatureLinks(reqKey: string, enabled: boolean) {
  return useQuery({
    queryKey: linksKey(reqKey),
    queryFn: async (): Promise<ArmatureLink[]> => (await api.GET("/requirements/{reqKey}/armature-links", { params: { path: { reqKey } } })).data!.links,
    enabled,
    refetchInterval: (query) => (query.state.data?.some((link) => link.sync === "pending" && !link.detachedAt) ? ARMATURE_LINKS_POLL_MS : false),
  });
}

export function useArmatureLinkActions(reqKey: string) {
  const queryClient = useQueryClient();
  // Coverage and the matrix (#149) count traces to issues, so they ask again too.
  const refresh = () => Promise.all([linksKey(reqKey), ["coverage"]].map((queryKey) => queryClient.invalidateQueries({ queryKey })));
  const create = useMutation({
    mutationFn: async (body: ArmatureLinkInput): Promise<ArmatureLink> =>
      (await api.POST("/requirements/{reqKey}/armature-links", { params: { path: { reqKey } }, body })).data!.link,
    onSuccess: refresh,
  });
  const remove = useMutation({
    mutationFn: async (link: ArmatureLink) => {
      await api.DELETE("/armature-links/{linkID}", { params: { path: { linkID: link.id } } });
    },
    onSuccess: refresh,
  });
  const clear = useMutation({
    mutationFn: async ({ id, ...body }: ArmatureClearInput & { id: string }): Promise<ArmatureLink> =>
      (await api.POST("/armature-links/{linkID}/clear", { params: { path: { linkID: id } }, body })).data!.link,
    onSuccess: () => refreshSuspects(queryClient),
  });
  return { create, remove, clear };
}

/** Confirms a trace to an issue against the requirement's revision as read (#36). */
export type ArmatureClearInput = Wire["ArmatureClearInput"];

/** An Armature project the caller may see, and whether they may file issues in it. */
export type ArmatureProject = Wire["ArmatureProject"];
export type ArmatureIssueType = Wire["ArmatureIssueType"];
export type ArmatureCreateInput = Wire["ArmatureCreateInput"];
/** One issue made from a requirement, and the link that traces the requirement to it. */
export type ArmatureCreated = Wire["ArmatureCreated"];
/** The requirement a round of creating stopped at, and why. */
export type ArmatureCreateFailure = Wire["ArmatureCreateFailure"];

/** What the create dialog offers: the caller's Armature projects, read only while it is open. */
export function useArmatureProjects(enabled: boolean) {
  return useQuery({
    queryKey: [...armatureQueryKey, "projects"],
    queryFn: async () => (await api.GET("/armature/projects")).data!,
    enabled,
  });
}

export function useArmatureIssueTypes(enabled: boolean) {
  return useQuery({
    queryKey: [...armatureQueryKey, "issue-types"],
    queryFn: async () => (await api.GET("/armature/issue-types")).data!,
    enabled,
  });
}

/** Makes one issue per requirement; every requirement that got one shows its new link. */
export function useCreateArmatureIssues(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: ArmatureCreateInput): Promise<{ created: ArmatureCreated[]; failed: ArmatureCreateFailure | null }> =>
      (await api.POST("/projects/{projectKey}/armature-issues", { params: { path: { projectKey } }, body })).data!,
    onSuccess: (answer) => {
      for (const each of answer.created) void queryClient.invalidateQueries({ queryKey: linksKey(each.requirementKey) });
    },
  });
}

/** Whether the caller follows their Armature theme, whether Armature answered, and why it cannot be used (#30). */
export type ArmatureFollow = Wire["ArmatureFollow"];

// Following changes the theme the page shows, which the loader reads from the active theme.
function themeChanged(queryClient: ReturnType<typeof useQueryClient>) {
  void queryClient.invalidateQueries({ queryKey: themesQueryKey });
}

/** Asks Armature now, so a theme changed there shows when the themes page opens. */
export function useArmatureThemeFollow(enabled: boolean) {
  const queryClient = useQueryClient();
  return useQuery({
    queryKey: armatureThemeQueryKey,
    enabled,
    queryFn: async (): Promise<ArmatureFollow> => {
      const follow = (await api.GET("/armature/theme")).data!.follow;
      if (follow.following) themeChanged(queryClient);
      return follow;
    },
  });
}

export function useFollowArmatureTheme() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<ArmatureFollow> => (await api.PUT("/armature/theme")).data!.follow,
    onSuccess: (follow) => {
      queryClient.setQueryData(armatureThemeQueryKey, follow);
      themeChanged(queryClient);
    },
  });
}

export function useUnfollowArmatureTheme() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/armature/theme");
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: armatureThemeQueryKey });
      themeChanged(queryClient);
    },
  });
}
