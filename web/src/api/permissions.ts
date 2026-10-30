import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { PICKER_LIMIT } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** Somebody a picker chose or a rule names: a person or a group, Everyone among the groups, with the name to show. */
export interface Subject {
  type: "user" | "group";
  id: string | null;
  name: string;
}
export type Person = Wire["Person"];
export type Group = Wire["Group"];
/** What one person may do in one project, and the steps behind each right, as the database answers it. */
export type AccessReport = Wire["AccessReport"];
export type AccessRight = Wire["AccessRight"];
export type AccessStep = Wire["AccessStep"];
export type Right = AccessRight["right"];

/** One string per subject, for keys and for telling two apart. */
export function subjectKey(subject: Pick<Subject, "type" | "id">): string {
  return `${subject.type}:${subject.id ?? ""}`;
}

export const projectAccessQueryKey = ["project-access"] as const;

/** What one person may do in a project and why, for its administrators. */
export function useInspectAccess(projectKey: string, userId: string) {
  return useQuery({
    queryKey: [...projectAccessQueryKey, projectKey.toUpperCase(), userId],
    queryFn: async (): Promise<AccessReport> =>
      (await api.GET("/projects/{projectKey}/access/{userID}", { params: { path: { projectKey, userID: userId } } })).data!.access,
  });
}

export interface SubjectSearchResult {
  people: Person[];
  groups: Group[];
  error: Error | null;
  /** The lists answer q itself, not a previous text kept up while q is asked. */
  current: boolean;
}

/** The people, and the groups unless left out, whose names start with q. */
export function useSubjectSearch(q: string, enabled: boolean, withGroups = true): SubjectSearchResult {
  const query = { q: q || undefined, limit: PICKER_LIMIT };
  const people = useQuery({
    queryKey: ["people", query],
    queryFn: async (): Promise<Person[]> => (await api.GET("/people", { params: { query } })).data!.people,
    enabled,
    placeholderData: keepPreviousData,
  });
  const groups = useQuery({
    queryKey: ["groups", query],
    queryFn: async (): Promise<Group[]> => (await api.GET("/groups", { params: { query } })).data!.groups,
    enabled: enabled && withGroups,
    placeholderData: keepPreviousData,
  });
  return {
    people: people.data ?? [],
    groups: withGroups ? (groups.data ?? []) : [],
    error: people.error ?? groups.error,
    current: people.data !== undefined && !people.isPlaceholderData && (!withGroups || (groups.data !== undefined && !groups.isPlaceholderData)),
  };
}
