import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { Requirement } from "./requirements";
import type { components } from "./schema";

type Wire = components["schemas"];

/** What a reused requirement knows of its library source (#49). */
export type Reuse = Wire["Reuse"];
export type ReuseRequest = Wire["ReuseInput"];

function useRefresh() {
  const queryClient = useQueryClient();
  return () => Promise.all([["tree"], ["spec"], ["requirements"]].map((queryKey) => queryClient.invalidateQueries({ queryKey })));
}

/** Reuses a library requirement, and what is below it when asked, in another project. */
export function useReuseRequirement() {
  const refresh = useRefresh();
  return useMutation({
    mutationFn: async ({ key, ...body }: ReuseRequest & { key: string }) =>
      (await api.POST("/requirements/{reqKey}/reuse", { params: { path: { reqKey: key } }, body })).data!,
    onSuccess: refresh,
  });
}

/** Takes the source's words at the revision named, which has to be its latest. */
export function useTakeSource(key: string) {
  const refresh = useRefresh();
  return useMutation({
    mutationFn: async (revision: number): Promise<Requirement> =>
      (await api.POST("/requirements/{reqKey}/reuse/take", { params: { path: { reqKey: key } }, body: { revision } })).data!.requirement,
    onSuccess: refresh,
  });
}

/** Lets a reused requirement have words of its own. */
export function useMakeVariant(key: string) {
  const refresh = useRefresh();
  return useMutation({
    mutationFn: async (): Promise<Requirement> =>
      (await api.POST("/requirements/{reqKey}/reuse/variant", { params: { path: { reqKey: key } } })).data!.requirement,
    onSuccess: refresh,
  });
}
