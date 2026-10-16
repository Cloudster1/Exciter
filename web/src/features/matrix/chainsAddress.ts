import type { Filter, Sort } from "@/api/search";
import type { ChainParams } from "@/api/trace";
import { fromAddress, gridParams, readAddress, toAddress, type GridAddress } from "@/features/grid/filter";

/** A chain report as its address holds it (#151): the grid's narrowing of the roots, the levels shown and the breaks. */
export interface ChainsAddress extends Omit<GridAddress, "columns" | "view"> {
  level?: string[];
  onlyBroken?: boolean;
}

/** Everything a chain report shows, as its toolbar changes it; no levels shows every one. */
export interface ChainsSetup {
  filter: Filter;
  sort: Sort;
  levels: number[];
  onlyBroken: boolean;
}

/** Reads a chain report's address, leaving out what is not there; the server judges the rest. */
export function readChainsAddress(search: Record<string, unknown>): ChainsAddress {
  const { columns: _columns, view: _view, ...grid } = readAddress(search);
  const address: ChainsAddress = grid;
  const levels = (Array.isArray(search.level) ? search.level : [search.level]).map(String).filter((each) => /^\d+$/.test(each));
  if (levels.length) address.level = levels;
  if (search.onlyBroken === true || search.onlyBroken === "true") address.onlyBroken = true;
  return address;
}

export function fromChainsAddress(address: ChainsAddress): ChainsSetup {
  const { filter, sort } = fromAddress(address);
  return { filter, sort, levels: (address.level ?? []).map(Number), onlyBroken: address.onlyBroken === true };
}

export function toChainsAddress(setup: ChainsSetup): ChainsAddress {
  const { columns: _columns, view: _view, ...grid } = toAddress(setup.filter, setup.sort);
  const address: ChainsAddress = grid;
  if (setup.levels.length) address.level = setup.levels.map(String);
  if (setup.onlyBroken) address.onlyBroken = true;
  return address;
}

/** A setup as the chain report's query string takes it. */
export function chainParams(setup: ChainsSetup): ChainParams {
  const params: ChainParams = { ...gridParams(setup.filter, setup.sort) };
  if (setup.levels.length) params.level = setup.levels;
  if (setup.onlyBroken) params.onlyBroken = true;
  return params;
}

/** The levels from one position to another, both included, as the report shows them; every level is none. */
export function levelRange(from: number, to: number, count: number): number[] {
  if (from === 0 && to === count - 1) return [];
  const out: number[] = [];
  for (let at = Math.min(from, to); at <= Math.max(from, to); at++) out.push(at);
  return out;
}
