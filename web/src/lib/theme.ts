export type Theme = "light" | "dark" | "system";

export const THEME_STORAGE_KEY = "exciter.theme";

/** The order the theme button steps through. */
export const THEME_CYCLE: readonly Theme[] = ["system", "light", "dark"];

/** Reads the stored preference, tolerating browsers that block site storage. */
export function readTheme(): Theme {
  try {
    const value = localStorage.getItem(THEME_STORAGE_KEY);
    if (value === "light" || value === "dark" || value === "system") return value;
  } catch {
    // Private windows and locked-down browsers throw on access; the system
    // default is a perfectly good answer.
  }
  return "system";
}

/**
 * Applies a theme by stamping the root element. "system" removes the attribute
 * entirely so the stylesheet's prefers-color-scheme rules take over.
 */
export function applyTheme(theme: Theme): void {
  const root = document.documentElement;
  if (theme === "system") {
    root.removeAttribute("data-theme");
  } else {
    root.setAttribute("data-theme", theme);
  }
  try {
    localStorage.setItem(THEME_STORAGE_KEY, theme);
  } catch {
    // A theme that does not persist is still a theme.
  }
}

/** The theme after this one in the button's cycle. */
export function nextTheme(theme: Theme): Theme {
  return THEME_CYCLE[(THEME_CYCLE.indexOf(theme) + 1) % THEME_CYCLE.length]!;
}

// A custom theme is one stylesheet in the head, written by the loader once
// the server has answered and, before that, from what this browser saw last.
export const CUSTOM_THEME_STYLE_ID = "exciter-theme";
export const CUSTOM_THEME_CACHE_KEY = "exciter.theme-css";

/** Puts a compiled theme on the page, or takes it off with null. */
export function applyCustomTheme(css: string | null): void {
  let style = document.getElementById(CUSTOM_THEME_STYLE_ID);
  if (css === null) {
    style?.remove();
    return;
  }
  if (!style) {
    style = document.createElement("style");
    style.id = CUSTOM_THEME_STYLE_ID;
    document.head.appendChild(style);
  }
  if (style.textContent !== css) style.textContent = css;
}

/** Remembers a compiled theme so the next load paints it before the server answers. */
export function cacheCustomTheme(entry: { key: string; css: string } | null): void {
  try {
    if (entry === null) localStorage.removeItem(CUSTOM_THEME_CACHE_KEY);
    else localStorage.setItem(CUSTOM_THEME_CACHE_KEY, JSON.stringify(entry));
  } catch {
    // A theme that is not remembered is still applied.
  }
}

/** What the last load left behind, if anything. */
export function readCachedTheme(): { key: string; css: string } | null {
  try {
    const raw = localStorage.getItem(CUSTOM_THEME_CACHE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as { key?: unknown; css?: unknown };
    if (typeof parsed.key === "string" && typeof parsed.css === "string") return { key: parsed.key, css: parsed.css };
  } catch {
    // Unreadable storage, or a cache written by an older version.
  }
  return null;
}
