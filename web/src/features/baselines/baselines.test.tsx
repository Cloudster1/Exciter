import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Baseline, BaselineComparison } from "@/api/baselines";
import type { TreeNode } from "@/api/tree";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const id = "0195f000-0000-7000-8000-0000000000b1";
const laterId = "0195f000-0000-7000-8000-0000000000b2";

function aBaseline(over: Partial<Baseline> = {}): Baseline {
  return {
    id,
    projectKey: "CHK",
    name: "Release 1.0",
    description: "As agreed with the customer.",
    root: null,
    items: 3,
    locked: false,
    lockedAt: null,
    createdByName: "Ada Lovelace",
    createdAt: "2026-10-01T08:00:00Z",
    ...over,
  };
}

const section: TreeNode = {
  id: "0195f000-0000-7000-8000-0000000000a2",
  key: "CHK-R2",
  kind: "section",
  title: "Braking",
  parentId: null,
  depth: 0,
  outline: "2",
  typeId: null,
  status: null,
  revision: 1,
  children: 1,
};

const ref = (key: string, title: string, revision = 1, kind: "requirement" | "section" = "requirement") => ({
  id: `0195f000-0000-7000-8000-0000000000${key.slice(-1)}0`,
  key,
  projectKey: "CHK",
  kind,
  title,
  revision,
  status: null,
  trashed: false,
});
const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

describe("a project's baselines", () => {
  it("are listed newest first, and an administrator takes one of a section", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/baselines": { status: 200, body: { baselines: [aBaseline({ locked: true, lockedAt: "2026-10-02T08:00:00Z" })] } },
      "GET /projects/CHK/tree": { status: 200, body: { nodes: [section] } },
      "POST /projects/CHK/baselines": {
        status: 201,
        body: { baseline: aBaseline({ id: laterId, name: "Braking 1.0", root: ref("CHK-R2", "Braking", 1, "section") }) },
      },
    });
    await renderAt("/p/CHK/baselines");
    const list = await screen.findByRole("list", { name: "Baselines" });
    expect(within(list).getByRole("link", { name: "Release 1.0" })).toHaveAttribute("href", `/p/CHK/baselines/${id}`);
    expect(list).toHaveTextContent("Locked");
    expect(list).toHaveTextContent("3 requirements");
    await userEvent.click(screen.getByRole("button", { name: "Take baseline" }));
    const form = screen.getByRole("form", { name: "Take a baseline" });
    await userEvent.type(within(form).getByLabelText("Name"), "Braking 1.0");
    await userEvent.selectOptions(await within(form).findByLabelText("Of"), "CHK-R2");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(form).getByRole("button", { name: "Take" }));
    await waitFor(() => expect(sent.find((each) => each.method === "POST")?.body).toEqual({ name: "Braking 1.0", rootKey: "CHK-R2" }));
    expect(await screen.findByText("The baseline Braking 1.0 is taken.")).toBeInTheDocument();
  });

  it("offer a reader nothing to take", async () => {
    stubApi({
      "GET /projects/CHK": {
        status: 200,
        body: { project: aProject({ can: { view: true, comment: false, edit: false, approve: false, administer: false, purge: false } }) },
      },
      "GET /projects/CHK/baselines": { status: 200, body: { baselines: [] } },
    });
    await renderAt("/p/CHK/baselines");
    expect(await screen.findByText("No baseline yet.")).toBeInTheDocument();
    expect(screen.getByText("Only the project's administrators take, change and delete baselines.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Take baseline" })).toBeNull();
  });
});

describe("a baseline", () => {
  function stubBaseline(baseline = aBaseline(), more: Parameters<typeof stubApi>[0] = {}) {
    return stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      [`GET /baselines/${id}`]: { status: 200, body: { baseline } },
      "GET /projects/CHK/baselines": { status: 200, body: { baselines: [aBaseline({ id: laterId, name: "Release 1.1" }), baseline] } },
      [`GET /baselines/${id}/spec`]: {
        status: 200,
        body: {
          entries: [
            {
              requirement: ref("CHK-R1", "Brakes engage within 100 ms"),
              outline: "1",
              depth: 0,
              statement: paragraph("Within 100 ms."),
              rationale: null,
              status: null,
            },
            { requirement: ref("CHK-R2", "", 2), outline: "2", depth: 0, statement: null, rationale: null, status: null },
          ],
          total: 2,
          limit: 50,
          offset: 0,
        },
      },
      ...more,
    });
  }

  it("reads as a specification at the revisions it holds, and its administrators lock it", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const sent = stubBaseline(aBaseline(), { [`PATCH /baselines/${id}`]: { status: 200, body: { baseline: aBaseline({ locked: true }) } } });
    await renderAt(`/p/CHK/baselines/${id}`);
    const spec = await screen.findByRole("list", { name: "Release 1.0" });
    const first = within(spec).getAllByRole("listitem")[0]!;
    expect(first).toHaveTextContent("1 Brakes engage within 100 ms");
    expect(first).toHaveTextContent("Revision 1");
    expect(first).toHaveTextContent("Within 100 ms.");
    expect(within(spec).getAllByRole("listitem")[1]).toHaveTextContent("Deleted for good since");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Lock" }));
    await waitFor(() => expect(sent.find((each) => each.method === "PATCH")?.body).toEqual({ locked: true }));
  });

  it("is compared with another baseline of the project, picked by its name", async () => {
    stubBaseline();
    const router = await renderAt(`/p/CHK/baselines/${id}`);
    await screen.findByRole("option", { name: "Release 1.1" });
    await userEvent.selectOptions(screen.getByLabelText("Compare with"), "Release 1.1");
    await userEvent.click(screen.getByRole("button", { name: "Compare" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/p/CHK/baselines/${id}/compare`));
    expect(router.state.location.search).toEqual({ with: laterId });
  });

  it("once locked offers nothing to change", async () => {
    stubBaseline(aBaseline({ locked: true, lockedAt: "2026-10-02T08:00:00Z" }));
    await renderAt(`/p/CHK/baselines/${id}`);
    expect(await screen.findByText("A locked baseline keeps its name and is never deleted.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Lock" })).toBeNull();
  });
});

describe("a comparison", () => {
  const comparison: BaselineComparison = {
    from: { baseline: aBaseline(), at: "2026-10-01T08:00:00Z" },
    to: { baseline: null, at: "2026-10-05T08:00:00Z" },
    items: [
      {
        key: "CHK-R3",
        change: "changed",
        revised: true,
        moved: true,
        statusChanged: false,
        from: { revision: 1, title: "Brake lights", outline: "2.1", status: null },
        to: { revision: 3, title: "Brake lights follow", outline: "2.2", status: null },
      },
      {
        key: "CHK-R4",
        change: "added",
        revised: false,
        moved: false,
        statusChanged: false,
        from: null,
        to: { revision: 1, title: "Fluid", outline: "2.3", status: null },
      },
      {
        key: "CHK-R1",
        change: "removed",
        revised: false,
        moved: false,
        statusChanged: false,
        from: { revision: 1, title: "Engage", outline: "1", status: null },
        to: null,
      },
    ],
    links: [{ change: "removed", typeName: "Derives", sourceKey: "CHK-R3", targetKey: "CHK-R1" }],
    externalLinks: [],
  };

  it("says what was added, removed and changed, each change opening its diff", async () => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      [`GET /baselines/${id}/compare`]: { status: 200, body: { comparison } },
    });
    await renderAt(`/p/CHK/baselines/${id}/compare?with=now`);
    const items = await screen.findByRole("list", { name: "Requirements" });
    const r3 = items.querySelector('[data-item-change="CHK-R3"]') as HTMLElement;
    expect(r3).toHaveTextContent("Revision 1 to 3");
    expect(r3).toHaveTextContent("Moved from 2.1 to 2.2");
    expect(within(r3).getByRole("link", { name: "Show the change" })).toHaveAttribute("href", "/p/CHK/r/CHK-R3/history?from=1&to=3");
    expect(items.querySelector('[data-item-change="CHK-R4"]')).toHaveTextContent("Added");
    expect(items.querySelector('[data-item-change="CHK-R1"]')).toHaveTextContent("Removed");
    expect(screen.getByRole("list", { name: "Trace links" })).toHaveTextContent("CHK-R3 derives CHK-R1");
    expect(await axeViolations()).toEqual([]);
  });

  it("says so when nothing differs", async () => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      [`GET /baselines/${id}/compare`]: { status: 200, body: { comparison: { ...comparison, items: [], links: [] } } },
    });
    await renderAt(`/p/CHK/baselines/${id}/compare`);
    expect(await screen.findByText("Nothing differs.")).toBeInTheDocument();
  });
});
