import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE, IMPORT_POLL_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";
import type { GridParams } from "./search";

type Wire = components["schemas"];

/** A CSV file on its way into a project: its mapping and problems, then its progress and what it did. */
export type Import = Wire["Import"];
export type MappingInput = Wire["ImportMapping"];
export type ImportWord = Wire["ImportWord"];
export type ImportPerson = Wire["ImportPerson"];
export type ImportColumn = Wire["ImportColumn"];
export type ImportState = Import["state"];

const importKey = (id: string) => ["imports", id] as const;

/** Whether an import has stopped moving, so nothing more is worth asking. */
export function ended(state: ImportState): boolean {
  return state === "done" || state === "failed" || state === "cancelled";
}

/** One import, read again every moment while the worker has it. */
export function useImport(id: string | undefined) {
  return useQuery({
    queryKey: importKey(id ?? ""),
    queryFn: async (): Promise<Import> => (await api.GET("/imports/{importID}", { params: { path: { importID: id! } } })).data!.import,
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

/** Uploads a file, mapping its columns from its headers; nothing is written to the project yet. */
export function useUploadImport(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (file: File): Promise<Import> =>
      (await api.POST("/projects/{projectKey}/import", { params: { path: { projectKey } }, body: fileForm(file) })).data!.import,
    onSuccess: (made) => queryClient.setQueryData(importKey(made.id), made),
  });
}

/** Maps, starts and discards one import. */
export function useImportActions(id: string) {
  const queryClient = useQueryClient();
  const keep = (changed: Import) => queryClient.setQueryData(importKey(id), changed);
  return {
    map: useMutation({
      mutationFn: async (body: MappingInput): Promise<Import> =>
        (await api.PUT("/imports/{importID}/mapping", { params: { path: { importID: id } }, body })).data!.import,
      onSuccess: keep,
    }),
    start: useMutation({
      mutationFn: async (): Promise<Import> => (await api.POST("/imports/{importID}/start", { params: { path: { importID: id } } })).data!.import,
      onSuccess: keep,
      // A refused start keeps the problems it found, which the import read again shows.
      onError: () => queryClient.invalidateQueries({ queryKey: importKey(id) }),
    }),
    cancel: useMutation({
      mutationFn: async () => {
        await api.DELETE("/imports/{importID}", { params: { path: { importID: id } } });
      },
      onSuccess: () => queryClient.invalidateQueries({ queryKey: importKey(id) }),
    }),
  };
}

/** Where a project's grid downloads as CSV, narrowed and ordered as shown, with the columns asked for. */
export function exportHref(projectKey: string, params: GridParams, columns: string[]): string {
  const query = new URLSearchParams();
  for (const [name, value] of Object.entries(params)) {
    for (const each of Array.isArray(value) ? value : [value]) {
      if (each !== undefined && each !== null) query.append(name, String(each));
    }
  }
  for (const column of columns) query.append("column", column);
  const rest = query.toString();
  return `${API_BASE}/projects/${encodeURIComponent(projectKey)}/export.csv${rest ? `?${rest}` : ""}`;
}
