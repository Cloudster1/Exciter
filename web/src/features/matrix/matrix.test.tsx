import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Matrix } from "@/api/coverage";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { derives, verifies } from "@/test/links";
import { aProject } from "@/test/projects";
import { fromMatrixAddress, matrixParams, readMatrixAddress, toMatrixAddress } from "./address";

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
const row = (n: number, title: string) => ({ ...ref(n, title), canLink: true });

const issues: Matrix = {
  rows: [row(1, "Brakes engage within 100 ms"), row(2, "Brake lights follow the pedal")],
  columns: [{ requirement: null, issueKey: "WEB-1" }],
  cells: [
    { row: 0, column: 0, links: [{ id: "0195f000-0000-7000-8000-0000000000e1", kind: "armature", name: "implements", direction: "outgoing", suspect: false }] },
    { row: 1, column: 0, links: [{ id: "0195f000-0000-7000-8000-0000000000e2", kind: "armature", name: "verifies", direction: "outgoing", suspect: true }] },
  ],
  rowLinks: [1, 1],
  columnLinks: [2],
  total: 2,
  limit: 50,
  offset: 0,
  columnTotal: 1,
  columnLimit: 100,
  columnOffset: 0,
};

function stubMatrix(matrix: Matrix = issues) {
  const asked: URLSearchParams[] = [];
  stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/statuses": { status: 200, body: { statuses: [] } },
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
    "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [derives, verifies] } },
    "GET /projects": { status: 200, body: { projects: [aProject(), aProject({ id: "0195f000-0000-7000-8000-0000000000f2", key: "SYS", name: "Systems" })] } },
    "GET /projects/CHK/matrix": (request) => {
      asked.push(new URL(request.url).searchParams);
      return { status: 200, body: { matrix } };
    },
  });
  return asked;
}

describe("the traceability matrix", () => {
  it("lays the links out, a suspect one marked, with the whole of it to download", async () => {
    stubMatrix();
    await renderAt("/p/CHK/matrix?columns=issues");
    const table = await screen.findByRole("table", { name: "Matrix" });
    expect(within(table).getByRole("columnheader", { name: "WEB-1" })).toBeInTheDocument();
    expect(table.querySelector('[data-cell="CHK-R1:WEB-1"]')).toHaveAttribute("title", "CHK-R1 to WEB-1: implements");
    expect(table.querySelector('[data-cell="CHK-R2:WEB-1"]')).toHaveAttribute("title", "CHK-R2 to WEB-1: verifies (suspect)");
    expect(screen.getByRole("link", { name: "Download CSV" })).toHaveAttribute("href", expect.stringContaining("/projects/CHK/matrix.csv?columns=issues"));
    expect(await axeViolations()).toEqual([]);
  });

  it("counts the link types and the direction picked, and keeps them in the address and the download", async () => {
    const asked = stubMatrix({ ...issues, columns: [], cells: [], columnLinks: [], columnTotal: 0 });
    const router = await renderAt("/p/CHK/matrix");
    const types = await screen.findByRole("group", { name: "Link types" });
    expect(screen.getByText("Every link type counts while none is picked.")).toBeInTheDocument();
    await userEvent.click(await within(types).findByRole("button", { name: "Verifies" }));
    await userEvent.click(screen.getByRole("button", { name: "Incoming" }));
    await waitFor(() => expect(asked.at(-1)?.getAll("linkType")).toEqual(["verifies"]));
    expect(asked.at(-1)?.get("direction")).toBe("incoming");
    await waitFor(() => expect(router.state.location.search).toEqual({ linkType: ["verifies"], direction: "incoming" }));
    const href = screen.getByRole("link", { name: "Download CSV" }).getAttribute("href") ?? "";
    expect(href).toContain("linkType=verifies");
    expect(href).toContain("direction=incoming");
    expect(within(types).getByRole("button", { name: "Verifies" })).toHaveAttribute("aria-pressed", "true");
    expect(await axeViolations()).toEqual([]);
  });

  it("offers relations for issue columns instead of link types", async () => {
    const asked = stubMatrix();
    await renderAt("/p/CHK/matrix?columns=issues");
    const relations = await screen.findByRole("group", { name: "Relations" });
    expect(screen.queryByRole("group", { name: "Link types" })).toBeNull();
    await userEvent.click(within(relations).getByRole("button", { name: "Verified by" }));
    await waitFor(() => expect(asked.at(-1)?.getAll("relation")).toEqual(["verifies"]));
  });

  it("starts from what the address holds", async () => {
    const asked = stubMatrix({ ...issues, columns: [], cells: [], columnLinks: [], columnTotal: 0 });
    await renderAt("/p/CHK/matrix?linkType=derives&direction=outgoing&columnProject=SYS&sort=key&desc=true");
    await waitFor(() => expect(asked.length).toBeGreaterThan(0));
    const first = asked[0]!;
    expect(first.getAll("linkType")).toEqual(["derives"]);
    expect(first.get("direction")).toBe("outgoing");
    expect(first.get("columnProject")).toBe("SYS");
    expect(first.get("sort")).toBe("key");
    expect(await screen.findByLabelText("Columns from")).toHaveValue("SYS");
  });
});

describe("the matrix's gaps", () => {
  const notes: Matrix = {
    ...issues,
    columns: [{ requirement: ref(4, "Brakes were discussed"), issueKey: "" }],
    cells: [],
    rowLinks: [0, 2],
    columnLinks: [0],
  };

  it("counts every row's and column's links and marks the ones without", async () => {
    stubMatrix(notes);
    await renderAt("/p/CHK/matrix?columnQuery=type%20%3D%20Note");
    const table = await screen.findByRole("table", { name: "Matrix" });
    const gaps = table.querySelectorAll("[data-gap]");
    expect(gaps).toHaveLength(2);
    expect(within(table).getAllByRole("rowheader", { name: "Links" })).toHaveLength(1);
    expect(table.querySelector("[data-column-links]")).toHaveTextContent("0");
    expect(await axeViolations()).toEqual([]);
  });

  it("keeps the column filter apart from the rows' and offers gaps in columns only with it", async () => {
    const asked = stubMatrix(notes);
    const router = await renderAt("/p/CHK/matrix");
    const gaps = await screen.findByLabelText("Only gaps");
    expect(within(gaps).getByRole("option", { name: "Columns without a link" })).toBeDisabled();
    expect(screen.getByText("Columns without a link need a column filter.")).toBeInTheDocument();
    const columnQuery = screen.getByRole("combobox", { name: "Column query" });
    await userEvent.type(columnQuery, "type = Note{Enter}");
    await waitFor(() => expect(asked.at(-1)?.get("columnQuery")).toBe("type = Note"));
    expect(within(gaps).getByRole("option", { name: "Columns without a link" })).toBeEnabled();
    await userEvent.selectOptions(gaps, "columns");
    await waitFor(() => expect(router.state.location.search).toEqual({ columnQuery: "type = Note", gaps: "columns" }));
    expect(asked.at(-1)?.get("gaps")).toBe("columns");
    expect(await axeViolations()).toEqual([]);
  });

  it("says a refused column query under the column filter, not under the rows' query", async () => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
      "GET /projects/CHK/statuses": { status: 200, body: { statuses: [] } },
      "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
      "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [] } },
      "GET /projects/CHK/matrix": {
        status: 400,
        body: { error: { code: "bad_query", message: "There is no field nonsense.", position: 1, fields: { columnQuery: "There is no field nonsense." } } },
      },
    });
    await renderAt("/p/CHK/matrix?columnQuery=nonsense");
    const filter = await screen.findByRole("search", { name: "Column filter" });
    expect(await within(filter).findByText("There is no field nonsense.")).toBeInTheDocument();
    expect(screen.getAllByText("There is no field nonsense.")).toHaveLength(1);
  });
});

describe("a matrix's address", () => {
  it("drops what does not fit the columns, and the defaults", () => {
    const setup = fromMatrixAddress(
      readMatrixAddress({ columns: "issues", linkType: ["derives"], relation: "verifies", direction: "both", columnQuery: "kind = requirement", gaps: "both" }),
    );
    expect(toMatrixAddress(setup)).toEqual({ columns: "issues", relation: ["verifies"] });
    expect(matrixParams(setup)).toEqual({ columns: "issues", relation: ["verifies"] });
  });

  it("ignores a direction, columns or gaps it does not know", () => {
    expect(readMatrixAddress({ columns: "pages", direction: "sideways", gaps: "everywhere" })).toEqual({});
  });

  it("keeps gaps in columns only with a column filter", () => {
    expect(toMatrixAddress(fromMatrixAddress({ gaps: "columns" }))).toEqual({});
    expect(toMatrixAddress(fromMatrixAddress({ gaps: "both", columnType: ["t1"] }))).toEqual({ gaps: "both", columnType: ["t1"] });
    expect(toMatrixAddress(fromMatrixAddress({ gaps: "rows" }))).toEqual({ gaps: "rows" });
  });
});
