import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { API_BASE, AUDIT_EXPORT_PATH, AUDIT_PAGE_SIZE } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** One entry of the organization's record: who did what, to what, when. */
export type AuditEntry = Wire["AuditEntry"];
/** What the log holds to narrow it by, and how long it keeps an entry. */
export type AuditFacets = Wire["AuditFacets"];
export type AuditAction = AuditEntry["action"];

/** How the log is narrowed; days are YYYY-MM-DD in the reader's own time, both inclusive. */
export interface AuditFilter {
  action?: AuditAction;
  actor?: string;
  targetType?: string;
  target?: string;
  from?: string;
  to?: string;
}

export const auditQueryKey = ["audit"] as const;

/** A day typed in a date field as the instant it starts where the reader is. */
function startOf(day: string, after = 0): string | undefined {
  const [year, month, date] = day.split("-").map(Number);
  if (!year || !month || !date) return undefined;
  return new Date(year, month - 1, date + after).toISOString();
}

/** The filter as the API reads it: the reader's days become the instants they span. */
export function auditQuery(filter: AuditFilter) {
  return {
    action: filter.action,
    actor: filter.actor,
    targetType: filter.targetType,
    target: filter.target,
    from: filter.from ? startOf(filter.from) : undefined,
    to: filter.to ? startOf(filter.to, 1) : undefined,
  };
}

/** One page of the log, newest first, after the cursor; next is the cursor of the page after. */
export function useAuditLog(filter: AuditFilter, cursor: string | undefined) {
  return useQuery({
    queryKey: [...auditQueryKey, "entries", filter, cursor ?? ""],
    queryFn: async () => (await api.GET("/audit", { params: { query: { ...auditQuery(filter), limit: AUDIT_PAGE_SIZE, cursor } } })).data!,
    placeholderData: keepPreviousData,
  });
}

export function useAuditFacets() {
  return useQuery({
    queryKey: [...auditQueryKey, "facets"],
    queryFn: async (): Promise<AuditFacets> => (await api.GET("/audit/facets")).data!.facets,
  });
}

/** Where the filtered log downloads as a CSV file; the download is itself recorded. */
export function auditExportHref(filter: AuditFilter): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(auditQuery(filter))) {
    if (value) params.set(key, value);
  }
  const query = params.toString();
  return `${API_BASE}${AUDIT_EXPORT_PATH}${query ? `?${query}` : ""}`;
}
