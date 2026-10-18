import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { DUPLICATES_PAGE_SIZE, QUALITY_CHECK_DEBOUNCE_MS } from "@/config";
import { useDebounced } from "@/lib/debounce";
import { ApiError, api } from "./client";
import { linksQueryKey } from "./links";
import { requirementsKey } from "./requirements";
import type { components } from "./schema";

// Duplicates (#208): the requirements a draft or a saved requirement is most like, a project's likely duplicates in
// pairs and what is decided about each, and how alike two must be to be listed.

type Wire = components["schemas"];

export type SimilarRequirement = Wire["SimilarRequirement"];
export type SimilarDraft = Wire["SimilarDraftInput"];
export type DuplicatePair = Wire["DuplicatePair"];
export type DuplicateSide = Wire["DuplicateSide"];
export type DuplicateDecision = Wire["DuplicateDecision"];
export type DuplicateMerge = Wire["DuplicateMerge"];
export type DuplicateMovedLink = Wire["DuplicateMovedLink"];
export type DuplicateSettings = Wire["DuplicateSettings"];

/** The requirements found alike, most alike first, and how alike they had to be. */
export interface Similar {
  similar: SimilarRequirement[];
  threshold: number;
}

/** What narrows the pairs: a type and a section either side has, and a query either side matches. */
export interface PairFilter {
  typeId: string;
  section: string;
  query: string;
}

const similarKey = ["similar"] as const;
const pairsKey = ["duplicates"] as const;
const settingsKey = (projectKey: string) => ["duplicate-settings", projectKey.toUpperCase()] as const;

/** The requirements a draft is most like, asked in the same pause as the check of the writing rules (#207). `current`
 * says the answer is of the draft as it is now. */
export function useSimilarDraft(projectKey: string, draft: SimilarDraft, enabled = true) {
  const asked = JSON.stringify(draft);
  const settled = useDebounced(asked, QUALITY_CHECK_DEBOUNCE_MS);
  // The draft is the key, so an answer to an older draft is never taken for the latest one's.
  const query = useQuery({
    queryKey: [...similarKey, "draft", projectKey.toUpperCase(), settled],
    queryFn: async (): Promise<Similar> =>
      (await api.POST("/projects/{projectKey}/similar", { params: { path: { projectKey } }, body: JSON.parse(settled) as SimilarDraft })).data!,
    enabled,
    placeholderData: keepPreviousData,
    staleTime: Number.POSITIVE_INFINITY,
    retry: false,
  });
  return { found: query.data, current: Boolean(query.data) && !query.isPlaceholderData && settled === asked, error: query.error };
}

/** The requirements a saved one is most like, leaving out pairs that stay decided. */
export function useSimilar(reqKey: string, enabled = true) {
  return useQuery({
    queryKey: [...similarKey, "of", reqKey.toUpperCase()],
    queryFn: async (): Promise<Similar> => (await api.GET("/requirements/{reqKey}/similar", { params: { path: { reqKey } } })).data!,
    enabled,
  });
}

/** A project's likely duplicates, most alike first, a page at a time. */
export function useDuplicatePairs(projectKey: string, filter: PairFilter) {
  return useInfiniteQuery({
    queryKey: [...pairsKey, projectKey.toUpperCase(), filter],
    initialPageParam: "",
    queryFn: async ({ pageParam }) =>
      (
        await api.GET("/projects/{projectKey}/duplicates", {
          params: {
            path: { projectKey },
            query: {
              limit: DUPLICATES_PAGE_SIZE,
              ...(pageParam ? { cursor: pageParam } : {}),
              ...(filter.typeId ? { type: [filter.typeId] } : {}),
              ...(filter.section ? { section: filter.section } : {}),
              ...(filter.query ? { query: filter.query } : {}),
            },
          },
        })
      ).data!,
    getNextPageParam: (last) => last.next || undefined,
    retry: false,
  });
}

/** What a decision changes: the pairs and similar lists, and with a merge or a link the requirements, where they
 * show, their links and the trash. */
function useAfterDecision() {
  const queryClient = useQueryClient();
  return () =>
    Promise.all(
      [pairsKey, similarKey, requirementsKey, ["tree"], ["spec"], linksQueryKey, ["history"], ["coverage"]].map((queryKey) =>
        queryClient.invalidateQueries({ queryKey }),
      ),
    );
}

/** Says two requirements do not say the same thing; the pair stays away until either is revised. */
export function useDismissPair(projectKey: string) {
  const after = useAfterDecision();
  return useMutation({
    mutationFn: async (body: { a: DuplicateSide; b: DuplicateSide; reason: string }): Promise<DuplicateDecision> =>
      (await api.POST("/projects/{projectKey}/duplicates/dismiss", { params: { path: { projectKey } }, body })).data!.decision,
    onSuccess: after,
  });
}

/** Links the two by one of the project's link types; the pair stays away while the link lasts. */
export function useLinkPair(projectKey: string) {
  const after = useAfterDecision();
  return useMutation({
    mutationFn: async (body: { source: DuplicateSide; target: DuplicateSide; linkTypeKey: string }): Promise<DuplicateDecision> =>
      (await api.POST("/projects/{projectKey}/duplicates/link", { params: { path: { projectKey } }, body })).data!.decision,
    onSuccess: after,
  });
}

/** Trashes one side and makes its links again on the side kept, saying which could not be. */
export function useMergePair(projectKey: string) {
  const after = useAfterDecision();
  return useMutation({
    mutationFn: async (body: { keep: DuplicateSide; trash: DuplicateSide; comment?: string }): Promise<DuplicateMerge> =>
      (await api.POST("/projects/{projectKey}/duplicates/merge", { params: { path: { projectKey } }, body })).data!.merge,
    onSuccess: after,
  });
}

/** How alike two of a project's requirements must be to be listed. */
export function useDuplicateSettings(projectKey: string) {
  return useQuery({
    queryKey: settingsKey(projectKey),
    queryFn: async (): Promise<DuplicateSettings> =>
      (await api.GET("/projects/{projectKey}/duplicate-settings", { params: { path: { projectKey } } })).data!.settings,
  });
}

/** Sets the threshold; the pairs and the similar lists follow. */
export function useSaveDuplicateSettings(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (threshold: number): Promise<DuplicateSettings> =>
      (await api.PUT("/projects/{projectKey}/duplicate-settings", { params: { path: { projectKey } }, body: { threshold } })).data!.settings,
    onSuccess: async (saved) => {
      queryClient.setQueryData(settingsKey(projectKey), saved);
      await Promise.all([pairsKey, similarKey].map((queryKey) => queryClient.invalidateQueries({ queryKey })));
    },
  });
}

/** Whether a refusal says a side was revised since the pair was read. */
export function isStale(error: unknown): boolean {
  return error instanceof ApiError && error.code === "stale_revision";
}

/** Reads the pairs and the requirements in them again, after a refusal over a side revised since. */
export function useReadPairsAgain() {
  const queryClient = useQueryClient();
  return () => Promise.all([pairsKey, requirementsKey].map((queryKey) => queryClient.invalidateQueries({ queryKey })));
}
