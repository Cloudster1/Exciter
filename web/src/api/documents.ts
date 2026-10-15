import { useQuery } from "@tanstack/react-query";
import { API_BASE } from "@/config";
import { t } from "@/i18n";
import { ApiError, api, type ApiErrorBody } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A specification or a baseline as one document to sign and send (#45). */
export type SpecDocument = Wire["SpecificationDocument"];
export type DocumentEntry = Wire["DocumentEntry"];
export type DocumentProperty = Wire["DocumentProperty"];

/** The files a document is exported as: a PDF to print and sign, a Word document to edit offline. */
export type DocumentFormat = "pdf" | "docx";

/** A project's document, or with section a requirement and what is below it. */
export function useProjectDocument(projectKey: string, section?: string) {
  return useQuery({
    queryKey: ["document", "project", projectKey, section ?? ""],
    queryFn: async (): Promise<SpecDocument> =>
      (await api.GET("/projects/{projectKey}/document", { params: { path: { projectKey }, query: section ? { section } : {} } })).data!.document,
  });
}

/** A baseline's document, as it was taken. */
export function useBaselineDocument(baselineId: string) {
  return useQuery({
    queryKey: ["document", "baseline", baselineId],
    queryFn: async (): Promise<SpecDocument> =>
      (await api.GET("/baselines/{baselineID}/document", { params: { path: { baselineID: baselineId } } })).data!.document,
  });
}

/** Where a project's document, or a section's, downloads in a format. */
export function projectDocumentHref(projectKey: string, format: DocumentFormat, section?: string): string {
  const path = `${API_BASE}/projects/${encodeURIComponent(projectKey)}/document.${format}`;
  return section ? `${path}?section=${encodeURIComponent(section)}` : path;
}

/** Where a baseline's document downloads in a format. */
export function baselineDocumentHref(baselineId: string, format: DocumentFormat): string {
  return `${API_BASE}/baselines/${encodeURIComponent(baselineId)}/document.${format}`;
}

/** An exported file and the name the API gave it. */
export interface ExportedFile {
  blob: Blob;
  name: string;
}

const FILENAME = /filename\*?=(?:UTF-8'')?"?([^";]+)"?/i;

/** The file name a Content-Disposition header gives, else the fallback. */
export function fileNameOf(disposition: string | null, fallback: string): string {
  const found = disposition ? FILENAME.exec(disposition)?.[1] : undefined;
  if (!found) return fallback;
  try {
    return decodeURIComponent(found);
  } catch {
    return found;
  }
}

/**
 * Fetches an exported file rather than letting the browser follow a link, as Stator does, since an export takes
 * seconds and a refusal has to be read as a sentence, not saved as a file.
 */
export async function fetchFile(href: string, fallbackName: string): Promise<ExportedFile> {
  let response: Response;
  try {
    response = await globalThis.fetch(new Request(new URL(href, window.location.origin), { credentials: "same-origin" }));
  } catch {
    throw new ApiError(0, { code: "network", message: t.documents.networkFailed });
  }
  if (!response.ok) {
    let body: { error?: ApiErrorBody } | undefined;
    try {
      body = (await response.json()) as { error?: ApiErrorBody };
    } catch {
      body = undefined;
    }
    throw new ApiError(response.status, body?.error ?? { code: "unexpected_response", message: t.api.unexpected(response.status) });
  }
  return { blob: await response.blob(), name: fileNameOf(response.headers.get("Content-Disposition"), fallbackName) };
}

/** Hands a fetched file to the browser to save under its name. */
export function saveFile({ blob, name }: ExportedFile): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  document.body.append(link);
  link.click();
  link.remove();
  // A moment later, so the browser has taken the file before its address goes.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}
