import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { TraceChains, TraceModel } from "@/api/trace";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { derives, verifies } from "@/test/links";
import { aProject } from "@/test/projects";
import { levelRange, readChainsAddress } from "./chainsAddress";

// A project's trace model and its chains (#151).

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const NEED = "0195f000-0000-7000-8000-0000000003a1";
const SYSTEM = "0195f000-0000-7000-8000-0000000003a2";
const type = (id: string, name: string) => ({ id, name, icon: "test", colour: "#2255aa", retired: false });
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

const unset: TraceModel = { levels: [], updatedAt: null, updatedBy: null, can: { edit: true } };
const model: TraceModel = {
  levels: [
    { name: "Need", kind: "requirements", types: [{ id: NEED, name: "Need", icon: "test", colour: "#2255aa" }], steps: [] },
    {
      name: "System",
      kind: "requirements",
      types: [{ id: SYSTEM, name: "System", icon: "test", colour: "#2255aa" }],
      steps: [{ typeId: derives.id, typeName: "Derives", direction: "incoming" }],
    },
    { name: "Issue", kind: "issues", types: [], steps: [{ relation: "implements" }] },
  ],
  updatedAt: "2026-10-16T08:00:00Z",
  updatedBy: { id: "0195f000-0000-7000-8000-0000000000aa", name: "Ada Lovelace" },
  can: { edit: true },
};

function stubSettings(answer: TraceModel, more: Parameters<typeof stubApi>[0] = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /projects/CHK/types": { status: 200, body: { types: [type(NEED, "Need"), type(SYSTEM, "System")] } },
    "GET /projects/CHK/statuses": { status: 200, body: { statuses: [] } },
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
    "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [derives, verifies] } },
    "GET /projects/CHK/trace-model": { status: 200, body: { model: answer } },
    ...more,
  });
}

describe("the trace model", () => {
  it("is built level by level and saved whole", async () => {
    const sent = stubSettings(unset, { "PUT /projects/CHK/trace-model": { status: 200, body: { model } } });
    await renderAt("/p/CHK/settings?tab=trace");
    expect(await screen.findByText("No trace model yet. Add the first level, such as Stakeholder.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Add a level" }));
    await userEvent.type(screen.getByLabelText("Name"), "Need");
    await userEvent.click(within(screen.getByRole("group", { name: "Types: Need" })).getByRole("button", { name: "Need" }));
    await userEvent.click(screen.getByRole("button", { name: "Add a level" }));
    await userEvent.type(screen.getAllByLabelText("Name")[1]!, "System");
    await userEvent.click(within(screen.getByRole("group", { name: "Types: System" })).getByRole("button", { name: "System" }));
    await userEvent.click(
      within(screen.getByRole("group", { name: "Reached from the level before by: System" })).getByRole("button", { name: "Is derived into" }),
    );
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Save the model" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "PUT")?.body).toEqual({
        levels: [
          { name: "Need", kind: "requirements", typeIds: [NEED], steps: [] },
          { name: "System", kind: "requirements", typeIds: [SYSTEM], steps: [{ typeId: derives.id, direction: "incoming" }] },
        ],
      }),
    );
    expect(await screen.findByText("The trace model is saved.")).toBeInTheDocument();
  });

  it("says a refusal beside the level it is about", async () => {
    stubSettings(model, {
      "PUT /projects/CHK/trace-model": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Check the fields.",
            fields: { "levels[1].steps": "Pick the links that lead to this level from the one before." },
          },
        },
      },
    });
    await renderAt("/p/CHK/settings?tab=trace");
    await userEvent.click(await screen.findByRole("button", { name: "Save the model" }));
    const level = document.querySelector('[data-trace-level="1"]') as HTMLElement;
    expect(await within(level).findByText("Pick the links that lead to this level from the one before.")).toBeInTheDocument();
  });

  it("is read-only for anybody who does not administer the project", async () => {
    stubSettings({ ...model, can: { edit: false } });
    await renderAt("/p/CHK/settings?tab=trace");
    expect(await screen.findByText("Only the project's administrators set its trace model.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save the model" })).toBeNull();
    expect(screen.getAllByLabelText("Name")[0]).toBeDisabled();
  });
});

const chains: TraceChains = {
  levels: model.levels,
  roots: [ref(1, "Stop within 40 m"), ref(2, "Sound a horn")],
  lines: [
    {
      root: 0,
      nodes: [
        { requirement: ref(1, "Stop within 40 m"), page: null, suspect: false },
        {
          requirement: ref(3, "Brakes engage within 100 ms"),
          page: null,
          via: "is derived into",
          suspect: true,
          lastRun: { outcome: "failed", executionId: "0195f000-0000-7000-8000-0000000003e1", outdated: true },
        },
        { requirement: null, issueKey: "WEB-9", page: null, via: "implements", suspect: false },
      ],
      broken: false,
    },
    { root: 1, nodes: [{ requirement: ref(2, "Sound a horn"), page: null, suspect: false }], broken: true },
  ],
  broken: 1,
  total: 2,
  limit: 50,
  offset: 0,
};

describe("the chains", () => {
  it("lay every path out and mark where one breaks, with the levels and breaks in the address", async () => {
    const asked: URLSearchParams[] = [];
    stubSettings(model, {
      "GET /projects/CHK/trace-chains": (request) => {
        asked.push(new URL(request.url).searchParams);
        return { status: 200, body: { chains } };
      },
    });
    const router = await renderAt("/p/CHK/chains");
    const table = await screen.findByRole("table", { name: "Chains" });
    expect(
      within(table)
        .getAllByRole("columnheader")
        .map((h) => h.textContent),
    ).toEqual(["Need", "System", "Issue"]);
    expect(table.querySelectorAll("[data-break]")).toHaveLength(1);
    expect(within(table).getByText("WEB-9")).toBeInTheDocument();
    // A test case carries its last result, marked when it ran an older revision (#252).
    const run = table.querySelector('[data-last-run="failed"]') as HTMLElement;
    expect(run).toHaveTextContent("Failed");
    expect(within(run).getByRole("img", { name: "The last result ran an older revision" })).toBeInTheDocument();
    expect(screen.getByText("2 chains on this page, 1 broken")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.selectOptions(screen.getByLabelText("To level"), "1");
    await userEvent.click(screen.getByLabelText("Only broken chains"));
    await waitFor(() => expect(router.state.location.search).toEqual({ level: ["0", "1"], onlyBroken: true }));
    expect(asked.at(-1)?.getAll("level")).toEqual(["0", "1"]);
    expect(asked.at(-1)?.get("onlyBroken")).toBe("true");
    expect(screen.getByRole("link", { name: "Download CSV" }).getAttribute("href")).toContain("/projects/CHK/trace-chains.csv?level=0&level=1&onlyBroken=true");
    expect(screen.getByRole("link", { name: "Download Excel" }).getAttribute("href")).toContain(
      "/projects/CHK/trace-chains.xlsx?level=0&level=1&onlyBroken=true",
    );
  });

  it("send an administrator to set a model when there is none", async () => {
    stubSettings(unset, {
      "GET /projects/CHK/trace-chains": { status: 200, body: { chains: { ...chains, levels: [], roots: [], lines: [], broken: 0, total: 0 } } },
    });
    await renderAt("/p/CHK/chains");
    expect(await screen.findByText("This project has no trace model, so there are no chains to follow.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Set the trace model" })).toHaveAttribute("href", "/p/CHK/settings?tab=trace");
  });
});

describe("a chain report's address", () => {
  it("shows every level as none, and a range otherwise", () => {
    expect(levelRange(0, 2, 3)).toEqual([]);
    expect(levelRange(2, 1, 3)).toEqual([1, 2]);
    expect(readChainsAddress({ level: ["1", "x"], onlyBroken: "true" })).toEqual({ level: ["1"], onlyBroken: true });
  });
});
