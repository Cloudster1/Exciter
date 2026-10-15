// Compares this repository's realm with Dynamo's canonical copy, keys sorted so
// that formatting alone is never a difference: node realm-check.mjs <url> <file>.
import { readFile } from "node:fs/promises";

const [url, file] = process.argv.slice(2);
const ours = "deploy/keycloak/realm.json";
// Dynamo answers this until it holds a realm, which is not yet a difference.
const NOT_FOUND = 404;

const sorted = (value) => {
  if (Array.isArray(value)) return value.map(sorted);
  if (value !== null && typeof value === "object") {
    return Object.fromEntries(Object.keys(value).sort().map((key) => [key, sorted(value[key])]));
  }
  return value;
};
const normalised = (text) => JSON.stringify(sorted(JSON.parse(text)), null, 2).split("\n");

let response;
try {
  response = await fetch(url);
} catch (error) {
  console.error(`Dynamo's realm at ${url} could not be fetched (${error.cause?.message ?? error.message}); check the network and run make realm-check again.`);
  process.exit(1);
}
if (response.status === NOT_FOUND) {
  console.log(`Dynamo holds no realm at ${url} yet, so there is nothing to compare ${ours} with.`);
  process.exit(0);
}
if (!response.ok) {
  console.error(`Dynamo's realm at ${url} answered ${response.status}; run make realm-check again, or set DYNAMO_REALM_URL if it moved.`);
  process.exit(1);
}

const theirs = normalised(await response.text());
const mine = normalised(await readFile(file, "utf8"));
const line = theirs.findIndex((text, i) => text !== mine[i]);
if (line < 0 && theirs.length === mine.length) {
  console.log(`${ours} is the same realm as Dynamo's.`);
  process.exit(0);
}
const at = line < 0 ? Math.min(theirs.length, mine.length) : line;
console.error(`${ours} differs from Dynamo's realm at ${url}; update ${ours} to Dynamo's copy, which is the canonical one.`);
console.error(`  first difference, line ${at + 1} of the key-sorted realm:`);
console.error(`    Dynamo: ${theirs[at]?.trim() ?? "(ends here)"}`);
console.error(`    here:   ${mine[at]?.trim() ?? "(ends here)"}`);
process.exit(1);
