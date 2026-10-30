import { useCallback, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { API_BASE, FILE_SIZE_DECIMALS_BELOW, INLINE_FILE_TYPES, KILOBYTE } from "@/config";
import { t } from "@/i18n";
import { formatNumber } from "@/lib/format";
import { api, ApiError } from "./client";
import type { components } from "./schema";

/** What is known about a file on a requirement; the bytes are behind attachmentUrl. */
export type Attachment = components["schemas"]["Attachment"];

export const attachmentsQueryKey = ["attachments"] as const;
const requirementFilesKey = (reqKey: string) => [...attachmentsQueryKey, reqKey.toUpperCase()] as const;

/** The files on a requirement, the latest first. */
export function useAttachments(reqKey: string) {
  return useQuery({
    queryKey: requirementFilesKey(reqKey),
    queryFn: async (): Promise<Attachment[]> => (await api.GET("/requirements/{reqKey}/attachments", { params: { path: { reqKey } } })).data!.attachments,
  });
}

/** Where the bytes are. The session cookie goes with the request, so a plain link or img works. */
export function attachmentUrl(id: string, inline = false): string {
  return `${API_BASE}/attachments/${encodeURIComponent(id)}${inline ? "?inline=true" : ""}`;
}

function mediaType(contentType: string): string {
  return (contentType.split(";")[0] ?? "").trim().toLowerCase();
}

/** Whether the browser may show this in a tab rather than download it. */
export function showsInPlace(contentType: string): boolean {
  return INLINE_FILE_TYPES.includes(mediaType(contentType));
}

/** Whether a file is drawn as a picture in a statement rather than as a download in the line. */
export function isImage(contentType: string): boolean {
  return showsInPlace(contentType) && mediaType(contentType).startsWith("image/");
}

const SIZE_UNITS = ["B", "KB", "MB", "GB"];

/** A file size the way a person reads one: 12 KB, 3.4 MB. */
export function formatSize(bytes: number): string {
  let value = bytes;
  let unit = 0;
  while (value >= KILOBYTE && unit < SIZE_UNITS.length - 1) {
    value /= KILOBYTE;
    unit += 1;
  }
  const shown =
    unit === 0 || value >= FILE_SIZE_DECIMALS_BELOW
      ? formatNumber(Math.round(value))
      : formatNumber(value, { minimumFractionDigits: 1, maximumFractionDigits: 1 });
  return `${shown} ${SIZE_UNITS[unit]}`;
}

// A multipart body in the generated types is an object of strings; the one that is sent is the form itself,
// which the client passes through untouched.
function fileForm(file: File): { file: string } {
  const form = new FormData();
  form.append("file", file, file.name);
  return form as unknown as { file: string };
}

/** The sentence a refused upload shows, which always says what to do next. */
export function uploadErrorMessage(file: File, error: unknown): string {
  if (error instanceof ApiError && error.code === "too_large") return t.attachments.tooLarge(file.name, formatSize(file.size), error.message);
  if (error instanceof ApiError && error.fields.file) return t.attachments.refused(file.name, error.fields.file);
  if (error instanceof ApiError && error.message) return t.attachments.refused(file.name, error.message);
  return t.attachments.uploadBroke(file.name);
}

/** Sends one file to a requirement; null when it was refused, which errors then says. */
export type UploadFile = (file: File) => Promise<Attachment | null>;

/** Uploads files to a requirement one request each, keeping which are on their way and why any was refused. */
export function useUploadAttachments(reqKey: string) {
  const queryClient = useQueryClient();
  const [pending, setPending] = useState<string[]>([]);
  const [errors, setErrors] = useState<string[]>([]);
  const active = useRef(0);

  const upload = useCallback<UploadFile>(
    async (file) => {
      // Files sent together are one attempt; what an earlier one refused is old news.
      if (active.current === 0) setErrors([]);
      active.current += 1;
      setPending((list) => [...list, file.name]);
      try {
        const made = (await api.POST("/requirements/{reqKey}/attachments", { params: { path: { reqKey } }, body: fileForm(file) })).data!.attachment;
        queryClient.setQueryData<Attachment[]>(requirementFilesKey(reqKey), (list) => (list ? [made, ...list.filter((a) => a.id !== made.id)] : list));
        void queryClient.invalidateQueries({ queryKey: requirementFilesKey(reqKey) });
        return made;
      } catch (error) {
        setErrors((list) => [...list, uploadErrorMessage(file, error)]);
        return null;
      } finally {
        active.current -= 1;
        setPending((list) => {
          const at = list.indexOf(file.name);
          return at < 0 ? list : [...list.slice(0, at), ...list.slice(at + 1)];
        });
      }
    },
    [reqKey, queryClient],
  );

  const clearErrors = useCallback(() => setErrors([]), []);
  return { upload, pending, errors, clearErrors };
}

/** Takes a file off a requirement: for good, or off the list while a revision shows it. */
export function useDeleteAttachment(reqKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/attachments/{attachmentID}", { params: { path: { attachmentID: id } } });
      return id;
    },
    onSuccess: (id) => {
      queryClient.setQueryData<Attachment[]>(requirementFilesKey(reqKey), (list) => list?.filter((a) => a.id !== id));
      return queryClient.invalidateQueries({ queryKey: requirementFilesKey(reqKey) });
    },
  });
}
