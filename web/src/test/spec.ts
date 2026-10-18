import { screen } from "@testing-library/react";
import type { SpecEntry, TreeNode } from "@/api/tree";
import { stubApi } from "./app";
import { aProject } from "./projects";

// The specification the document view's tests read: a section, Scope, with two requirements below it, Pay and Ship.

export const project = aProject();
export const node = (over: Partial<TreeNode>): TreeNode => ({
  id: "x",
  key: "CHK-R1",
  kind: "requirement",
  title: "X",
  parentId: null,
  depth: 0,
  outline: "1",
  typeId: null,
  status: null,
  revision: 1,
  children: 0,
  quality: null,
  ...over,
});
export const scope = node({ id: "s", key: "CHK-R1", kind: "section", title: "Scope", children: 2 });
export const pay = node({ id: "p", key: "CHK-R2", title: "Pay", parentId: "s", depth: 1, outline: "1.1" });
export const ship = node({ id: "h", key: "CHK-R3", title: "Ship", parentId: "s", depth: 1, outline: "1.2" });
export const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
export const can = { edit: true, transition: true, delete: true, link: true, comment: true };
export const entry = (n: TreeNode, text: string, over: Partial<SpecEntry> = {}): SpecEntry => ({
  node: n,
  statement: paragraph(text),
  rationale: null,
  attributes: {},
  labels: [],
  can,
  comments: { threads: 0, open: 0 },
  suspectLinks: 0,
  reuse: null,
  testCase: null,
  type: null,
  owner: null,
  priority: null,
  approval: { pendingReviewId: null, approvedRevision: null, current: false },
  change: null,
  review: null,
  lock: null,
  ...over,
});

export function stubDocument(entries: SpecEntry[], answers: Parameters<typeof stubApi>[0] = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project } },
    "GET /projects/CHK/tree": { status: 200, body: { nodes: [scope, pay, ship] } },
    "GET /projects/CHK/spec": { status: 200, body: { entries, total: entries.length, limit: 50, offset: 0 } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
    ...answers,
  });
}

export const entryOf = (key: string) => screen.getByRole("article", { name: "Specification" }).querySelector<HTMLElement>(`[data-spec-entry="${key}"]`)!;
