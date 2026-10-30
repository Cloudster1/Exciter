import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Attribute } from "@/api/attributes";
import type { RequirementRow } from "@/api/requirements";
import type { View } from "@/api/search";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { draft } from "@/test/workflows";
import { emptyFilter } from "./filter";

afterEach(() => vi.unstubAllGlobals());

const project = aProject();
const low = { id: "0195f000-0000-7000-8000-0000000000e1", name: "Low", retired: false };
const high = { id: "0195f000-0000-7000-8000-0000000000e2", name: "High", retired: false };
const risk: Attribute = {
  id: "0195f000-0000-7000-8000-0000000000d1",
  name: "Risk",
  description: "",
  kind: "select",
  required: false,
  options: [low, high],
  typeIds: [],
  position: 0,
  retired: false,
  requiredFrom: null,
  dependsOn: null,
  formula: null,
  onSections: false,
  editableBy: [],
  createdAt: "2026-10-01T08:00:00Z",
  updatedAt: "2026-10-01T08:00:00Z",
};
const pay = aRequirement({ revision: 2, status: { id: draft.id, name: "Draft", category: "draft" } });
const row: RequirementRow = { ...pay, outline: "1", trashed: false, attributes: { [risk.id]: low.id }, labels: ["checkout"], suspectLinks: 0, quality: null };
const team: View = {
  id: "0195f000-0000-7000-8000-000000000f01",
  projectKey: "CHK",
  name: "Team",
  ownerId: "0195f000-0000-7000-8000-00000000000b",
  ownerName: "Bob",
  shared: true,
  kind: "grid",
  filter: { ...emptyFilter(), labels: ["checkout"] },
  columns: ["key", "title", risk.id],
  settings: {},
  dropped: [],
  sort: { column: "title", desc: false },
  canEdit: false,
  createdAt: "2026-10-01T08:00:00Z",
  updatedAt: "2026-10-01T08:00:00Z",
};

function stubGrid(more: Parameters<typeof stubApi>[0] = {}) {
  const asked: URLSearchParams[] = [];
  const sent = stubApi({
    "GET /projects/CHK": { status: 200, body: { project } },
    "GET /projects/CHK/requirements": (request) => {
      asked.push(new URL(request.url).searchParams);
      return { status: 200, body: { rows: [row], total: 1, limit: 100, offset: 0 } };
    },
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [risk] } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/statuses": { status: 200, body: { statuses: [draft] } },
    "GET /projects/CHK/views": { status: 200, body: { views: [team] } },
    ...more,
  });
  return { asked, sent };
}

describe("the grid", () => {
  it("orders by a heading, and keeps the order in the address", async () => {
    const { asked } = stubGrid();
    const router = await renderAt("/projects/CHK/grid");
    const table = await screen.findByRole("table", { name: "Requirements of the project, as the grid shows them" });
    expect(within(table).getByRole("link", { name: "Pay the basket" })).toHaveAttribute("href", "/projects/CHK/r/CHK-R1");
    expect(screen.getByText("1 requirement")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(table).getByRole("button", { name: "Sort by Title" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ sort: "title" }));
    await waitFor(() => expect(asked.at(-1)?.get("sort")).toBe("title"));
    await userEvent.click(within(table).getByRole("button", { name: /Sort by Title, sorted A to Z/ }));
    await waitFor(() => expect(router.state.location.search).toEqual({ sort: "title", desc: true }));
    expect(within(table).getByRole("columnheader", { name: /Title/ })).toHaveAttribute("aria-sort", "descending");
  });

  it("keeps only requirements with a suspect link, and marks how many each has", async () => {
    const asked: URLSearchParams[] = [];
    stubGrid({
      "GET /projects/CHK/requirements": (request) => {
        asked.push(new URL(request.url).searchParams);
        return { status: 200, body: { rows: [{ ...row, suspectLinks: 2 }], total: 1, limit: 100, offset: 0 } };
      },
    });
    const router = await renderAt("/projects/CHK/grid");
    const table = await screen.findByRole("table", { name: "Requirements of the project, as the grid shows them" });
    expect(await within(table).findByText("2 suspect links")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Suspect links only" }));
    await waitFor(() => expect(asked.at(-1)?.get("suspect")).toBe("true"));
    expect(router.state.location.search).toMatchObject({ suspect: true });
    expect(screen.getByRole("button", { name: "Suspect links only" })).toHaveAttribute("aria-pressed", "true");
  });

  it("narrows by an attribute condition and a field, and clears both", async () => {
    const { asked } = stubGrid();
    const router = await renderAt("/projects/CHK/grid?sort=title");
    await screen.findByRole("table");
    await userEvent.selectOptions(screen.getByLabelText("Attribute"), risk.id);
    await userEvent.selectOptions(screen.getByLabelText("Comparison"), "not");
    await userEvent.selectOptions(screen.getByLabelText("Value"), high.id);
    await userEvent.click(screen.getByRole("button", { name: "Add condition" }));
    await waitFor(() => expect(asked.at(-1)?.getAll("attr")).toEqual([`${risk.id}:not:${high.id}`]));
    expect(screen.getByText("Risk is not High")).toBeInTheDocument();

    await userEvent.selectOptions(screen.getByLabelText("Priority"), "must");
    await waitFor(() => expect(asked.at(-1)?.getAll("priority")).toEqual(["must"]));
    await userEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ sort: "title" }));
  });

  it("shows the columns chosen and changes a cell in place over the revision it shows", async () => {
    const { sent } = stubGrid({
      "PATCH /requirements/CHK-R1": async (request) => ({ status: 200, body: { requirement: { ...pay, ...((await request.json()) as object) } } }),
    });
    const router = await renderAt("/projects/CHK/grid");
    await screen.findByRole("table");
    await userEvent.click(screen.getByRole("button", { name: "Columns" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "Risk" }));
    await waitFor(() =>
      expect(router.state.location.search).toEqual({ columns: ["outline", "key", "title", "status", "owner", "priority", "updated", risk.id] }),
    );

    await userEvent.selectOptions(await screen.findByLabelText("Risk of CHK-R1"), high.id);
    await waitFor(() => expect(sent.find((each) => each.method === "PATCH")?.body).toEqual({ attributes: { [risk.id]: high.id }, baseRevision: 2 }));
    expect(await screen.findByText("Saved CHK-R1.")).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByLabelText("Priority of CHK-R1"), "must");
    await waitFor(() => expect(sent.filter((each) => each.method === "PATCH").at(-1)?.body).toEqual({ priority: "must" }));
  });

  it("opens a shared view and saves the grid as a view of one's own", async () => {
    const { sent } = stubGrid({
      "POST /projects/CHK/views": async (request) => ({
        status: 201,
        body: { view: { ...team, id: "0195f000-0000-7000-8000-000000000f02", canEdit: true, ...((await request.json()) as object) } },
      }),
    });
    const router = await renderAt("/projects/CHK/grid");
    await screen.findByRole("table");
    await userEvent.selectOptions(screen.getByLabelText("Views"), team.id);
    await waitFor(() =>
      expect(router.state.location.search).toEqual({ label: ["checkout"], sort: "title", columns: ["key", "title", risk.id], view: team.id }),
    );
    expect(screen.queryByRole("button", { name: "Save changes to this view" })).toBeNull();

    await userEvent.click(screen.getByRole("button", { name: "Save as a view" }));
    const dialog = screen.getByRole("dialog", { name: "Save as a view" });
    await userEvent.type(within(dialog).getByLabelText("Name"), "Mine");
    await userEvent.click(within(dialog).getByRole("checkbox", { name: "Share with everybody in the project" }));
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ view: "0195f000-0000-7000-8000-000000000f02" }));
    expect(sent.find((each) => each.method === "POST")?.body).toEqual({
      name: "Mine",
      shared: true,
      kind: "grid",
      filter: { ...emptyFilter(), labels: ["checkout"] },
      sort: { column: "title", desc: false },
      columns: ["key", "title", risk.id],
    });
    expect(await screen.findByText("Saved the view Mine.")).toBeInTheDocument();
  });
});
