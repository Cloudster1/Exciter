import { useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { api } from "./client";
import { reqifKey, type ReqIFImport } from "./reqif";

/** What a part of a Word document becomes, as the preview shows it. */
export type ImportPreviewNode = ReqIFImport["preview"]["tree"][number];
export type ImportStyleUse = ReqIFImport["preview"]["styles"][number];

/** The keys of a Word import's own choices, beside the schema's identifiers in the same map. */
export const WORD_UNIT = "word.unit";
export const WORD_STYLES = "word.styles";
export const WORD_PATTERN = "word.pattern";
export const WORD_KEYWORDS = "word.keywords";
export const WORD_FRONT = "word.front";
export const wordTableChoice = (n: number) => `word.table.${n}`;

// A multipart body in the generated types is an object of strings; the one sent is the form itself.
function wordForm(file: File, parentKey: string): { file: string; parentKey?: string } {
  const form = new FormData();
  form.append("file", file, file.name);
  if (parentKey) form.append("parentKey", parentKey);
  return form as unknown as { file: string };
}

/** Uploads a .docx to import below a section, the project's top without one; nothing is written yet. */
export function useUploadWord(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ file, parentKey }: { file: File; parentKey: string }): Promise<ReqIFImport> =>
      (await api.POST("/projects/{projectKey}/word-imports", { params: { path: { projectKey } }, body: wordForm(file, parentKey) })).data!.import,
    // It goes on as a ReqIF import, so it is kept where that one is read.
    onSuccess: (made) => queryClient.setQueryData(reqifKey(made.id), made),
  });
}
