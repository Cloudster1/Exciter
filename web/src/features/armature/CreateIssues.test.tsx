import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ArmatureAccount, ArmatureCreated } from "@/api/armature";
import type { RequirementRow } from "@/api/requirements";
import type { TreeNode } from "@/api/tree";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { byOutline } from "./CreateIssues";

afterEach(() => vi.unstubAllGlobals());

const BASE = "https://armature.example.com";
const project = aProject();
const account: ArmatureAccount = {
  configured: true,
  baseUrl: BASE,
  connected: true,
  status: "ok",
  user: { id: "0195f000-0000-7000-8000-0000000000b1", name: "Bob", email: "bob@acme.armature.test" },
  readOnly: false,
  checkedAt: "2026-10-01T09:00:00Z",
};
const bug = "0195f000-0000-7000-8000-0000000000c2";
const projects = {
  status: "ok",
  projects: [
    { key: "CP", name: "Core platform", canCreate: true },
    { key: "SEC", name: "Security", canCreate: false },
  ],
};
const issueTypes = {
  status: "ok",
  issueTypes: [
    { id: "0195f000-0000-7000-8000-0000000000c1", name: "Task", icon: "task" },
    { id: bug, name: "Bug", icon: "bug" },
  ],
};

const node = (over: Partial<TreeNode>): TreeNode => ({
  id: "x",
  key: "CHK-R1",
  kind: "requirement",
  title: "X",
  parentId: null,
  depth: 0,
  outline: "1",
  typeId: null,
  status: null,
  revision: 1,
  children: 0,
  ...over,
});
const scope = node({ id: "s", key: "CHK-R1", kind: "section", title: "Scope", children: 2 });
const pay = node({ id: "p", key: "CHK-R2", title: "Pay at once", parentId: "s", depth: 1, outline: "1.2" });
const ship = node({ id: "h", key: "CHK-R3", title: "Ship in a day", parentId: "s", depth: 1, outline: "1.10" });
const entry = (n: TreeNode, edit = true) => ({
  node: n,
  statement: { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: n.title }] }] },
  rationale: null,
  attributes: {},
  labels: [],
  can: { edit, transition: edit, delete: edit, link: edit, comment: true },
});

function made(requirementKey: string, key: string, summary: string): ArmatureCreated {
  return {
    requirementKey,
    issue: {
      key,
      summary,
      url: `${BASE}/issues/${key}`,
      type: "Bug",
      status: { name: "To do", category: "todo" },
      assigneeName: "",
      updatedAt: "2026-10-01T09:00:00Z",
    },
    link: {
      id: "0195f000-0000-7000-8000-0000000000a1",
      relation: "verifies",
      issueKey: key,
      statusCategory: "todo",
      statusStale: false,
      sync: "pending",
      error: "",
      brokenAt: null,
      detachedAt: null,
      createdByName: "Bob",
      createdAt: "2026-10-01T09:00:00Z",
    },
  };
}

function stubSpec(more: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}, editable = true) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: editable ? project : { ...project, can: { ...project.can, edit: false } } } },
    "GET /projects/CHK/tree": { status: 200, body: { nodes: [scope, pay, ship] } },
    "GET /projects/CHK/spec": { status: 200, body: { entries: [entry(scope), entry(pay), entry(ship)], total: 3, limit: 50, offset: 0 } },
    "GET /armature/account": { status: 200, body: { account } },
    "GET /armature/projects": { status: 200, body: projects },
    "GET /armature/issue-types": { status: 200, body: issueTypes },
    ...more,
  });
}

describe("picking requirements for Armature issues", () => {
  it("orders them as the outline reads", () => {
    const picked = [
      { key: "B", title: "", outline: "1.10" },
      { key: "A", title: "", outline: "1.2" },
      { key: "C", title: "", outline: "2" },
      { key: "D", title: "", outline: "1" },
    ];
    expect(picked.sort(byOutline).map((each) => each.key)).toEqual(["D", "A", "B", "C"]);
  });

  it("files the picked requirements in outline order and says what became of each", async () => {
    const sent = stubSpec({
      "POST /projects/CHK/armature-issues": {
        status: 201,
        body: {
          created: [made("CHK-R2", "CP-6", "CHK-R2 Pay at once")],
          failed: { requirementKey: "CHK-R3", code: "validation_failed", message: "Armature refuses this summary." },
        },
      },
    });
    await renderAt("/p/CHK/spec");
    const document = await screen.findByRole("article", { name: "Specification" });
    await within(document).findByRole("checkbox", { name: "Select CHK-R3" });
    expect(within(document).queryByRole("checkbox", { name: "Select CHK-R1" })).toBeNull();
    await userEvent.click(within(document).getByRole("checkbox", { name: "Select CHK-R3" }));
    await userEvent.click(within(document).getByRole("checkbox", { name: "Select CHK-R2" }));
    expect(screen.getByText("2 requirements selected")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Create Armature issues" }));

    const dialog = await screen.findByRole("dialog", { name: "Create 2 Armature issues" });
    const project = await within(dialog).findByLabelText("Armature project");
    expect(
      within(project)
        .getAllByRole("option")
        .map((each) => each.textContent),
    ).toEqual(["CP Core platform"]);
    await userEvent.selectOptions(within(dialog).getByLabelText("Issue type"), "Bug");
    await userEvent.selectOptions(within(dialog).getByLabelText("The new issues"), "verifies");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "Create 2 issues" }));

    await within(dialog).findByText("1 of 2 issues were made.");
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({
      requirementKeys: ["CHK-R2", "CHK-R3"],
      armatureProject: "CP",
      relation: "verifies",
      typeId: bug,
    });
    expect(within(dialog).getByRole("link", { name: "CP-6" })).toHaveAttribute("href", `${BASE}/issues/CP-6`);
    expect(dialog.querySelector('[data-requirement="CHK-R3"]')).toHaveTextContent("Armature refuses this summary.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Done" }));
    await waitFor(() => expect(screen.queryByText("2 requirements selected")).toBeNull());
  });

  it("leaves a requirement out, and shows a refusal of the whole round", async () => {
    const sent = stubSpec({
      "POST /projects/CHK/armature-issues": {
        status: 409,
        body: { error: { code: "armature_rejected", message: "Your Armature token may only read, so it cannot make issues or links." } },
      },
    });
    await renderAt("/p/CHK/spec");
    await userEvent.click(await screen.findByRole("checkbox", { name: "Select CHK-R2" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "Select CHK-R3" }));
    await userEvent.click(screen.getByRole("button", { name: "Create Armature issues" }));
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByLabelText("Armature project");
    await userEvent.click(within(dialog).getByRole("button", { name: "Leave out CHK-R3" }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Create 1 issue" }));
    await within(dialog).findByText("Your Armature token may only read, so it cannot make issues or links.");
    expect((sent.find((r) => r.method === "POST")?.body as { requirementKeys: string[] }).requirementKeys).toEqual(["CHK-R2"]);
  });

  it("says how to connect when the caller has no token", async () => {
    stubSpec({ "GET /armature/projects": { status: 200, body: { status: "not_connected", projects: [] } } });
    await renderAt("/p/CHK/spec");
    await userEvent.click(await screen.findByRole("checkbox", { name: "Select CHK-R2" }));
    await userEvent.click(screen.getByRole("button", { name: "Create Armature issues" }));
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByRole("link", { name: /Connect it under Your account/ })).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Create 1 issue" })).toBeDisabled();
  });

  it("offers nothing to pick to a reader, or without Armature", async () => {
    stubSpec({}, false);
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.2 Pay at once" });
    expect(screen.queryByRole("checkbox")).toBeNull();
    vi.unstubAllGlobals();
    stubSpec({ "GET /armature/account": { status: 200, body: { account: { ...account, configured: false, baseUrl: "", status: "not_configured" } } } });
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.2 Pay at once" });
    expect(screen.queryByRole("checkbox")).toBeNull();
  });

  it("picks every requirement a grid shows at once", async () => {
    const one: RequirementRow = { ...aRequirement({ key: "CHK-R2", title: "Pay at once" }), outline: "1", trashed: false, attributes: {}, labels: [] };
    const two: RequirementRow = {
      ...aRequirement({ key: "CHK-R4", title: "Refund", id: "0195f000-0000-7000-8000-0000000000f4" }),
      outline: "2",
      trashed: false,
      attributes: {},
      labels: [],
    };
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project } },
      "GET /projects/CHK/requirements": { status: 200, body: { rows: [one, two], total: 2, limit: 100, offset: 0 } },
      "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
      "GET /projects/CHK/statuses": { status: 200, body: { statuses: [] } },
      "GET /projects/CHK/views": { status: 200, body: { views: [] } },
      "GET /armature/account": { status: 200, body: { account } },
    });
    await renderAt("/p/CHK/grid");
    await userEvent.click(await screen.findByRole("checkbox", { name: "Select every requirement shown" }));
    expect(screen.getByRole("checkbox", { name: "Select CHK-R4" })).toBeChecked();
    expect(screen.getByText("2 requirements selected")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Clear the selection" }));
    expect(screen.getByRole("checkbox", { name: "Select CHK-R2" })).not.toBeChecked();
  });
});
