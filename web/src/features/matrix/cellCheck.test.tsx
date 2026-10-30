import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Comparison } from "@/api/history";
import type { Matrix } from "@/api/coverage";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aLink, derives } from "@/test/links";
import { aProject } from "@/test/projects";

// Checking a suspect link from a matrix cell (#315).

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const ref = (n: number, title: string, revision: number) => ({
  id: `0195f000-0000-7000-8000-00000000000${n}`,
  key: `CHK-R${n}`,
  projectKey: "CHK",
  kind: "requirement" as const,
  title,
  revision,
  status: null,
  trashed: false,
});

// CHK-R1, at revision 3, derives from CHK-R2, which moved from revision 1 to 2 since the link was confirmed.
const lights = { key: "CHK-R2", title: "Brake lights follow the pedal" };
const moved = {
  ...aLink(lights),
  other: { ...aLink(lights).other, revision: 2 },
  suspect: [{ end: "target" as const, reviewed: 1, current: 2 }],
};

const comparison: Comparison = {
  from: { number: 1, title: "Brake lights follow the pedal", type: null, authorName: "Ada", createdAt: "2026-10-01T08:00:00Z" },
  to: { number: 2, title: "Brake lights follow the pedal", type: null, authorName: "Ada", createdAt: "2026-10-02T08:00:00Z" },
  title: [],
  statement: [{ change: "modified", node: { type: "paragraph", content: [{ type: "text", text: "within 50 ms", marks: [{ type: "diffInsert" }] }] } }],
  rationale: [],
  preconditions: [],
  steps: [],
  parameters: [],
  risk: [],
  attributes: [],
};

function aMatrix(canLink: boolean): Matrix {
  return {
    rows: [{ ...ref(1, "Brakes engage within 100 ms", 3), canLink }],
    columns: [{ requirement: ref(2, lights.title, 2), issueKey: "", page: null }],
    cells: [{ row: 0, column: 0, links: [{ id: moved.id, kind: "trace", name: "Derives", typeKey: "derives", direction: "outgoing", suspect: true }] }],
    rowLinks: [1],
    columnLinks: [1],
    total: 1,
    limit: 50,
    offset: 0,
    columnTotal: 1,
    columnLimit: 100,
    columnOffset: 0,
  };
}

function stubCheck(canLink: boolean, more: Parameters<typeof stubApi>[0] = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/statuses": { status: 200, body: { statuses: [] } },
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
    "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [derives] } },
    "GET /projects/CHK/matrix": { status: 200, body: { matrix: aMatrix(canLink) } },
    "GET /requirements/CHK-R1/links": { status: 200, body: { links: [moved] } },
    "GET /requirements/CHK-R2/diff": { status: 200, body: { comparison } },
    [`GET /links/${moved.id}/clearances`]: { status: 200, body: { clearances: [] } },
    ...more,
  });
}

describe("a suspect link in a matrix cell", () => {
  it("is checked in the dialog of the link between the cell's row and column, which leaves the panel open", async () => {
    const sent = stubCheck(true, { [`POST /links/${moved.id}/clear`]: { status: 200, body: { link: { ...moved, suspect: [] } } } });
    await renderAt("/p/CHK/matrix");
    await userEvent.click(await screen.findByRole("button", { name: "CHK-R1 to CHK-R2: Derives (suspect)" }));
    const panel = await screen.findByRole("dialog", { name: "CHK-R1 and CHK-R2" });
    expect(await within(panel).findByText("Suspect")).toBeInTheDocument();
    await userEvent.click(within(panel).getByRole("button", { name: "Check the link to CHK-R2" }));

    const dialog = await screen.findByRole("dialog", { name: "Check: CHK-R1 derives from CHK-R2" });
    const change = await within(dialog).findByRole("region", { name: "CHK-R2, revision 1 to 2" });
    expect(await within(change).findByText("within 50 ms")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.type(within(dialog).getByLabelText("Why the link still holds"), "The lights still follow the pedal.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Confirm link" }));
    // The row's revision is the matrix's, the column's the link's other end as read.
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST")?.body).toEqual({ sourceRevision: 3, targetRevision: 2, comment: "The lights still follow the pedal." }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Check: CHK-R1 derives from CHK-R2" })).toBeNull());
    expect(screen.getByRole("dialog", { name: "CHK-R1 and CHK-R2" })).toBeInTheDocument();
    expect(await screen.findByText("The link to CHK-R2 is confirmed.")).toBeInTheDocument();
  });

  it("is marked for a reader, who is offered no check", async () => {
    stubCheck(false);
    await renderAt("/p/CHK/matrix");
    await userEvent.click(await screen.findByRole("button", { name: "CHK-R1 to CHK-R2: Derives (suspect)" }));
    const panel = await screen.findByRole("dialog", { name: "CHK-R1 and CHK-R2" });
    expect(await within(panel).findByText("Suspect")).toBeInTheDocument();
    expect(within(panel).queryByRole("button", { name: "Check the link to CHK-R2" })).toBeNull();
    expect(within(panel).queryByRole("button", { name: "Check" })).toBeNull();
  });
});
