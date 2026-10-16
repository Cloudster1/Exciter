import type { Matrix, MatrixColumns, MatrixParams } from "@/api/coverage";
import type { Filter, Sort } from "@/api/search";
import { fromAddress, gridParams, readAddress, toAddress, type GridAddress } from "@/features/grid/filter";

/** Which way a link must point from its row to be counted. */
export type MatrixDirection = "both" | "outgoing" | "incoming";

export const MATRIX_DIRECTIONS: MatrixDirection[] = ["both", "outgoing", "incoming"];
/** Which rows or columns without a link a matrix keeps (#148); empty keeps every one. */
export type MatrixGaps = "" | "rows" | "columns" | "both";

export const MATRIX_GAPS: MatrixGaps[] = ["", "rows", "columns", "both"];
/** The relations an Armature link has, in the order the picker offers them. */
export const ISSUE_RELATIONS = ["implements", "verifies", "relates"] as const;
/** The relations a Stator page link has (#150), in the order the picker offers them. */
export const PAGE_RELATIONS = ["specifies", "describes", "relates"] as const;

/** A matrix as its address holds it: the grid's narrowing and order for the rows, then what the columns are. */
export interface MatrixAddress extends Omit<GridAddress, "columns" | "view"> {
  columns?: MatrixColumns;
  columnProject?: string;
  linkType?: string[];
  direction?: MatrixDirection;
  relation?: string[];
  columnQuery?: string;
  columnType?: string[];
  gaps?: Exclude<MatrixGaps, "">;
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
  /** The column filter, which makes the columns what it keeps, linked or not; empty leaves them what the rows reach. */
  columnQuery: string;
  columnTypes: string[];
  gaps: MatrixGaps;
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
  if (search.columns === "issues" || search.columns === "pages") address.columns = search.columns;
  if (typeof search.columnProject === "string" && search.columnProject) address.columnProject = search.columnProject;
  const linkType = list(search.linkType);
  if (linkType) address.linkType = linkType;
  if (search.direction === "outgoing" || search.direction === "incoming") address.direction = search.direction;
  const relation = list(search.relation);
  if (relation) address.relation = relation;
  if (typeof search.columnQuery === "string" && search.columnQuery) address.columnQuery = search.columnQuery;
  const columnType = list(search.columnType);
  if (columnType) address.columnType = columnType;
  if (search.gaps === "rows" || search.gaps === "columns" || search.gaps === "both") address.gaps = search.gaps;
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
    columnQuery: address.columnQuery ?? "",
    columnTypes: address.columnType ?? [],
    gaps: address.gaps ?? "",
  };
}

/** The address of a setup; the defaults and what does not fit the columns are left out. */
export function toMatrixAddress(setup: MatrixSetup): MatrixAddress {
  const { columns: _gridColumns, view: _view, ...grid } = toAddress(setup.filter, setup.sort);
  const address: MatrixAddress = grid;
  if (setup.columns !== "requirements") {
    address.columns = setup.columns;
    if (setup.relations.length) address.relation = setup.relations;
    if (setup.gaps === "rows") address.gaps = "rows";
    return address;
  }
  if (setup.columnProject) address.columnProject = setup.columnProject;
  if (setup.linkTypes.length) address.linkType = setup.linkTypes;
  if (setup.direction !== "both") address.direction = setup.direction;
  const query = setup.columnQuery.trim();
  if (query) address.columnQuery = query;
  if (setup.columnTypes.length) address.columnType = setup.columnTypes;
  // Columns the rows reach always have a link, so their gaps need a column filter.
  if (setup.gaps === "rows" || (setup.gaps && hasColumnFilter(setup))) address.gaps = setup.gaps;
  return address;
}

/** Whether the columns are a column filter's rather than what the rows reach. */
export function hasColumnFilter(setup: MatrixSetup): boolean {
  return setup.columns === "requirements" && (setup.columnQuery.trim() !== "" || setup.columnTypes.length > 0);
}

/** A setup as the matrix's query string takes it. */
export function matrixParams(setup: MatrixSetup): MatrixParams {
  const address = toMatrixAddress(setup);
  return { ...gridParams(setup.filter, setup.sort), ...pick(address) };
}

function pick({ columns, columnProject, linkType, direction, relation, columnQuery, columnType, gaps }: MatrixAddress): MatrixParams {
  const params: MatrixParams = {};
  if (columns) params.columns = columns;
  if (columnProject) params.columnProject = columnProject;
  if (linkType) params.linkType = linkType;
  if (direction) params.direction = direction;
  if (relation) params.relation = relation as MatrixParams["relation"];
  if (columnQuery) params.columnQuery = columnQuery;
  if (columnType) params.columnType = columnType;
  if (gaps) params.gaps = gaps;
  return params;
}

/** A column's name: the requirement's key, the issue's, or a page's title as Stator told the reader, its space and id
 * where it told none. */
export function columnName(column: Matrix["columns"][number], titles?: ReadonlyMap<string, string>): string {
  if (column.requirement) return column.requirement.key;
  if (column.page) return titles?.get(column.page.id) ?? `${column.page.spaceKey}/${column.page.id}`;
  return column.issueKey;
}

/** A column's identity, unique on a page, whatever it is called. */
export function columnKey(column: Matrix["columns"][number]): string {
  return column.requirement?.id ?? column.page?.id ?? column.issueKey;
}
