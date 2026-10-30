import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { api } from "./client";
import type { components } from "./schema";

// Test results from CI (#200): what verifies a requirement, per test and branch, and the branch whose results count.

type Wire = components["schemas"];

export type TestResult = Wire["TestResult"];
export type TestOutcome = TestResult["outcome"];
export type EvidenceSettings = Wire["EvidenceSettings"];

/** Outcomes, worst first, as a requirement's tests are summed up. */
export const TEST_OUTCOMES: TestOutcome[] = ["errored", "failed", "skipped", "passed"];

const resultsKey = (reqKey: string) => ["test-results", reqKey.toUpperCase()] as const;
const settingsKey = (projectKey: string) => ["evidence-settings", projectKey.toUpperCase()] as const;

/** The latest result of each test that verifies a requirement, the counting branch first. */
export function useTestResults(reqKey: string, enabled = true) {
  return useQuery({
    queryKey: resultsKey(reqKey),
    queryFn: async () => (await api.GET("/requirements/{reqKey}/test-results", { params: { path: { reqKey } } })).data!,
    enabled,
  });
}

/** The branch whose evidence counts for a project. */
export function useEvidenceSettings(projectKey: string) {
  return useQuery({
    queryKey: settingsKey(projectKey),
    queryFn: async (): Promise<EvidenceSettings> =>
      (await api.GET("/projects/{projectKey}/evidence-settings", { params: { path: { projectKey } } })).data!.settings,
  });
}

/** Names the counting branch; coverage and every requirement's results read it again. */
export function useSaveEvidenceSettings(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (countingBranch: string): Promise<EvidenceSettings> =>
      (await api.PUT("/projects/{projectKey}/evidence-settings", { params: { path: { projectKey } }, body: { countingBranch } })).data!.settings,
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: settingsKey(projectKey) }),
        queryClient.invalidateQueries({ queryKey: ["test-results"] }),
        queryClient.invalidateQueries({ queryKey: ["coverage", projectKey.toUpperCase()] }),
      ]);
    },
  });
}
