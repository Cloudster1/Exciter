import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { SPEC_PAGE_SIZE } from "@/config";
import { api } from "./client";
import type { Requirement } from "./requirements";
import type { StatusCategory } from "./workflows";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A requirement or section as the tree shows it, numbered where it stands. */
export type TreeNode = Wire["TreeNode"];
export type SpecEntry = Wire["SpecEntry"];
/** Where a node goes: below a parent, after a sibling; nulls are the top and the first place. */
export type Place = Wire["Place"];
export type MoveRequest = Wire["MoveRequirement"];
export type CopyRequest = Wire["CopyRequirement"];

function treeKey(projectKey: string) {
  return ["tree", projectKey.toUpperCase()] as const;
}

/** What the specification is narrowed to, as its address holds it (#169). */
export interface SpecAddress {
  status?: string[];
  category?: StatusCategory[];
}

function specKey(projectKey: string, address: SpecAddress = {}) {
  return ["spec", projectKey.toUpperCase(), address] as const;
}

/** A project's whole tree out of the trash, in reading order. */
export function useTree(projectKey: string) {
  return useQuery({
    queryKey: treeKey(projectKey),
    queryFn: async (): Promise<TreeNode[]> => (await api.GET("/projects/{projectKey}/tree", { params: { path: { projectKey } } })).data!.nodes,
    enabled: Boolean(projectKey),
  });
}

/** The specification, read a window at a time as the reader goes on, narrowed as its address says. */
export function useSpec(projectKey: string, address: SpecAddress = {}) {
  return useInfiniteQuery({
    queryKey: specKey(projectKey, address),
    initialPageParam: 0,
    queryFn: async ({ pageParam }) =>
      (
        await api.GET("/projects/{projectKey}/spec", {
          params: { path: { projectKey }, query: { limit: SPEC_PAGE_SIZE, offset: pageParam, status: address.status, category: address.category } },
        })
      ).data!,
    getNextPageParam: (last) => (last.offset + last.entries.length < last.total ? last.offset + last.entries.length : undefined),
  });
}

/** Has everything that shows a place or a key ask again, after a move or a copy. */
function useRefresh() {
  const queryClient = useQueryClient();
  return () => Promise.all([["tree"], ["spec"], ["requirements"]].map((queryKey) => queryClient.invalidateQueries({ queryKey })));
}

/** Moves a requirement and what is below it, within its project or to another. */
export function useMoveRequirement() {
  const refresh = useRefresh();
  return useMutation({
    mutationFn: async ({ key, ...body }: MoveRequest & { key: string }): Promise<Requirement> =>
      (await api.POST("/requirements/{reqKey}/move", { params: { path: { reqKey: key } }, body })).data!.requirement,
    onSuccess: refresh,
  });
}

/** Copies a requirement, and what is below it when asked, as new requirements. */
export function useCopyRequirement() {
  const refresh = useRefresh();
  return useMutation({
    mutationFn: async ({ key, ...body }: CopyRequest & { key: string }) =>
      (await api.POST("/requirements/{reqKey}/copy", { params: { path: { reqKey: key } }, body })).data!,
    onSuccess: refresh,
  });
}
