import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Attribute, FieldRule } from "@/api/attributes";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { approved, draft, review } from "@/test/workflows";

// Field rules (#206): the administrators say when a field is required, which choices another allows and who changes
// it; the editor and the requirement's page show what the reader may not change, and why.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const car = { id: "0195f000-0000-7000-8000-0000000000e1", name: "Car", retired: false };
const bike = { id: "0195f000-0000-7000-8000-0000000000e2", name: "Bike", retired: false };
const disc = { id: "0195f000-0000-7000-8000-0000000000e3", name: "Disc", retired: false };
const rim = { id: "0195f000-0000-7000-8000-0000000000e4", name: "Rim", retired: false };
const anAttribute = (over: Partial<Attribute>): Attribute => ({
  id: "x",
  name: "X",
  description: "",
  kind: "select",
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
const vehicle = anAttribute({ id: "0195f000-0000-7000-8000-0000000000d1", name: "Vehicle", options: [car, bike] });
const brake = anAttribute({ id: "0195f000-0000-7000-8000-0000000000d2", name: "Brake", options: [disc, rim], position: 1 });
const FIXED = ["title", "statement", "rationale", "type", "owner", "priority"] as const;
/** The fixed fields' rules, none unless a field is given its own. */
const fixed = (own: Partial<Record<FieldRule["field"], Partial<FieldRule>>> = {}): FieldRule[] =>
  FIXED.map((field) => ({ field, requiredFrom: null, editableBy: [], ...own[field] }));

function stubSettings(more: Record<string, Answer> = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [vehicle, brake] } },
    "GET /attribute-kinds": { status: 200, body: { kinds: [{ kind: "select", title: "Select", description: "", hasOptions: true }] } },
    "GET /projects/CHK/statuses": { status: 200, body: { statuses: [draft, review, approved] } },
    "GET /projects/CHK/field-rules": { status: 200, body: { rules: fixed() } },
    ...more,
  });
}

describe("the field rules' settings", () => {
  it("make an attribute required from a status, depend on another, and changed only by some roles", async () => {
    const sent = stubSettings({ [`PATCH /projects/CHK/attributes/${brake.id}`]: { status: 200, body: { attribute: brake } } });
    await renderAt("/p/CHK/settings?tab=attributes");
    const row = (await screen.findByText("Brake")).closest("tr")!;
    await userEvent.click(within(row).getByRole("button", { name: "Edit" }));
    const form = document.querySelector<HTMLElement>("[data-attribute-form]")!;
    await userEvent.selectOptions(within(form).getByLabelText("Required from"), review.id);
    await userEvent.selectOptions(within(form).getByLabelText("Depends on"), vehicle.id);
    const forCar = form.querySelector<HTMLElement>('[data-choices-for="Car"]')!;
    await userEvent.click(within(forCar).getByLabelText("Disc"));
    await userEvent.click(within(form).getByRole("button", { name: "Add a line" }));
    const line = form.querySelector<HTMLElement>('[data-permit="1"]')!;
    await userEvent.click(within(line).getByLabelText("Admin"));
    await userEvent.click(within(line).getByLabelText(approved.name));
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(form).getByRole("button", { name: "Save attribute" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "PATCH")?.body).toMatchObject({
        requiredFrom: review.id,
        dependsOn: { attributeId: vehicle.id, choices: { [car.id]: [disc.id] } },
        editableBy: [{ roles: ["admin"], statuses: [approved.id] }],
      }),
    );
  });

  it("set a fixed field's rule, and keep a title and a statement required always", async () => {
    const sent = stubSettings({
      "PUT /projects/CHK/field-rules/owner": { status: 200, body: { rule: { field: "owner", requiredFrom: review.id, editableBy: [] } } },
    });
    await renderAt("/p/CHK/settings?tab=fields");
    const title = await screen.findByRole("form", { name: "Title" });
    expect(within(title).getByText("Always required")).toBeInTheDocument();
    const owner = screen.getByRole("form", { name: "Owner" });
    await userEvent.selectOptions(within(owner).getByLabelText("Required from"), review.id);
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(owner).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ requiredFrom: review.id, editableBy: [] }));
    expect(await within(owner).findByText("The rule of Owner is saved.")).toBeInTheDocument();
  });
});

describe("a requirement under field rules", () => {
  function stubRequirement(
    rules: FieldRule[],
    attributes: Attribute[],
    requirement = aRequirement({ status: { id: approved.id, name: "Approved", category: "approved" } }),
  ) {
    return stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject({ roles: ["editor"] }) } },
      "GET /requirements/CHK-R1": { status: 200, body: { requirement } },
      "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
      "GET /requirements/CHK-R1/comments": { status: 200, body: { threads: [] } },
      "GET /requirements/CHK-R1/reviews": { status: 200, body: { reviews: [], total: 0, limit: 20, offset: 0 } },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
      "GET /projects/CHK/attributes": { status: 200, body: { attributes } },
      "GET /projects/CHK/field-rules": { status: 200, body: { rules } },
    });
  }

  it("shows a field the reader's roles may not change in its status as it is, with who does", async () => {
    const adminsOnly = [{ roles: ["admin"], statuses: [] }];
    stubRequirement(fixed({ priority: { editableBy: adminsOnly } }), [anAttribute({ ...vehicle, editableBy: adminsOnly })]);
    await renderAt("/p/CHK/r/CHK-R1");
    const facts = (await screen.findByText("Only admins change the priority in Approved.")).closest("[data-requirement-facts]") as HTMLElement;
    expect(within(facts).getByRole("combobox", { name: "Priority" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Edit" }));
    const form = document.querySelector<HTMLElement>("[data-requirement-edit]")!;
    expect(await within(form).findByText("Only admins change Vehicle in Approved.")).toBeInTheDocument();
    expect(within(form).queryByRole("combobox", { name: /Vehicle/ })).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("offers a dependent attribute only the choices the other allows, and clears one it no longer allows", async () => {
    const dependent = anAttribute({ ...brake, dependsOn: { attributeId: vehicle.id, choices: { [car.id]: [disc.id] } } });
    stubRequirement(fixed(), [vehicle, dependent], aRequirement({ attributes: { [vehicle.id]: bike.id, [brake.id]: rim.id } }));
    await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const brakes = await screen.findByLabelText("Brake");
    expect(
      within(brakes)
        .getAllByRole("option")
        .map((each) => each.textContent),
    ).toEqual(["None", "Disc", "Rim"]);
    await userEvent.selectOptions(screen.getByLabelText("Vehicle"), car.id);
    expect(
      within(screen.getByLabelText("Brake"))
        .getAllByRole("option")
        .map((each) => each.textContent),
    ).toEqual(["None", "Disc"]);
    expect(screen.getByLabelText("Brake")).toHaveValue("");
  });
});
