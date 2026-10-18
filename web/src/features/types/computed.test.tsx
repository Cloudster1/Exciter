import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Attribute, AttributeKind, FormulaCheck } from "@/api/attributes";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { verifies } from "@/test/links";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

// Computed attributes (#213): defined with a formula checked as it is written, shown read-only where they are read.

afterEach(() => vi.unstubAllGlobals());

const weight: Attribute = {
  id: "0195f000-0000-7000-8000-0000000000d1",
  name: "Weight",
  description: "",
  kind: "number",
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
};
const score: Attribute = { ...weight, id: "0195f000-0000-7000-8000-0000000000d2", name: "Score", position: 1, formula: `"Weight" * 2` };
const kinds: AttributeKind[] = [{ kind: "number", title: "Number", description: "A number.", hasOptions: false }];
const project = aProject();
const answer = (check: Partial<FormulaCheck>): FormulaCheck => ({ formula: "", errors: [], reads: [], linkTypes: [], value: null, ...check });

function stubSettings(more: Parameters<typeof stubApi>[0] = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/attributes": {
      status: 200,
      body: { attributes: [weight, score], recompute: { done: 3, total: 10, startedAt: "2026-10-18T08:00:00Z" } },
    },
    "GET /attribute-kinds": { status: 200, body: { kinds } },
    "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [verifies] } },
    ...more,
  });
}

describe("computed attributes", () => {
  it("are listed with their formula, and say how far a recompute has got", async () => {
    stubSettings();
    await renderAt("/p/CHK/settings");
    await userEvent.click(await screen.findByRole("tab", { name: "Attributes" }));
    const row = (await screen.findByText("Score")).closest("tr")!;
    expect(within(row).getByText("Computed")).toBeInTheDocument();
    expect(within(row).getByText(`"Weight" * 2`)).toBeInTheDocument();
    expect(await screen.findByRole("status")).toHaveTextContent("Working the computed attributes out again: 3 of 10 requirements.");
  });

  it("are defined with a formula that is completed, checked as it is written and tried on a requirement", async () => {
    const sent = stubSettings({
      "POST /projects/CHK/formula-check": async (request: Request) => {
        const body = (await request.json()) as { formula: string; requirementKey?: string };
        if (body.formula.includes("'")) {
          return {
            status: 200,
            body: { check: answer({ formula: body.formula, errors: [{ from: 12, to: 16, message: "This is text, where a number belongs." }] }) },
          };
        }
        return { status: 200, body: { check: answer({ formula: body.formula, kind: "number", reads: ["Weight"], value: body.requirementKey ? 8 : null }) } };
      },
      "POST /projects/CHK/attributes": { status: 201, body: { attribute: score } },
    });
    await renderAt("/p/CHK/settings");
    await userEvent.click(await screen.findByRole("tab", { name: "Attributes" }));
    await userEvent.click(await screen.findByRole("button", { name: "New attribute" }));
    const form = document.querySelector<HTMLElement>("[data-attribute-form]")!;
    await userEvent.type(within(form).getByLabelText("Name"), "Score");
    await userEvent.click(within(form).getByLabelText("Work it out from a formula"));
    expect(within(form).queryByLabelText("Kind")).toBeNull();
    const formula = within(form).getByLabelText("Formula");
    await userEvent.type(formula, `"We`);
    await userEvent.click(within(form).getByRole("button", { name: `Insert "Weight"` }));
    expect(formula).toHaveValue(`"Weight"`);
    await userEvent.type(formula, ` * 'two'`);
    expect(await within(form).findByText("Characters 12 to 16: This is text, where a number belongs.")).toBeInTheDocument();
    await userEvent.clear(formula);
    await userEvent.type(formula, `"Weight" * 2`);
    expect(await within(form).findByText("Gives a number. Reads Weight")).toBeInTheDocument();
    await userEvent.type(within(form).getByLabelText("Try it on"), "CHK-R1");
    expect(await within(form).findByText("Its value there: 8")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(form).getByRole("button", { name: "New attribute" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST" && r.path === "/projects/CHK/attributes")?.body).toEqual({
        name: "Score",
        typeIds: [],
        formula: `"Weight" * 2`,
        onSections: false,
      }),
    );
  });

  it("show on a requirement beside its typed values, marked computed, and are never typed in the editor", async () => {
    const requirement = aRequirement({ attributes: { [weight.id]: 4 }, computed: { [score.id]: 8 } });
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project } },
      "GET /requirements/CHK-R1": { status: 200, body: { requirement } },
      "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
      "GET /projects/CHK/attributes": { status: 200, body: { attributes: [weight, score], recompute: null } },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    const shown = await waitFor(() => {
      const found = document.querySelector('[data-attribute-value="Score"]');
      expect(found).not.toBeNull();
      return found;
    });
    expect(shown).toHaveTextContent("Score(Computed)8");
    await userEvent.click(screen.getByRole("button", { name: "Edit" }));
    const fields = await waitFor(() => document.querySelector<HTMLElement>("[data-attribute-fields]")!);
    expect(within(fields).getByLabelText("Weight")).toBeInTheDocument();
    expect(within(fields).queryByLabelText("Score")).toBeNull();
    expect(fields.querySelector('[data-attribute-value="Score"]')).toHaveTextContent("8");
  });
});
