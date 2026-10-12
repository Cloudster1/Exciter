import { useEffect, useState } from "react";
import { useActiveTheme } from "@/api/themes";
import { applyCustomTheme, cacheCustomTheme } from "@/lib/theme";
import { compileTheme } from "@/lib/theme-css";

// The editor's live preview stands in for the chosen theme while it is on:
// its stylesheet, and the moving picture it asks for.
let previewCSS: string | null = null;
let previewEffect: string | null | undefined;
const listeners = new Set<() => void>();

/** Shows a draft on the page, or null to show the chosen theme again. */
export function setThemePreview(css: string | null, effect?: string | null): void {
  previewCSS = css;
  previewEffect = css === null ? undefined : (effect ?? null);
  for (const listener of listeners) listener();
}

/** The moving picture the draft asks for; undefined while no draft is shown. */
export function themePreviewEffect(): string | null | undefined {
  return previewEffect;
}

/** Hears every change of the draft; returns what stops the hearing. */
export function subscribeThemePreview(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Keeps the page in the theme the reader chose. Until the server answers,
 * the one this browser saw last stays on; leaving the shell takes it off.
 */
export function ThemeLoader() {
  const { data } = useActiveTheme();
  const [preview, setPreview] = useState(previewCSS);

  useEffect(() => {
    const listener = () => setPreview(previewCSS);
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  }, []);

  useEffect(() => {
    if (preview !== null) {
      applyCustomTheme(preview);
      return;
    }
    if (!data) return;
    if (!data.theme) {
      applyCustomTheme(null);
      cacheCustomTheme(null);
      return;
    }
    const css = compileTheme(data.theme);
    applyCustomTheme(css);
    cacheCustomTheme({ key: `${data.theme.id}:${data.theme.updatedAt}`, css });
  }, [data, preview]);

  useEffect(() => () => applyCustomTheme(null), []);
  return null;
}
