import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { SIDEBAR_DRAWER_BELOW_PX, SIDEBAR_RAIL_BELOW_PX } from "@/config";

export const SIDEBAR_KEY = "exciter.sidebar";
export const GROUPS_KEY = "exciter.sidebar-groups";
export const PROJECT_KEY = "exciter.last-project";
export const OUTSIDE_KEY = "exciter.outside-settings";

/** The ids the sidebar's switches name in aria-controls. */
export const SIDEBAR_ID = "shell-sidebar";
export const DRAWER_ID = "shell-drawer";

export type SidebarMode = "open" | "rail";

function read(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // A browser that refuses storage still gets a sidebar; it just forgets.
  }
}

function onResize(notify: () => void) {
  window.addEventListener("resize", notify);
  return () => window.removeEventListener("resize", notify);
}

/** Whether the viewport is narrow enough that the sidebar is a drawer rather than a column. */
export function useNarrow(): boolean {
  // The viewport's width rather than matchMedia: it is the same number the
  // shell breakpoint compares, and jsdom has it where it has no matchMedia.
  return useSyncExternalStore(
    onResize,
    () => window.innerWidth < SIDEBAR_DRAWER_BELOW_PX,
    () => false,
  );
}

// The sidebar starts as a rail on a narrow screen and remembers what the
// reader last chose, so a laptop and a wide monitor each keep their own habit.
export function useSidebarMode(): [SidebarMode, () => void] {
  const [mode, setMode] = useState<SidebarMode>(() => {
    const stored = read(SIDEBAR_KEY);
    if (stored === "open" || stored === "rail") return stored;
    return typeof window !== "undefined" && window.innerWidth < SIDEBAR_RAIL_BELOW_PX ? "rail" : "open";
  });
  const toggle = useCallback(() => {
    setMode((current) => {
      const next = current === "open" ? "rail" : "open";
      write(SIDEBAR_KEY, next);
      return next;
    });
  }, []);
  return [mode, toggle];
}

/** Which sidebar groups are folded, by id, kept per browser; absent is the group's own default. */
export function useSidebarGroups(): [Record<string, boolean>, (id: string, defaultOpen: boolean) => void] {
  const [groups, setGroups] = useState<Record<string, boolean>>(() => {
    try {
      const parsed: unknown = JSON.parse(read(GROUPS_KEY) ?? "{}");
      return parsed && typeof parsed === "object" ? (parsed as Record<string, boolean>) : {};
    } catch {
      return {};
    }
  });
  const toggle = useCallback((id: string, defaultOpen: boolean) => {
    setGroups((current) => {
      const open = current[id] ?? defaultOpen;
      const next = { ...current, [id]: !open };
      write(GROUPS_KEY, JSON.stringify(next));
      return next;
    });
  }, []);
  return [groups, toggle];
}

/** The project the sidebar shows: the one the route names, else the last one visited, as in Armature. */
export function useLastProject(current: string | undefined): string | undefined {
  const [last, setLast] = useState<string | undefined>(() => read(PROJECT_KEY) ?? undefined);
  useEffect(() => {
    if (current) {
      write(PROJECT_KEY, current);
      setLast(current);
    }
  }, [current]);
  return current ?? last;
}

// Kept per tab, since "back" means where this tab came from.
function readSession(key: string): string | null {
  try {
    return window.sessionStorage.getItem(key);
  } catch {
    return null;
  }
}

/** Notes the last page outside the settings area, which "Back to Exciter" returns to. */
export function rememberOutsideSettings(href: string) {
  try {
    window.sessionStorage.setItem(OUTSIDE_KEY, href);
  } catch {
    // Without storage the way back is the home page.
  }
}

/** The last page outside the settings area this tab showed, or home. */
export function lastOutsideSettings(): string {
  return readSession(OUTSIDE_KEY) ?? "/";
}
