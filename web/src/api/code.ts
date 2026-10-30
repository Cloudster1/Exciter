import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { CODE_TAGS_PAGE_SIZE } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

// Code tags (#201): the places in a repository that implement or verify a requirement, the project's tags gone
// outdated or orphaned, and the words its tags use.

type Wire = components["schemas"];

export type CodeLink = Wire["CodeLink"];
export type CodeTag = Wire["CodeTag"];
export type CodeTagWords = Wire["CodeTagWords"];
export type CodeTagState = "outdated" | "orphaned";

const linksKey = (reqKey: string) => ["code-links", reqKey.toUpperCase()] as const;
const tagsKey = (projectKey: string) => ["code-tags", projectKey.toUpperCase()] as const;
const wordsKey = (projectKey: string) => ["code-tag-words", projectKey.toUpperCase()] as const;

/** The places that implement or verify a requirement, the counting branch first. */
export function useCodeLinks(reqKey: string, enabled = true) {
  return useQuery({
    queryKey: linksKey(reqKey),
    queryFn: async () => (await api.GET("/requirements/{reqKey}/code-links", { params: { path: { reqKey } } })).data!,
    enabled,
  });
}

/** A window of a project's outdated or orphaned tags on its counting branch; both when no state is given. */
export function useCodeTags(projectKey: string, state: CodeTagState | "", offset: number) {
  return useQuery({
    queryKey: [...tagsKey(projectKey), state, offset],
    queryFn: async () =>
      (
        await api.GET("/projects/{projectKey}/code-tags", {
          params: { path: { projectKey }, query: { ...(state ? { state } : {}), limit: CODE_TAGS_PAGE_SIZE, offset } },
        })
      ).data!,
    placeholderData: keepPreviousData,
  });
}

/** The words a project's tags use, and the keys they may carry. */
export function useCodeTagWords(projectKey: string) {
  return useQuery({
    queryKey: wordsKey(projectKey),
    queryFn: async (): Promise<CodeTagWords> => (await api.GET("/projects/{projectKey}/code-tag-words", { params: { path: { projectKey } } })).data!.words,
  });
}

/** Sets the tag words; every tag is read by them from then on. */
export function useSaveCodeTagWords(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: { implements: string[]; verifies: string[] }): Promise<CodeTagWords> =>
      (await api.PUT("/projects/{projectKey}/code-tag-words", { params: { path: { projectKey } }, body })).data!.words,
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: wordsKey(projectKey) }),
        queryClient.invalidateQueries({ queryKey: ["code-links"] }),
        queryClient.invalidateQueries({ queryKey: tagsKey(projectKey) }),
        queryClient.invalidateQueries({ queryKey: ["coverage", projectKey.toUpperCase()] }),
      ]);
    },
  });
}
