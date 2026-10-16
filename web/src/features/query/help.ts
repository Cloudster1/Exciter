// What a query can say, written once for the help and the tests, as Armature's search help is. The server's catalog
// is the authority; this describes its fixed fields for a reader, whose words are in t.query.takes.

/** The fields every project has, in the order the help lists them, each with an example. */
export const QUERY_FIELDS: Array<{ name: string; example: string }> = [
  { name: "key", example: "key IN (CHK-R1, CHK-R2)" },
  { name: "title", example: "title ~ brake" },
  { name: "text", example: 'text ~ "brake pad"' },
  { name: "statement", example: "statement ~ within" },
  { name: "rationale", example: "rationale IS EMPTY" },
  { name: "kind", example: "kind = requirement" },
  { name: "type", example: 'type = "Safety goal"' },
  { name: "status", example: 'status IN (Draft, "In review")' },
  { name: "category", example: "category != closed" },
  { name: "owner", example: "owner = currentUser()" },
  { name: "author", example: "author = ada@exciter.test" },
  { name: "priority", example: "priority >= should" },
  { name: "labels", example: "labels IN (safety, ui)" },
  { name: "parent", example: "parent = CHK-R4" },
  { name: "revision", example: "revision > 3" },
  { name: "created", example: "created >= -7d" },
  { name: "updated", example: "updated >= startOfWeek()" },
  { name: "suspect", example: "suspect > 0" },
  { name: "approval", example: "approval = outdated" },
  { name: "reuse", example: "reuse = behind" },
  { name: "decomposed", example: "decomposed = none" },
  { name: "implemented", example: "implemented != full" },
  { name: "verified", example: "verified IN (none, partial)" },
];

export const QUERY_ATTRIBUTE_EXAMPLE = '"Risk" = High';
export const QUERY_LINK_EXAMPLE = "verifiedBy IS EMPTY";

export const QUERY_OPERATORS = ["=", "!=", "<", "<=", ">", ">=", "~", "!~", "IN", "NOT IN", "IS EMPTY", "IS NOT EMPTY"];

export const QUERY_FUNCTIONS = ["currentUser()", "now()", "startOfDay()", "endOfDay()", "startOfWeek()", "endOfWeek()", "startOfMonth()", "endOfMonth()"];

export const QUERY_EXAMPLES = [
  "owner = currentUser() AND category != closed ORDER BY priority DESC",
  "kind = requirement AND verifiedBy IS EMPTY",
  "approval = outdated OR suspect > 0",
  "verified != full AND updated >= startOfWeek(-1w) ORDER BY updated DESC",
];

/** A line of spaces with a caret under the character an error points at, so the query shows as typed with the trouble marked. */
export function caretLine(position: number | undefined): string {
  if (!position || position < 1) return "";
  return `${" ".repeat(position - 1)}^`;
}
