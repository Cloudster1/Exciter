import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render } from "@testing-library/react";
import { CUSTOM_THEME_CACHE_KEY, CUSTOM_THEME_STYLE_ID, applyCustomTheme, readCachedTheme } from "@/lib/theme";
import { compileTheme, emptySpec } from "@/lib/theme-css";
import { ThemeLoader, setThemePreview } from "./ThemeLoader";

const active = vi.fn();
vi.mock("@/api/themes", () => ({ useActiveTheme: () => ({ data: active() }) }));

const magenta = {
  id: "11111111-1111-1111-1111-111111111111",
  updatedAt: "2026-09-29T08:00:00Z",
  spec: { ...emptySpec(), colors: { light: { accent: "#ff0066" }, dark: {} } },
  assets: [],
};

function styleText() {
  return document.getElementById(CUSTOM_THEME_STYLE_ID)?.textContent ?? null;
}

describe("the theme loader", () => {
  beforeEach(() => {
    localStorage.clear();
    applyCustomTheme(null);
  });
  afterEach(() => act(() => setThemePreview(null)));

  it("keeps what the last load cached on the page until the server answers", () => {
    applyCustomTheme(".cached {}");
    active.mockReturnValue(undefined);
    render(<ThemeLoader />);
    expect(styleText()).toBe(".cached {}");
  });

  it("applies the chosen theme and caches it under its id and version for the next load", () => {
    active.mockReturnValue({ theme: magenta, source: "chosen" });
    render(<ThemeLoader />);
    const css = compileTheme(magenta);
    expect(styleText()).toBe(css);
    expect(readCachedTheme()).toEqual({ key: `${magenta.id}:${magenta.updatedAt}`, css });
    expect(localStorage.getItem(CUSTOM_THEME_CACHE_KEY)).not.toBeNull();
  });

  it("takes the theme off and forgets the cache when the built-in theme is the answer", () => {
    applyCustomTheme(".stale {}");
    localStorage.setItem(CUSTOM_THEME_CACHE_KEY, JSON.stringify({ key: "old", css: ".stale {}" }));
    active.mockReturnValue({ theme: null, source: "" });
    render(<ThemeLoader />);
    expect(styleText()).toBeNull();
    expect(readCachedTheme()).toBeNull();
  });

  it("shows the editor's draft instead while one is previewed, and the chosen theme after", () => {
    active.mockReturnValue({ theme: magenta, source: "chosen" });
    render(<ThemeLoader />);
    act(() => setThemePreview(".draft {}", null));
    expect(styleText()).toBe(".draft {}");
    act(() => setThemePreview(null));
    expect(styleText()).toBe(compileTheme(magenta));
  });

  it("takes the theme off when the shell goes", () => {
    active.mockReturnValue({ theme: magenta, source: "chosen" });
    const { unmount } = render(<ThemeLoader />);
    unmount();
    expect(styleText()).toBeNull();
  });
});
