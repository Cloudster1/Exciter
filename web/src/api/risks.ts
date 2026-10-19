import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE } from "@/config";
import { api, queryString } from "./client";
import type { components } from "./schema";

// Risks (#259): a project's risk scheme, and the rating a risk carries on it.

type Wire = components["schemas"];

export type RiskScheme = Wire["RiskScheme"];
export type RiskSchemeInput = Wire["RiskSchemeInput"];
export type RiskScale = Wire["RiskScale"];
export type RiskLevel = Wire["RiskLevel"];
export type RiskLevelRef = Wire["RiskLevelRef"];
export type Rating = Wire["RiskRating"];
export type RatingSide = Wire["RiskRatingSide"];
export type RiskView = Wire["RiskRatingView"];
export type RiskDiff = Wire["RiskDiff"];
export type Dimension = RiskScale["dimension"];
export type Method = RiskScheme["method"];

/** The scales in the order a rating gives them. */
export const DIMENSIONS: Dimension[] = ["severity", "probability", "detection"];

const schemeKey = (projectKey: string) => ["risk-scheme", projectKey.toUpperCase()] as const;

/** A project's risk scheme, null until its administrators set one. */
export function useRiskScheme(projectKey: string) {
  return useQuery({
    queryKey: schemeKey(projectKey),
    queryFn: async (): Promise<RiskScheme | null> =>
      (await api.GET("/projects/{projectKey}/risk-scheme", { params: { path: { projectKey } } })).data!.scheme ?? null,
  });
}

/** Sets a project's risk scheme whole; every risk's level follows, so its requirements read again. */
export function useSaveRiskScheme(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: RiskSchemeInput): Promise<RiskScheme> =>
      (await api.PUT("/projects/{projectKey}/risk-scheme", { params: { path: { projectKey } }, body })).data!.scheme,
    onSuccess: (scheme) => {
      queryClient.setQueryData(schemeKey(projectKey), scheme);
      void queryClient.invalidateQueries({ queryKey: ["requirements"] });
    },
  });
}

// Mitigations, the FMEA table and the risk matrix (#260).

export type Mitigation = Wire["RiskMitigation"];
export type Mitigations = Wire["RiskMitigations"];
export type RiskRow = Wire["RiskRow"];
export type RiskRows = Wire["RiskRows"];
export type RiskMatrix = Wire["RiskMatrix"];
export type RiskState = Mitigations["state"];
export type Side = RiskMatrix["side"];

/** What the FMEA table and the matrix read: a query, the side, and a cell of the matrix. */
export type RiskParams = {
  query?: string;
  side?: Side;
  severity?: number;
  probability?: number;
};

const risksKey = (projectKey: string) => ["risks", projectKey.toUpperCase()] as const;

/** The mitigations of a risk and the state they put it in. */
export function useMitigations(reqKey: string, enabled = true) {
  return useQuery({
    queryKey: ["risks", "mitigations", reqKey.toUpperCase()],
    enabled,
    queryFn: async (): Promise<Mitigations> => (await api.GET("/requirements/{reqKey}/mitigations", { params: { path: { reqKey } } })).data!,
  });
}

/** A page of the FMEA table. */
export function useRiskRows(projectKey: string, params: RiskParams, offset: number, limit: number) {
  return useQuery({
    queryKey: [...risksKey(projectKey), "rows", params, offset, limit],
    queryFn: async (): Promise<RiskRows> =>
      (await api.GET("/projects/{projectKey}/risks", { params: { path: { projectKey }, query: { ...params, offset, limit } } })).data!,
    placeholderData: keepPreviousData,
  });
}

/** The risk matrix of one side. */
export function useRiskMatrix(projectKey: string, params: Pick<RiskParams, "query" | "side">) {
  return useQuery({
    queryKey: [...risksKey(projectKey), "matrix", params],
    queryFn: async (): Promise<RiskMatrix> =>
      (await api.GET("/projects/{projectKey}/risk-matrix", { params: { path: { projectKey }, query: params } })).data!.matrix,
    placeholderData: keepPreviousData,
  });
}

/** Reads the FMEA table and the matrix again, as after a rating changed in place. */
export function useRefreshRisks(projectKey: string) {
  const queryClient = useQueryClient();
  return () => queryClient.invalidateQueries({ queryKey: risksKey(projectKey) });
}

/** Where the FMEA table downloads as an Excel workbook. */
export function risksHref(projectKey: string, params: Pick<RiskParams, "query" | "side">): string {
  return `${API_BASE}/projects/${encodeURIComponent(projectKey)}/risks.xlsx${queryString(params)}`;
}

// The risk report (#261).

export type RiskReport = Wire["RiskReport"];

/** A project's risk report, as its print view reads it. */
export function useRiskReport(projectKey: string, query?: string) {
  return useQuery({
    queryKey: [...risksKey(projectKey), "report", query ?? ""],
    queryFn: async (): Promise<RiskReport> =>
      (await api.GET("/projects/{projectKey}/risk-report", { params: { path: { projectKey }, query: { query: query || undefined } } })).data!.report,
  });
}

/** Where the risk report downloads, as PDF or Word. */
export function riskReportHref(projectKey: string, format: "pdf" | "docx", query?: string): string {
  return `${API_BASE}/projects/${encodeURIComponent(projectKey)}/risk-report.${format}${queryString({ query: query || undefined })}`;
}
