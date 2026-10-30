import { RECENT_MAX, RECENT_STORAGE_KEY } from "@/config";

/** A requirement opened in this browser, as the palette offers it again. */
export interface Recent {
  id: string;
  key: string;
  title: string;
  projectKey: string;
}

/** The requirements opened last in this browser, newest first; none where storage is refused. */
export function recentRequirements(): Recent[] {
  try {
    const kept: unknown = JSON.parse(localStorage.getItem(RECENT_STORAGE_KEY) ?? "[]");
    return Array.isArray(kept) ? kept.filter(isRecent).slice(0, RECENT_MAX) : [];
  } catch {
    return [];
  }
}

/** Notes a requirement as opened, ahead of the others and once. */
export function rememberRequirement(opened: Recent) {
  const rest = recentRequirements().filter((each) => each.id !== opened.id);
  try {
    localStorage.setItem(RECENT_STORAGE_KEY, JSON.stringify([opened, ...rest].slice(0, RECENT_MAX)));
  } catch {
    // Without storage the palette simply offers no recent requirements.
  }
}

function isRecent(value: unknown): value is Recent {
  const each = value as Partial<Recent> | null;
  return typeof each?.id === "string" && typeof each.key === "string" && typeof each.title === "string" && typeof each.projectKey === "string";
}
