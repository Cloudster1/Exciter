import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { PICKER_LIMIT } from "@/config";
import { api } from "./client";
import { projectQueryKey, projectsQueryKey } from "./projects";
import type { components } from "./schema";

type Wire = components["schemas"];

/** Somebody a grant names, with the name to show. */
export type Subject = Wire["Subject"];
export type SubjectRef = Wire["SubjectRef"];
export type Grant = Wire["ProjectGrant"];
export type Role = Grant["role"];
export type Person = Wire["Person"];
export type Group = Wire["Group"];
/** What one person may do in one project, and the steps behind each right, as the database answers it. */
export type AccessReport = Wire["AccessReport"];
export type AccessRight = Wire["AccessRight"];
export type AccessStep = Wire["AccessStep"];
export type Right = AccessRight["right"];

/** The roles in the order the table offers them, the narrowest first. */
export const ROLES: Role[] = ["viewer", "reviewer", "editor", "admin"];

/** The request form of a subject: everyone carries no id. */
export function subjectRef(subject: Subject): SubjectRef {
  return subject.type === "everyone" || !subject.id ? { type: subject.type } : { type: subject.type, id: subject.id };
}

/** One string per subject, for keys and for telling two apart. */
export function subjectKey(subject: Pick<Subject, "type" | "id">): string {
  return subject.type === "everyone" ? "everyone" : `${subject.type}:${subject.id ?? ""}`;
}

function grantsQueryKey(projectKey: string) {
  return ["project-grants", projectKey.toUpperCase()] as const;
}

/** Who holds which role in a project; only its administrators may ask. */
export function useProjectGrants(projectKey: string, enabled: boolean) {
  return useQuery({
    queryKey: grantsQueryKey(projectKey),
    queryFn: async (): Promise<Grant[]> => (await api.GET("/projects/{projectKey}/grants", { params: { path: { projectKey } } })).data!.grants,
    enabled,
  });
}

/** Replaces a project's whole table of grants; what everybody may do there changes with it. */
export function useSetProjectGrants(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (grants: Grant[]): Promise<Grant[]> =>
      (
        await api.PUT("/projects/{projectKey}/grants", {
          params: { path: { projectKey } },
          body: { grants: grants.map((grant) => ({ subject: subjectRef(grant.subject), role: grant.role })) },
        })
      ).data!.grants,
    onSuccess: async (grants) => {
      queryClient.setQueryData(grantsQueryKey(projectKey), grants);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: projectQueryKey(projectKey) }),
        queryClient.invalidateQueries({ queryKey: [...projectsQueryKey, "list"] }),
        queryClient.invalidateQueries({ queryKey: ["project-access", projectKey.toUpperCase()] }),
      ]);
    },
  });
}

/** What one person may do in a project and why, for its administrators. */
export function useInspectAccess(projectKey: string, userId: string) {
  return useQuery({
    queryKey: ["project-access", projectKey.toUpperCase(), userId],
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
