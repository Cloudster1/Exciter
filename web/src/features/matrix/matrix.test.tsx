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

const row = (n: number, title: string) => ({
  id: `0195f000-0000-7000-8000-00000000000${n}`,
  key: `CHK-R${n}`,
  projectKey: "CHK",
  kind: "requirement" as const,
  title,
  revision: 1,
  status: null,
  trashed: false,
  canLink: true,
});

const issues: Matrix = {
  rows: [row(1, "Brakes engage within 100 ms"), row(2, "Brake lights follow the pedal")],
  columns: [{ requirement: null, issueKey: "WEB-1" }],
  cells: [
    { row: 0, column: 0, links: [{ id: "0195f000-0000-7000-8000-0000000000e1", kind: "armature", name: "implements", direction: "outgoing", suspect: false }] },
    { row: 1, column: 0, links: [{ id: "0195f000-0000-7000-8000-0000000000e2", kind: "armature", name: "verifies", direction: "outgoing", suspect: true }] },
  ],
  total: 2,
  limit: 50,
  offset: 0,
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
    const asked = stubMatrix({ ...issues, columns: [], cells: [] });
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
    const asked = stubMatrix({ ...issues, columns: [], cells: [] });
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

describe("a matrix's address", () => {
  it("drops what does not fit the columns, and the defaults", () => {
    const setup = fromMatrixAddress(readMatrixAddress({ columns: "issues", linkType: ["derives"], relation: "verifies", direction: "both" }));
    expect(toMatrixAddress(setup)).toEqual({ columns: "issues", relation: ["verifies"] });
    expect(matrixParams(setup)).toEqual({ columns: "issues", relation: ["verifies"] });
  });

  it("ignores a direction or columns it does not know", () => {
    expect(readMatrixAddress({ columns: "pages", direction: "sideways" })).toEqual({});
  });
});
