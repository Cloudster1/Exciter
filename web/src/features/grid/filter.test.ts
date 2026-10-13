import { describe, expect, it } from "vitest";
import { conditionParam, emptyFilter, fromAddress, gridParams, narrows, parseCondition, readAddress, toAddress } from "./filter";

const risk = "0195f000-0000-7000-8000-0000000000a1";
const high = "0195f000-0000-7000-8000-0000000000a2";
const note = "0195f000-0000-7000-8000-0000000000a5";

describe("the grid's filter builder", () => {
  it("spells each condition as the contract does, and reads it back with colons in the value", () => {
    expect(conditionParam({ attributeId: risk, op: "is", value: high })).toBe(`${risk}:is:${high}`);
    expect(conditionParam({ attributeId: note, op: "set", value: "" })).toBe(`${note}:set:`);
    expect(parseCondition(`${note}:contains:at 10:30`)).toEqual({ attributeId: note, op: "contains", value: "at 10:30" });
    expect(parseCondition("nonsense")).toBeUndefined();
  });

  it("asks only for what narrows, and the order when there is one", () => {
    expect(gridParams(emptyFilter(), { column: "", desc: false })).toEqual({});
    const filter = {
      ...emptyFilter(),
      q: "  pay ",
      kinds: ["requirement" as const],
      labels: ["checkout", "paper"],
      priorities: ["must" as const],
      attributes: [{ attributeId: risk, op: "is" as const, value: high }],
    };
    expect(gridParams(filter, { column: "title", desc: true })).toEqual({
      q: "pay",
      kind: ["requirement"],
      label: ["checkout", "paper"],
      priority: ["must"],
      attr: [`${risk}:is:${high}`],
      sort: "title",
      desc: true,
    });
    expect(gridParams(filter, { column: risk, desc: false }).desc).toBeUndefined();
  });

  it("keeps a grid in its address and reads the same grid back", () => {
    const filter = { ...emptyFilter(), statuses: ["s1"], owners: ["u1"], attributes: [{ attributeId: note, op: "contains" as const, value: "a:b" }] };
    const address = toAddress(filter, { column: "priority", desc: false }, { columns: ["key", "title"], view: "v1" });
    expect(address).toEqual({ status: ["s1"], owner: ["u1"], attr: [`${note}:contains:a:b`], sort: "priority", columns: ["key", "title"], view: "v1" });
    expect(fromAddress(readAddress(address as Record<string, unknown>))).toEqual({ filter, sort: { column: "priority", desc: false } });
    expect(readAddress({ label: "one", desc: "true", kind: [3, "section", ""], sort: "" })).toEqual({ label: ["one"], desc: true, kind: ["section"] });
  });

  it("says whether a filter narrows anything", () => {
    expect(narrows(emptyFilter())).toBe(false);
    expect(narrows({ ...emptyFilter(), q: " " })).toBe(false);
    expect(narrows({ ...emptyFilter(), types: ["t1"] })).toBe(true);
  });
});
