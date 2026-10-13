import { t } from "@/i18n";

/** What an attribute's target starts with, the API's csvio.AttributePrefix. */
export const ATTRIBUTE_PREFIX = "attr:";

/** Targets a file may fill from several columns; the server says the same. */
export const MANY_VALUED = new Set(["labels", "links"]);

/** The priorities a word of the file may mean, the API's own. */
export const PRIORITIES = ["must", "should", "could", "wont"] as const;

/** A target as a person mapping columns reads it. */
export function targetLabel(target: string): string {
  if (target.startsWith(ATTRIBUTE_PREFIX)) return t.imports.attribute(target.slice(ATTRIBUTE_PREFIX.length));
  return t.imports.targets[target] ?? target;
}
