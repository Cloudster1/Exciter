import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CoverageRow, CoverageRule, Matrix } from "@/api/coverage";
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
    await waitFor(() => expect(sent.find((each) => each.method === "PUT")?.body).toEqual({ decomposed: true, implemented: true, verified: true }));
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

  it("shows a reader the rules without letting them change them", async () => {
    stubCoverage([], { view: true, comment: false, edit: false, approve: false, administer: false, purge: false });
    await renderAt("/p/CHK/coverage");
    const rules = await screen.findByRole("list", { name: "Rules" });
    expect(within(rules).getByLabelText("Decomposed")).toBeDisabled();
    expect(screen.getByText("Only the project's administrators set what coverage asks.")).toBeInTheDocument();
    expect(screen.queryByLabelText("Add a rule for")).toBeNull();
  });

  it("lays the links out as a matrix, a suspect one marked, with the whole of it to download", async () => {
    const matrix: Matrix = {
      rows: [ref(1, "Brakes engage within 100 ms"), ref(2, "Brake lights follow the pedal")],
      columns: [{ requirement: null, issueKey: "WEB-1" }],
      cells: [
        { row: 0, column: 0, links: [{ name: "implements", suspect: false }] },
        { row: 1, column: 0, links: [{ name: "verifies", suspect: true }] },
      ],
      total: 2,
      limit: 50,
      offset: 0,
    };
    const asked: URLSearchParams[] = [];
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/coverage": { status: 200, body: { summary, rows, total: 2, limit: 50, offset: 0 } },
      "GET /projects/CHK/coverage-rules": { status: 200, body: { rules: [every] } },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
      "GET /projects/CHK/matrix": (request) => {
        asked.push(new URL(request.url).searchParams);
        return { status: 200, body: { matrix } };
      },
    });
    await renderAt("/p/CHK/coverage");
    await userEvent.click(await screen.findByRole("button", { name: "Matrix" }));
    await userEvent.click(await screen.findByRole("button", { name: "Armature issues" }));
    const table = await screen.findByRole("table", { name: "Matrix" });
    expect(asked.at(-1)?.get("columns")).toBe("issues");
    expect(within(table).getByRole("columnheader", { name: "WEB-1" })).toBeInTheDocument();
    expect(table.querySelector('[data-cell="CHK-R1:WEB-1"]')).toHaveAttribute("title", "CHK-R1 to WEB-1: implements");
    expect(table.querySelector('[data-cell="CHK-R2:WEB-1"]')).toHaveAttribute("title", "CHK-R2 to WEB-1: verifies (suspect)");
    expect(screen.getByRole("link", { name: "Download CSV" })).toHaveAttribute("href", expect.stringContaining("/projects/CHK/matrix.csv?columns=issues"));
    expect(await axeViolations()).toEqual([]);
  });
});
