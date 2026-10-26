import type { Place, TreeNode } from "@/api/tree";

/** What numbering needs of a node: who it is and what it is below, in reading order. */
export type Numbered = Pick<TreeNode, "id" | "parentId">;

/** Outline numbers by the server's rule: the 1-based place among siblings, joined by dots, in reading order. */
export function outlines(nodes: Numbered[]): Map<string, string> {
  const out = new Map<string, string>();
  const counts = new Map<string | null, number>();
  for (const node of nodes) {
    const parent = node.parentId ?? null;
    const n = (counts.get(parent) ?? 0) + 1;
    counts.set(parent, n);
    const above = parent ? out.get(parent) : undefined;
    out.set(node.id, above ? `${above}.${n}` : String(n));
  }
  return out;
}

/** The node and everything below it, from a list in reading order. */
export function subtreeOf<T extends Numbered>(nodes: T[], id: string): T[] {
  const start = nodes.findIndex((n) => n.id === id);
  if (start < 0) return [];
  const inside = new Set([id]);
  const out = [nodes[start]!];
  for (const node of nodes.slice(start + 1)) {
    if (!node.parentId || !inside.has(node.parentId)) break;
    inside.add(node.id);
    out.push(node);
  }
  return out;
}

/** The tree in reading order as it would read with a node, and what is below it, moved to a place. */
export function afterMove<T extends Numbered>(nodes: T[], id: string, place: Place): T[] {
  const moving = subtreeOf(nodes, id) as T[];
  if (moving.length === 0) return nodes;
  const ids = new Set(moving.map((n) => n.id));
  const rest = nodes.filter((n) => !ids.has(n.id));
  const root = { ...moving[0]!, parentId: place.parentId };
  const moved = [root, ...moving.slice(1)];
  let at: number;
  if (place.afterId) {
    // After the sibling and everything below it.
    at = rest.findIndex((n) => n.id === place.afterId) + subtreeOf(rest, place.afterId).length;
  } else if (place.parentId) {
    at = rest.findIndex((n) => n.id === place.parentId) + 1;
  } else {
    at = 0;
  }
  return [...rest.slice(0, at), ...moved, ...rest.slice(at)];
}
