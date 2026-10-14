import type { Kind } from "@/api/attributes";
import type { AttributeCondition, Filter, GridParams, Op, Sort } from "@/api/search";

/** The comparisons each kind of attribute takes, as the server's search.opsOf allows them. */
export const OPS_BY_KIND: Record<Kind, Op[]> = {
  text: ["is", "not", "contains", "set", "unset"],
  richText: ["is", "not", "contains", "set", "unset"],
  number: ["is", "not", "gte", "lte", "set", "unset"],
  select: ["is", "not", "set", "unset"],
  multiSelect: ["is", "not", "set", "unset"],
  date: ["is", "not", "gte", "lte", "set", "unset"],
  user: ["is", "not", "set", "unset"],
  boolean: ["is", "not", "set", "unset"],
  url: ["is", "not", "set", "unset"],
};

/** Comparisons that ask nothing but whether a value is there. */
export const VALUELESS: Op[] = ["set", "unset"];

/** The columns the grid sorts by besides an attribute's id, and those it only shows, as search.SortColumns says. */
export const SORT_COLUMNS = ["key", "title", "type", "status", "owner", "priority", "updated"] as const;
export const SHOW_COLUMNS = ["labels", "outline"] as const;
/** What a grid shows when nobody chose. */
export const DEFAULT_COLUMNS = ["outline", "key", "title", "status", "owner", "priority", "updated"];

export function emptyFilter(): Filter {
  return { q: "", kinds: [], types: [], statuses: [], categories: [], labels: [], owners: [], priorities: [], attributes: [], suspect: false };
}

/** One condition as the attr parameter spells it: attributeId:op:value. */
export function conditionParam(c: AttributeCondition): string {
  return `${c.attributeId}:${c.op}:${c.value}`;
}

/** Reads attributeId:op:value back; the value may hold colons. Undefined for what is no condition. */
export function parseCondition(raw: string): AttributeCondition | undefined {
  const [attributeId, op, ...rest] = raw.split(":");
  if (!attributeId || !op) return undefined;
  return { attributeId, op: op as Op, value: rest.join(":") };
}

/** A filter and an order as the grid's query string takes them; a list left empty is left out. */
export function gridParams(filter: Filter, sort: Sort): GridParams {
  const params: GridParams = {};
  const q = filter.q.trim();
  if (q) params.q = q;
  if (filter.kinds.length) params.kind = filter.kinds;
  if (filter.types.length) params.type = filter.types;
  if (filter.statuses.length) params.status = filter.statuses;
  if (filter.categories.length) params.category = filter.categories;
  if (filter.labels.length) params.label = filter.labels;
  if (filter.owners.length) params.owner = filter.owners;
  if (filter.priorities.length) params.priority = filter.priorities;
  if (filter.attributes.length) params.attr = filter.attributes.map(conditionParam);
  if (filter.suspect) params.suspect = true;
  if (sort.column) {
    params.sort = sort.column;
    if (sort.desc) params.desc = true;
  }
  return params;
}

/** The grid as its address holds it: the filter's lists, conditions spelled as attr is, and the view open. */
export interface GridAddress {
  q?: string;
  kind?: string[];
  type?: string[];
  status?: string[];
  category?: string[];
  label?: string[];
  owner?: string[];
  priority?: string[];
  attr?: string[];
  suspect?: boolean;
  sort?: string;
  desc?: boolean;
  columns?: string[];
  view?: string;
}

const strings = (value: unknown): string[] | undefined => {
  const list = (Array.isArray(value) ? value : typeof value === "string" ? [value] : []).filter(
    (each): each is string => typeof each === "string" && each !== "",
  );
  return list.length ? list : undefined;
};

/** Reads the grid's address, leaving out what is not there; the server judges the rest. */
export function readAddress(search: Record<string, unknown>): GridAddress {
  const address: GridAddress = {};
  if (typeof search.q === "string" && search.q) address.q = search.q;
  for (const name of ["kind", "type", "status", "category", "label", "owner", "priority", "attr", "columns"] as const) {
    const list = strings(search[name]);
    if (list) address[name] = list;
  }
  if (search.suspect === true || search.suspect === "true") address.suspect = true;
  if (typeof search.sort === "string" && search.sort) address.sort = search.sort;
  if (search.desc === true || search.desc === "true") address.desc = true;
  if (typeof search.view === "string" && search.view) address.view = search.view;
  return address;
}

/** The filter and order an address holds. */
export function fromAddress(address: GridAddress): { filter: Filter; sort: Sort } {
  const filter: Filter = {
    q: address.q ?? "",
    kinds: (address.kind ?? []) as Filter["kinds"],
    types: address.type ?? [],
    statuses: address.status ?? [],
    categories: (address.category ?? []) as Filter["categories"],
    labels: address.label ?? [],
    owners: address.owner ?? [],
    priorities: (address.priority ?? []) as Filter["priorities"],
    attributes: (address.attr ?? []).map(parseCondition).filter((c): c is AttributeCondition => c !== undefined),
    suspect: address.suspect === true,
  };
  return { filter, sort: { column: address.sort ?? "", desc: address.desc === true } };
}

/** The address of a filter and an order, with the columns and the view kept as they are. */
export function toAddress(filter: Filter, sort: Sort, keep: Pick<GridAddress, "columns" | "view"> = {}): GridAddress {
  const params = gridParams(filter, sort);
  const address: GridAddress = { ...keep };
  if (params.q) address.q = params.q;
  if (params.kind) address.kind = params.kind;
  if (params.type) address.type = params.type;
  if (params.status) address.status = params.status;
  if (params.category) address.category = params.category;
  if (params.label) address.label = params.label;
  if (params.owner) address.owner = params.owner;
  if (params.priority) address.priority = params.priority;
  if (params.attr) address.attr = params.attr;
  if (params.suspect) address.suspect = true;
  if (params.sort) address.sort = params.sort;
  if (params.desc) address.desc = true;
  return address;
}

/** Whether a filter narrows anything. */
export function narrows(filter: Filter): boolean {
  return filter.q.trim() !== "" || filter.suspect || Object.entries(filter).some(([name, value]) => name !== "q" && Array.isArray(value) && value.length > 0);
}
