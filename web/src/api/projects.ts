import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A project: its key, its name, its settings and what the reader may do in it. */
export type Project = Wire["Project"];
export type NewProject = Wire["NewProject"];
export type ProjectChanges = Wire["ProjectUpdate"];
export type KeyCheck = Wire["ProjectKeyCheck"];
/** A structure a new project can start from, naming what it makes. */
export type ProjectTemplate = Wire["ProjectTemplate"];

export const projectsQueryKey = ["projects"] as const;

export function projectQueryKey(key: string) {
  return [...projectsQueryKey, "one", key.toUpperCase()] as const;
}

/** The projects the reader may view; archived ones only when asked for. */
export function useProjects(includeArchived = false, enabled = true) {
  return useQuery({
    queryKey: [...projectsQueryKey, "list", { archived: includeArchived }],
    queryFn: async (): Promise<Project[]> => (await api.GET("/projects", { params: { query: includeArchived ? { archived: true } : {} } })).data!.projects,
    enabled,
  });
}

/** One project by its key or a key it had; the answer carries its current key. */
export function useProject(key: string) {
  const queryClient = useQueryClient();
  return useQuery({
    queryKey: projectQueryKey(key),
    queryFn: async (): Promise<Project> => {
      const project = (await api.GET("/projects/{projectKey}", { params: { path: { projectKey: key } } })).data!.project;
      // Found by a key it had: the address is about to name the current one, which then needs no second read.
      if (project.key !== key.toUpperCase()) queryClient.setQueryData(projectQueryKey(project.key), project);
      return project;
    },
    enabled: Boolean(key),
  });
}

/** A free key for a name, or whether a key typed is free; nothing is asked for blank input. */
export function useKeyCheck(query: { name?: string; key?: string }) {
  return useQuery({
    queryKey: [...projectsQueryKey, "key-check", query],
    queryFn: async (): Promise<KeyCheck> => (await api.GET("/projects/key-check", { params: { query } })).data!.check,
    enabled: Boolean(query.name?.trim() || query.key?.trim()),
    staleTime: 0,
  });
}

/** Keeps a changed project wherever it is read, under its current key and, after a key change, its old one. */
function useStore() {
  const queryClient = useQueryClient();
  return (project: Project, oldKey?: string) => {
    queryClient.setQueryData(projectQueryKey(project.key), project);
    if (oldKey && oldKey.toUpperCase() !== project.key) queryClient.setQueryData(projectQueryKey(oldKey), project);
    return queryClient.invalidateQueries({ queryKey: [...projectsQueryKey, "list"] });
  };
}

/** The templates a new project can start from, in the order offered. */
export function useProjectTemplates() {
  return useQuery({
    queryKey: [...projectsQueryKey, "templates"],
    queryFn: async (): Promise<ProjectTemplate[]> => (await api.GET("/project-templates")).data!.templates,
    staleTime: Number.POSITIVE_INFINITY,
  });
}

export function useCreateProject() {
  const store = useStore();
  return useMutation({
    mutationFn: async (body: NewProject): Promise<Project> => (await api.POST("/projects", { body })).data!.project,
    onSuccess: (made) => store(made),
  });
}

export function useUpdateProject(key: string) {
  const store = useStore();
  return useMutation({
    mutationFn: async (body: ProjectChanges): Promise<Project> =>
      (await api.PATCH("/projects/{projectKey}", { params: { path: { projectKey: key } }, body })).data!.project,
    onSuccess: (changed) => store(changed),
  });
}

export function useChangeProjectKey(key: string) {
  const store = useStore();
  return useMutation({
    mutationFn: async (next: string): Promise<Project> =>
      (await api.PUT("/projects/{projectKey}/key", { params: { path: { projectKey: key } }, body: { key: next } })).data!.project,
    onSuccess: (changed) => store(changed, key),
  });
}

export function useSetArchived(key: string) {
  const store = useStore();
  return useMutation({
    mutationFn: async (archived: boolean): Promise<Project> => {
      const params = { params: { path: { projectKey: key } } };
      const answer = archived ? await api.PUT("/projects/{projectKey}/archive", params) : await api.DELETE("/projects/{projectKey}/archive", params);
      return answer.data!.project;
    },
    onSuccess: (changed) => store(changed),
  });
}

export function useDeleteProject(key: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/projects/{projectKey}", { params: { path: { projectKey: key } } });
    },
    onSuccess: () => {
      queryClient.removeQueries({ queryKey: projectQueryKey(key) });
      return queryClient.invalidateQueries({ queryKey: [...projectsQueryKey, "list"] });
    },
  });
}

/** Where a key comes from while the server has not answered: initials of several words, the start of one. Mirrors the API's SuggestKey. */
export function suggestKey(name: string, maxLength: number): string {
  const words = name
    .toUpperCase()
    .split(/[^\p{L}\p{N}]+/u)
    .map((word) => word.replace(/[^A-Z0-9]/g, ""))
    .filter(Boolean);
  if (words.length === 0) return "";
  let key = words.length === 1 ? words[0]! : words.map((word) => word[0]).join("");
  key = key.slice(0, maxLength);
  if (!/^[A-Z]/.test(key)) return "";
  return key.length === 1 ? key + key : key;
}
