import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Project } from "@/api/projects";
import { renderAt } from "@/test/app";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { entry, node, pay, scope, ship, stubDocument } from "@/test/spec";

afterEach(() => vi.unstubAllGlobals());

// Scope holds Pay and Ship; Later stands at the top after it.
const later = node({ id: "l", key: "CHK-R4", title: "Later", outline: "2" });
const nodes = [scope, pay, ship, later];

function stubTree(project: Project = aProject()) {
  return stubDocument([entry(scope, "What is in scope."), entry(pay, "Pay at once."), entry(ship, "Ship in a day."), entry(later, "Not now.")], {
    "GET /projects/CHK": { status: 200, body: { project } },
    "GET /projects": { status: 200, body: { projects: [project] } },
    "GET /projects/CHK/tree": { status: 200, body: { nodes } },
    "POST /requirements/CHK-R3/move": { status: 200, body: { requirement: aRequirement({ id: "h", key: "CHK-R3", title: "Ship", outline: "2.1" }) } },
  });
}

const item = (name: string) => within(screen.getByRole("tree")).getByRole("treeitem", { name });
const row = (name: string) => item(name).querySelector<HTMLElement>("[data-tree-row]")!;
// jsdom has no DataTransfer; the tree only writes the key into it.
const transfer = () => ({ dataTransfer: { setData: vi.fn(), effectAllowed: "", dropEffect: "" } });

describe("the tree from the keyboard", () => {
  it("walks, folds, opens and follows a requirement", async () => {
    stubTree();
    const router = await renderAt("/projects/CHK/spec");
    await screen.findByRole("tree");
    item("1 Scope").focus();
    await userEvent.keyboard("{ArrowDown}");
    expect(item("1.1 Pay")).toHaveFocus();
    await userEvent.keyboard("{End}");
    expect(item("2 Later")).toHaveFocus();
    await userEvent.keyboard("{Home}");
    expect(item("1 Scope")).toHaveFocus();

    await userEvent.keyboard("{ArrowLeft}");
    expect(item("1 Scope")).toHaveAttribute("aria-expanded", "false");
    expect(within(screen.getByRole("tree")).queryByRole("treeitem", { name: "1.1 Pay" })).toBeNull();
    await userEvent.keyboard("{ArrowDown}");
    expect(item("2 Later")).toHaveFocus();
    await userEvent.keyboard("{Home}{ArrowRight}");
    expect(item("1 Scope")).toHaveAttribute("aria-expanded", "true");
    expect(item("1 Scope")).toHaveFocus();
    await userEvent.keyboard("{ArrowRight}");
    expect(item("1.1 Pay")).toHaveFocus();
    await userEvent.keyboard("{ArrowLeft}");
    expect(item("1 Scope")).toHaveFocus();

    await userEvent.keyboard("{ArrowDown}{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe("/projects/CHK/r/CHK-R2"));
  });

  it("moves with M where the project is open to its reader's edits", async () => {
    stubTree();
    await renderAt("/projects/CHK/spec");
    await screen.findByRole("tree");
    expect(item("1.2 Ship")).toHaveAttribute("aria-keyshortcuts", "M");
    expect(item("1.2 Ship")).toHaveAttribute("draggable", "true");
    item("1.2 Ship").focus();
    await userEvent.keyboard("m");
    expect(await screen.findByRole("dialog", { name: "Move CHK-R3" })).toBeInTheDocument();
  });

  it.each([
    ["a reader", aProject({ can: { view: true, comment: true, edit: false, approve: false, administer: false, purge: false }, roles: ["viewer"] })],
    ["an archived project", aProject({ archivedAt: "2026-10-20T08:00:00Z", archivedByName: "Ada Lovelace" })],
  ])("offers no move to %s", async (_, project) => {
    stubTree(project);
    await renderAt("/projects/CHK/spec");
    await screen.findByRole("tree");
    for (const name of ["1 Scope", "1.1 Pay", "1.2 Ship", "2 Later"]) {
      expect(item(name)).toHaveAttribute("draggable", "false");
      expect(item(name)).not.toHaveAttribute("aria-keyshortcuts");
    }
    item("1.2 Ship").focus();
    await userEvent.keyboard("m");
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("dragging in the tree", () => {
  it("takes no drop below the dragged requirement itself, and one elsewhere", async () => {
    const sent = stubTree();
    await renderAt("/projects/CHK/spec");
    await screen.findByRole("tree");
    fireEvent.dragStart(item("1 Scope"), transfer());
    // An event the tree accepts is one it prevents the default of, which fireEvent answers false.
    expect(fireEvent.dragOver(row("1.1 Pay"), transfer())).toBe(true);
    expect(row("1.1 Pay")).not.toHaveAttribute("data-drop");
    fireEvent.drop(row("1.1 Pay"), transfer());
    fireEvent.dragOver(row("1 Scope"), transfer());
    fireEvent.drop(row("1 Scope"), transfer());
    fireEvent.dragEnd(item("1 Scope"));

    fireEvent.dragStart(item("1.2 Ship"), transfer());
    expect(fireEvent.dragOver(row("2 Later"), transfer())).toBe(false);
    expect(row("2 Later")).toHaveAttribute("data-drop", "inside");
    fireEvent.drop(row("2 Later"), transfer());
    await waitFor(() => expect(sent.filter((each) => each.method === "POST")).toHaveLength(1));
    expect(sent.find((each) => each.method === "POST")).toMatchObject({ path: "/requirements/CHK-R3/move", body: { place: { parentId: "l", afterId: null } } });
  });
});

describe("a move the server refuses for a status", () => {
  it("says why in the banner and keeps the dialog open", async () => {
    const other = aProject({ id: "0195f000-0000-7000-8000-0000000000f9", key: "SYS", name: "System" });
    const refusal = "System has no status called Draft. Map each to one of its statuses.";
    const sent = stubDocument([entry(scope, "What is in scope."), entry(ship, "Ship in a day.")], {
      "GET /projects": { status: 200, body: { projects: [aProject(), other] } },
      "GET /projects/CHK/tree": { status: 200, body: { nodes } },
      "GET /projects/SYS": { status: 200, body: { project: other } },
      "GET /projects/SYS/tree": { status: 200, body: { nodes: [] } },
      "GET /projects/SYS/statuses": { status: 200, body: { statuses: [] } },
      "POST /requirements/CHK-R3/move": {
        status: 422,
        body: { error: { code: "validation_failed", message: "Some fields need attention.", fields: { statusMapping: refusal } } },
      },
    });
    await renderAt("/projects/CHK/spec");
    await screen.findByRole("tree");
    item("1.2 Ship").focus();
    await userEvent.keyboard("m");
    const dialog = await screen.findByRole("dialog", { name: "Move CHK-R3" });
    await userEvent.selectOptions(within(dialog).getByLabelText("Project"), "SYS");
    await userEvent.click(within(dialog).getByRole("button", { name: "Move" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(refusal);
    expect(screen.getByRole("dialog", { name: "Move CHK-R3" })).toBe(dialog);
    expect(sent.filter((each) => each.method === "POST")).toHaveLength(1);
  });
});
