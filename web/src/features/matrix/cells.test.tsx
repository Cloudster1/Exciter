import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ArmatureLink } from "@/api/armature";
import type { Matrix } from "@/api/coverage";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aLink, derives, verifies } from "@/test/links";
import { aProject } from "@/test/projects";

// A matrix cell's links in a panel (#149).

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
  revision: 2,
  status: null,
  trashed: false,
});

const suspectLink = aLink({ key: "CHK-R2", title: "Brake lights follow the pedal" }, { suspect: [{ end: "target", reviewed: 1, current: 2 }] });

function aMatrix(canLink: boolean, over: Partial<Matrix> = {}): Matrix {
  return {
    rows: [{ ...ref(1, "Brakes engage within 100 ms"), canLink }],
    columns: [
      { requirement: ref(2, "Brake lights follow the pedal"), issueKey: "", page: null },
      { requirement: ref(3, "Horn sounds"), issueKey: "", page: null },
    ],
    cells: [{ row: 0, column: 0, links: [{ id: suspectLink.id, kind: "trace", name: "Derives", typeKey: "derives", direction: "outgoing", suspect: true }] }],
    rowLinks: [1],
    columnLinks: [1, 0],
    total: 1,
    limit: 50,
    offset: 0,
    columnTotal: 2,
    columnLimit: 100,
    columnOffset: 0,
    ...over,
  };
}

function stubCells(matrix: Matrix, more: Parameters<typeof stubApi>[0] = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/statuses": { status: 200, body: { statuses: [] } },
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
    "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [derives, verifies] } },
    "GET /projects/CHK/matrix": { status: 200, body: { matrix } },
    "GET /requirements/CHK-R1/links": { status: 200, body: { links: [suspectLink] } },
    ...more,
  });
}

describe("a matrix cell", () => {
  it("lists its links, removes one, and links the row to the column", async () => {
    const sent = stubCells(aMatrix(true), {
      [`DELETE /links/${suspectLink.id}`]: { status: 204 },
      "POST /requirements/CHK-R1/links": { status: 201, body: { link: aLink({ key: "CHK-R3", title: "Horn sounds" }) } },
    });
    await renderAt("/p/CHK/matrix");
    await userEvent.click(await screen.findByRole("button", { name: "CHK-R1 to CHK-R2: Derives (suspect)" }));
    const panel = await screen.findByRole("dialog", { name: "CHK-R1 and CHK-R2" });
    expect(within(panel).getByText("Derives from CHK-R2")).toBeInTheDocument();
    expect(within(panel).getByRole("button", { name: "Check the link to CHK-R2" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(panel).getByRole("button", { name: "Remove the link derives from CHK-R2" }));
    await waitFor(() => expect(sent.some((r) => r.method === "DELETE" && r.path === `/links/${suspectLink.id}`)).toBe(true));
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    await userEvent.click(screen.getByRole("button", { name: "Links between CHK-R1 and CHK-R3" }));
    const empty = await screen.findByRole("dialog", { name: "CHK-R1 and CHK-R3" });
    expect(within(empty).getByText("No link between these yet.")).toBeInTheDocument();
    await userEvent.selectOptions(within(empty).getByLabelText("Link type"), "This requirement verifies");
    await userEvent.click(within(empty).getByRole("button", { name: "Link them" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST" && r.path === "/requirements/CHK-R1/links")?.body).toEqual({
        typeId: verifies.id,
        direction: "outgoing",
        otherKey: "CHK-R3",
      }),
    );
  });

  it("is read-only for a reader who may not link the row, and an empty cell is no button", async () => {
    stubCells(aMatrix(false));
    await renderAt("/p/CHK/matrix");
    await userEvent.click(await screen.findByRole("button", { name: "CHK-R1 to CHK-R2: Derives (suspect)" }));
    const panel = await screen.findByRole("dialog", { name: "CHK-R1 and CHK-R2" });
    expect(await within(panel).findByText("Derives from CHK-R2")).toBeInTheDocument();
    expect(within(panel).getByText("You may read these links but not change them.")).toBeInTheDocument();
    expect(within(panel).queryByRole("button", { name: /Remove/ })).toBeNull();
    expect(within(panel).queryByRole("form", { name: "Link them" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Links between CHK-R1 and CHK-R3" })).toBeNull();
  });

  it("traces the row to an issue column with the relation picked", async () => {
    const trace: ArmatureLink = {
      id: "0195f000-0000-7000-8000-0000000000e1",
      relation: "implements",
      issueKey: "WEB-1",
      statusCategory: "done",
      statusStale: false,
      sync: "synced",
      reviewedRevision: 2,
      suspect: false,
      error: "",
      brokenAt: null,
      detachedAt: null,
      createdByName: "Bob",
      createdAt: "2026-10-01T09:00:00Z",
    };
    const sent = stubCells(
      aMatrix(true, {
        columns: [
          { requirement: null, issueKey: "WEB-1", page: null },
          { requirement: null, issueKey: "WEB-2", page: null },
        ],
        cells: [{ row: 0, column: 0, links: [{ id: trace.id, kind: "armature", name: "implements", direction: "outgoing", suspect: false }] }],
      }),
      {
        "GET /requirements/CHK-R1/armature-links": { status: 200, body: { links: [trace] } },
        "POST /requirements/CHK-R1/armature-links": { status: 201, body: { link: { ...trace, issueKey: "WEB-2", relation: "verifies" } } },
      },
    );
    await renderAt("/p/CHK/matrix?columns=issues");
    await userEvent.click(await screen.findByRole("button", { name: "CHK-R1 to WEB-1: implements" }));
    expect(await within(await screen.findByRole("dialog", { name: "CHK-R1 and WEB-1" })).findByText("Implemented by WEB-1")).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    await userEvent.click(screen.getByRole("button", { name: "Links between CHK-R1 and WEB-2" }));
    const panel = await screen.findByRole("dialog", { name: "CHK-R1 and WEB-2" });
    await userEvent.selectOptions(within(panel).getByLabelText("Relation"), "verifies");
    await userEvent.click(within(panel).getByRole("button", { name: "Link them" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST" && r.path === "/requirements/CHK-R1/armature-links")?.body).toEqual({
        issueKey: "WEB-2",
        relation: "verifies",
      }),
    );
  });
});
