import { describe, expect, it } from "vitest";
import { draft, rejected } from "@/test/workflows";
import { ANYWHERE_NODE_ID, fromNodeId, toFlow, toNodeId } from "./flow";
import { ANYWHERE, type Design } from "./graph";

const statuses = new Map([draft, rejected].map((status) => [status.id, status]));

const design: Design = {
  name: "Two steps",
  description: "",
  nodes: [
    { statusId: draft.id, x: 40, y: 40, onRevisionId: "" },
    { statusId: rejected.id, x: 360, y: 40, onRevisionId: "" },
  ],
  initialId: draft.id,
  edges: [
    { key: "e1", name: "Reject", description: "", from: draft.id, to: rejected.id, rules: [] },
    { key: "e2", name: "Reopen", description: "", from: rejected.id, to: draft.id, rules: [] },
  ],
};

describe("toFlow", () => {
  it("gives the any-status box a name React Flow accepts and takes it back", () => {
    expect(toNodeId(ANYWHERE)).toBe(ANYWHERE_NODE_ID);
    expect(fromNodeId(ANYWHERE_NODE_ID)).toBe(ANYWHERE);
    expect(fromNodeId(toNodeId(draft.id))).toBe(draft.id);
  });

  it("puts the box's place and name where the suite reads them", () => {
    const { nodes } = toFlow(design, statuses, { kind: "node", statusId: draft.id }, false);
    const node = nodes.find((each) => each.id === draft.id);
    expect(node?.position).toEqual({ x: 40, y: 40 });
    expect(node?.domAttributes).toMatchObject({ "data-workflow-node": "Draft", "data-x": 40, "data-y": 40, "aria-pressed": true });
    expect(node?.ariaLabel).toBe("Draft, where new requirements start");
    expect(node?.selected).toBe(true);
  });

  it("bows two transitions between the same pair apart", () => {
    const [reject, reopen] = toFlow(design, statuses, { kind: "none" }, false).edges;
    expect(reject?.data?.shape.path).not.toBe(reopen?.data?.shape.path);
    expect(reject?.domAttributes).toMatchObject({ "data-workflow-edge": "Reject" });
  });

  it("read-only, nothing can be pressed and the any-status box waits for a transition from everywhere", () => {
    const { nodes, edges } = toFlow(design, statuses, { kind: "none" }, true);
    expect(nodes.every((node) => node.ariaRole === undefined && !node.draggable && !node.focusable)).toBe(true);
    expect(edges.every((edge) => edge.ariaRole === undefined && !edge.focusable && edge.data?.readOnly)).toBe(true);
    expect(nodes.some((node) => node.id === ANYWHERE_NODE_ID)).toBe(false);
    const everywhere = { ...design, edges: [...design.edges, { key: "e3", name: "Drop", description: "", from: ANYWHERE, to: rejected.id, rules: [] }] };
    expect(toFlow(everywhere, statuses, { kind: "none" }, true).nodes.some((node) => node.id === ANYWHERE_NODE_ID)).toBe(true);
  });

  it("leaves out an edge whose end is not drawn", () => {
    expect(toFlow({ ...design, nodes: design.nodes.slice(0, 1) }, statuses, { kind: "none" }, false).edges).toHaveLength(0);
  });
});
