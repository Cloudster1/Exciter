import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Attribute, FieldRule } from "@/api/attributes";
import type { RequirementRow } from "@/api/requirements";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { approved, draft } from "@/test/workflows";

afterEach(() => vi.unstubAllGlobals());

const anAttribute = (over: Partial<Attribute>): Attribute => ({
  id: "x",
  name: "X",
  description: "",
  kind: "text",
  required: false,
  options: [],
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
  ...over,
});
const note = anAttribute({ id: "0195f000-0000-7000-8000-0000000000d1", name: "Note" });
const cost = anAttribute({ id: "0195f000-0000-7000-8000-0000000000d2", name: "Cost", kind: "number", position: 1 });
const legacy = anAttribute({ id: "0195f000-0000-7000-8000-0000000000d3", name: "Legacy", retired: true, position: 2 });
const elsewhere = anAttribute({ id: "0195f000-0000-7000-8000-0000000000d4", name: "Torque", typeIds: ["0195f000-0000-7000-8000-0000000000c9"], position: 3 });
const attributes = [note, cost, legacy, elsewhere];
const columns = ["key", "title", note.id, cost.id, legacy.id, elsewhere.id];

const rowOf = (over: Partial<RequirementRow> = {}): RequirementRow => ({
  ...aRequirement({ revision: 2, status: { id: draft.id, name: "Draft", category: "draft" } }),
  outline: "1",
  trashed: false,
  attributes: { [note.id]: "Card or cash", [cost.id]: 5, [legacy.id]: "Old words", [elsewhere.id]: "12 Nm" },
  labels: [],
  suspectLinks: 0,
  quality: null,
  ...over,
});

const FIXED = ["title", "statement", "rationale", "type", "owner", "priority"] as const;
const rules = (own: Partial<Record<FieldRule["field"], Partial<FieldRule>>> = {}): FieldRule[] =>
  FIXED.map((field) => ({ field, requiredFrom: null, editableBy: [], ...own[field] }));

function stubCells(row: RequirementRow, more: Parameters<typeof stubApi>[0] = {}, fieldRules = rules()) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject({ roles: ["editor"] }) } },
    "GET /projects/CHK/requirements": { status: 200, body: { rows: [row], total: 1, limit: 100, offset: 0 } },
    "GET /projects/CHK/attributes": { status: 200, body: { attributes } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/statuses": { status: 200, body: { statuses: [draft, approved] } },
    "GET /projects/CHK/views": { status: 200, body: { views: [] } },
    "GET /projects/CHK/field-rules": { status: 200, body: { rules: fieldRules } },
    ...more,
  });
}

const at = `/p/CHK/grid?columns=${encodeURIComponent(JSON.stringify(columns))}`;
const patches = (sent: { method: string; body: unknown }[]) => sent.filter((each) => each.method === "PATCH").map((each) => each.body);
const saved = async (request: Request): Promise<Answer> => ({
  status: 200,
  body: { requirement: { ...aRequirement(), ...((await request.json()) as object) } },
});

describe("a title in the grid", () => {
  it("saves the trimmed title over the revision it shows on Enter, and nothing on Escape or when it is unchanged or blank", async () => {
    const sent = stubCells(rowOf(), { "PATCH /requirements/CHK-R1": saved });
    await renderAt(at);
    const edit = await screen.findByRole("button", { name: "Title of CHK-R1" });

    await userEvent.click(edit);
    await userEvent.keyboard("{Control>}a{/Control}Pay later{Escape}");
    expect(screen.getByRole("link", { name: "Pay the basket" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Title of CHK-R1" }));
    await userEvent.keyboard("{Enter}");
    await userEvent.click(screen.getByRole("button", { name: "Title of CHK-R1" }));
    await userEvent.keyboard("{Control>}a{/Control}   {Enter}");
    expect(patches(sent)).toEqual([]);

    await userEvent.click(screen.getByRole("button", { name: "Title of CHK-R1" }));
    const box = screen.getByRole("textbox", { name: "Title of CHK-R1" });
    expect(box).toHaveFocus();
    await userEvent.clear(box);
    await userEvent.type(box, "  Pay the basket at once  {Enter}");
    await waitFor(() => expect(patches(sent)).toEqual([{ title: "Pay the basket at once", baseRevision: 2 }]));
    expect(await screen.findByText("Saved CHK-R1.")).toBeInTheDocument();
  });

  it("says why a save over a newer revision was refused", async () => {
    stubCells(rowOf(), {
      "PATCH /requirements/CHK-R1": {
        status: 409,
        body: { error: { code: "revision_conflict", message: "Somebody saved CHK-R1 since you opened it. Reload it, then make your change again." } },
      },
    });
    await renderAt(at);
    await userEvent.click(await screen.findByRole("button", { name: "Title of CHK-R1" }));
    await userEvent.keyboard("{Control>}a{/Control}Pay later{Enter}");
    expect(await screen.findByRole("alert")).toHaveTextContent("Somebody saved CHK-R1 since you opened it.");
    expect(screen.queryByText("Saved CHK-R1.")).toBeNull();
  });
});

describe("a value in the grid", () => {
  it("saves text and a number on Enter, trimmed, and nothing when it is unchanged", async () => {
    const sent = stubCells(rowOf(), { "PATCH /requirements/CHK-R1": saved });
    await renderAt(at);
    const text = await screen.findByRole("textbox", { name: "Note of CHK-R1" });
    await userEvent.click(text);
    await userEvent.keyboard("{Enter}");
    expect(patches(sent)).toEqual([]);
    await userEvent.clear(text);
    await userEvent.type(text, "  Card only  {Enter}");
    await waitFor(() => expect(patches(sent)).toEqual([{ attributes: { [note.id]: "Card only" }, baseRevision: 2 }]));

    const number = screen.getByRole("spinbutton", { name: "Cost of CHK-R1" });
    await userEvent.clear(number);
    await userEvent.type(number, "12.5{Enter}");
    await waitFor(() => expect(patches(sent).at(-1)).toEqual({ attributes: { [cost.id]: 12.5 }, baseRevision: 2 }));
    expect(await axeViolations()).toEqual([]);
  });

  it("shows plain text and offers no control where the reader may not change it", async () => {
    stubCells(rowOf(), {}, rules());
    await renderAt(at);
    const row = (await screen.findByRole("link", { name: "Pay the basket" })).closest("tr") as HTMLElement;
    // Retired, or not shown on the row's type: read as it is, never changed in place.
    expect(within(row).getByText("Old words")).toBeInTheDocument();
    expect(within(row).getByText("12 Nm")).toBeInTheDocument();
    expect(within(row).queryByRole("textbox", { name: "Legacy of CHK-R1" })).toBeNull();
    expect(within(row).queryByRole("textbox", { name: "Torque of CHK-R1" })).toBeNull();
    expect(within(row).getByRole("textbox", { name: "Note of CHK-R1" })).toBeInTheDocument();
  });

  it("offers nothing to change on a row the reader does not edit", async () => {
    stubCells(rowOf({ can: { edit: false, transition: false, delete: false, link: false, comment: true } }));
    await renderAt(at);
    const row = (await screen.findByRole("link", { name: "Pay the basket" })).closest("tr") as HTMLElement;
    expect(within(row).queryByRole("button", { name: "Title of CHK-R1" })).toBeNull();
    expect(within(row).queryAllByRole("textbox")).toEqual([]);
    expect(within(row).queryAllByRole("spinbutton")).toEqual([]);
    expect(within(row).getByText("Card or cash")).toBeInTheDocument();
    expect(within(row).getByText("5")).toBeInTheDocument();
  });

  it("offers nothing to change in a field a status rule closes to the reader", async () => {
    const adminsOnly = [{ roles: ["admin"], statuses: [] }];
    const closed: Attribute[] = [{ ...note, editableBy: adminsOnly }, cost, legacy, elsewhere];
    stubCells(rowOf(), { "GET /projects/CHK/attributes": { status: 200, body: { attributes: closed } } }, rules({ title: { editableBy: adminsOnly } }));
    await renderAt(at);
    const row = (await screen.findByRole("link", { name: "Pay the basket" })).closest("tr") as HTMLElement;
    await waitFor(() => expect(within(row).queryByRole("textbox", { name: "Note of CHK-R1" })).toBeNull());
    expect(within(row).getByText("Card or cash")).toBeInTheDocument();
    expect(within(row).queryByRole("button", { name: "Title of CHK-R1" })).toBeNull();
    expect(within(row).getByRole("spinbutton", { name: "Cost of CHK-R1" })).toBeInTheDocument();
  });
});
