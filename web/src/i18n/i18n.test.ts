import { afterEach, describe, expect, it, vi } from "vitest";
import { LANGUAGE_STORAGE_KEY } from "@/config";
import { formatNumber, localDateFormat } from "@/lib/format";
import { de } from "./de";
import { en } from "./en";
import { applyLanguage, browserLanguage, language, locale, resolveLanguage, t } from "./index";

afterEach(() => {
  applyLanguage("");
  localStorage.clear();
});

type Leaf = { path: string; kind: string; arity?: number };

function leaves(value: unknown, path = ""): Leaf[] {
  if (typeof value === "function") return [{ path, kind: "function", arity: value.length }];
  if (Array.isArray(value)) return [{ path, kind: `array of ${value.length}` }, ...value.flatMap((each, i) => leaves(each, `${path}[${i}]`))];
  if (value && typeof value === "object")
    return Object.keys(value).flatMap((key) => leaves((value as Record<string, unknown>)[key], path ? `${path}.${key}` : key));
  return [{ path, kind: typeof value }];
}

// Every string a catalogue can produce for some arguments; a function that
// cannot take them, such as one lowercasing a number, simply adds nothing.
function strings(value: unknown, args: unknown[] = ["X", "Y", "Z"]): string[] {
  if (typeof value === "string") return [value];
  if (typeof value === "function") {
    try {
      const out = (value as (...a: unknown[]) => unknown)(...args);
      return typeof out === "string" ? [out] : [];
    } catch {
      return [];
    }
  }
  if (value && typeof value === "object") return Object.values(value).flatMap((each) => strings(each, args));
  return [];
}

describe("the German catalogue", () => {
  it("has every key, of the same kind, that the English one has, and no other", () => {
    expect(leaves(de)).toEqual(leaves(en));
  });

  it("is German, not a copy of the English", () => {
    const english = new Set(strings(en));
    const german = strings(de);
    const same = german.filter((each) => english.has(each));
    // Names, units and shortcuts read the same in both languages; most sentences must not.
    expect(same.length).toBeLessThan(german.length / 10);
  });

  it("keeps to the house typography in both languages", () => {
    for (const each of [...strings(en), ...strings(de), ...strings(de, [1, 1, 1]), ...strings(de, [0, 0, 0]), ...strings(de, [2, 5, 9])]) {
      expect(each, each).not.toMatch(/[\u2013\u2014\u201c\u201d\u201e\u2018\u2019\u201a\u00ab\u00bb\u2026]/);
    }
  });

  it("counts in the singular and the plural", () => {
    expect(de.members.count(1)).toBe("1 Mitglied");
    expect(de.members.count(3)).toBe("3 Mitglieder");
  });
});

describe("choosing the language", () => {
  it("takes the first of the browser's languages it speaks, else English", () => {
    expect(browserLanguage(["de-AT", "en-US"])).toBe("de");
    expect(browserLanguage(["fr-FR", "de-CH", "en"])).toBe("de");
    expect(browserLanguage(["fr-FR", "es"])).toBe("en");
    expect(browserLanguage([])).toBe("en");
  });

  it("puts the person's choice before the browser's", () => {
    expect(resolveLanguage("en", ["de-DE"])).toBe("en");
    expect(resolveLanguage("de", ["en-US"])).toBe("de");
    expect(resolveLanguage("", ["de-DE"])).toBe("de");
    expect(resolveLanguage("fr", ["en-GB"])).toBe("en");
  });

  it("switches the strings, the page's language and the remembered choice together", () => {
    expect(t).toBe(en);
    expect(applyLanguage("de")).toBe("de");
    expect(language()).toBe("de");
    expect(t).toBe(de);
    expect(t.nav.home).toBe("Start");
    expect(document.documentElement.lang).toBe("de");
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe("de");

    applyLanguage("");
    expect(t).toBe(en);
    expect(document.documentElement.lang).toBe("en");
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBeNull();
  });

  it("writes dates and numbers in the locale of the language shown", () => {
    const day = new Date(Date.UTC(2026, 10, 2, 12));
    const onDay = localDateFormat({ dateStyle: "long", timeZone: "UTC" });
    expect(onDay.format(day)).toBe("November 2, 2026");
    expect(formatNumber(1234.5)).toBe("1,234.5");

    applyLanguage("de");
    expect(locale()).toBe("de");
    expect(onDay.format(day)).toBe("2. November 2026");
    expect(formatNumber(1234.5)).toBe("1.234,5");
  });

  it("keeps the browser's variant of the language shown", () => {
    vi.spyOn(navigator, "languages", "get").mockReturnValue(["en-GB", "de-DE"]);
    try {
      expect(locale()).toBe("en-GB");
      applyLanguage("de");
      expect(locale()).toBe("de-DE");
    } finally {
      vi.restoreAllMocks();
    }
  });
});

describe("the words on screen", () => {
  // An installation is one team's: what a setting reaches is everyone, all projects or Exciter, never an organization.
  it("never speak of an organization, in either language", () => {
    for (const [name, catalogue] of [
      ["English", en],
      ["German", de],
    ] as const) {
      const found = [...strings(catalogue), ...strings(catalogue, [1, 2, 3])].filter((each) => /organi[sz]ation/i.test(each));
      expect(found, `${name} says organization`).toEqual([]);
    }
  });
});
