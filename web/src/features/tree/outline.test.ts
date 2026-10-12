import { describe, expect, it } from "vitest";
import { afterMove, outlines, subtreeOf } from "./outline";

// A section with two requirements, the second with one below it, then a requirement at the top.
const nodes = [
  { id: "s", parentId: null },
  { id: "a", parentId: "s" },
  { id: "b", parentId: "s" },
  { id: "b1", parentId: "b" },
  { id: "t", parentId: null },
];

describe("outline numbers", () => {
  it("number by place among siblings, joined by dots, as the server does", () => {
    expect(Object.fromEntries(outlines(nodes))).toEqual({ s: "1", a: "1.1", b: "1.2", b1: "1.2.1", t: "2" });
  });

  it("take what is below a node along with it", () => {
    expect(subtreeOf(nodes, "b").map((n) => n.id)).toEqual(["b", "b1"]);
    expect(subtreeOf(nodes, "t").map((n) => n.id)).toEqual(["t"]);
  });

  it("number a move's preview as the tree will read after it", () => {
    const first = afterMove(nodes, "b", { parentId: null, afterId: null });
    expect(Object.fromEntries(outlines(first))).toEqual({ b: "1", b1: "1.1", s: "2", a: "2.1", t: "3" });
    const after = afterMove(nodes, "t", { parentId: "s", afterId: "a" });
    expect(Object.fromEntries(outlines(after))).toEqual({ s: "1", a: "1.1", t: "1.2", b: "1.3", b1: "1.3.1" });
    const inside = afterMove(nodes, "a", { parentId: "b", afterId: "b1" });
    expect(Object.fromEntries(outlines(inside))).toEqual({ s: "1", b: "1.1", b1: "1.1.1", a: "1.1.2", t: "2" });
  });
});
