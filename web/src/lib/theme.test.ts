import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  applyCustomTheme,
  applyTheme,
  cacheCustomTheme,
  CUSTOM_THEME_CACHE_KEY,
  CUSTOM_THEME_STYLE_ID,
  nextTheme,
  readCachedTheme,
  readTheme,
  THEME_STORAGE_KEY,
} from "./theme";

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("theme", () => {
  it("defaults to following the system", () => {
    expect(readTheme()).toBe("system");
  });

  it("round trips an explicit choice under the exciter key", () => {
    applyTheme("dark");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    expect(readTheme()).toBe("dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");

    applyTheme("light");
    expect(readTheme()).toBe("light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });

  // "system" must remove the attribute rather than stamp a value, or the page
  // would be frozen in whichever theme was last active.
  it("removes the attribute when following the system", () => {
    applyTheme("dark");
    applyTheme("system");
    expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
    expect(readTheme()).toBe("system");
  });

  it("ignores a stored value it does not recognise", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "chartreuse");
    expect(readTheme()).toBe("system");
  });

  it("falls back to the system default when storage cannot be read", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("access denied");
    });
    expect(readTheme()).toBe("system");
  });

  it("still applies a theme when storage cannot be written", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("access denied");
    });
    expect(() => applyTheme("dark")).not.toThrow();
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  });

  it("cycles system, light, dark and back", () => {
    expect(nextTheme("system")).toBe("light");
    expect(nextTheme("light")).toBe("dark");
    expect(nextTheme("dark")).toBe("system");
  });
});

describe("a custom theme", () => {
  it("is one style element, replaced in place and removed with null", () => {
    applyCustomTheme(":root { --color-accent: #f06; }");
    expect(document.getElementById(CUSTOM_THEME_STYLE_ID)?.textContent).toBe(":root { --color-accent: #f06; }");
    applyCustomTheme(":root { --color-accent: #0cf; }");
    expect(document.querySelectorAll(`#${CUSTOM_THEME_STYLE_ID}`)).toHaveLength(1);
    expect(document.getElementById(CUSTOM_THEME_STYLE_ID)?.textContent).toBe(":root { --color-accent: #0cf; }");
    applyCustomTheme(null);
    expect(document.getElementById(CUSTOM_THEME_STYLE_ID)).toBeNull();
  });

  it("is remembered for the next load under the exciter key, and forgotten with null", () => {
    cacheCustomTheme({ key: "t1:2026", css: ".x {}" });
    expect(CUSTOM_THEME_CACHE_KEY.startsWith("exciter.")).toBe(true);
    expect(JSON.parse(localStorage.getItem(CUSTOM_THEME_CACHE_KEY)!)).toEqual({ key: "t1:2026", css: ".x {}" });
    expect(readCachedTheme()).toEqual({ key: "t1:2026", css: ".x {}" });
    cacheCustomTheme(null);
    expect(readCachedTheme()).toBeNull();
  });

  it("ignores a cache it cannot read", () => {
    localStorage.setItem(CUSTOM_THEME_CACHE_KEY, "not json");
    expect(readCachedTheme()).toBeNull();
    localStorage.setItem(CUSTOM_THEME_CACHE_KEY, JSON.stringify({ key: 1 }));
    expect(readCachedTheme()).toBeNull();
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("access denied");
    });
    expect(readCachedTheme()).toBeNull();
  });

  it("still applies when storage cannot be written", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("access denied");
    });
    expect(() => cacheCustomTheme({ key: "k", css: "" })).not.toThrow();
  });
});
