import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { HISTORY_PAGE_SIZE } from "@/config";
import { api } from "./client";
import { requirementQueryKey, type Requirement } from "./requirements";
import type { components } from "./schema";

type Wire = components["schemas"];

/** One revision as the history lists it. */
export type RevisionEntry = Wire["RevisionEntry"];
/** One revision with what it said. */
export type Revision = Wire["Revision"];
/** What changed between two revisions. */
export type Comparison = Wire["Comparison"];
export type AttributeDiff = Wire["AttributeDiff"];
export type DiffBlock = Wire["DiffBlock"];
export type ActivityEntry = Wire["ActivityEntry"];

function historyKey(key: string) {
  return ["history", key.toUpperCase()] as const;
}

/** A window of a requirement's revisions, the latest first. */
export function useRevisions(key: string, offset: number) {
  return useQuery({
    queryKey: [...historyKey(key), "revisions", offset],
    queryFn: async () =>
      (await api.GET("/requirements/{reqKey}/revisions", { params: { path: { reqKey: key }, query: { limit: HISTORY_PAGE_SIZE, offset } } })).data!,
    placeholderData: keepPreviousData,
  });
}

/** One revision with its content. */
export function useRevision(key: string, number: number) {
  return useQuery({
    queryKey: [...historyKey(key), "revision", number],
    queryFn: async (): Promise<Revision> =>
      (await api.GET("/requirements/{reqKey}/revisions/{revisionNumber}", { params: { path: { reqKey: key, revisionNumber: number } } })).data!.revision,
  });
}

/** Two revisions compared; absent sides take the server's defaults, the latest and the one before. */
export function useComparison(key: string, from?: number, to?: number) {
  return useQuery({
    queryKey: [...historyKey(key), "diff", from ?? null, to ?? null],
    queryFn: async (): Promise<Comparison> =>
      (await api.GET("/requirements/{reqKey}/diff", { params: { path: { reqKey: key }, query: { from, to } } })).data!.comparison,
  });
}

/** Revisions and logged changes in one list, newest first, read a window at a time. */
export function useActivity(key: string) {
  return useInfiniteQuery({
    queryKey: [...historyKey(key), "activity"],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) =>
      (await api.GET("/requirements/{reqKey}/activity", { params: { path: { reqKey: key }, query: { limit: HISTORY_PAGE_SIZE, cursor: pageParam } } })).data!,
    getNextPageParam: (last) => last.next ?? undefined,
  });
}

/** Writes an older revision's content as the next revision; the history and the requirement follow. */
export function useRestoreRevision(key: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ number, baseRevision, comment }: { number: number; baseRevision: number; comment?: string }) =>
      (
        await api.POST("/requirements/{reqKey}/revisions/{revisionNumber}/restore", {
          params: { path: { reqKey: key, revisionNumber: number } },
          body: { baseRevision, comment },
        })
      ).data!,
    onSuccess: ({ requirement }) => {
      queryClient.setQueryData<Requirement>(requirementQueryKey(requirement.key), requirement);
      return queryClient.invalidateQueries({ queryKey: historyKey(key) });
    },
    // A refusal over a newer revision reads the requirement again, so the next restore is made over that one.
    onError: () => queryClient.invalidateQueries({ queryKey: requirementQueryKey(key) }),
  });
}
