import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { SSO_WAITING } from "@/config";
import { de } from "./de";
import { en } from "./en";

// The catalogues look some sentences up by a word the server sends. Those maps are typed Record<string, string>, so
// TypeScript cannot say when the server gains a word they lack; the server's own source is read for every word.

const backend = (path: string) => readFileSync(resolve(process.cwd(), "../backend/internal", path), "utf8");

/** Every match of the pattern's first group, at least one, or the pattern no longer reads the source. */
function words(source: string, pattern: RegExp, what: string): string[] {
  const found = [...source.matchAll(pattern)].map((m) => m[1]!).filter(Boolean);
  expect(found.length, `no ${what} found in the server's source`).toBeGreaterThan(0);
  return [...new Set(found)];
}

/** The body of a Go function, up to the brace that closes it at the start of a line. */
function goFunc(source: string, name: string): string {
  const at = source.indexOf(`func ${name}(`);
  expect(at, `func ${name} is in the source`).toBeGreaterThanOrEqual(0);
  return source.slice(at, source.indexOf("\n}\n", at));
}

const catalogues = [
  ["English", en],
  ["German", de],
] as const;

describe("the words the server sends", () => {
  it("each reason a sign-in fails for is a sentence in both languages, or the wait for an administrator", () => {
    const oidc = backend("httpapi/oidc_handlers.go");
    const reasons = [
      ...words(goFunc(oidc, "signInFailure"), /return "([a-z_]+)"/g, "sign-in failure"),
      ...words(oidc, /landAfterSignIn\(w, r, "", "([a-z_]+)"\)/g, "refusal by the provider"),
    ];
    expect(reasons).toContain("denied");
    for (const [name, catalogue] of catalogues) {
      const failures: Record<string, string | undefined> = catalogue.login.failures;
      const missing = reasons.filter((reason) => reason !== SSO_WAITING && !failures[reason]);
      expect(missing, `${name} has no sentence for`).toEqual([]);
    }
  });

  it("each way a signature's sign-in fails is a sentence in both languages", () => {
    const reasons = words(backend("httpapi/oidc_handlers.go"), /landWithSignature\([^)]*, "", "([a-z_]+)"\)/g, "signing failure");
    for (const [name, catalogue] of catalogues) {
      const missing = reasons.filter((reason) => !catalogue.review.signatureError[reason]);
      expect(missing, `${name} has no sentence for`).toEqual([]);
    }
  });

  it("each reason a review session leaves a requirement out is said in both languages", () => {
    const reasons = words(backend("review/sessions.go"), /Excluded\{Key: [^,]+, Reason: "([a-z_]+)"\}/g, "exclusion reason");
    for (const [name, catalogue] of catalogues) {
      const missing = reasons.filter((reason) => !catalogue.sessions.excludedReasons[reason]);
      expect(missing, `${name} has no words for`).toEqual([]);
    }
  });
});

type Counted = { path: string; f: (n: number) => unknown };

/** Every function of a catalogue that takes exactly one argument, by its path. */
function oneArgument(value: unknown, path = ""): Counted[] {
  if (typeof value === "function") return value.length === 1 ? [{ path, f: value as (n: number) => unknown }] : [];
  if (value && typeof value === "object") return Object.entries(value).flatMap(([key, each]) => oneArgument(each, path ? `${path}.${key}` : key));
  return [];
}

/** What a function says for a count, with the count taken out; null when it says nothing for one. */
function withoutCount(f: (n: number) => unknown, n: number): string | null {
  try {
    const out = f(n);
    return typeof out === "string" ? out.replaceAll(String(n), "#") : null;
  } catch {
    return null;
  }
}

describe("counts in both catalogues", () => {
  it("say one and two differently in German wherever they do in English", () => {
    const german = new Map(oneArgument(de).map((each) => [each.path, each.f]));
    const counted = oneArgument(en).filter(({ f }) => {
      const one = withoutCount(f, 1);
      return one !== null && one !== withoutCount(f, 2);
    });
    // members.count and themes.people at least, so a walk that found none would not pass by finding nothing.
    expect(counted.map((each) => each.path)).toEqual(expect.arrayContaining(["members.count", "themes.people"]));
    const same = counted
      .map(({ path }) => path)
      .filter((path) => {
        const f = german.get(path);
        return !f || withoutCount(f, 1) === withoutCount(f, 2);
      });
    expect(same, "German says one and two alike in").toEqual([]);
  });
});
