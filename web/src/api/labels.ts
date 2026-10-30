import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { LABEL_NAME_MAX_LENGTH, LABEL_PAGE_SIZE, LABEL_SUGGESTION_LIMIT } from "@/config";
import { api } from "./client";
import { requirementQueryKey, type Requirement } from "./requirements";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A label the reader can see somewhere, with how many of their requirements carry it. */
export type LabelSuggestion = Wire["LabelSuggestion"];
/** A requirement that carries a label, and where it lives. */
export type LabelledRequirement = Wire["LabelledRequirement"];

export const labelsQueryKey = ["labels"] as const;

const LABEL = /^[\p{L}\p{Nd}][\p{L}\p{Nd}_.-]*$/u;

/** Why a typed name cannot be a label, or undefined when it can. */
export type LabelProblem = "invalid" | "tooLong";

/**
 * A label as the API stores it: lower case with spaces as hyphens, as label.Normalize reads it, so the client
 * can tell a new label from one it has.
 */
export function normalizeLabel(raw: string): { name: string; problem?: LabelProblem } {
  const name = raw.trim().toLowerCase().split(/\s+/).filter(Boolean).join("-");
  if (!LABEL.test(name)) return { name, problem: "invalid" };
  if ([...name].length > LABEL_NAME_MAX_LENGTH) return { name, problem: "tooLong" };
  return { name };
}

/** Labels on requirements the reader may view that start with what was typed, the most used first. */
export function useLabelSuggestions(typed: string, enabled: boolean) {
  return useQuery({
    queryKey: [...labelsQueryKey, "suggest", typed],
    queryFn: async (): Promise<LabelSuggestion[]> =>
      (await api.GET("/labels", { params: { query: { q: typed || undefined, limit: LABEL_SUGGESTION_LIMIT } } })).data!.labels,
    enabled,
    placeholderData: keepPreviousData,
  });
}

/** The requirements with a label, a page of the list at a time, in one project or all of them. */
export function useLabelledRequirements(name: string, projectKey: string | undefined, offset: number) {
  return useQuery({
    queryKey: [...labelsQueryKey, "requirements", name, projectKey ?? "", offset],
    queryFn: async () =>
      (
        await api.GET("/labels/{labelName}/requirements", {
          params: { path: { labelName: name }, query: { project: projectKey, limit: LABEL_PAGE_SIZE, offset: offset || undefined } },
        })
      ).data!,
    placeholderData: keepPreviousData,
  });
}

/** Keeps a requirement's new labels where it is read, and has every list that shows labels ask again. */
function useRelabel(reqKey: string) {
  const queryClient = useQueryClient();
  return (next: (labels: string[]) => string[]) => {
    queryClient.setQueryData<Requirement>(requirementQueryKey(reqKey), (current) => (current ? { ...current, labels: next(current.labels) } : current));
    void queryClient.invalidateQueries({ queryKey: labelsQueryKey });
    void queryClient.invalidateQueries({ queryKey: ["requirements", "list"] });
    void queryClient.invalidateQueries({ queryKey: ["history"] });
  };
}

/** Puts a label on a requirement; its labels are what the API answers. */
export function useAddLabel(reqKey: string) {
  const relabel = useRelabel(reqKey);
  return useMutation({
    mutationFn: async (name: string): Promise<string[]> =>
      (await api.POST("/requirements/{reqKey}/labels", { params: { path: { reqKey } }, body: { name } })).data!.labels,
    onSuccess: (labels) => relabel(() => labels),
  });
}

/** Takes a label off a requirement. */
export function useRemoveLabel(reqKey: string) {
  const relabel = useRelabel(reqKey);
  return useMutation({
    mutationFn: async (name: string): Promise<string> => {
      await api.DELETE("/requirements/{reqKey}/labels/{labelName}", { params: { path: { reqKey, labelName: name } } });
      return name;
    },
    onSuccess: (name) => relabel((labels) => labels.filter((each) => each !== name)),
  });
}
