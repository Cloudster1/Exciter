import type { SpecAddress } from "@/api/tree";
import { STATUS_CATEGORIES, type StatusCategory } from "@/api/workflows";

/** Reads the specification's address, dropping what is no status id or category. */
export function readSpecAddress(search: Record<string, unknown>): SpecAddress {
  const strings = (value: unknown): string[] =>
    (Array.isArray(value) ? value : [value]).filter((each): each is string => typeof each === "string" && each !== "");
  const address: SpecAddress = {};
  const status = strings(search.status);
  const category = strings(search.category).filter((each): each is StatusCategory => STATUS_CATEGORIES.includes(each as StatusCategory));
  if (status.length) address.status = status;
  if (category.length) address.category = category;
  if (search.changes === "previous" || search.changes === "approved" || search.changes === "baseline") address.changes = search.changes;
  if (address.changes === "baseline" && typeof search.baseline === "string" && search.baseline) address.baseline = search.baseline;
  if (address.changes && (search.onlyChanged === true || search.onlyChanged === "true")) address.onlyChanged = true;
  return address;
}

/** Whether the address narrows the specification at all. */
export function narrowed(address: SpecAddress): boolean {
  return Boolean(address.status?.length || address.category?.length);
}
