import type { GraphParams } from "@/api/trace";
import { GRAPH_MAX_DEPTH } from "@/config";

/** What a trace graph's address says: which way and how deep, through which link types and relations, and how it
 * is shown; left out is both ways, the API's depth, every type and relation, and the picture. */
export interface GraphAddress {
  direction?: "up" | "down";
  depth?: number;
  type?: string[];
  relation?: string[];
  view?: "list";
}

function strings(value: unknown): string[] | undefined {
  const all = (Array.isArray(value) ? value : value === undefined ? [] : [value]).filter((each): each is string => typeof each === "string" && each !== "");
  return all.length ? all : undefined;
}

export function readGraphAddress(search: Record<string, unknown>): GraphAddress {
  const depth = Number(search.depth);
  return {
    direction: search.direction === "up" || search.direction === "down" ? search.direction : undefined,
    depth: Number.isInteger(depth) && depth >= 1 && depth <= GRAPH_MAX_DEPTH ? depth : undefined,
    type: strings(search.type),
    relation: strings(search.relation),
    view: search.view === "list" ? "list" : undefined,
  };
}

/** What the API is asked for an address. */
export function graphParams(address: GraphAddress): GraphParams {
  const params: GraphParams = {};
  if (address.direction) params.direction = address.direction;
  if (address.depth) params.depth = address.depth;
  if (address.type) params.type = address.type;
  if (address.relation) params.relation = address.relation;
  return params;
}
