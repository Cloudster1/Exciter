import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { MENTION_LIMIT } from "@/config";
import type { Mentionable } from "@/features/editor/extensions";
import { attachmentsQueryKey } from "./attachments";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A requirement or a section, with its latest revision's content and what the reader may do to it. */
export type Requirement = Wire["Requirement"];
export type NewRequirement = Wire["NewRequirement"];
export type RequirementChanges = Wire["RequirementUpdate"];
export type RequirementRow = Wire["GridRow"];
export type TrashItem = Wire["TrashItem"];
export type Priority = NonNullable<Requirement["priority"]>;

/** The priorities in the order a chooser offers them, the highest first. */
export const PRIORITIES: Priority[] = ["must", "should", "could", "wont"];

export const requirementsKey = ["requirements"] as const;

export function requirementQueryKey(key: string) {
  return [...requirementsKey, "one", key.toUpperCase()] as const;
}

function listKey(projectKey: string) {
  return [...requirementsKey, "list", projectKey.toUpperCase()] as const;
}

function trashKey(projectKey: string) {
  return [...requirementsKey, "trash", projectKey.toUpperCase()] as const;
}

/** A window of a project's requirements, in key order, narrowed to those with a label when one is named. */
export function useRequirements(projectKey: string, window: { limit: number; offset: number; label?: string }) {
  return useQuery({
    queryKey: [...listKey(projectKey), window],
    queryFn: async () =>
      (
        await api.GET("/projects/{projectKey}/requirements", {
          params: { path: { projectKey }, query: { limit: window.limit, offset: window.offset, label: window.label ? [window.label] : undefined } },
        })
      ).data!,
    placeholderData: keepPreviousData,
  });
}

/** One requirement by its key or a key it had; the answer carries its current key. */
export function useRequirement(key: string) {
  const queryClient = useQueryClient();
  return useQuery({
    queryKey: requirementQueryKey(key),
    queryFn: async (): Promise<Requirement> => {
      const found = (await api.GET("/requirements/{reqKey}", { params: { path: { reqKey: key } } })).data!.requirement;
      // Found by a key it had: the address is about to name the current one, which then needs no second read.
      if (found.key !== key.toUpperCase()) queryClient.setQueryData(requirementQueryKey(found.key), found);
      return found;
    },
    enabled: Boolean(key),
  });
}

/** One requirement by its id, for a permalink. */
export function useRequirementById(id: string) {
  return useQuery({
    queryKey: [...requirementsKey, "id", id],
    queryFn: async (): Promise<Requirement> =>
      (await api.GET("/requirements/by-id/{requirementID}", { params: { path: { requirementID: id } } })).data!.requirement,
  });
}

/** Keeps a changed requirement where it is read, and has the lists ask again. */
function useStore() {
  const queryClient = useQueryClient();
  return (requirement: Requirement) => {
    queryClient.setQueryData(requirementQueryKey(requirement.key), requirement);
    return queryClient.invalidateQueries({ queryKey: [...requirementsKey, "list"] });
  };
}

export function useCreateRequirement(projectKey: string) {
  const store = useStore();
  return useMutation({
    mutationFn: async (body: NewRequirement): Promise<Requirement> =>
      (await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey } }, body })).data!.requirement,
    onSuccess: store,
  });
}

export function useUpdateRequirement(key: string) {
  const store = useStore();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: RequirementChanges): Promise<Requirement> =>
      (await api.PATCH("/requirements/{reqKey}", { params: { path: { reqKey: key } }, body })).data!.requirement,
    onSuccess: (saved) => {
      // A revision that shows a file keeps it, which the files list says.
      void queryClient.invalidateQueries({ queryKey: attachmentsQueryKey });
      return store(saved);
    },
    // A refusal over a newer revision reads the requirement again, so the next save is made over that one.
    onError: () => queryClient.invalidateQueries({ queryKey: requirementQueryKey(key) }),
  });
}

/** Moves a requirement to its project's trash. */
export function useTrashRequirement(key: string, projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/requirements/{reqKey}", { params: { path: { reqKey: key } } });
    },
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: requirementQueryKey(key) }),
        queryClient.invalidateQueries({ queryKey: [...requirementsKey, "list"] }),
        queryClient.invalidateQueries({ queryKey: trashKey(projectKey) }),
      ]),
  });
}

/** What went to a project's trash, the latest first. */
export function useTrash(projectKey: string, enabled = true) {
  return useQuery({
    queryKey: trashKey(projectKey),
    queryFn: async (): Promise<TrashItem[]> => (await api.GET("/projects/{projectKey}/trash", { params: { path: { projectKey } } })).data!.items,
    enabled,
  });
}

/** Restores, purges or empties a project's trash, and has every list ask again. */
export function useTrashActions(projectKey: string) {
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: requirementsKey });
  const path = { projectKey };
  return {
    restore: useMutation({
      mutationFn: async (id: string) => {
        await api.POST("/projects/{projectKey}/trash/{requirementID}/restore", { params: { path: { ...path, requirementID: id } } });
      },
      onSuccess: refresh,
    }),
    purge: useMutation({
      mutationFn: async (id: string) => {
        await api.DELETE("/projects/{projectKey}/trash/{requirementID}", { params: { path: { ...path, requirementID: id } } });
      },
      onSuccess: refresh,
    }),
    empty: useMutation({
      mutationFn: async () => {
        await api.DELETE("/projects/{projectKey}/trash", { params: { path } });
      },
      onSuccess: refresh,
    }),
  };
}

/** The members an at sign may name, for what was typed after it. */
export async function findPeople(q: string): Promise<Mentionable[]> {
  const people = (await api.GET("/people", { params: { query: { q: q || undefined, limit: MENTION_LIMIT } } })).data?.people ?? [];
  return people.map((person) => ({ id: person.id, name: person.name || person.email, email: person.email }));
}
