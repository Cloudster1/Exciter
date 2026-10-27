import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { RequirementRow } from "@/api/requirements";
import type { View } from "@/api/search";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { draft } from "@/test/workflows";
import { emptyFilter } from "./filter";

afterEach(() => vi.unstubAllGlobals());

const row: RequirementRow = { ...aRequirement(), outline: "1", trashed: false, attributes: {}, labels: [], suspectLinks: 0, quality: null };
const mine: View = {
  id: "0195f000-0000-7000-8000-000000000f01",
  projectKey: "CHK",
  name: "Mine",
  ownerId: "0195f000-0000-7000-8000-00000000000a",
  ownerName: "Ada Lovelace",
  shared: false,
  kind: "grid",
  filter: emptyFilter(),
  columns: ["key", "title"],
  settings: {},
  dropped: [],
  sort: { column: "title", desc: false },
  canEdit: true,
  createdAt: "2026-10-01T08:00:00Z",
  updatedAt: "2026-10-01T08:00:00Z",
};
const forbidden = (message: string) => ({ status: 403, body: { error: { code: "forbidden", message } } });

function stubViews(more: Parameters<typeof stubApi>[0] = {}, project = aProject()) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project } },
    "GET /projects/CHK/requirements": { status: 200, body: { rows: [row], total: 1, limit: 100, offset: 0 } },
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/statuses": { status: 200, body: { statuses: [draft] } },
    "GET /projects/CHK/views": { status: 200, body: { views: [mine] } },
    ...more,
  });
}

const openMine = `/p/CHK/grid?sort=title&columns=${encodeURIComponent(JSON.stringify(mine.columns))}&view=${mine.id}`;

describe("a saved view", () => {
  it("saves the grid's filter, order and columns into the open view", async () => {
    const sent = stubViews({
      "PATCH /views/0195f000-0000-7000-8000-000000000f01": async (request) => ({
        status: 200,
        body: { view: { ...mine, ...((await request.json()) as object) } },
      }),
    });
    await renderAt(openMine);
    await screen.findByRole("table");
    expect(screen.getByLabelText("Views")).toHaveValue(mine.id);
    await userEvent.selectOptions(screen.getByLabelText("Priority"), "must");
    await userEvent.click(within(screen.getByRole("table")).getByRole("button", { name: /Sort by Title/ }));
    await userEvent.click(screen.getByRole("button", { name: "Save changes to this view" }));
    await waitFor(() =>
      expect(sent.find((each) => each.method === "PATCH")?.body).toEqual({
        filter: { ...emptyFilter(), priorities: ["must"] },
        sort: { column: "title", desc: true },
        columns: ["key", "title"],
      }),
    );
    expect(await screen.findByText("Saved the view Mine.")).toBeInTheDocument();
  });

  it("deletes the open view and goes back to all requirements", async () => {
    const sent = stubViews({ "DELETE /views/0195f000-0000-7000-8000-000000000f01": { status: 204 } });
    const router = await renderAt(openMine);
    await screen.findByRole("table");
    await userEvent.click(screen.getByRole("button", { name: "Delete this view" }));
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === `/views/${mine.id}`)).toBe(true));
    expect(await screen.findByText("Deleted the view Mine.")).toBeInTheDocument();
    await waitFor(() => expect(router.state.location.search).toEqual({}));
    expect(screen.getByLabelText("Views")).toHaveValue("");
    expect(screen.getByRole("option", { name: "All requirements" })).toBeInTheDocument();
  });

  it("says why a change or a delete of the view was refused", async () => {
    stubViews({
      "PATCH /views/0195f000-0000-7000-8000-000000000f01": forbidden("Only the project's editors change a shared view."),
      "DELETE /views/0195f000-0000-7000-8000-000000000f01": forbidden("Only a view's owner changes it."),
    });
    const router = await renderAt(openMine);
    await screen.findByRole("table");
    await userEvent.click(screen.getByRole("button", { name: "Save changes to this view" }));
    expect(await screen.findByText("Only the project's editors change a shared view.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Delete this view" }));
    expect(await screen.findByText("Only a view's owner changes it.")).toBeInTheDocument();
    expect(router.state.location.search).toMatchObject({ view: mine.id });
    expect(screen.queryByText("Deleted the view Mine.")).toBeNull();
  });

  it("keeps sharing to the project's editors, and says so", async () => {
    stubViews({}, aProject({ roles: ["viewer"], can: { view: true, comment: true, edit: false, approve: false, administer: false, purge: false } }));
    await renderAt("/p/CHK/grid");
    await screen.findByRole("table");
    await userEvent.click(screen.getByRole("button", { name: "Save as a view" }));
    const dialog = screen.getByRole("dialog", { name: "Save as a view" });
    expect(within(dialog).getByRole("checkbox", { name: "Share with everybody in the project" })).toBeDisabled();
    expect(within(dialog).getByText("Only the project's editors share views.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });
});
