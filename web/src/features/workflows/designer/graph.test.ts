import { describe, expect, it } from "vitest";
import type { RuleType } from "@/api/workflows";
import {
  WORKFLOW_COLUMN_GAP,
  WORKFLOW_EDGE_BEND,
  WORKFLOW_GRID,
  WORKFLOW_LAYOUT_ORIGIN_X,
  WORKFLOW_LAYOUT_ORIGIN_Y,
  WORKFLOW_NODE_HEIGHT,
  WORKFLOW_NODE_WIDTH,
  WORKFLOW_ROW_GAP,
} from "@/config";
import { aStatus, aWorkflow, approved, draft, rejected, review } from "@/test/workflows";
import {
  ANYWHERE,
  addStatus,
  anywhereRect,
  bendClearOf,
  bends,
  clipToRect,
  connect,
  describeRule,
  edgeShape,
  emptyDesign,
  fromWorkflow,
  moveNode,
  nodeRect,
  problems,
  removeStatus,
  setOnRevision,
  toInput,
  type Design,
  type Node,
} from "./graph";

const statuses = new Map([draft, review, approved, rejected].map((status) => [status.id, status]));
const saved = aWorkflow();

describe("reading a saved workflow", () => {
  it("keeps the statuses where they were drawn, and where a new revision sends them", () => {
    const design = fromWorkflow(saved);
    expect(design.nodes.find((node) => node.statusId === draft.id)).toEqual({ statusId: draft.id, x: 40, y: 40, onRevisionId: "" });
    expect(design.nodes.find((node) => node.statusId === approved.id)?.onRevisionId).toBe(draft.id);
    expect(design.initialId).toBe(draft.id);
  });

  // A workflow nobody drew yet, such as the default one, reads left to right by category rather than as a heap.
  it("lays out what was never placed by category column", () => {
    const design = fromWorkflow(saved);
    expect(design.nodes.find((node) => node.statusId === review.id)).toMatchObject({
      x: WORKFLOW_LAYOUT_ORIGIN_X + WORKFLOW_COLUMN_GAP,
      y: WORKFLOW_LAYOUT_ORIGIN_Y,
    });
    expect(design.nodes.find((node) => node.statusId === approved.id)?.x).toBe(WORKFLOW_LAYOUT_ORIGIN_X + 2 * WORKFLOW_COLUMN_GAP);
  });

  it("carries the transitions with their rules, a transition from everywhere leaving any status", () => {
    const design = fromWorkflow(saved);
    expect(design.edges[2]).toEqual({
      key: saved.transitions[2]!.id,
      id: saved.transitions[2]!.id,
      name: "Reject",
      description: "",
      from: ANYWHERE,
      to: rejected.id,
      rules: [{ kind: "validator", type: "validator.comment_required", config: {} }],
    });
  });

  it("round-trips into what the server takes, with layout, revision step and rules", () => {
    const input = toInput(fromWorkflow(saved));
    expect(input.steps[0]).toEqual({ statusId: draft.id, isInitial: true, onRevisionStatusId: undefined, layout: { x: 40, y: 40 } });
    expect(input.steps[2]).toMatchObject({ statusId: approved.id, onRevisionStatusId: draft.id });
    expect(input.transitions[2]).toEqual({
      id: saved.transitions[2]!.id,
      name: "Reject",
      description: "",
      fromStatusId: undefined,
      toStatusId: rejected.id,
      rules: [{ kind: "validator", type: "validator.comment_required", config: {} }],
    });
  });
});

describe("placing statuses", () => {
  it("stacks statuses of one category down their column", () => {
    const other = aStatus("s-other", "Second look", "review");
    const design = addStatus(addStatus(emptyDesign(), review), other);
    const [first, second] = design.nodes as [Node, Node];
    expect(first.x).toBe(second.x);
    expect(second.y - first.y).toBe(WORKFLOW_ROW_GAP);
  });

  it("makes the first status placed the one requirements start in, and adds a status once", () => {
    const design = addStatus(addStatus(addStatus(emptyDesign(), review), draft), draft);
    expect(design.initialId).toBe(review.id);
    expect(design.nodes).toHaveLength(2);
  });

  it("snaps a dropped status to the grid and keeps it on the canvas", () => {
    const design = moveNode(addStatus(emptyDesign(), draft), draft.id, 13, -20);
    expect(design.nodes[0]).toEqual({ statusId: draft.id, x: 2 * WORKFLOW_GRID, y: 0, onRevisionId: "" });
  });

  it("removes a status with the transitions that touched it and the revision steps that named it", () => {
    const design = removeStatus(fromWorkflow(saved), draft.id);
    expect(design.nodes.map((node) => node.statusId)).toEqual([review.id, approved.id, rejected.id]);
    expect(design.edges.map((edge) => edge.name)).toEqual(["Approve", "Reject"]);
    expect(design.nodes.find((node) => node.statusId === approved.id)?.onRevisionId).toBe("");
    expect(design.initialId).toBe("");
  });

  it("says where a new revision sends a status", () => {
    const design = setOnRevision(fromWorkflow(saved), review.id, draft.id);
    expect(toInput(design).steps[1]?.onRevisionStatusId).toBe(draft.id);
  });
});

describe("drawing transitions", () => {
  it("connects two statuses and hands back the new edge", () => {
    const result = connect(fromWorkflow(saved), approved.id, rejected.id, "Withdraw");
    const edge = result?.design.edges.find((each) => each.key === result.key);
    expect(edge).toMatchObject({ name: "Withdraw", from: approved.id, to: rejected.id, rules: [] });
    expect(edge?.id).toBeUndefined();
  });

  it("refuses a move from a status to itself, and an end the canvas does not have", () => {
    expect(connect(fromWorkflow(saved), draft.id, draft.id, "Loop")).toBeNull();
    expect(connect(fromWorkflow(saved), draft.id, "s-elsewhere", "Away")).toBeNull();
    expect(connect(fromWorkflow(saved), draft.id, ANYWHERE, "Nowhere")).toBeNull();
  });

  it("can start from any status", () => {
    expect(connect(fromWorkflow(saved), ANYWHERE, draft.id, "Restart")?.design.edges.at(-1)?.from).toBe(ANYWHERE);
  });
});

describe("edge geometry", () => {
  const left = { x: 0, y: 0, width: 100, height: 40 };
  const right = { x: 300, y: 0, width: 100, height: 40 };

  it("leaves a rect at its border, and runs straight when nothing is between the pair", () => {
    expect(clipToRect(left, { x: 500, y: 20 })).toEqual({ x: 100, y: 20 });
    expect(edgeShape(left, right, 0)).toEqual({ path: "M 100 20 Q 200 20 300 20", label: { x: 200, y: 20 } });
    expect(edgeShape(left, right, 40).label.y).toBeGreaterThan(20);
  });

  // Submit and Send back between the same two statuses must not lie on top of each other.
  it("bends opposite directions to opposite sides, and spreads several of one direction", () => {
    const edge = (key: string, from: string, to: string) => ({ key, name: key, description: "", from, to, rules: [] });
    const bent = bends([edge("a", "s1", "s2"), edge("b", "s2", "s1"), edge("c", "s1", "s3")]);
    expect(bent.get("c")).toBe(0);
    const control = (path: string) => Number(path.split(" ")[5]);
    const there = control(edgeShape(left, right, bent.get("a")!).path);
    const back = control(edgeShape(right, left, bent.get("b")!).path);
    expect(Math.sign(there - 20)).toBe(-Math.sign(back - 20));
    const spread = bends(["a", "b", "c"].map((key) => edge(key, "s1", "s2")));
    expect([spread.get("a"), spread.get("b"), spread.get("c")]).toEqual([0, WORKFLOW_EDGE_BEND, -WORKFLOW_EDGE_BEND]);
  });

  it("bows round a card in the way, and keeps a bend that already clears", () => {
    const a = { x: 0, y: 0, width: 240, height: 72 };
    const b = { x: 352, y: 0, width: 240, height: 72 };
    const c = { x: 704, y: 0, width: 240, height: 72 };
    expect(bendClearOf(a, b, [c], 0, 64)).toBe(0);
    expect(bendClearOf(a, c, [b], 0, 64)).not.toBe(0);
    expect(bendClearOf(a, b, [c], 64, 64)).toBe(64);
  });

  it("sizes rects the way the canvas draws them, and lays nothing over the any-status box", () => {
    let design = emptyDesign();
    for (const id of ["a", "b", "c", "d", "e"]) design = addStatus(design, aStatus(id, id, "draft"));
    expect(nodeRect(design.nodes[0]!)).toEqual({ x: design.nodes[0]!.x, y: design.nodes[0]!.y, width: WORKFLOW_NODE_WIDTH, height: WORKFLOW_NODE_HEIGHT });
    const anywhere = anywhereRect();
    for (const node of design.nodes) {
      expect(Math.abs(node.x - anywhere.x) < WORKFLOW_NODE_WIDTH && Math.abs(node.y - anywhere.y) < WORKFLOW_NODE_HEIGHT).toBe(false);
    }
  });
});

describe("saying what would stop a save", () => {
  it("names each thing the server would refuse", () => {
    const design: Design = {
      name: "",
      description: "",
      nodes: [
        { statusId: draft.id, x: 0, y: 0, onRevisionId: "" },
        { statusId: rejected.id, x: 300, y: 0, onRevisionId: "" },
      ],
      initialId: "",
      edges: [
        { key: "a", name: "", description: "", from: draft.id, to: rejected.id, rules: [] },
        { key: "b", name: "Reject", description: "", from: ANYWHERE, to: rejected.id, rules: [] },
        { key: "c", name: "reject", description: "", from: ANYWHERE, to: draft.id, rules: [] },
      ],
    };
    expect(problems(design, statuses)).toEqual([
      "The workflow needs a name.",
      "Choose the status new requirements start in.",
      "Every transition needs a name.",
      "Two transitions called reject leave any status.",
    ]);
    expect(problems(fromWorkflow(saved), statuses)).toEqual([]);
  });
});

describe("describing a rule", () => {
  const catalogue: RuleType[] = [
    {
      kind: "condition",
      type: "condition.project_role",
      label: "Project role",
      description: "",
      options: [{ name: "roles", label: "Roles", kind: "roles", required: true, choices: ["reviewer", "admin"] }],
    },
    { kind: "validator", type: "validator.comment_required", label: "Comment required", description: "", options: [] },
  ];

  it("uses the label and spells out the configuration in the reader's words", () => {
    expect(describeRule({ kind: "validator", type: "validator.comment_required", config: {} }, catalogue)).toBe("Comment required");
    expect(describeRule({ kind: "condition", type: "condition.project_role", config: { roles: ["reviewer", "admin"] } }, catalogue)).toBe(
      "Project role (Roles: Reviewer, Project administrator)",
    );
    expect(describeRule({ kind: "condition", type: "condition.moon_phase", config: {} }, catalogue)).toBe("condition.moon_phase");
  });
});
