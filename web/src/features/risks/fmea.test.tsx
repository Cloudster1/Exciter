import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Mitigations, RiskMatrix, RiskRow, RiskScheme } from "@/api/risks";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { defaultScheme } from "./level";

afterEach(() => vi.unstubAllGlobals());

const hazardId = "0195f000-0000-7000-8000-00000000d0b1";
const high = { name: "High", colour: "#c2410c", acceptable: false };
const medium = { name: "Medium", colour: "#d97706", acceptable: false };

function scheme(): RiskScheme {
  const input = defaultScheme();
  return {
    ...input,
    scales: input.scales.map((s) => ({ ...s, held: [] })),
    updatedBy: { id: "0195f000-0000-7000-8000-0000000000a1", name: "Alice" },
    updatedAt: "2026-10-19T08:00:00Z",
  };
}

const view = {
  before: { severity: 4, probability: 3, detection: null },
  after: { severity: 2, probability: 3, detection: null },
  beforeRpn: 12,
  afterRpn: 6,
  beforeLevel: high,
  afterLevel: medium,
  state: "claimed" as const,
  level: high,
};

const mitigations: Mitigations = {
  state: "claimed",
  mitigations: [
    {
      linkId: "0195f000-0000-7000-8000-00000000e0a1",
      source: { id: "0195f000-0000-7000-8000-00000000e0b1", key: "CHK-R1", title: "Stop on ice", projectKey: "CHK", hidden: false },
      implemented: "na",
      verified: "none",
      suspect: false,
      inEffect: false,
    },
  ],
};

const row: RiskRow = {
  risk: {
    id: "0195f000-0000-7000-8000-00000000e0c1",
    key: "CHK-R2",
    title: "Brakes fail on ice",
    revision: 1,
    type: { id: hazardId, name: "Hazard", icon: "risk", colour: "#c2410c" },
  },
  attributes: {},
  rating: view,
  mitigations: mitigations.mitigations,
  can: { edit: true },
};

function matrix(): RiskMatrix {
  const s = scheme();
  const cells = [];
  for (let probability = 1; probability <= 5; probability++) {
    for (let severity = 1; severity <= 5; severity++) {
      cells.push({ severity, probability, count: severity === 4 && probability === 3 ? 1 : 0, level: severity * probability >= 12 ? high : null });
    }
  }
  return { side: "effective", severity: s.scales[0]!, probability: s.scales[1]!, cells, unrated: 1 };
}

const stubs = {
  "GET /projects/CHK": { status: 200, body: { project: aProject() } },
  "GET /projects/CHK/risk-scheme": { status: 200, body: { scheme: scheme() } },
  "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
  "GET /projects/CHK/types": { status: 200, body: { types: [] } },
  "GET /projects/CHK/risks": { status: 200, body: { risks: [row], total: 1, unrated: 1 } },
  "GET /projects/CHK/risk-matrix": { status: 200, body: { matrix: matrix() } },
};

describe("a risk's mitigations", () => {
  it("list each with whether it is in effect, and the state they put the risk in", async () => {
    stubApi({
      ...stubs,
      "GET /requirements/CHK-R2": { status: 200, body: { requirement: aRequirement({ key: "CHK-R2", title: "Brakes fail on ice", risk: view }) } },
      "GET /requirements/CHK-R2/mitigations": { status: 200, body: mitigations },
    });
    await renderAt("/p/CHK/r/CHK-R2");
    const panel = await screen.findByRole("region", { name: /Mitigations/ });
    expect(await within(panel).findByText("Claimed")).toBeInTheDocument();
    expect(within(panel).getByRole("link", { name: "CHK-R1 Stop on ice" })).toBeInTheDocument();
    expect(within(panel).getByText("Not in effect")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });
});

describe("the FMEA table", () => {
  it("lists the risks and rates one in place over the revision it read", async () => {
    const sent = stubApi({ ...stubs, "PATCH /requirements/CHK-R2": { status: 200, body: { requirement: aRequirement({ key: "CHK-R2", revision: 2 }) } } });
    await renderAt("/p/CHK/risks");
    const table = await screen.findByRole("table", { name: "FMEA table" });
    expect(within(table).getByRole("link", { name: "CHK-R1" })).toBeInTheDocument();
    expect(screen.getByText("1 risk is not rated yet.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.selectOptions(within(table).getByRole("combobox", { name: "Severity after mitigation of CHK-R2" }), "1");
    await waitFor(() => expect(sent.some((r) => r.method === "PATCH")).toBe(true));
    expect(sent.find((r) => r.method === "PATCH")?.body).toEqual({
      risk: { before: view.before, after: { severity: 1, probability: 3, detection: null } },
      baseRevision: 1,
    });
  });
});

describe("the risk matrix", () => {
  it("counts the risks in each cell and opens a cell's risks", async () => {
    const sent = stubApi(stubs);
    await renderAt("/p/CHK/risks?view=matrix");
    const grid = await screen.findByRole("table", { name: "Risk matrix" });
    await userEvent.click(within(grid).getByRole("button", { name: "1 risk at severity 4 and probability 3" }));
    const risks = await screen.findByRole("region", { name: "Risks at severity 4 and probability 3" });
    expect(within(risks).getByRole("link", { name: "CHK-R2" })).toBeInTheDocument();
    expect(sent.some((r) => r.path === "/projects/CHK/risks")).toBe(true);
    expect(await axeViolations()).toEqual([]);
  });
});
