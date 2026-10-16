// Reads a theme-tokens.ts from stdin and writes the colour token names its TOKEN_GROUPS lists, in its order, as
// JSON with where they came from. Armature names each token as { name: "..." }; Stator and Exciter list strings.
import { readFileSync } from "node:fs";

const [source] = process.argv.slice(2);
if (!source) {
  console.error("Say where the file came from: node theme-tokens.mjs '<repo> <sha> <path>' < theme-tokens.ts");
  process.exit(1);
}
const text = readFileSync(0, "utf8");
const start = text.indexOf("export const TOKEN_GROUPS");
const end = text.indexOf("\n];", start);
if (start < 0 || end < 0) {
  console.error("The file has no TOKEN_GROUPS list; the token list moved, so this script has to follow it.");
  process.exit(1);
}
const groups = text.slice(start, end);
const named = [...groups.matchAll(/name:\s*"([a-z][a-z0-9-]*)"/g)].map((m) => m[1]);
const listed = [...groups.matchAll(/tokens:\s*\[([^\]]*)\]/g)].flatMap((m) => [...m[1].matchAll(/"([a-z][a-z0-9-]*)"/g)].map((s) => s[1]));
const tokens = named.length > 0 ? named : listed;
if (tokens.length === 0) {
  console.error("TOKEN_GROUPS lists no token this script can read.");
  process.exit(1);
}
process.stdout.write(`${JSON.stringify({ source, tokens }, null, 2)}\n`);
