import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE, BASELINE_SPEC_PAGE_SIZE, MATRIX_PAGE_SIZE } from "@/config";
import { api, queryString } from "./client";
import type { ExportFormat } from "./coverage";
import type { components, operations } from "./schema";

// Baselines (#37): named snapshots of a project or a section, read as a specification and compared.

type Wire = components["schemas"];

export type Baseline = Wire["Baseline"];
export type BaselineInput = Wire["NewBaseline"];
export type BaselineUpdate = Wire["BaselineUpdate"];
export type BaselineEntry = Wire["BaselineEntry"];
export type BaselineComparison = Wire["BaselineComparison"];
export type ItemChange = Wire["BaselineItemChange"];
export type BaselineMatrix = Wire["BaselineMatrix"];
export type BaselineMatrixLink = Wire["BaselineMatrixLink"];
/** What a baseline's matrix lays out (#153), as its query string takes it. */
export type BaselineMatrixParams = Omit<NonNullable<operations["baselineMatrix"]["parameters"]["query"]>, "limit" | "offset">;

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

/** A window of a baseline's matrix, compared with another side when params say. */
export function useBaselineMatrix(id: string, params: BaselineMatrixParams, offset: number) {
  return useQuery({
    queryKey: [...baselineKey(id), "matrix", params, offset],
    queryFn: async (): Promise<BaselineMatrix> =>
      (await api.GET("/baselines/{baselineID}/matrix", { params: { path: { baselineID: id }, query: { ...params, limit: MATRIX_PAGE_SIZE, offset } } })).data!
        .matrix,
    placeholderData: keepPreviousData,
  });
}

/** Where a baseline's whole matrix downloads as a CSV file or an Excel workbook. */
export function baselineMatrixHref(id: string, params: BaselineMatrixParams, format: ExportFormat = "csv"): string {
  return `${API_BASE}/baselines/${encodeURIComponent(id)}/matrix.${format}${queryString(params)}`;
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
      // The deleted baseline is not asked for again: its page would show it gone before it leads to the list.
      onSuccess: (_, id) => queryClient.invalidateQueries({ queryKey: baselinesKey, predicate: (query) => query.queryKey[1] !== id }),
    }),
  };
}

// Signed baselines (#205): who signs a project's baselines, and the signatures that release one.

export type SigningRule = Wire["BaselineSigningRule"];
export type SigningRuleInput = Wire["BaselineSigningRuleInput"];
export type BaselineSigner = Wire["BaselineSigner"];
export type BaselineSignature = Wire["BaselineSignature"];
export type BaselineSigning = Wire["BaselineSigning"];

const signingRuleKey = (projectKey: string) => [...baselinesKey, "signing", projectKey.toUpperCase()] as const;

/** Who signs a project's baselines, null while nobody does. */
export function useSigningRule(projectKey: string) {
  return useQuery({
    queryKey: signingRuleKey(projectKey),
    queryFn: async (): Promise<SigningRule | null> =>
      (await api.GET("/projects/{projectKey}/baseline-signing", { params: { path: { projectKey } } })).data!.rule ?? null,
  });
}

/** Names who signs a project's baselines, or stops them being signed; every baseline asks again, as its signing reads the rule. */
export function useSigningRuleActions(projectKey: string) {
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: baselinesKey });
  return {
    save: useMutation({
      mutationFn: async (body: SigningRuleInput): Promise<SigningRule> =>
        (await api.PUT("/projects/{projectKey}/baseline-signing", { params: { path: { projectKey } }, body })).data!.rule,
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: async () => {
        await api.DELETE("/projects/{projectKey}/baseline-signing", { params: { path: { projectKey } } });
      },
      onSuccess: refresh,
    }),
  };
}

/** Every signature of a baseline, in the order given. */
export function useBaselineSignatures(id: string, enabled = true) {
  return useQuery({
    queryKey: [...baselineKey(id), "signatures"],
    queryFn: async (): Promise<BaselineSignature[]> =>
      (await api.GET("/baselines/{baselineID}/signatures", { params: { path: { baselineID: id } } })).data!.signatures,
    enabled,
  });
}

/** Signs a baseline with the meaning shown and the password or a fresh sign-in. */
export function useSignBaseline(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: Wire["BaselineSignRequest"]): Promise<Baseline> =>
      (await api.POST("/baselines/{baselineID}/signatures", { params: { path: { baselineID: id } }, body })).data!.baseline,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: baselinesKey }),
  });
}
