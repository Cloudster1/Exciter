import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { TreeNode } from "@/api/tree";
import { renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { dropPlace, dropZone } from "./RequirementTree";

afterEach(() => vi.unstubAllGlobals());

const project = aProject();
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
const pay = node({ id: "p", key: "CHK-R2", title: "Pay", parentId: "s", depth: 1, outline: "1.1" });
const ship = node({ id: "h", key: "CHK-R3", title: "Ship", parentId: "s", depth: 1, outline: "1.2" });
const nodes = [scope, pay, ship];
const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
const entry = (n: TreeNode, text: string) => ({
  node: n,
  statement: paragraph(text),
  rationale: null,
  attributes: {},
  labels: [],
  can: { edit: true, transition: true, delete: true, link: true, comment: true },
  comments: { threads: 0, open: 0 },
  suspectLinks: 0,
});

describe("a drop in the tree", () => {
  it("lands beside a row at its edges and below it in its middle", () => {
    expect(dropZone(1, 32)).toBe("before");
    expect(dropZone(16, 32)).toBe("inside");
    expect(dropZone(31, 32)).toBe("after");
  });

  it("asks for the place the zone means", () => {
    expect(dropPlace(nodes, ship, "before")).toEqual({ parentId: "s", afterId: "p" });
    expect(dropPlace(nodes, pay, "before")).toEqual({ parentId: "s", afterId: null });
    expect(dropPlace(nodes, pay, "after")).toEqual({ parentId: "s", afterId: "p" });
    expect(dropPlace(nodes, scope, "inside")).toEqual({ parentId: "s", afterId: "h" });
  });
});

describe("the specification", () => {
  it("shows the tree beside the numbered document", async () => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project } },
      "GET /projects/CHK/tree": { status: 200, body: { nodes } },
      "GET /projects/CHK/spec": {
        status: 200,
        body: { entries: [entry(scope, "What is in scope."), entry(pay, "Pay at once.")], total: 3, limit: 50, offset: 0 },
      },
    });
    await renderAt("/p/CHK/spec");
    const tree = await screen.findByRole("tree");
    expect(within(tree).getByRole("treeitem", { name: "1.1 Pay" })).toHaveAttribute("aria-level", "2");
    const document = screen.getByRole("article", { name: "Specification" });
    expect(within(document).getByText("Pay at once.")).toBeInTheDocument();
    expect(within(document).getByRole("heading", { name: "1.1 Pay" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Show more" })).toBeInTheDocument();
  });
});

describe("moving and copying from a requirement's page", () => {
  it("moves below another requirement, last among its children", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project } },
      "GET /projects": { status: 200, body: { projects: [project] } },
      "GET /projects/CHK/tree": { status: 200, body: { nodes } },
      "GET /requirements/CHK-R3": { status: 200, body: { requirement: aRequirement({ id: "h", key: "CHK-R3", title: "Ship" }) } },
      "POST /requirements/CHK-R3/move": { status: 200, body: { requirement: aRequirement({ id: "h", key: "CHK-R3", title: "Ship", outline: "1.1.1" }) } },
    });
    await renderAt("/p/CHK/r/CHK-R3");
    await userEvent.click(await screen.findByRole("button", { name: "Move" }));
    const dialog = screen.getByRole("dialog", { name: "Move CHK-R3" });
    // Itself and what is below it are no place for it.
    await waitFor(() => expect(within(dialog).getByRole("option", { name: /1\.1 Pay/ })).toBeInTheDocument());
    expect(within(dialog).queryByRole("option", { name: /1\.2 Ship/ })).toBeNull();
    await userEvent.selectOptions(within(dialog).getByLabelText("Below"), "p");
    await userEvent.click(within(dialog).getByRole("button", { name: "Move" }));
    await waitFor(() => expect(sent.find((r) => r.method === "POST")?.body).toEqual({ place: { parentId: "p", afterId: null } }));
  });

  it("copies with what is below it into another project", async () => {
    const other = aProject({ id: "0195f000-0000-7000-8000-0000000000f9", key: "SYS", name: "System" });
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project } },
      "GET /projects/SYS": { status: 200, body: { project: other } },
      "GET /projects": { status: 200, body: { projects: [project, other] } },
      "GET /projects/CHK/tree": { status: 200, body: { nodes } },
      "GET /projects/SYS/tree": { status: 200, body: { nodes: [] } },
      "GET /requirements/CHK-R1": { status: 200, body: { requirement: aRequirement({ id: "s", key: "CHK-R1", kind: "section", title: "Scope" }) } },
      "POST /requirements/CHK-R1/copy": { status: 201, body: { requirement: aRequirement({ key: "SYS-R1", projectKey: "SYS" }), copied: 3 } },
      "GET /requirements/SYS-R1": { status: 200, body: { requirement: aRequirement({ key: "SYS-R1", projectKey: "SYS" }) } },
    });
    const router = await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(await screen.findByRole("button", { name: "Copy" }));
    const dialog = screen.getByRole("dialog", { name: "Copy CHK-R1" });
    await userEvent.selectOptions(within(dialog).getByLabelText("Project"), "SYS");
    await userEvent.click(within(dialog).getByRole("button", { name: "Copy" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST")?.body).toEqual({
        projectKey: "SYS",
        place: { parentId: null, afterId: null },
        children: true,
        links: false,
      }),
    );
    await waitFor(() => expect(router.state.location.pathname).toBe("/p/SYS/r/SYS-R1"));
  });
});
