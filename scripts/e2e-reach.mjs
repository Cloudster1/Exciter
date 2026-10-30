#!/usr/bin/env node
// Fails when a route of web/src/routes was never a page's address in the browser suite, or a data-action of
// web/src/features was never clicked, unless e2e/reach-exemptions.json says why (#334). Usage: e2e-reach.mjs [reports]
import { readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = join(dirname(fileURLToPath(import.meta.url)), "..");
const REPORTS = process.argv[2] ?? join(ROOT, "reports");
const SRC = join(ROOT, "web", "src");

function files(dir, keep) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) out.push(...files(path, keep));
    else if (keep(path)) out.push(path);
  }
  return out;
}

// The constants routes take their paths from, config.ts's and the shell's, as plain strings.
const constants = new Map();
for (const file of files(SRC, (p) => /\.tsx?$/.test(p) && !/\.test\.tsx?$/.test(p))) {
  for (const [, name, value] of readFileSync(file, "utf8").matchAll(/^export const ([A-Z_]+) = "([^"]*)";/gm)) constants.set(name, value);
}

const routes = new Set();
for (const file of files(join(SRC, "routes"), (p) => p.endsWith(".tsx") && !p.endsWith(".test.tsx"))) {
  for (const [, , literal, constant] of readFileSync(file, "utf8").matchAll(/createRoute\(\{[\s\S]*?\bpath:\s*("([^"]+)"|([A-Z_]+))/g)) {
    const path = literal ?? constants.get(constant);
    if (!path) throw new Error(`${relative(ROOT, file)} names a route by ${constant}, which web/src exports as no plain string.`);
    routes.add(path);
  }
}

// Only literal values: one a template builds, as data-action={`confirm-${mode}`}, is named by what it builds from.
const actions = new Set();
for (const file of files(join(SRC, "features"), (p) => p.endsWith(".tsx") && !p.endsWith(".test.tsx"))) {
  for (const [, action] of readFileSync(file, "utf8").matchAll(/data-action="([^"]+)"/g)) actions.add(action);
}

const reachedPaths = new Set();
const clicked = new Set();
const reachFiles = (() => {
  try {
    return files(REPORTS, (p) => p.includes("e2e-reach") && p.endsWith(".jsonl"));
  } catch {
    return [];
  }
})();
if (reachFiles.length === 0) {
  console.error(`No reach notes under ${REPORTS}; run the whole browser suite first.`);
  process.exit(2);
}
for (const file of reachFiles) {
  for (const line of readFileSync(file, "utf8").split("\n")) {
    if (!line) continue;
    const entry = JSON.parse(line);
    if (entry.path) reachedPaths.add(entry.path.replace(/\/+$/, "") || "/");
    if (entry.action) clicked.add(entry.action);
  }
}

// A parameter stands for one segment, and a trailing splat, as /p/$, for at least one more.
const pattern = (route) =>
  new RegExp(`^${route.replace(/[.*+?^{}()|[\]\\]/g, "\\$&").replace(/\$[A-Za-z]+/g, "[^/]+").replace(/\/\$$/, "/.+")}$`);
const reached = (route) => {
  const re = pattern(route);
  for (const path of reachedPaths) if (re.test(path)) return true;
  return false;
};

const exemptions = JSON.parse(readFileSync(join(ROOT, "e2e", "reach-exemptions.json"), "utf8"));
const problems = [];
const summary = [];
for (const [what, all, isReached, exempt] of [
  ["route", [...routes].sort(), reached, exemptions.routes ?? {}],
  ["data-action", [...actions].sort(), (a) => clicked.has(a), exemptions.actions ?? {}],
]) {
  const missed = all.filter((each) => !isReached(each));
  for (const each of missed) if (!exempt[each]) problems.push(`the ${what} ${each} was never reached; reach it in a spec, or say why not in e2e/reach-exemptions.json`);
  for (const each of Object.keys(exempt)) {
    if (!all.includes(each)) problems.push(`e2e/reach-exemptions.json exempts the ${what} ${each}, which no longer exists`);
    else if (isReached(each)) problems.push(`e2e/reach-exemptions.json exempts the ${what} ${each}, which a spec now reaches; take it off the list`);
  }
  summary.push(`| ${what}s | ${all.length - missed.length} of ${all.length} | ${missed.filter((each) => exempt[each]).length} |`);
}

writeFileSync(
  join(REPORTS, "e2e-reach.md"),
  ["## Reached in the browser", "", "| Kind | Reached | Exempt |", "| --- | ---: | ---: |", ...summary, ""].join("\n"),
);
if (problems.length > 0) {
  console.error(`The browser suite leaves ${problems.length} things unchecked:\n  ${problems.join("\n  ")}`);
  process.exit(1);
}
console.log(`Every route and data-action is reached, or exempt with a reason.\n${summary.join("\n")}`);
