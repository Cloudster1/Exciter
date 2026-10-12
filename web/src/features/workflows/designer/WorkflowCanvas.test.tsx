import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { draft, rejected } from "@/test/workflows";
import { ANYWHERE, type Design } from "./graph";
import { WorkflowCanvas } from "./WorkflowCanvas";

const statuses = new Map([draft, rejected].map((status) => [status.id, status]));

const design: Design = {
  name: "Two steps",
  description: "",
  nodes: [
    { statusId: draft.id, x: 40, y: 40, onRevisionId: "" },
    { statusId: rejected.id, x: 360, y: 40, onRevisionId: "" },
  ],
  initialId: draft.id,
  edges: [{ key: "e1", name: "Reject", description: "", from: draft.id, to: rejected.id, rules: [] }],
};

describe("WorkflowCanvas read-only", () => {
  it("draws the same boxes and arrows with nothing to grab", () => {
    render(<WorkflowCanvas design={design} statuses={statuses} readOnly label="Workflow Two steps" />);
    expect(document.querySelector('[data-workflow-node="Draft"]')).not.toBeNull();
    expect(document.querySelector('[data-workflow-edge="Reject"]')).not.toBeNull();
    expect(document.querySelector("[data-workflow-initial]")).not.toBeNull();
    expect(document.querySelectorAll('[role="button"]')).toHaveLength(0);
    expect(document.querySelectorAll("[data-connect-handle]")).toHaveLength(0);
    expect(document.querySelector('[data-workflow-canvas][aria-label="Workflow Two steps"]')).not.toBeNull();
  });

  // The dashed box only means something when a transition leaves it.
  it("leaves out the any-status box until a transition from everywhere needs it", () => {
    const { rerender } = render(<WorkflowCanvas design={design} statuses={statuses} readOnly />);
    expect(document.querySelector("[data-workflow-anywhere]")).toBeNull();
    const everywhere = { ...design, edges: [...design.edges, { key: "e2", name: "Drop", description: "", from: ANYWHERE, to: rejected.id, rules: [] }] };
    rerender(<WorkflowCanvas design={everywhere} statuses={statuses} readOnly />);
    expect(document.querySelector("[data-workflow-anywhere]")).not.toBeNull();
  });

  it("puts the description, else the category, on the card and the name on the arrow", () => {
    const described = new Map(statuses);
    described.set(draft.id, { ...draft, description: "Still being written." });
    render(<WorkflowCanvas design={design} statuses={described} readOnly />);
    expect(Array.from(document.querySelectorAll("[data-workflow-subtitle]")).map((el) => el.textContent)).toEqual(["Still being written.", "Closed"]);
    expect(document.querySelector('[data-workflow-edge-label="Reject"]')).not.toBeNull();
  });

  it("stays interactive by default", () => {
    render(<WorkflowCanvas design={design} statuses={statuses} />);
    expect(document.querySelectorAll('[role="button"]').length).toBeGreaterThan(0);
    expect(document.querySelectorAll("[data-connect-handle]").length).toBeGreaterThan(0);
  });
});
