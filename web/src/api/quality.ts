import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { QUALITY_CHECK_DEBOUNCE_MS, QUALITY_PAGE_SIZE } from "@/config";
import { useDebounced } from "@/lib/debounce";
import { api } from "./client";
import { requirementQueryKey } from "./requirements";
import type { components } from "./schema";

// Writing rules (#207): a draft checked as it is typed, a revision's findings and the ones accepted, a project's
// scores, and the rules and words a project sets.

type Wire = components["schemas"];

export type QualityCheck = Wire["QualityCheck"];
export type QualityFinding = Wire["QualityFinding"];
export type QualityFindings = Wire["QualityFindings"];
export type QualitySentence = Wire["QualitySentence"];
export type QualitySettings = Wire["QualitySettings"];
export type QualitySettingsInput = Wire["QualitySettingsInput"];
export type QualityRule = Wire["QualityRule"];
export type QualityRuleSetting = Wire["QualityRuleSetting"];
export type QualitySummary = Wire["QualitySummary"];
export type QualityWords = Wire["QualityWords"];
export type RuleId = QualityRule["id"];
export type Severity = QualityFinding["severity"];
export type EarsPattern = QualitySentence["pattern"];

/** Every rule, in the order the settings and the help list them. */
export const RULE_IDS: readonly RuleId[] = [
  "vague",
  "unbounded",
  "escape",
  "placeholder",
  "and_or",
  "multiple_shall",
  "negative",
  "passive",
  "pronoun",
  "ears",
  "avoid_term",
];
/** The rules a project adds words to and excepts words from. */
export const WORD_RULES = ["vague", "unbounded", "escape", "placeholder"] as const;
export type WordRule = (typeof WORD_RULES)[number];
export const SEVERITIES: readonly Severity[] = ["error", "warning", "hint"];

/** What the check reads of a requirement being written. */
export interface Draft {
  title: string;
  statement: unknown;
  typeId?: string;
  requirementKey?: string;
}

const checkKey = ["quality-check"] as const;
const findingsKey = (reqKey: string) => ["quality", reqKey.toUpperCase()] as const;
const summaryKey = (projectKey: string) => ["project-quality", projectKey.toUpperCase()] as const;
const settingsKey = (projectKey: string) => ["quality-settings", projectKey.toUpperCase()] as const;

/** A draft checked once typing pauses. `current` says the answer is of the draft as it is now, not one it moved past. */
export function useQualityCheck(projectKey: string, draft: Draft, enabled = true) {
  const asked = JSON.stringify(draft);
  const settled = useDebounced(asked, QUALITY_CHECK_DEBOUNCE_MS);
  // The draft is the key, so an answer to an older draft is never taken for the latest one's.
  const query = useQuery({
    queryKey: [...checkKey, projectKey.toUpperCase(), settled],
    queryFn: async (): Promise<QualityCheck> =>
      (await api.POST("/projects/{projectKey}/quality-check", { params: { path: { projectKey } }, body: JSON.parse(settled) as Draft })).data!.check,
    enabled,
    placeholderData: keepPreviousData,
    staleTime: Number.POSITIVE_INFINITY,
    retry: false,
  });
  return { check: query.data, current: Boolean(query.data) && !query.isPlaceholderData && settled === asked, error: query.error };
}

/** The findings of a revision of a requirement, its current one when none is named. */
export function useQuality(reqKey: string, revision?: number, enabled = true) {
  return useQuery({
    queryKey: [...findingsKey(reqKey), revision ?? "current"],
    queryFn: async (): Promise<QualityFindings> =>
      (await api.GET("/requirements/{reqKey}/quality", { params: { path: { reqKey }, query: revision === undefined ? {} : { revision } } })).data!.quality,
    enabled,
  });
}

/** What changes with an acceptance: the requirement's score wherever it shows, and the draft checks that carry it. */
function useAfterAcceptance(reqKey: string) {
  const queryClient = useQueryClient();
  return async (quality: QualityFindings) => {
    queryClient.setQueryData([...findingsKey(reqKey), quality.revision], quality);
    await Promise.all(
      [requirementQueryKey(reqKey), findingsKey(reqKey), checkKey, ["tree"], ["spec"], ["requirements", "list"], ["project-quality"]].map((queryKey) =>
        queryClient.invalidateQueries({ queryKey }),
      ),
    );
  };
}

/** Accepts a finding of the current revision with a reason; it stays shown and no longer counts. */
export function useAcceptFinding(reqKey: string) {
  const after = useAfterAcceptance(reqKey);
  return useMutation({
    mutationFn: async (body: { revision: number; key: string; reason: string }): Promise<QualityFindings> =>
      (await api.POST("/requirements/{reqKey}/quality/accepted", { params: { path: { reqKey } }, body })).data!.quality,
    onSuccess: after,
  });
}

/** Takes an acceptance back, so the finding counts again. */
export function useWithdrawAcceptance(reqKey: string) {
  const after = useAfterAcceptance(reqKey);
  return useMutation({
    mutationFn: async ({ revision, key }: { revision: number; key: string }): Promise<QualityFindings> =>
      (
        await api.DELETE("/requirements/{reqKey}/quality/accepted/{findingKey}", {
          // The route reads the revision from the query, which the generated document does not list yet.
          params: { path: { reqKey, findingKey: key }, query: { revision } },
        })
      ).data!.quality,
    onSuccess: after,
  });
}

/** A project's scores: the bands, the rules broken most, and a window of its requirements lowest first. */
export function useProjectQuality(projectKey: string, query: string, offset: number) {
  return useQuery({
    queryKey: [...summaryKey(projectKey), query, offset],
    queryFn: async (): Promise<QualitySummary> =>
      (
        await api.GET("/projects/{projectKey}/quality", {
          params: { path: { projectKey }, query: { ...(query ? { query } : {}), limit: QUALITY_PAGE_SIZE, offset } },
        })
      ).data!.quality,
    placeholderData: keepPreviousData,
    retry: false,
  });
}

/** The rules a project switched on, their severities and types, and its word lists. */
export function useQualitySettings(projectKey: string) {
  return useQuery({
    queryKey: settingsKey(projectKey),
    queryFn: async (): Promise<QualitySettings> =>
      (await api.GET("/projects/{projectKey}/quality-settings", { params: { path: { projectKey } } })).data!.settings,
  });
}

/** Saves every rule and word list at once; the scores follow. */
export function useSaveQualitySettings(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: QualitySettingsInput): Promise<QualitySettings> =>
      (await api.PUT("/projects/{projectKey}/quality-settings", { params: { path: { projectKey } }, body })).data!.settings,
    onSuccess: async (saved) => {
      queryClient.setQueryData(settingsKey(projectKey), saved);
      await Promise.all(
        [["quality"], checkKey, ["tree"], ["spec"], ["requirements"], summaryKey(projectKey)].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
      );
    },
  });
}

/** Every rule there is, with its defaults; the same for every project. */
export function useQualityRules() {
  return useQuery({
    queryKey: ["quality-rules"],
    queryFn: async (): Promise<QualityRule[]> => (await api.GET("/quality-rules")).data!.rules,
    staleTime: Number.POSITIVE_INFINITY,
  });
}
