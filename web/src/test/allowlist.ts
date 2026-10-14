import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { safeHref, type DocNode } from "@/features/editor/schema";

// The server's allowlist, generated from its Go table by make openapi; the Go side refuses a stale copy.
export interface Attr {
  kind: "string" | "integer" | "boolean" | "integers" | "null";
  nullable?: boolean;
  enum?: string[];
  min?: number;
  max?: number;
  maxLength?: number;
  pattern?: string;
  url?: boolean;
}
export interface NodeSpec {
  attrs?: Record<string, Attr>;
  content?: string[];
  inline?: boolean;
  allowsMarks?: boolean;
}
interface Allowlist {
  nodes: Record<string, NodeSpec>;
  marks: Record<string, { attrs?: Record<string, Attr> }>;
}
export const allowlist = JSON.parse(readFileSync(resolve(process.cwd(), "../api/document-allowlist.json"), "utf8")) as Allowlist;
/** What a comment may hold (#33), generated the same way. */
export const commentAllowlist = JSON.parse(readFileSync(resolve(process.cwd(), "../api/comment-allowlist.json"), "utf8")) as Allowlist;

export function attrProblem(rule: Attr, value: unknown): string | null {
  if (value === null || value === undefined) return rule.nullable || rule.kind === "null" ? null : "is empty";
  const integer = (v: unknown) => typeof v === "number" && Number.isInteger(v) && v >= (rule.min ?? 0) && v <= (rule.max ?? 0);
  switch (rule.kind) {
    case "null":
      return "must be empty";
    case "boolean":
      return typeof value === "boolean" ? null : "is not a boolean";
    case "integer":
      return integer(value) ? null : "is out of range";
    case "integers":
      return Array.isArray(value) && value.length <= (rule.maxLength ?? 0) && value.every(integer) ? null : "is not a list of widths";
    case "string": {
      if (typeof value !== "string") return "is not a string";
      if (rule.maxLength && [...value].length > rule.maxLength) return "is too long";
      if (rule.enum && !rule.enum.includes(value)) return "is not one of the allowed values";
      if (rule.pattern && !new RegExp(rule.pattern, "u").test(value)) return "does not match its pattern";
      if (rule.url && safeHref(value) === null) return "is not a safe address";
      return null;
    }
  }
}

/** Everything the server would refuse in a document, as readable lines. */
export function problems(node: DocNode, parent: NodeSpec = { content: ["doc"] }, path = "doc"): string[] {
  const spec = allowlist.nodes[node.type];
  if (!spec) return [`${path}: node ${node.type} is not allowed`];
  const out: string[] = [];
  if (!parent.content?.includes(node.type)) out.push(`${path}: ${node.type} may not sit here`);
  if (node.type === "text" && !node.text) out.push(`${path}: text is empty`);
  for (const [name, value] of Object.entries(node.attrs ?? {})) {
    const rule = spec.attrs?.[name];
    if (!rule) out.push(`${path}: attribute ${name} is not allowed`);
    else {
      const problem = attrProblem(rule, value);
      if (problem) out.push(`${path}: ${name}=${JSON.stringify(value)} ${problem}`);
    }
  }
  if (node.marks?.length && !(spec.inline && parent.allowsMarks)) out.push(`${path}: ${node.type} may not carry marks`);
  for (const mark of node.marks ?? []) {
    const markSpec = allowlist.marks[mark.type];
    if (!markSpec) {
      out.push(`${path}: mark ${mark.type} is not allowed`);
      continue;
    }
    for (const [name, value] of Object.entries(mark.attrs ?? {})) {
      const rule = markSpec.attrs?.[name];
      const problem = rule ? attrProblem(rule, value) : "is not allowed";
      if (problem) out.push(`${path}: ${mark.type}.${name}=${JSON.stringify(value)} ${problem}`);
    }
  }
  (node.content ?? []).forEach((child, i) => {
    out.push(...problems(child, spec, `${path}/${child.type}[${i}]`));
  });
  return out;
}
