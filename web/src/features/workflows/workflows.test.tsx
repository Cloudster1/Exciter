import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { RequirementType } from "@/api/attributes";
import type { RuleType } from "@/api/workflows";
import { arrival, renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { aWorkflow, approved, draft, rejected, review } from "@/test/workflows";

afterEach(() => vi.unstubAllGlobals());

const project = aProject();
const flow = aWorkflow();
const lean = aWorkflow({
  id: "0195f000-0000-7000-8000-000000000c02",
  name: "Lean",
  projectDefault: false,
  steps: [
    { status: draft, isInitial: true, onRevisionStatusId: null, layout: null },
    { status: rejected, isInitial: false, onRevisionStatusId: null, layout: null },
  ],
  transitions: [],
});
const functional: RequirementType = {
  id: "0195f000-0000-7000-8000-0000000000c1",
  name: "Functional",
  description: "",
  icon: "requirement",
  colour: "#4f6bed",
  position: 0,
  workflowId: null,
  retired: false,
  role: "requirement",
  roleHeld: false,
  requirements: 3,
  createdAt: "2026-10-01T08:00:00Z",
  updatedAt: "2026-10-01T08:00:00Z",
};
const ruleTypes: RuleType[] = [
  {
    kind: "condition",
    type: "condition.project_role",
    label: "Project role",
    description: "Only people holding one of these roles may take it.",
    options: [{ name: "roles", label: "Roles", kind: "roles", required: true, choices: ["viewer", "reviewer", "editor", "admin"] }],
  },
  { kind: "validator", type: "validator.comment_required", label: "Comment required", description: "It asks for a comment.", options: [] },
];
const shaped = {
  "GET /projects/CHK": { status: 200, body: { project } },
  "GET /projects/CHK/workflows": { status: 200, body: { workflows: [flow, lean] } },
  "GET /projects/CHK/statuses": { status: 200, body: { statuses: [draft, review, approved, rejected] } },
  "GET /projects/CHK/types": { status: 200, body: { types: [functional] } },
  "GET /workflow-rule-types": { status: 200, body: { ruleTypes } },
};

function node(name: string) {
  return document.querySelector(`[data-workflow-node="${name}"]`) as HTMLElement;
}

describe("a project's workflow settings", () => {
  it("draws the workflow it follows, lists its workflows and statuses, and maps statuses when it follows another", async () => {
    const sent = stubApi({ ...shaped, "PUT /projects/CHK/workflow": { status: 200, body: { workflow: { ...lean, projectDefault: true } } } });
    await renderAt("/p/CHK/settings?tab=workflow");
    expect(await screen.findByRole("figure", { name: "Workflow Default" })).toBeInTheDocument();
    expect(node("Approved")).toBeInTheDocument();
    expect(document.querySelector('[data-workflow-card="Default"]')).toHaveTextContent("The project's");
    expect(document.querySelector('[data-status-row="Approved"]')).toHaveTextContent("1");

    await userEvent.selectOptions(screen.getByLabelText("The project follows"), "Lean");
    const dialog = await screen.findByRole("dialog", { name: "Follow Lean" });
    // Draft is in Lean too; Approved holds a requirement Lean has no place for, and it goes to Lean's start.
    expect(within(dialog).queryByLabelText("Requirements in Draft go to")).toBeNull();
    await userEvent.selectOptions(within(dialog).getByLabelText("Requirements in Approved go to"), "Rejected");
    await userEvent.click(within(dialog).getByRole("button", { name: "Follow this workflow" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ workflowId: lean.id, statusMapping: { [approved.id]: rejected.id } });
  });

  it("says what the server refused when a mapping leaves a status out", async () => {
    stubApi({
      ...shaped,
      "PUT /projects/CHK/workflow": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Some fields need attention.",
            fields: { statusMapping: "Requirements are in Approved, which this workflow lacks." },
          },
        },
      },
    });
    await renderAt("/p/CHK/settings?tab=workflow");
    await userEvent.selectOptions(await screen.findByLabelText("Workflow of Functional"), "Lean");
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Follow this workflow" }));
    expect(await screen.findByText("Requirements are in Approved, which this workflow lacks.")).toBeInTheDocument();
  });

  it("shows a reader the workflow and nothing to change", async () => {
    stubApi({ ...shaped, "GET /projects/CHK": { status: 200, body: { project: aProject({ can: { ...project.can, administer: false } }) } } });
    await renderAt("/p/CHK/settings?tab=workflow");
    expect(await screen.findByRole("figure", { name: "Workflow Default" })).toBeInTheDocument();
    expect(screen.getByLabelText("The project follows")).toBeDisabled();
    expect(screen.queryByRole("button", { name: "New workflow" })).toBeNull();
    expect(screen.queryByRole("button", { name: "New status" })).toBeNull();
  });
});

describe("the workflow designer", () => {
  it("builds a workflow from the inspector alone and sends the picture with it", async () => {
    const sent = stubApi({ ...shaped, "POST /projects/CHK/workflows": { status: 201, body: { workflow: lean } } });
    const router = await renderAt("/p/CHK/settings/workflows/new");
    await userEvent.type(await screen.findByLabelText("Name"), "Lean");

    // The first status placed is where requirements start, and a status on the canvas is no longer on offer.
    await userEvent.selectOptions(screen.getByLabelText("Add a status"), "Draft");
    await userEvent.click(screen.getByRole("button", { name: "Add status" }));
    expect(node("Draft").querySelector("[data-workflow-initial]")).not.toBeNull();
    expect(within(screen.getByLabelText("Add a status")).queryByRole("option", { name: "Draft" })).toBeNull();
    await userEvent.selectOptions(screen.getByLabelText("Add a status"), "Rejected");
    await userEvent.click(screen.getByRole("button", { name: "Add status" }));

    // A click, not a press: d3-drag wants a window jsdom's mousedown does not carry.
    fireEvent.click(screen.getByRole("button", { name: "Draft, where new requirements start" }));
    await userEvent.selectOptions(screen.getByLabelText("Add a transition to"), "Rejected");
    await userEvent.click(screen.getByRole("button", { name: "Add transition" }));
    const name = screen.getByLabelText("Transition name") as HTMLInputElement;
    expect(name.value).toBe("Rejected");
    await userEvent.clear(name);
    await userEvent.type(name, "Reject");

    // A rule with a setting waits for it before it can be added.
    await userEvent.selectOptions(screen.getByLabelText("Add a rule"), "condition.project_role");
    expect(screen.getByRole("button", { name: "Add rule" })).toBeDisabled();
    await userEvent.click(screen.getByRole("checkbox", { name: "Reviewer" }));
    await userEvent.click(screen.getByRole("button", { name: "Add rule" }));
    await userEvent.selectOptions(screen.getByLabelText("Add a rule"), "validator.comment_required");
    await userEvent.click(screen.getByRole("button", { name: "Add rule" }));
    expect(screen.getByText("Project role (Roles: Reviewer)")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Create workflow" }));
    await arrival(router, "/p/CHK/settings");
    const input = sent.find((r) => r.method === "POST")?.body as { name: string; steps: unknown[]; transitions: unknown[] };
    expect(input.name).toBe("Lean");
    expect(input.steps).toEqual([
      { statusId: draft.id, isInitial: true, layout: expect.objectContaining({ x: expect.any(Number) }) },
      { statusId: rejected.id, isInitial: false, layout: expect.objectContaining({ y: expect.any(Number) }) },
    ]);
    expect(input.transitions).toEqual([
      {
        name: "Reject",
        description: "",
        fromStatusId: draft.id,
        toStatusId: rejected.id,
        rules: [
          { kind: "condition", type: "condition.project_role", config: { roles: ["reviewer"] } },
          { kind: "validator", type: "validator.comment_required", config: {} },
        ],
      },
    ]);
    expect(router.state.location.search).toEqual({ tab: "workflow" });
  });

  it("opens a saved workflow and keeps its transitions' ids and revision steps on save", async () => {
    const sent = stubApi({
      ...shaped,
      [`GET /projects/CHK/workflows/${flow.id}`]: { status: 200, body: { workflow: flow } },
      [`PUT /projects/CHK/workflows/${flow.id}`]: { status: 200, body: { workflow: flow } },
    });
    const router = await renderAt(`/p/CHK/settings/workflows/${flow.id}`);
    await waitFor(() => expect(screen.getByLabelText("Name")).toHaveValue("Default"));
    expect(node("Draft").dataset.x).toBe("40");
    expect(Number(node("In review").dataset.x)).toBeGreaterThan(40);

    fireEvent.click(screen.getByRole("button", { name: "Transition Reject" }));
    const panel = document.querySelector("[data-edge-panel]") as HTMLElement;
    await userEvent.click(within(panel).getByRole("button", { name: "Remove rule Comment required" }));
    await userEvent.click(screen.getByRole("button", { name: "Save workflow" }));
    await arrival(router, "/p/CHK/settings");
    const input = sent.find((r) => r.method === "PUT")?.body as { steps: { onRevisionStatusId?: string }[]; transitions: { id: string; rules: unknown[] }[] };
    expect(input.transitions.map((each) => each.id)).toEqual(flow.transitions.map((each) => each.id));
    expect(input.transitions[2]?.rules).toEqual([]);
    expect(input.steps[2]?.onRevisionStatusId).toBe(draft.id);
  });

  it("says what would stop a save before the server has to", async () => {
    stubApi(shaped);
    await renderAt("/p/CHK/settings/workflows/new");
    expect(await screen.findByText(/The workflow needs a name/)).toBeInTheDocument();
    expect(screen.getByText(/Put at least one status on the canvas/)).toBeInTheDocument();
  });
});

describe("a requirement's status", () => {
  const submit = { id: flow.transitions[0]!.id, name: "Submit for review", needsComment: false, toStatus: review };
  const reject = { id: flow.transitions[2]!.id, name: "Reject", needsComment: true, toStatus: rejected };
  const inDraft = aRequirement({ status: { id: draft.id, name: "Draft", category: "draft" } });
  const base = {
    "GET /projects/CHK": { status: 200, body: { project } },
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement: inDraft } },
    "GET /requirements/CHK-R1/transitions": { status: 200, body: { transitions: [submit, reject] } },
  };

  it("offers the transitions the reader may take, and takes one", async () => {
    const sent = stubApi({
      ...base,
      "POST /requirements/CHK-R1/transitions": {
        status: 200,
        body: { requirement: aRequirement({ status: { id: review.id, name: "In review", category: "review" } }) },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    const bar = await waitFor(() => {
      const found = document.querySelector<HTMLElement>("[data-transition-bar]");
      expect(found).not.toBeNull();
      return found!;
    });
    expect(within(bar).getByText("Draft")).toBeInTheDocument();
    await userEvent.click(await within(bar).findByRole("button", { name: "Submit for review" }));
    expect(await screen.findByText("Moved to In review.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ transitionId: submit.id });
  });

  it("asks why when a transition wants a comment, and says what refused it", async () => {
    const sent = stubApi({
      ...base,
      "POST /requirements/CHK-R1/transitions": {
        status: 409,
        body: { error: { code: "transition_unavailable", message: "CHK-R1 moved to In review meanwhile. Pick one of the transitions it offers now." } },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(await screen.findByRole("button", { name: "Reject" }));
    const dialog = await screen.findByRole("dialog", { name: "Reject" });
    expect(within(dialog).getByRole("button", { name: "Reject" })).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText("Why"), "Out of scope.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Reject" }));
    expect(await within(dialog).findByText(/moved to In review meanwhile/)).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ transitionId: reject.id, comment: "Out of scope." });
  });

  it("shows the status of a requirement in the trash, and offers nothing", async () => {
    stubApi({ ...base, "GET /requirements/CHK-R1": { status: 200, body: { requirement: { ...inDraft, trashedAt: "2026-10-02T08:00:00Z" } } } });
    await renderAt("/p/CHK/r/CHK-R1");
    const bar = await waitFor(() => {
      const found = document.querySelector<HTMLElement>("[data-transition-bar]");
      expect(found).not.toBeNull();
      return found!;
    });
    expect(within(bar).getByText("Draft")).toBeInTheDocument();
    expect(within(bar).queryByRole("button")).toBeNull();
  });
});
