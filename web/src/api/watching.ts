import { keepPreviousData, useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { WATCHERS_LIMIT, WATCHES_PAGE_SIZE } from "@/config";
import { api } from "./client";
import { projectQueryKey, projectsQueryKey } from "./projects";
import { requirementQueryKey, requirementsKey, type Requirement } from "./requirements";
import type { components } from "./schema";

// Watching requirements and projects (#34), after Stator's api/watching.ts.

type Wire = components["schemas"];

/** How the caller follows one requirement: by itself, through its project, or not. */
export type Watching = Wire["Watching"];
/** Somebody who hears about a requirement, and through which watch. */
export type Watcher = Wire["Watcher"];
/** One of the caller's own watches. */
export type Watch = Wire["Watch"];

export const watchesQueryKey = ["watches"] as const;

function watchersQueryKey(reqKey: string) {
  return [...requirementQueryKey(reqKey), "watchers"] as const;
}

// A project watch changes how every requirement in it is followed, so each read so far is read again.
function rewatched(queryClient: QueryClient) {
  void queryClient.invalidateQueries({ queryKey: requirementsKey });
  void queryClient.invalidateQueries({ queryKey: projectsQueryKey });
  void queryClient.invalidateQueries({ queryKey: watchesQueryKey });
}

/** Watches a requirement by itself, or stops; unwatching keeps the caller's own edits from watching it again. */
export function useWatchRequirement(reqKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (watch: boolean): Promise<Watching> => {
      const params = { params: { path: { reqKey } } };
      if (watch) return (await api.PUT("/requirements/{reqKey}/watch", params)).data!.watching;
      await api.DELETE("/requirements/{reqKey}/watch", params);
      const current = queryClient.getQueryData<Requirement>(requirementQueryKey(reqKey));
      return { requirement: false, project: current?.watching.project ?? false };
    },
    onSuccess: (watching) => {
      queryClient.setQueryData<Requirement>(requirementQueryKey(reqKey), (current) => (current ? { ...current, watching } : current));
      void queryClient.invalidateQueries({ queryKey: watchersQueryKey(reqKey) });
      void queryClient.invalidateQueries({ queryKey: watchesQueryKey });
    },
  });
}

/** Watches every requirement of a project, now and later, or stops; the watches on its requirements stay either way. */
export function useWatchProject(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (watch: boolean) => {
      const params = { params: { path: { projectKey } } };
      if (watch) await api.PUT("/projects/{projectKey}/watch", params);
      else await api.DELETE("/projects/{projectKey}/watch", params);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: projectQueryKey(projectKey) });
      rewatched(queryClient);
    },
  });
}

/** Who hears about a requirement, by name. */
export function useWatchers(reqKey: string) {
  return useQuery({
    queryKey: watchersQueryKey(reqKey),
    queryFn: async () => (await api.GET("/requirements/{reqKey}/watchers", { params: { path: { reqKey }, query: { limit: WATCHERS_LIMIT } } })).data!,
  });
}

/** The caller's own watches, a page of the list at a time, the latest first. */
export function useWatches(offset: number) {
  return useQuery({
    queryKey: [...watchesQueryKey, offset],
    queryFn: async () => (await api.GET("/watches", { params: { query: { limit: WATCHES_PAGE_SIZE, offset: offset || undefined } } })).data!,
    placeholderData: keepPreviousData,
  });
}

/** Ends one of the caller's watches from the list, whatever it is on. */
export function useStopWatching() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (watch: Watch) => {
      if (watch.requirement) await api.DELETE("/requirements/{reqKey}/watch", { params: { path: { reqKey: watch.requirement.key } } });
      else await api.DELETE("/projects/{projectKey}/watch", { params: { path: { projectKey: watch.projectKey } } });
      return watch;
    },
    onSuccess: () => rewatched(queryClient),
  });
}
