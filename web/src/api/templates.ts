import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { API_BASE } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A Word template exports go through (#214), its fields and what it includes. */
export type ExportTemplate = Wire["ExportTemplate"];
export type TemplateField = Wire["ExportTemplateField"];
export type TemplateInclude = Wire["ExportTemplateInclude"];
export type TemplatePatch = Wire["ExportTemplatePatch"];

const templatesKey = (projectKey: string) => ["export-templates", projectKey.toUpperCase()] as const;

/** The templates a project may export through: its own, then those other projects share. */
export function useExportTemplates(projectKey: string | undefined) {
  return useQuery({
    queryKey: templatesKey(projectKey ?? ""),
    enabled: Boolean(projectKey),
    queryFn: async (): Promise<ExportTemplate[]> =>
      (await api.GET("/projects/{projectKey}/export-templates", { params: { path: { projectKey: projectKey! } } })).data!.templates,
  });
}

// A multipart body in the generated types is an object of strings; the one sent is the form itself.
function templateForm(file: File, name: string, description: string): { file: string; name: string } {
  const form = new FormData();
  form.append("file", file, file.name);
  form.append("name", name);
  if (description) form.append("description", description);
  return form as unknown as { file: string; name: string };
}

/** Uploads a Word file as a template of the project; one that cannot be used is refused with what is wrong. */
export function useUploadTemplate(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ file, name, description }: { file: File; name: string; description: string }): Promise<ExportTemplate> =>
      (
        await api.POST("/projects/{projectKey}/export-templates", {
          params: { path: { projectKey } },
          body: templateForm(file, name, description),
        })
      ).data!.template,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["export-templates"] }),
  });
}

/** Changes a template's name, fields, inclusions or sharing. */
export function useUpdateTemplate(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (patch: TemplatePatch): Promise<ExportTemplate> =>
      (await api.PATCH("/export-templates/{templateID}", { params: { path: { templateID: id } }, body: patch })).data!.template,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["export-templates"] }),
  });
}

/** Deletes a template; exports made with it stay as they are. */
export function useDeleteTemplate(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/export-templates/{templateID}", { params: { path: { templateID: id } } });
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["export-templates"] }),
  });
}

/** Where a template's Word file downloads as it was uploaded. */
export function templateFileHref(id: string): string {
  return `${API_BASE}/export-templates/${encodeURIComponent(id)}/file`;
}

/** An export's address with the template it goes through, and the baseline its change marks count from. */
export function withTemplate(href: string, templateId?: string, since?: string): string {
  if (!templateId) return href;
  const q = new URLSearchParams({ template: templateId });
  if (since) q.set("since", since);
  return `${href}${href.includes("?") ? "&" : "?"}${q.toString()}`;
}
