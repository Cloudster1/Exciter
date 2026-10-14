import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BASELINE_SPEC_PAGE_SIZE } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

// Baselines (#37): named snapshots of a project or a section, read as a specification and compared.

type Wire = components["schemas"];

export type Baseline = Wire["Baseline"];
export type BaselineInput = Wire["NewBaseline"];
export type BaselineUpdate = Wire["BaselineUpdate"];
export type BaselineEntry = Wire["BaselineEntry"];
export type BaselineComparison = Wire["BaselineComparison"];
export type ItemChange = Wire["BaselineItemChange"];

/** The other side of a comparison when it is no baseline. */
export const NOW = "now";

const baselinesKey = ["baselines"] as const;
const projectBaselinesKey = (projectKey: string) => [...baselinesKey, "of", projectKey.toUpperCase()] as const;
const baselineKey = (id: string) => [...baselinesKey, id] as const;

/** A project's baselines, newest first. */
export function useBaselines(projectKey: string) {
  return useQuery({
    queryKey: projectBaselinesKey(projectKey),
    queryFn: async (): Promise<Baseline[]> => (await api.GET("/projects/{projectKey}/baselines", { params: { path: { projectKey } } })).data!.baselines,
  });
}

export function useBaseline(id: string) {
  return useQuery({
    queryKey: baselineKey(id),
    queryFn: async (): Promise<Baseline> => (await api.GET("/baselines/{baselineID}", { params: { path: { baselineID: id } } })).data!.baseline,
  });
}

/** A window of a baseline read as a specification. */
export function useBaselineSpec(id: string, offset: number) {
  return useQuery({
    queryKey: [...baselineKey(id), "spec", offset],
    queryFn: async () =>
      (await api.GET("/baselines/{baselineID}/spec", { params: { path: { baselineID: id }, query: { limit: BASELINE_SPEC_PAGE_SIZE, offset } } })).data!,
    placeholderData: keepPreviousData,
  });
}

/** What differs from a baseline to another, or to now. */
export function useBaselineComparison(id: string, other: string) {
  return useQuery({
    queryKey: [...baselineKey(id), "compare", other],
    queryFn: async (): Promise<BaselineComparison> =>
      (await api.GET("/baselines/{baselineID}/compare", { params: { path: { baselineID: id }, query: { with: other } } })).data!.comparison,
  });
}

/** Takes, renames, locks and deletes a project's baselines; every list of them asks again. */
export function useBaselineActions(projectKey: string) {
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: baselinesKey });
  return {
    take: useMutation({
      mutationFn: async (body: BaselineInput): Promise<Baseline> =>
        (await api.POST("/projects/{projectKey}/baselines", { params: { path: { projectKey } }, body })).data!.baseline,
      onSuccess: refresh,
    }),
    update: useMutation({
      mutationFn: async ({ id, ...body }: BaselineUpdate & { id: string }): Promise<Baseline> =>
        (await api.PATCH("/baselines/{baselineID}", { params: { path: { baselineID: id } }, body })).data!.baseline,
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: async (id: string) => {
        await api.DELETE("/baselines/{baselineID}", { params: { path: { baselineID: id } } });
      },
      onSuccess: refresh,
    }),
  };
}
