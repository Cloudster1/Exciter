import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { t } from "@/i18n";
import armature from "../../../api/armature/theme-tokens.json";
import stator from "../../../api/stator/theme-tokens.json";
import { CURSOR_KINDS, TOKEN_NAMES } from "./theme-tokens";

// The editor offers exactly the tokens the stylesheet defines: a token in one and not the other is a colour
// nobody can theme, or one that does nothing when themed.
describe("the token catalogue", () => {
  it("names every --color-* token in the stylesheet, and nothing else", () => {
    const css = readFileSync(join(__dirname, "..", "styles", "index.css"), "utf8");
    const declared = new Set([...css.matchAll(/--color-([a-z0-9-]+):/g)].map((m) => m[1]!));
    expect([...declared].sort()).toEqual([...TOKEN_NAMES].sort());
  });

  it("lists each token once", () => {
    expect(new Set(TOKEN_NAMES).size).toBe(TOKEN_NAMES.length);
  });

  it("has a label for every token and every cursor, and none left over", () => {
    expect(Object.keys(t.themes.tokens).sort()).toEqual([...TOKEN_NAMES].sort());
    expect(Object.keys(t.themes.cursors).sort()).toEqual(CURSOR_KINDS.map((c) => c.kind).sort());
  });
});

// A theme names its colours by token, so a token renamed in one product is a colour every theme made there loses on
// its way here (#59). The lists are Armature's and Stator's own, vendored by make theme-tokens with their commit.
describe("the tokens Exciter shares with Armature and Stator", () => {
  it.each([
    ["Armature", armature],
    ["Stator", stator],
  ])("are %s's, in the same order", (_, vendored) => {
    expect(TOKEN_NAMES, `vendored from ${vendored.source}`).toEqual(vendored.tokens);
  });
});
