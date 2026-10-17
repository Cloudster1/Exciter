import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SpecEntry, TreeNode } from "@/api/tree";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { addedPlace, editableOf, lastBelow, stepTo } from "./SpecPage";

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
const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
const can = { edit: true, transition: true, delete: true, link: true, comment: true };
const entry = (n: TreeNode, text: string, over: Partial<SpecEntry> = {}): SpecEntry => ({
  node: n,
  statement: paragraph(text),
  rationale: null,
  attributes: {},
  labels: [],
  can,
  comments: { threads: 0, open: 0 },
  suspectLinks: 0,
  reuse: null,
  type: null,
  owner: null,
  priority: null,
  approval: { pendingReviewId: null, approvedRevision: null, current: false },
  ...over,
});

function stubDocument(entries: SpecEntry[], answers: Parameters<typeof stubApi>[0] = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project } },
    "GET /projects/CHK/tree": { status: 200, body: { nodes: [scope, pay, ship] } },
    "GET /projects/CHK/spec": { status: 200, body: { entries, total: entries.length, limit: 50, offset: 0 } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
    ...answers,
  });
}

const entryOf = (key: string) => screen.getByRole("article", { name: "Specification" }).querySelector<HTMLElement>(`[data-spec-entry="${key}"]`)!;

describe("the document's pieces", () => {
  it("adds after an entry, or first below it", () => {
    expect(addedPlace(pay, "after")).toEqual({ parentId: "s", afterId: "p" });
    expect(addedPlace(scope, "below")).toEqual({ parentId: "s", afterId: null });
  });

  it("puts a requirement added after an entry below everything under it", () => {
    const entries = [entry(scope, ""), entry(pay, ""), entry(ship, ""), entry(node({ id: "o", key: "CHK-R4", title: "Other", outline: "2" }), "")];
    expect(lastBelow(entries, "CHK-R1")).toBe("CHK-R3");
    expect(lastBelow(entries, "CHK-R2")).toBe("CHK-R2");
    expect(lastBelow(entries, "CHK-R4")).toBe("CHK-R4");
  });

  it("edits an entry from what it holds", () => {
    const type = { id: "t", name: "Functional", icon: "test", colour: "#22aa55" };
    expect(editableOf(entry(node({ ...pay, revision: 4 }), "Pay at once.", { type }))).toMatchObject({
      key: "CHK-R2",
      kind: "requirement",
      title: "Pay",
      revision: 4,
      type,
    });
  });

  it("steps between entries and stops at either end", () => {
    const keys = ["A", "B", "C"];
    expect(stepTo(keys, "A", "ArrowDown")).toBe("B");
    expect(stepTo(keys, "C", "ArrowDown")).toBe("C");
    expect(stepTo(keys, "A", "ArrowUp")).toBe("A");
    expect(stepTo(keys, "B", "End")).toBe("C");
    expect(stepTo(keys, "B", "Home")).toBe("A");
    expect(stepTo(keys, "B", "x")).toBeUndefined();
  });
});

describe("editing in the document", () => {
  it("turns the entry into its editor and saves over the revision it was read at", async () => {
    const sent = stubDocument([entry(scope, "What is in scope."), entry(node({ ...pay, revision: 3 }), "Pay at once.")], {
      "PATCH /requirements/CHK-R2": { status: 200, body: { requirement: aRequirement({ key: "CHK-R2", title: "Pay by card", revision: 4 }) } },
    });
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    await userEvent.click(within(entryOf("CHK-R2")).getByRole("button", { name: "Edit" }));
    const title = within(entryOf("CHK-R2")).getByLabelText("Title");
    expect(title).toHaveFocus();
    expect(title).toHaveValue("Pay");
    await userEvent.clear(title);
    await userEvent.type(title, "Pay by card");
    await userEvent.click(within(entryOf("CHK-R2")).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.find((r) => r.method === "PATCH")?.body).toMatchObject({ title: "Pay by card", baseRevision: 3 }));
    await waitFor(() => expect(document.body.querySelector("[data-spec-announced]")).toHaveTextContent("CHK-R2: Saved as revision 4."));
    expect(within(entryOf("CHK-R2")).queryByLabelText("Title")).toBeNull();
  });

  it("shows what a newer revision changed and saves over it", async () => {
    let revision = 1;
    let saves = 0;
    const sent = stubDocument([], {
      "GET /projects/CHK/spec": () => ({ status: 200, body: { entries: [entry(node({ ...pay, revision }), "Pay at once.")], total: 1, limit: 50, offset: 0 } }),
      "PATCH /requirements/CHK-R2": () => {
        saves++;
        if (saves === 1) {
          revision = 2;
          return { status: 409, body: { error: { code: "revision_conflict", message: "Somebody saved a newer revision of CHK-R2." } } };
        }
        return { status: 200, body: { requirement: aRequirement({ key: "CHK-R2", revision: 3 }) } };
      },
      "GET /requirements/CHK-R2/diff": {
        status: 200,
        body: {
          comparison: {
            from: { number: 1, title: "Pay", type: null, authorName: "Alice", createdAt: "2026-10-01T10:00:00Z" },
            to: { number: 2, title: "Pay in cash", type: null, authorName: "Bob", createdAt: "2026-10-02T10:00:00Z" },
            statement: [],
            rationale: [],
            attributes: [],
          },
        },
      },
    });
    await renderAt("/p/CHK/spec");
    await userEvent.click(await screen.findByRole("button", { name: "Edit" }));
    await userEvent.type(screen.getByLabelText("Title"), " now");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    const newer = await screen.findByRole("region", { name: "What revision 2 changed since you opened it" });
    expect(await within(newer).findByText("Pay in cash")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "GET" && r.path === "/requirements/CHK-R2/diff")).toBe(true);
    expect(screen.getByLabelText("Title")).toHaveValue("Pay now");
    await userEvent.click(screen.getByRole("button", { name: "Save over revision 2" }));
    await waitFor(() => expect(sent.filter((r) => r.method === "PATCH").map((r) => (r.body as { baseRevision: number }).baseRevision)).toEqual([1, 2]));
  });

  it("asks before it lets go of unsaved changes, and opens one entry at a time", async () => {
    stubDocument([entry(pay, "Pay at once."), entry(ship, "Ship in a day.")]);
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    await userEvent.click(within(entryOf("CHK-R2")).getByRole("button", { name: "Edit" }));
    await userEvent.type(within(entryOf("CHK-R2")).getByLabelText("Title"), " later");
    await userEvent.click(within(entryOf("CHK-R3")).getByRole("button", { name: "Edit" }));
    const dialog = screen.getByRole("dialog", { name: "Discard your changes?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep editing" }));
    expect(within(entryOf("CHK-R2")).getByLabelText("Title")).toHaveValue("Pay later");
    expect(within(entryOf("CHK-R3")).queryByLabelText("Title")).toBeNull();

    await userEvent.click(within(entryOf("CHK-R3")).getByRole("button", { name: "Edit" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Discard" }));
    expect(within(entryOf("CHK-R2")).queryByLabelText("Title")).toBeNull();
    expect(within(entryOf("CHK-R3")).getByLabelText("Title")).toHaveValue("Ship");
  });

  it("opens a draft left unchanged without asking", async () => {
    stubDocument([entry(pay, "Pay at once."), entry(ship, "Ship in a day.")]);
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    await userEvent.click(within(entryOf("CHK-R2")).getByRole("button", { name: "Edit" }));
    await userEvent.click(within(entryOf("CHK-R3")).getByRole("button", { name: "Edit" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(within(entryOf("CHK-R3")).getByLabelText("Title")).toBeInTheDocument();
  });

  it("adds a requirement after an entry, written where it will stand", async () => {
    const sent = stubDocument([entry(scope, "What is in scope."), entry(pay, "Pay at once.")], {
      "POST /projects/CHK/requirements": { status: 201, body: { requirement: aRequirement({ key: "CHK-R4", title: "Refund" }) } },
    });
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    await userEvent.click(within(entryOf("CHK-R2")).getByRole("button", { name: "Add next to CHK-R2" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "After this" }));
    const adding = document.body.querySelector<HTMLElement>("[data-spec-adding]")!;
    expect(within(adding).getByText("New, after CHK-R2")).toBeInTheDocument();
    await userEvent.type(within(adding).getByLabelText("Title"), "Refund");
    await userEvent.click(within(adding).getByRole("button", { name: "New requirement" }));
    await waitFor(() => expect(sent.find((r) => r.method === "POST")?.body).toMatchObject({ title: "Refund", place: { parentId: "s", afterId: "p" } }));
    await waitFor(() => expect(document.body.querySelector("[data-spec-announced]")).toHaveTextContent("CHK-R4 added."));
  });

  it("writes a requirement added after a section below the section's last entry", async () => {
    stubDocument([entry(scope, "What is in scope."), entry(pay, "Pay at once."), entry(ship, "Ship in a day.")]);
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    await userEvent.click(within(entryOf("CHK-R1")).getByRole("button", { name: "Add next to CHK-R1" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "After this" }));
    const adding = document.body.querySelector<HTMLElement>("[data-spec-adding]")!;
    expect(adding.previousElementSibling).toBe(entryOf("CHK-R3"));
  });

  it("moves between entries and edits from the keyboard", async () => {
    stubDocument([entry(scope, "What is in scope."), entry(pay, "Pay at once.")]);
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    entryOf("CHK-R1").focus();
    await userEvent.keyboard("{ArrowDown}");
    await waitFor(() => expect(entryOf("CHK-R2")).toHaveFocus());
    await userEvent.keyboard("{Enter}");
    expect(within(entryOf("CHK-R2")).getByLabelText("Title")).toHaveFocus();
    await userEvent.keyboard("{Escape}");
    expect(within(entryOf("CHK-R2")).queryByLabelText("Title")).toBeNull();
    await waitFor(() => expect(entryOf("CHK-R2")).toHaveFocus());
    expect(document.body.querySelector("[data-spec-announced]")).toHaveTextContent("Left CHK-R2 as it was.");
    expect(await axeViolations()).toEqual([]);
  });

  it("offers no editing to a reader, nor in an archived project", async () => {
    stubDocument([entry(pay, "Pay at once.", { can: { ...can, edit: false } })], {
      "GET /projects/CHK": { status: 200, body: { project: aProject({ archivedAt: "2026-10-01T10:00:00Z" }) } },
    });
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Add next to/ })).toBeNull();
    expect(screen.getByText("Arrow keys move between entries.")).toBeInTheDocument();
  });
});
