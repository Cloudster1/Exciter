import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

// The kit and the stylesheet decide how things look, and config.ts names the values the rest reads; anything else
// reaches for a component or a token, so a size or a colour changes in one place. Adapted from Armature's.
function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) walk(path, out);
    else if (/\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name) && !name.endsWith(".d.ts")) out.push(path);
  }
  return out;
}

const root = join(__dirname, "..");
const decided = ["components/ui/", "styles/", "config.ts"];
const outsideKit = walk(root).filter((file) => !decided.some((prefix) => relative(root, file).startsWith(prefix)));

// What a comment says is no size or colour; the guard on // keeps the slashes of an address.
function code(file: string): string {
  return readFileSync(file, "utf8")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:"'`])\/\/.*$/gm, "$1");
}

/** Controls drawn by hand before this test, with why each is not the kit's; a count may only go down. */
const allowed: Record<string, { count: number; why: string }> = {
  "features/shell/QuickSwitcher.tsx: <input": { count: 1, why: "the switcher's combobox sits bare in the dialog's head, without a field's frame" },
  "features/audit/AuditLog.tsx: <button": { count: 2, why: "the actor and the target of an entry are text that narrows the log when pressed" },
};

function hits(pattern: RegExp): string[] {
  const found = outsideKit.flatMap((file) => (code(file).match(pattern) ?? []).map((m) => `${relative(root, file)}: ${m}`));
  return found.filter((hit, index) => {
    const allowance = allowed[hit];
    return !allowance || found.slice(0, index + 1).filter((each) => each === hit).length > allowance.count;
  });
}

describe("the kit", () => {
  it("is where every size is decided", () => {
    expect(hits(/\b[\w-]+-\[-?[\d.]+(?:px|rem)\]/g)).toEqual([]);
    expect(hits(/(?<![\w$.-])\d+(?:\.\d+)?px\b/g)).toEqual([]);
  });

  it("is where every colour is decided, the rest naming tokens", () => {
    expect(hits(/["'`]#[0-9a-fA-F]{3,8}["'`]/g)).toEqual([]);
    expect(hits(/-\[#[0-9a-fA-F]{3,8}\]/g)).toEqual([]);
    expect(hits(/\b(?:rgba?|hsla?|oklch|oklab)\(\s*\d/g)).toEqual([]);
  });

  it("is where every button is drawn", () => {
    expect(hits(/<button\b/g)).toEqual([]);
  });

  // A file input is hidden and pressed through a button, so it is not a drawn control.
  it("is where every input, select and textarea is drawn", () => {
    expect(hits(/<(select|textarea)\b|<input\b(?![^>]*type="file")/g)).toEqual([]);
  });

  it("owns the accent's name", () => {
    expect(hits(/\b(bg|text|border|ring|fill|stroke|border-[tlrb])-(on-)?brand\b/g)).toEqual([]);
  });

  it("keeps its exceptions to controls that are still there", () => {
    const counts = Object.keys(allowed).map((hit) => {
      const [file, pattern] = hit.split(": ");
      return code(join(root, file!)).split(pattern!).length - 1;
    });
    expect(counts).toEqual(Object.values(allowed).map((each) => each.count));
  });
});
