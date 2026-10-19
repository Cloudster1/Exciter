import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
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
