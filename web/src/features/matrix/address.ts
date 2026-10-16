import type { MatrixColumns, MatrixParams } from "@/api/coverage";
import type { Filter, Sort } from "@/api/search";
import { fromAddress, gridParams, readAddress, toAddress, type GridAddress } from "@/features/grid/filter";

/** Which way a link must point from its row to be counted. */
export type MatrixDirection = "both" | "outgoing" | "incoming";

export const MATRIX_DIRECTIONS: MatrixDirection[] = ["both", "outgoing", "incoming"];
/** The relations an Armature link has, in the order the picker offers them. */
export const ISSUE_RELATIONS = ["implements", "verifies", "relates"] as const;

/** A matrix as its address holds it: the grid's narrowing and order for the rows, then what the columns are. */
export interface MatrixAddress extends Omit<GridAddress, "columns" | "view"> {
  columns?: MatrixColumns;
  columnProject?: string;
  linkType?: string[];
  direction?: MatrixDirection;
  relation?: string[];
}

/** Everything a matrix lays out, as its toolbar changes it. */
export interface MatrixSetup {
  filter: Filter;
  sort: Sort;
  columns: MatrixColumns;
  columnProject: string;
  linkTypes: string[];
  direction: MatrixDirection;
  relations: string[];
}

const list = (value: unknown): string[] | undefined => {
  const all = (Array.isArray(value) ? value : typeof value === "string" ? [value] : []).filter(
    (each): each is string => typeof each === "string" && each !== "",
  );
  return all.length ? all : undefined;
};

/** Reads a matrix's address, leaving out what is not there; the server judges the rest. */
export function readMatrixAddress(search: Record<string, unknown>): MatrixAddress {
  const { columns: _gridColumns, view: _view, ...grid } = readAddress(search);
  const address: MatrixAddress = grid;
  if (search.columns === "issues") address.columns = "issues";
  if (typeof search.columnProject === "string" && search.columnProject) address.columnProject = search.columnProject;
  const linkType = list(search.linkType);
  if (linkType) address.linkType = linkType;
  if (search.direction === "outgoing" || search.direction === "incoming") address.direction = search.direction;
  const relation = list(search.relation);
  if (relation) address.relation = relation;
  return address;
}

/** The setup an address holds, with its defaults filled in. */
export function fromMatrixAddress(address: MatrixAddress): MatrixSetup {
  const { columns: _columns, ...grid } = address;
  const { filter, sort } = fromAddress(grid);
  return {
    filter,
    sort,
    columns: address.columns ?? "requirements",
    columnProject: address.columnProject ?? "",
    linkTypes: address.linkType ?? [],
    direction: address.direction ?? "both",
    relations: address.relation ?? [],
  };
}

/** The address of a setup; the defaults and what does not fit the columns are left out. */
export function toMatrixAddress(setup: MatrixSetup): MatrixAddress {
  const { columns: _gridColumns, view: _view, ...grid } = toAddress(setup.filter, setup.sort);
  const address: MatrixAddress = grid;
  if (setup.columns === "issues") {
    address.columns = "issues";
    if (setup.relations.length) address.relation = setup.relations;
    return address;
  }
  if (setup.columnProject) address.columnProject = setup.columnProject;
  if (setup.linkTypes.length) address.linkType = setup.linkTypes;
  if (setup.direction !== "both") address.direction = setup.direction;
  return address;
}

/** A setup as the matrix's query string takes it. */
export function matrixParams(setup: MatrixSetup): MatrixParams {
  const address = toMatrixAddress(setup);
  return { ...gridParams(setup.filter, setup.sort), ...pick(address) };
}

function pick({ columns, columnProject, linkType, direction, relation }: MatrixAddress): MatrixParams {
  const params: MatrixParams = {};
  if (columns) params.columns = columns;
  if (columnProject) params.columnProject = columnProject;
  if (linkType) params.linkType = linkType;
  if (direction) params.direction = direction;
  if (relation) params.relation = relation as MatrixParams["relation"];
  return params;
}
