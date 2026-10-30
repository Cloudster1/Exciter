import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { TraceGraph } from "@/api/trace";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { derives, verifies } from "@/test/links";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { readGraphAddress } from "./address";
import { layoutGraph, reachedFrom } from "./layout";

// A requirement's trace graph (#211).

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const id = (n: number) => `0195f000-0000-7000-8000-0000000004${String(n).padStart(2, "0")}`;
const req = (n: number, title: string, level: number, more: Partial<TraceGraph["nodes"][number]> = {}): TraceGraph["nodes"][number] => ({
  id: id(n),
  level,
  kind: "requirement",
  requirement: {
    id: id(n),
    key: `CHK-R${n}`,
    projectKey: "CHK",
    kind: "requirement",
    title,
    revision: 1,
    status: { id: id(90), name: "Draft", category: "draft" },
    trashed: false,
  },
  type: { id: id(91), name: "System", icon: "test", colour: "#2255aa" },
  page: null,
  gaps: [],
  broken: false,
  hidden: 0,
  ...more,
});

/** R1 derives from R2; R3 derives from R1, and verifies with a failing test; WEB-9 implements R3, deleted in Armature. */
const graph: TraceGraph = {
  root: id(1),
  direction: "both",
  depth: 2,
  nodes: [
    req(1, "Brakes engage within 100 ms", 0, { hidden: 2 }),
    req(2, "Stop within 40 m", -1, { gaps: ["verified"] }),
    req(3, "Brake bench run", 1),
    { id: "issue:WEB-9", level: 2, kind: "issue", issueKey: "WEB-9", requirement: null, type: null, page: null, gaps: [], broken: true, hidden: 0 },
    {
      id: "test:acme/brakes/bench",
      level: 2,
      kind: "test",
      test: { repository: "acme", suite: "brakes", name: "bench", outcome: "failed", outdated: false },
      requirement: null,
      type: null,
      page: null,
      gaps: [],
      broken: false,
      hidden: 0,
    },
  ],
  edges: [
    { from: id(1), to: id(2), linkId: id(51), typeKey: "derives", phrase: "derives from", suspect: true, broken: false },
    { from: id(3), to: id(1), linkId: id(52), typeKey: "derives", phrase: "derives from", suspect: false, broken: false },
    { from: id(3), to: "issue:WEB-9", linkId: null, relation: "implements", suspect: false, broken: true },
    { from: id(3), to: "test:acme/brakes/bench", linkId: null, relation: "verifies", suspect: false, broken: false },
  ],
  hidden: 2,
  truncated: false,
};

function stubGraph(more: Parameters<typeof stubApi>[0] = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement: aRequirement({ title: "Brakes engage within 100 ms" }) } },
    "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [derives, verifies] } },
    "GET /requirements/CHK-R1/graph": { status: 200, body: { graph } },
    "GET /armature/issues": { status: 200, body: { status: "ok", issues: [{ key: "WEB-9", issue: null }] } },
    ...more,
  });
}

describe("the trace graph", () => {
  it("lays levels out top to bottom, up above the centre, and reads each node from the one it was reached by", () => {
    const layout = layoutGraph(graph);
    const y = (n: string) => layout.places.get(n)!.y;
    expect(y(id(2))).toBeLessThan(y(id(1)));
    expect(y(id(1))).toBeLessThan(y(id(3)));
    expect(y("issue:WEB-9")).toBe(y("test:acme/brakes/bench"));
    expect(layout.shapes.size).toBe(4);
    const reached = reachedFrom(graph);
    expect(reached.get(id(2))?.from.id).toBe(id(1));
    expect(reached.get("issue:WEB-9")?.from.id).toBe(id(3));
    expect(reached.has(id(1))).toBe(false);
  });

  it("reads its address, dropping what cannot be walked", () => {
    expect(readGraphAddress({ direction: "up", depth: "3", type: "derives", relation: ["verifies"], view: "list" })).toEqual({
      direction: "up",
      depth: 3,
      type: ["derives"],
      relation: ["verifies"],
      view: "list",
    });
    expect(readGraphAddress({ direction: "sideways", depth: "9", type: [""] })).toEqual({
      direction: undefined,
      depth: undefined,
      type: undefined,
      relation: undefined,
      view: undefined,
    });
  });

  it("draws every node with its marks, says what the reader may not see, and lists the impact by level", async () => {
    stubGraph();
    await renderAt("/projects/CHK/r/CHK-R1/trace");
    const canvas = await screen.findByRole("group", { name: /The trace graph of CHK-R1/ });
    await waitFor(() => expect(canvas.querySelectorAll("[data-graph-node]")).toHaveLength(5));
    expect(screen.getByText("2 linked requirements you may not see")).toBeInTheDocument();
    const impact = screen.getByRole("region", { name: "Down 2" });
    expect(within(impact).getByText("WEB-9")).toBeInTheDocument();
    expect(within(impact).getByText("Not visible in Armature")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Download as CSV" })).toHaveAttribute("href", "/api/v1/requirements/CHK-R1/graph.csv");
    expect(await axeViolations()).toEqual([]);
  });

  it("asks again when the direction, depth or link types change", async () => {
    const asked: URLSearchParams[] = [];
    stubGraph({
      "GET /requirements/CHK-R1/graph": (request) => {
        asked.push(new URL(request.url).searchParams);
        return { status: 200, body: { graph } };
      },
    });
    await renderAt("/projects/CHK/r/CHK-R1/trace");
    await screen.findByRole("group", { name: /The trace graph of CHK-R1/ });
    await userEvent.selectOptions(screen.getByLabelText("Direction"), "up");
    await userEvent.selectOptions(screen.getByLabelText("Depth"), "4");
    const types = screen.getByText("Link types (2/2)").closest("details")!;
    await userEvent.click(within(types).getByRole("checkbox", { name: "Verifies" }));
    await waitFor(() => expect(screen.getByText("Link types (1/2)")).toBeInTheDocument());
    await waitFor(() => {
      const last = asked.at(-1)!;
      expect([last.get("direction"), last.get("depth"), last.getAll("type")]).toEqual(["up", "4", ["derives"]]);
    });
    expect(asked[0]!.toString()).toBe("");
  });

  it("is listed as nested lists, each node below the one it was reached by, with its marks in words", async () => {
    stubGraph();
    await renderAt("/projects/CHK/r/CHK-R1/trace?view=list");
    const list = await screen.findByRole("list", { name: /The trace graph of CHK-R1/ });
    const r3 = within(list).getByText("CHK-R3").closest("li")!;
    expect(within(r3).getByText("WEB-9")).toBeInTheDocument();
    expect(within(r3).getByText(/Broken/)).toBeInTheDocument();
    expect(within(r3).getByText(/Failing/)).toBeInTheDocument();
    const r2 = within(list).getByText("CHK-R2").closest("li")!;
    expect(within(r2).getByText(/Suspect link, Not covered: Verified/)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("says where a graph too large stops, and offers to zoom and fit it", async () => {
    stubGraph({ "GET /requirements/CHK-R1/graph": { status: 200, body: { graph: { ...graph, truncated: true } } } });
    await renderAt("/projects/CHK/r/CHK-R1/trace");
    const stop = await screen.findByText("The graph stops at 5 nodes. Follow fewer levels or link types to see the rest.");
    expect(stop).toHaveAttribute("role", "status");
    for (const name of ["Zoom in", "Zoom out", "Fit the graph"]) {
      const button = screen.getByRole("button", { name });
      expect(button).toBeEnabled();
      await userEvent.click(button);
    }
    expect(screen.getByRole("group", { name: /The trace graph of CHK-R1/ }).querySelectorAll("[data-graph-node]")).toHaveLength(5);
    expect(await axeViolations()).toEqual([]);
  });

  it("says nothing of a stop when the whole graph is drawn", async () => {
    stubGraph();
    await renderAt("/projects/CHK/r/CHK-R1/trace");
    await screen.findByRole("group", { name: /The trace graph of CHK-R1/ });
    expect(screen.queryByText(/The graph stops at/)).not.toBeInTheDocument();
  });

  it("is reached from the requirement's page", async () => {
    stubGraph({ "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } } });
    await renderAt("/projects/CHK/r/CHK-R1");
    expect(await screen.findByRole("link", { name: "Trace graph" })).toHaveAttribute("href", "/projects/CHK/r/CHK-R1/trace");
  });
});
