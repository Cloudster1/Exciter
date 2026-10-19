import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { RiskScheme, RiskSchemeInput } from "@/api/risks";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { defaultScheme, levelOf, rpnOf } from "./level";

afterEach(() => vi.unstubAllGlobals());

const hazardId = "0195f000-0000-7000-8000-00000000d0a1";
const hazard = {
  id: hazardId,
  name: "Hazard",
  description: "",
  icon: "risk",
  colour: "#c2410c",
  position: 1,
  workflowId: null,
  role: "risk" as const,
  roleHeld: true,
  retired: false,
  requirements: 1,
  createdAt: "2026-10-19T08:00:00Z",
  updatedAt: "2026-10-19T08:00:00Z",
};

/** The scheme a project starts from, as the server answers it once saved. */
function savedScheme(): RiskScheme {
  const input = defaultScheme();
  return {
    ...input,
    scales: input.scales.map((s) => ({ ...s, held: s.dimension === "severity" ? [4] : [3] })),
    updatedBy: { id: "0195f000-0000-7000-8000-0000000000a1", name: "Alice" },
    updatedAt: "2026-10-19T08:00:00Z",
  };
}

const ice = aRequirement({
  key: "CHK-R2",
  title: "Brakes fail on ice",
  type: { id: hazardId, name: "Hazard", icon: "risk", colour: "#c2410c" },
  risk: {
    before: { severity: 4, probability: 3, detection: null },
    after: { severity: 2, probability: 3, detection: null },
    beforeRpn: 12,
    afterRpn: 6,
    beforeLevel: { name: "High", colour: "#c2410c", acceptable: false },
    afterLevel: { name: "Medium", colour: "#d97706", acceptable: false },
    state: "unmitigated",
    level: { name: "High", colour: "#c2410c", acceptable: false },
  },
});

const stubs = {
  "GET /projects/CHK": { status: 200, body: { project: aProject() } },
  "GET /requirements/CHK-R2": { status: 200, body: { requirement: ice } },
  "GET /projects/CHK/types": { status: 200, body: { types: [hazard] } },
  "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
  "GET /projects/CHK/risk-scheme": { status: 200, body: { scheme: savedScheme() } },
};

describe("a risk's level", () => {
  it("follows from the matrix, or from the priority number and its thresholds", () => {
    const scheme = defaultScheme();
    expect(levelOf(scheme, { severity: 4, probability: 3, detection: null })?.name).toBe("High");
    expect(levelOf(scheme, { severity: 1, probability: 4, detection: null })?.name).toBe("Low");
    expect(levelOf(scheme, { severity: 4, probability: null, detection: null })).toBeNull();
    const rpn = { ...scheme, method: "rpn" as const, detection: true, levels: scheme.levels.map((l, i) => ({ ...l, minRpn: [null, 20, 60][i]! })) };
    expect(rpnOf(rpn, { severity: 5, probability: 4, detection: 3 })).toBe(60);
    expect(levelOf(rpn, { severity: 5, probability: 4, detection: 3 })?.name).toBe("High");
    expect(levelOf(rpn, { severity: 5, probability: 4, detection: null })).toBeNull();
  });
});

describe("a risk", () => {
  it("shows its rating with the level of each side", async () => {
    stubApi(stubs);
    await renderAt("/p/CHK/r/CHK-R2");
    const rating = await screen.findByRole("table", { name: "Risk rating" });
    expect(within(rating).getByRole("row", { name: /Before mitigation 4 3 12 High/ })).toBeInTheDocument();
    expect(within(rating).getByRole("row", { name: /After mitigation 2 3 6 Medium/ })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("is rated by picking values, the level showing as they are picked", async () => {
    const sent = stubApi({ ...stubs, "PATCH /requirements/CHK-R2": { status: 200, body: { requirement: { ...ice, revision: 2 } } } });
    await renderAt("/p/CHK/r/CHK-R2");
    await userEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const rating = await screen.findByRole("group", { name: "Risk rating" });
    await userEvent.selectOptions(within(rating).getByRole("combobox", { name: "Severity after mitigation" }), "1");
    expect(within(rating).getByRole("row", { name: /After mitigation .* 3 Low/ })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.some((r) => r.method === "PATCH")).toBe(true));
    const body = sent.find((r) => r.method === "PATCH")?.body as { risk: unknown };
    expect(body.risk).toEqual({ before: { severity: 4, probability: 3, detection: null }, after: { severity: 1, probability: 3, detection: null } });
  });
});

describe("the risk scheme", () => {
  it("starts from five by five and is saved whole", async () => {
    const sent = stubApi({
      ...stubs,
      "GET /projects/CHK/risk-scheme": { status: 200, body: { scheme: null } },
      "PUT /projects/CHK/risk-scheme": { status: 200, body: { scheme: savedScheme() } },
    });
    await renderAt("/p/CHK/settings?tab=risks");
    await userEvent.click(await screen.findByRole("button", { name: "Start from five by five" }));
    const matrix = screen.getByRole("table", { name: "Matrix" });
    await userEvent.selectOptions(within(matrix).getByRole("combobox", { name: "Level at severity 5 and probability 1" }), "High");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Save the scheme" }));
    await waitFor(() => expect(sent.some((r) => r.method === "PUT")).toBe(true));
    const body = sent.find((r) => r.method === "PUT")?.body as RiskSchemeInput;
    expect(body.matrix).toHaveLength(25);
    expect(body.matrix.find((c) => c.severity === 5 && c.probability === 1)?.level).toBe("High");
    expect(await screen.findByText("The risk scheme is saved.")).toBeInTheDocument();
  });

  it("keeps a value a rating uses", async () => {
    stubApi(stubs);
    await renderAt("/p/CHK/settings?tab=risks");
    const severity = await screen.findByRole("group", { name: "Severity" });
    expect(within(severity).getByText("A risk is rated on this value, so it stays.")).toBeInTheDocument();
    const probability = screen.getByRole("group", { name: "Probability" });
    expect(within(probability).getByRole("button", { name: "Remove the highest value of probability" })).toBeEnabled();
  });
});
