import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { API_BASE, CHAIN_PAGE_SIZE } from "@/config";
import { api, queryString } from "./client";
import type { ExportFormat } from "./coverage";
import type { components, operations } from "./schema";

// A project's trace model and the chains that follow it (#151).

type Wire = components["schemas"];

export type TraceModel = Wire["TraceModel"];
export type TraceModelLevel = Wire["TraceModelLevel"];
export type TraceModelInput = Wire["TraceModelInput"];
export type TraceModelLevelInput = Wire["TraceModelLevelInput"];
export type TraceModelStep = Wire["TraceModelStep"];
export type LevelKind = TraceModelLevel["kind"];
export type TraceChains = Wire["TraceChains"];
export type TraceChainNode = Wire["TraceChainNode"];
/** What a chain report shows, as its query string takes it: the grid's narrowing of the roots, the levels and the breaks. */
export type ChainParams = Omit<NonNullable<operations["traceChains"]["parameters"]["query"]>, "limit" | "offset">;

/** Every kind a level holds, in the order the picker offers them. */
export const LEVEL_KINDS: LevelKind[] = ["requirements", "issues", "pages", "tests", "code"];

// Under the coverage key, so a link made anywhere asks the chains again with the matrix.
const modelKey = (projectKey: string) => ["coverage", projectKey.toUpperCase(), "model"] as const;
const chainsKey = (projectKey: string) => ["coverage", projectKey.toUpperCase(), "chains"] as const;

/** A project's trace levels, first to last. */
export function useTraceModel(projectKey: string) {
  return useQuery({
    queryKey: modelKey(projectKey),
    queryFn: async (): Promise<TraceModel> => (await api.GET("/projects/{projectKey}/trace-model", { params: { path: { projectKey } } })).data!.model,
  });
}

/** Replaces a project's whole trace model; its chains ask again. */
export function useSaveTraceModel(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: TraceModelInput): Promise<TraceModel> =>
      (await api.PUT("/projects/{projectKey}/trace-model", { params: { path: { projectKey } }, body })).data!.model,
    onSuccess: (model) => {
      queryClient.setQueryData(modelKey(projectKey), model);
      void queryClient.invalidateQueries({ queryKey: chainsKey(projectKey) });
    },
  });
}

/** A page of a project's chains. */
export function useChains(projectKey: string, params: ChainParams, offset: number) {
  return useQuery({
    queryKey: [...chainsKey(projectKey), params, offset],
    queryFn: async (): Promise<TraceChains> =>
      (await api.GET("/projects/{projectKey}/trace-chains", { params: { path: { projectKey }, query: { ...params, limit: CHAIN_PAGE_SIZE, offset } } })).data!
        .chains,
    placeholderData: keepPreviousData,
  });
}

/** Where every chain the params keep downloads as a CSV file or an Excel workbook. */
export function chainsHref(projectKey: string, params: ChainParams, format: ExportFormat = "csv"): string {
  return `${API_BASE}/projects/${encodeURIComponent(projectKey)}/trace-chains.${format}${queryString(params)}`;
}

// A requirement's trace graph and the impact of changing it (#211).

export type TraceGraph = Wire["TraceGraph"];
export type TraceGraphNode = Wire["TraceGraphNode"];
export type TraceGraphEdge = Wire["TraceGraphEdge"];
export type GraphDirection = TraceGraph["direction"];
/** What a graph is asked for, as its query string takes it. */
export type GraphParams = NonNullable<operations["traceGraph"]["parameters"]["query"]>;

/** Every relation the graph reaches below a requirement, in the order the picker offers them. */
export const GRAPH_RELATIONS = ["implements", "verifies", "specifies", "describes", "relates"] as const;

/** Everything a requirement traces to, as far as the params ask; a link made anywhere asks it again. */
export function useTraceGraph(reqKey: string, params: GraphParams) {
  return useQuery({
    queryKey: ["coverage", "graph", reqKey.toUpperCase(), params],
    queryFn: async (): Promise<TraceGraph> => (await api.GET("/requirements/{reqKey}/graph", { params: { path: { reqKey }, query: params } })).data!.graph,
    placeholderData: keepPreviousData,
  });
}

/** Where the impact list of a graph downloads as a CSV file. */
export function graphHref(reqKey: string, params: GraphParams): string {
  return `${API_BASE}/requirements/${encodeURIComponent(reqKey)}/graph.csv${queryString(params)}`;
}
