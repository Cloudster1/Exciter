import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE } from "@/config";
import type { ThemeSpec } from "@/lib/theme-css";
import { api } from "./client";
import type { components } from "./schema";

export type { ThemeSpec } from "@/lib/theme-css";
export { emptySpec, themeAssetURL } from "@/lib/theme-css";

type Wire = components["schemas"];

/** One uploaded file of a theme. */
export type ThemeAsset = Wire["Asset"];

/** A saved theme, with its spec in the compiler's shape. */
export type Theme = Omit<Wire["Theme"], "spec"> & { spec: ThemeSpec };

/** A theme shipped with the product, to start one's own from. */
export type ThemeExample = Omit<Wire["Example"], "spec"> & { spec: ThemeSpec };

export interface ThemeInput {
  name?: string;
  shared?: boolean;
  spec?: ThemeSpec;
}

/** Where the theme the reader sees came from: their choice, the organization's default, or nothing. */
export type ThemeSource = "chosen" | "organization" | "";

// The server leaves out a backdrop rather than sending null, and takes either;
// the compiler's null is dropped on the way out so the wire type holds.
function toWire(spec: ThemeSpec): Wire["Spec"] {
  const { backdrop, ...rest } = spec;
  return backdrop ? { ...rest, backdrop } : rest;
}

function fromWire<T extends { spec: Wire["Spec"] }>(value: T): Omit<T, "spec"> & { spec: ThemeSpec } {
  return value as Omit<T, "spec"> & { spec: ThemeSpec };
}

export const themesQueryKey = ["themes"] as const;
export const activeThemeQueryKey = ["themes", "active"] as const;
export const defaultThemeQueryKey = ["themes", "default"] as const;
export const themeExamplesQueryKey = ["themes", "examples"] as const;

export function useThemes() {
  return useQuery({
    queryKey: themesQueryKey,
    queryFn: async () => {
      const { data } = await api.GET("/themes");
      return { themes: (data?.themes ?? []).map(fromWire) };
    },
  });
}

export function useTheme(id: string | undefined) {
  return useQuery({
    queryKey: [...themesQueryKey, id],
    queryFn: async () => {
      const { data } = await api.GET("/themes/{themeID}", { params: { path: { themeID: id! } } });
      return { theme: fromWire(data!.theme) };
    },
    enabled: Boolean(id),
  });
}

export function useThemeExamples() {
  return useQuery({
    queryKey: themeExamplesQueryKey,
    queryFn: async () => {
      const { data } = await api.GET("/themes/examples");
      return { examples: (data?.examples ?? []).map(fromWire) };
    },
    staleTime: Infinity,
  });
}

/** The theme the reader sees; null is the built-in one. */
export function useActiveTheme() {
  return useQuery({
    queryKey: activeThemeQueryKey,
    queryFn: async () => {
      const { data } = await api.GET("/themes/active");
      return { theme: data?.theme ? fromWire(data.theme) : null, source: (data?.source ?? "") as ThemeSource };
    },
  });
}

/** The organization's own theme, whatever the reader chose; null is the built-in one. */
export function useDefaultTheme(enabled = true) {
  return useQuery({
    queryKey: defaultThemeQueryKey,
    enabled,
    queryFn: async () => {
      const { data } = await api.GET("/themes/default");
      return data?.theme ? fromWire(data.theme) : null;
    },
  });
}

function useThemeMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: run, onSuccess: () => queryClient.invalidateQueries({ queryKey: themesQueryKey }) });
}

export function useCreateTheme() {
  return useThemeMutation(async (input: ThemeInput) => {
    const { data } = await api.POST("/themes", { body: { ...input, spec: input.spec && toWire(input.spec) } });
    return { theme: fromWire(data!.theme) };
  });
}

export function useUpdateTheme() {
  return useThemeMutation(async ({ id, ...input }: ThemeInput & { id: string }) => {
    const { data } = await api.PATCH("/themes/{themeID}", {
      params: { path: { themeID: id } },
      body: { ...input, spec: input.spec && toWire(input.spec) },
    });
    return { theme: fromWire(data!.theme) };
  });
}

export function useDeleteTheme() {
  return useThemeMutation(async (id: string) => {
    await api.DELETE("/themes/{themeID}", { params: { path: { themeID: id } } });
  });
}

/** A choice: a theme, null for whatever the organization shows, or the built-in theme over it. */
export type ThemeChoice = string | null | { builtIn: true };

/** Uses a theme, returns to the organization's default with null, or keeps the built-in one over it. */
export function useChooseTheme() {
  return useThemeMutation(async (choice: ThemeChoice) => {
    const body = choice !== null && typeof choice === "object" ? { themeId: null, builtIn: true } : { themeId: choice };
    const { data } = await api.PUT("/themes/active", { body });
    return { theme: data?.theme ? fromWire(data.theme) : null };
  });
}

/** Names the shared theme everybody sees until they choose, or null for the built-in one. */
export function useSetDefaultTheme() {
  return useThemeMutation(async (themeId: string | null) => {
    const { data } = await api.PUT("/themes/default", { body: { themeId } });
    return { theme: data?.theme ? fromWire(data.theme) : null };
  });
}

/** Where a theme's export is fetched from, as a download the browser handles itself. */
export function themeExportHref(id: string): string {
  return `${API_BASE}/themes/${id}/export`;
}

// A multipart body in the generated types is an object of strings; the one
// that is sent is the form itself, which the client passes through untouched.
function fileForm(file: File): { file: string } {
  const form = new FormData();
  form.append("file", file, file.name);
  return form as unknown as { file: string };
}

/** Makes a theme of one's own from an exported theme file. */
export function useImportTheme() {
  return useThemeMutation(async (file: File) => {
    const { data } = await api.POST("/themes/import", { body: fileForm(file) });
    return { theme: fromWire(data!.theme) };
  });
}

export function useUploadThemeAsset() {
  return useThemeMutation(async ({ id, file }: { id: string; file: File }) => {
    const { data } = await api.POST("/themes/{themeID}/assets", { params: { path: { themeID: id } }, body: fileForm(file) });
    return { asset: data!.asset };
  });
}

export function useDeleteThemeAsset() {
  return useThemeMutation(async ({ id, assetId }: { id: string; assetId: string }) => {
    await api.DELETE("/themes/{themeID}/assets/{assetID}", { params: { path: { themeID: id, assetID: assetId } } });
  });
}
