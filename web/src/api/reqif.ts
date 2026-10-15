import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE, IMPORT_POLL_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A ReqIF file on its way into a project: what its schema matches, then its progress and what it did. */
export type ReqIFImport = Wire["ReqIFImport"];
export type ReqIFSchemaItem = Wire["ReqIFSchemaItem"];
export type ReqIFLoss = Wire["ReqIFLoss"];
export type ReqIFState = ReqIFImport["state"];

/** What a schema item may be given besides an id of the project's: made new, or left out. */
export const CHOICE_CREATE = "create";
export const CHOICE_SKIP = "skip";

const reqifKey = (id: string) => ["reqif-imports", id] as const;

/** Whether an import has stopped moving, so nothing more is worth asking. */
export function reqifEnded(state: ReqIFState): boolean {
  return state === "done" || state === "failed" || state === "cancelled";
}

/** One import, read again every moment while the worker has it. */
export function useReqIFImport(id: string | undefined) {
  return useQuery({
    queryKey: reqifKey(id ?? ""),
    queryFn: async (): Promise<ReqIFImport> => (await api.GET("/reqif-imports/{importID}", { params: { path: { importID: id! } } })).data!.import,
    enabled: Boolean(id),
    refetchInterval: (query) => {
      const state = query.state.data?.state;
      return state === "queued" || state === "running" ? IMPORT_POLL_MS : false;
    },
  });
}

// A multipart body in the generated types is an object of strings; the one sent is the form itself.
function fileForm(file: File): { file: string } {
  const form = new FormData();
  form.append("file", file, file.name);
  return form as unknown as { file: string };
}

/** Uploads a .reqif or .reqifz; nothing is written to the project yet. */
export function useUploadReqIF(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (file: File): Promise<ReqIFImport> =>
      (await api.POST("/projects/{projectKey}/reqif-imports", { params: { path: { projectKey } }, body: fileForm(file) })).data!.import,
    onSuccess: (made) => queryClient.setQueryData(reqifKey(made.id), made),
  });
}

/** Maps, starts and discards one import. */
export function useReqIFActions(id: string) {
  const queryClient = useQueryClient();
  const keep = (changed: ReqIFImport) => queryClient.setQueryData(reqifKey(id), changed);
  return {
    choose: useMutation({
      mutationFn: async (choices: Record<string, string>): Promise<ReqIFImport> =>
        (await api.PUT("/reqif-imports/{importID}/choices", { params: { path: { importID: id } }, body: { choices } })).data!.import,
      onSuccess: keep,
    }),
    start: useMutation({
      mutationFn: async (): Promise<ReqIFImport> => (await api.POST("/reqif-imports/{importID}/start", { params: { path: { importID: id } } })).data!.import,
      onSuccess: keep,
      // A refused start keeps the problems it found, which the import read again shows.
      onError: () => queryClient.invalidateQueries({ queryKey: reqifKey(id) }),
    }),
    cancel: useMutation({
      mutationFn: async () => {
        await api.DELETE("/reqif-imports/{importID}", { params: { path: { importID: id } } });
      },
      onSuccess: () => queryClient.invalidateQueries({ queryKey: reqifKey(id) }),
    }),
  };
}

/** Where a project, or a section and what is below it, downloads as ReqIF; an archive brings its files along. */
export function reqifExportHref(projectKey: string, archive: boolean, section?: string): string {
  const path = `${API_BASE}/projects/${encodeURIComponent(projectKey)}/export.${archive ? "reqifz" : "reqif"}`;
  return section ? `${path}?section=${encodeURIComponent(section)}` : path;
}
