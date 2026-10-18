import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CoverageRow, CoverageRule } from "@/api/coverage";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const ref = (n: number, title: string) => ({
  id: `0195f000-0000-7000-8000-00000000000${n}`,
  key: `CHK-R${n}`,
  projectKey: "CHK",
  kind: "requirement" as const,
  title,
  revision: 1,
  status: null,
  trashed: false,
});

const rows: CoverageRow[] = [
  { requirement: ref(1, "Brakes engage within 100 ms"), decomposed: "full", implemented: "partial", verified: "none" },
  { requirement: ref(2, "Brake lights follow the pedal"), decomposed: "none", implemented: "na", verified: "none" },
];

const summary = {
  decomposed: { full: 1, partial: 0, none: 1, total: 2 },
  implemented: { full: 0, partial: 1, none: 0, total: 1 },
  verified: { full: 0, partial: 0, none: 2, total: 2 },
};

const every: CoverageRule = {
  id: "0195f000-0000-7000-8000-0000000000c1",
  typeId: null,
  typeName: "",
  decomposed: true,
  implemented: true,
  verified: false,
  verifiedFrom: ["links"],
  implementedFrom: ["links"],
};

const noteType = { id: "0195f000-0000-7000-8000-0000000000d1", name: "Note", retired: false };

function stubCoverage(asked: URLSearchParams[] = [], can = aProject().can) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject({ can }) } },
    "GET /projects/CHK/coverage": (request) => {
      asked.push(new URL(request.url).searchParams);
      return { status: 200, body: { summary, rows, total: rows.length, limit: 50, offset: 0 } };
    },
    "GET /projects/CHK/coverage-rules": { status: 200, body: { rules: [every] } },
    "GET /projects/CHK/types": { status: 200, body: { types: [noteType] } },
    "PUT /projects/CHK/coverage-rules": { status: 200, body: { rule: { ...every, verified: true } } },
  });
}

describe("a project's coverage", () => {
  it("shows each dimension's share and each requirement's levels, kept at a level on request", async () => {
    const asked: URLSearchParams[] = [];
    stubCoverage(asked);
    await renderAt("/p/CHK/coverage");
    const list = await screen.findByRole("list", { name: "Requirements" });
    expect(within(list).getAllByRole("listitem")[0]).toHaveTextContent("Decomposed: Full");
    expect(within(list).getAllByRole("listitem")[0]).toHaveTextContent("Implemented: Partial");
    expect(document.querySelector('[data-dimension="decomposed"]')).toHaveTextContent("50%");
    expect(document.querySelector('[data-dimension="verified"]')).toHaveTextContent("0 full, 0 partial, 2 none");
    expect(await axeViolations()).toEqual([]);
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Verified" }), "none");
    await waitFor(() => expect(asked.at(-1)?.get("verified")).toBe("none"));
  });

  it("lets an administrator tick what a rule asks for and add one for a type", async () => {
    const sent = stubCoverage();
    await renderAt("/p/CHK/coverage");
    const rules = await screen.findByRole("list", { name: "Rules" });
    await userEvent.click(within(rules).getByLabelText("Verified"));
    await waitFor(() =>
      expect(sent.find((each) => each.method === "PUT")?.body).toEqual({
        decomposed: true,
        implemented: true,
        verified: true,
        verifiedFrom: ["links"],
        implementedFrom: ["links"],
      }),
    );
    await userEvent.selectOptions(screen.getByLabelText("Add a rule for"), "Note");
    await userEvent.click(screen.getByRole("button", { name: "Add" }));
    await waitFor(() =>
      expect(sent.filter((each) => each.method === "PUT").at(-1)?.body).toEqual({
        typeId: noteType.id,
        decomposed: false,
        implemented: false,
        verified: false,
      }),
    );
  });

  it("lets a rule count test results and code beside links, never nothing (#200, #201)", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/coverage": { status: 200, body: { summary, rows, total: rows.length, limit: 50, offset: 0 } },
      "GET /projects/CHK/coverage-rules": { status: 200, body: { rules: [{ ...every, verified: true }] } },
      "GET /projects/CHK/types": { status: 200, body: { types: [noteType] } },
      "PUT /projects/CHK/coverage-rules": { status: 200, body: { rule: { ...every, verified: true } } },
    });
    await renderAt("/p/CHK/coverage");
    const verified = await screen.findByRole("group", { name: "Verified from: Every other type" });
    expect(within(verified).getByRole("button", { name: "Links" })).toHaveAttribute("aria-pressed", "true");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(verified).getByRole("button", { name: "Links" }));
    expect(sent.some((each) => each.method === "PUT")).toBe(false);
    await userEvent.click(within(verified).getByRole("button", { name: "Test results" }));
    await waitFor(() =>
      expect(sent.find((each) => each.method === "PUT")?.body).toEqual({
        decomposed: true,
        implemented: true,
        verified: true,
        verifiedFrom: ["links", "tests"],
        implementedFrom: ["links"],
      }),
    );
    const implemented = screen.getByRole("group", { name: "Implemented from: Every other type" });
    await userEvent.click(within(implemented).getByRole("button", { name: "Code" }));
    await waitFor(() => expect(sent.filter((each) => each.method === "PUT").at(-1)?.body).toMatchObject({ implementedFrom: ["links", "code"] }));
    // Test cases by their last results in test executions count too (#252).
    await userEvent.click(within(verified).getByRole("button", { name: "Test executions" }));
    await waitFor(() => expect(sent.filter((each) => each.method === "PUT").at(-1)?.body).toMatchObject({ verifiedFrom: expect.arrayContaining(["runs"]) }));
  });

  it("shows a reader the rules without letting them change them", async () => {
    stubCoverage([], { view: true, comment: false, edit: false, approve: false, administer: false, purge: false });
    await renderAt("/p/CHK/coverage");
    const rules = await screen.findByRole("list", { name: "Rules" });
    expect(within(rules).getByLabelText("Decomposed")).toBeDisabled();
    expect(screen.getByText("Only the project's administrators set what coverage asks.")).toBeInTheDocument();
    expect(screen.queryByLabelText("Add a rule for")).toBeNull();
  });

  it("leads to the traceability matrix", async () => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/coverage": { status: 200, body: { summary, rows, total: 2, limit: 50, offset: 0 } },
      "GET /projects/CHK/coverage-rules": { status: 200, body: { rules: [every] } },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    });
    await renderAt("/p/CHK/coverage");
    expect(await screen.findByRole("link", { name: "Open the matrix" })).toHaveAttribute("href", "/p/CHK/matrix");
  });
});
