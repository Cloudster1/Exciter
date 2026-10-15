import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Attribute, AttributeKind, RequirementType } from "@/api/attributes";
import { renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

afterEach(() => vi.unstubAllGlobals());

const functional: RequirementType = {
  id: "0195f000-0000-7000-8000-0000000000c1",
  name: "Functional",
  description: "",
  icon: "requirement",
  colour: "#4f6bed",
  position: 0,
  workflowId: null,
  retired: false,
  requirements: 3,
  createdAt: "2026-10-01T08:00:00Z",
  updatedAt: "2026-10-01T08:00:00Z",
};
const iface: RequirementType = {
  ...functional,
  id: "0195f000-0000-7000-8000-0000000000c2",
  name: "Interface",
  icon: "interface",
  position: 1,
  requirements: 0,
};
const project = aProject({ settings: { defaultTypeId: functional.id, workflowId: null, requireRevisionComment: false, signedApprovals: false } });
const test = { id: "0195f000-0000-7000-8000-0000000000e1", name: "Test", retired: false };
const analysis = { id: "0195f000-0000-7000-8000-0000000000e2", name: "Analysis", retired: false };
const verification: Attribute = {
  id: "0195f000-0000-7000-8000-0000000000d1",
  name: "Verification",
  description: "",
  kind: "select",
  required: true,
  options: [test, analysis],
  typeIds: [functional.id],
  position: 0,
  retired: false,
  createdAt: "2026-10-01T08:00:00Z",
  updatedAt: "2026-10-01T08:00:00Z",
};
const kinds: AttributeKind[] = [
  { kind: "text", title: "Text", description: "A line of text.", hasOptions: false },
  { kind: "select", title: "Select", description: "One of a list of options.", hasOptions: true },
];
const shaped = {
  "GET /projects/CHK": { status: 200, body: { project } },
  "GET /projects/CHK/types": { status: 200, body: { types: [functional, iface] } },
  "GET /projects/CHK/attributes": { status: 200, body: { attributes: [verification] } },
  "GET /attribute-kinds": { status: 200, body: { kinds } },
};

describe("a project's types", () => {
  it("lists them with the default marked, and makes a new one", async () => {
    const sent = stubApi({ ...shaped, "POST /projects/CHK/types": { status: 201, body: { type: { ...iface, id: "new", name: "Constraint" } } } });
    await renderAt("/p/CHK/settings");
    await userEvent.click(await screen.findByRole("tab", { name: "Types" }));
    const row = (await screen.findByText("Functional")).closest("tr")!;
    expect(within(row).getByText("Default")).toBeInTheDocument();
    expect(within(row).queryByRole("button", { name: "Retire" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "New type" }));
    await userEvent.type(screen.getByLabelText("Name"), "Constraint");
    await userEvent.selectOptions(screen.getByLabelText("Icon"), "constraint");
    await userEvent.click(screen.getByRole("button", { name: "New type" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST")?.body).toEqual({ name: "Constraint", description: "", icon: "constraint", colour: "#4f6bed" }),
    );
  });
});

describe("a project's attributes", () => {
  it("defines a select with its options, on the types picked", async () => {
    const sent = stubApi({ ...shaped, "POST /projects/CHK/attributes": { status: 201, body: { attribute: verification } } });
    await renderAt("/p/CHK/settings");
    await userEvent.click(await screen.findByRole("tab", { name: "Attributes" }));
    await userEvent.click(await screen.findByRole("button", { name: "New attribute" }));
    const form = document.querySelector<HTMLElement>("[data-attribute-form]")!;
    await userEvent.type(within(form).getByLabelText("Name"), "Release");
    await userEvent.selectOptions(within(form).getByLabelText("Kind"), "select");
    await userEvent.click(within(form).getByLabelText("Interface"));
    await userEvent.type(within(form).getByLabelText("New option"), "R1{Enter}");
    await userEvent.type(within(form).getByLabelText("New option"), "R2{Enter}");
    await userEvent.click(within(form).getByRole("button", { name: "New attribute" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST")?.body).toEqual({
        name: "Release",
        kind: "select",
        required: false,
        typeIds: [iface.id],
        options: [{ name: "R1" }, { name: "R2" }],
      }),
    );
  });

  it("renames an option under its own id", async () => {
    const sent = stubApi({ ...shaped, [`PATCH /projects/CHK/attributes/${verification.id}`]: { status: 200, body: { attribute: verification } } });
    await renderAt("/p/CHK/settings");
    await userEvent.click(await screen.findByRole("tab", { name: "Attributes" }));
    const row = (await screen.findByText("Verification")).closest("tr")!;
    await userEvent.click(within(row).getByRole("button", { name: "Edit" }));
    const first = screen.getByLabelText("Option 1");
    await userEvent.clear(first);
    await userEvent.type(first, "Testing");
    await userEvent.click(screen.getByRole("button", { name: "Save attribute" }));
    await waitFor(() =>
      expect((sent.find((r) => r.method === "PATCH")?.body as { options: unknown[] }).options[0]).toEqual({ id: test.id, name: "Testing", retired: false }),
    );
  });
});

describe("a requirement's attributes", () => {
  it("asks for those that show on the type picked, and sends what was given", async () => {
    const made = aRequirement({ key: "CHK-R2" });
    const sent = stubApi({
      ...shaped,
      "POST /projects/CHK/requirements": { status: 201, body: { requirement: made } },
      "GET /requirements/CHK-R2": { status: 200, body: { requirement: made } },
    });
    await renderAt("/p/CHK/new");
    await userEvent.type(await screen.findByLabelText("Title"), "Pay");
    await userEvent.selectOptions(await screen.findByLabelText("Verification (required)"), analysis.id);
    await userEvent.selectOptions(screen.getByLabelText("Type"), iface.id);
    expect(screen.queryByLabelText("Verification (required)")).toBeNull();
    await userEvent.selectOptions(screen.getByLabelText("Type"), functional.id);
    // The value left with the type it does not show on, so it is asked for again.
    expect(screen.getByLabelText("Verification (required)")).toHaveValue("");
    await userEvent.selectOptions(screen.getByLabelText("Verification (required)"), test.id);
    await userEvent.click(screen.getByRole("button", { name: "New requirement" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST")?.body).toMatchObject({ typeId: functional.id, attributes: { [verification.id]: test.id } }),
    );
  });

  it("shows a requirement's values by their names", async () => {
    stubApi({
      ...shaped,
      "GET /requirements/CHK-R1": {
        status: 200,
        body: {
          requirement: aRequirement({
            type: { id: functional.id, name: "Functional", icon: "requirement", colour: "#4f6bed" },
            attributes: { [verification.id]: analysis.id },
          }),
        },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    const value = await waitFor(() => {
      const found = document.querySelector('[data-attribute-value="Verification"]');
      expect(found).not.toBeNull();
      return found!;
    });
    expect(value.closest("dl")).toHaveTextContent("Analysis");
    expect(document.querySelector("[data-requirement-type]")).toHaveTextContent("Functional");
  });
});
