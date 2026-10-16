import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE, COVERAGE_PAGE_SIZE, MATRIX_COLUMN_PAGE_SIZE, MATRIX_PAGE_SIZE } from "@/config";
import { api, queryString } from "./client";
import type { components, operations } from "./schema";

// Coverage and the traceability matrix (#38, #147): what each type is asked for, how far the requirements are,
// and the links between them laid out.

type Wire = components["schemas"];

export type CoverageRule = Wire["CoverageRule"];
export type CoverageRuleInput = Wire["CoverageRuleInput"];
export type CoverageRow = Wire["CoverageRow"];
export type CoverageSummary = Wire["CoverageSummary"];
export type CoverageTotals = Wire["CoverageTotals"];
export type Matrix = Wire["TraceMatrix"];
export type MatrixLink = Wire["MatrixLink"];
/** What a matrix lays out, as its query string takes it: the grid's narrowing and order, the columns and the links counted. */
export type MatrixParams = Omit<NonNullable<operations["traceMatrix"]["parameters"]["query"]>, "limit" | "offset" | "columnLimit" | "columnOffset">;
export type Level = CoverageRow["decomposed"];
export type Dimension = "decomposed" | "implemented" | "verified";
export type MatrixColumns = "requirements" | "issues" | "pages";

/** Every dimension, in report order. */
export const DIMENSIONS: Dimension[] = ["decomposed", "implemented", "verified"];
/** The levels a row may be kept at. */
export const LEVELS: Level[] = ["none", "partial", "full", "na"];

/** The level the report keeps its rows at in each dimension; one left out keeps every level. */
export type LevelFilter = Partial<Record<Dimension, Level>>;

const coverageKey = (projectKey: string) => ["coverage", projectKey.toUpperCase()] as const;

/** What a project's coverage asks of each type, the rule for every other type first. */
export function useCoverageRules(projectKey: string) {
  return useQuery({
    queryKey: [...coverageKey(projectKey), "rules"],
    queryFn: async (): Promise<CoverageRule[]> => (await api.GET("/projects/{projectKey}/coverage-rules", { params: { path: { projectKey } } })).data!.rules,
  });
}

/** A project's coverage: the totals, and a window of its requirements at the levels asked for. */
export function useCoverage(projectKey: string, only: LevelFilter, offset: number) {
  return useQuery({
    queryKey: [...coverageKey(projectKey), "report", only, offset],
    queryFn: async () =>
      (await api.GET("/projects/{projectKey}/coverage", { params: { path: { projectKey }, query: { ...only, limit: COVERAGE_PAGE_SIZE, offset } } })).data!,
    placeholderData: keepPreviousData,
  });
}

/** A window of a project's traceability matrix, of its rows and of its columns. */
export function useMatrix(projectKey: string, params: MatrixParams, offset: number, columnOffset = 0) {
  return useQuery({
    queryKey: [...coverageKey(projectKey), "matrix", params, offset, columnOffset],
    queryFn: async (): Promise<Matrix> =>
      (
        await api.GET("/projects/{projectKey}/matrix", {
          params: { path: { projectKey }, query: { ...params, limit: MATRIX_PAGE_SIZE, offset, columnLimit: MATRIX_COLUMN_PAGE_SIZE, columnOffset } },
        })
      ).data!.matrix,
    placeholderData: keepPreviousData,
  });
}

/** Where the whole matrix, laid out as params says, downloads as a CSV file. */
export function matrixHref(projectKey: string, params: MatrixParams): string {
  return `${API_BASE}/projects/${encodeURIComponent(projectKey)}/matrix.csv${queryString(params)}`;
}

/** Sets and removes a project's coverage rules; its coverage asks again. */
export function useCoverageRuleActions(projectKey: string) {
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: coverageKey(projectKey) });
  return {
    save: useMutation({
      mutationFn: async (body: CoverageRuleInput): Promise<CoverageRule> =>
        (await api.PUT("/projects/{projectKey}/coverage-rules", { params: { path: { projectKey } }, body })).data!.rule,
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: async (ruleID: string) => {
        await api.DELETE("/coverage-rules/{ruleID}", { params: { path: { ruleID } } });
      },
      onSuccess: refresh,
    }),
  };
}
