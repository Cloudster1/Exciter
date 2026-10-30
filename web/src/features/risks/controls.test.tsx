import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { RiskMatrix, RiskRow, RiskScheme, RiskSchemeInput } from "@/api/risks";
import { renderAt, stubApi, LONG_TEST_MS } from "@/test/app";
import { aProject } from "@/test/projects";
import { RISK_DEFAULT_LEVELS, RISK_PAGE_SIZE } from "@/config";
import { defaultScheme } from "./level";

// The controls of the risk scheme, the matrix and the FMEA table that change what they ask the server for (#325).

afterEach(() => vi.unstubAllGlobals());

const high = { name: "High", colour: "#c2410c", acceptable: false };

function savedScheme(): RiskScheme {
  const input = defaultScheme();
  return {
    ...input,
    scales: input.scales.map((s) => ({ ...s, held: [] })),
    updatedBy: { id: "0195f000-0000-7000-8000-0000000000a1", name: "Alice" },
    updatedAt: "2026-10-19T08:00:00Z",
  };
}

const row: RiskRow = {
  risk: {
    id: "0195f000-0000-7000-8000-00000000f0c1",
    key: "CHK-R2",
    title: "Brakes fail on ice",
    revision: 1,
    type: { id: "0195f000-0000-7000-8000-00000000f0d1", name: "Hazard", icon: "risk", colour: "#c2410c" },
  },
  attributes: {},
  rating: {
    before: { severity: 4, probability: 3, detection: null },
    after: { severity: null, probability: null, detection: null },
    beforeRpn: 12,
    afterRpn: null,
    beforeLevel: high,
    afterLevel: null,
    state: "unmitigated",
    level: high,
  },
  mitigations: [],
  can: { edit: true },
};

function matrix(side: "before" | "effective"): RiskMatrix {
  const s = savedScheme();
  const cells = [];
  for (let probability = 1; probability <= 5; probability++) {
    for (let severity = 1; severity <= 5; severity++) {
      cells.push({ severity, probability, count: severity === 4 && probability === 3 ? 1 : 0, level: null });
    }
  }
  return { side, severity: s.scales[0]!, probability: s.scales[1]!, cells, unrated: 0 };
}

const stubs = {
  "GET /projects/CHK": { status: 200, body: { project: aProject() } },
  "GET /projects/CHK/risk-scheme": { status: 200, body: { scheme: savedScheme() } },
  "GET /projects/CHK/types": { status: 200, body: { types: [] } },
  "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
  "GET /projects/CHK/risks": { status: 200, body: { risks: [row], total: 1, unrated: 0 } },
};

/** The scheme the form sent when saved. */
async function savedBody(sent: { method: string; body: unknown }[]): Promise<RiskSchemeInput> {
  await userEvent.click(screen.getByRole("button", { name: "Save the scheme" }));
  await waitFor(() => expect(sent.some((r) => r.method === "PUT")).toBe(true));
  return sent.find((r) => r.method === "PUT")!.body as RiskSchemeInput;
}

describe("the risk scheme's controls", () => {
  it(
    "by priority number asks a threshold of every level but the first and has no matrix",
    async () => {
      const sent = stubApi({ ...stubs, "PUT /projects/CHK/risk-scheme": { status: 200, body: { scheme: savedScheme() } } });
      await renderAt("/projects/CHK/settings/risks");
      expect(await screen.findByRole("table", { name: "Matrix" })).toBeInTheDocument();
      await userEvent.selectOptions(screen.getByRole("combobox", { name: "A rating gives its level by" }), "rpn");
      expect(screen.queryByRole("table", { name: "Matrix" })).toBeNull();
      expect(screen.queryByLabelText("Low from priority number")).toBeNull();
      expect(screen.getByLabelText("Medium from priority number")).toHaveValue(RISK_DEFAULT_LEVELS[1].from);
      expect(screen.getByLabelText("High from priority number")).toHaveValue(RISK_DEFAULT_LEVELS[2].from);
      const body = await savedBody(sent);
      expect(body.method).toBe("rpn");
      expect(body.matrix).toEqual([]);
      expect(body.levels.map((l) => l.minRpn)).toEqual([null, RISK_DEFAULT_LEVELS[1].from, RISK_DEFAULT_LEVELS[2].from]);
    },
    LONG_TEST_MS,
  );

  it(
    "rates detection too once it is ticked",
    async () => {
      const sent = stubApi({ ...stubs, "PUT /projects/CHK/risk-scheme": { status: 200, body: { scheme: savedScheme() } } });
      await renderAt("/projects/CHK/settings/risks");
      expect(screen.queryByRole("group", { name: "Detection" })).toBeNull();
      await userEvent.click(await screen.findByRole("checkbox", { name: "Rate detection too" }));
      const detection = screen.getByRole("group", { name: "Detection" });
      expect(within(detection).getByLabelText("Detection 5")).toHaveValue("Detection 5");
      const body = await savedBody(sent);
      expect(body.detection).toBe(true);
      expect(body.scales.map((s) => s.dimension)).toEqual(["severity", "probability", "detection"]);
    },
    LONG_TEST_MS,
  );

  it(
    "gives the cells of a removed level to the first level",
    async () => {
      const sent = stubApi({ ...stubs, "PUT /projects/CHK/risk-scheme": { status: 200, body: { scheme: savedScheme() } } });
      await renderAt("/projects/CHK/settings/risks");
      const cell = await screen.findByRole("combobox", { name: "Level at severity 5 and probability 5" });
      expect(cell).toHaveValue("High");
      await userEvent.click(screen.getByRole("button", { name: "Remove the level High" }));
      expect(cell).toHaveValue("Low");
      expect(within(cell).queryByRole("option", { name: "High" })).toBeNull();
      const body = await savedBody(sent);
      expect(body.levels.map((l) => l.name)).toEqual(["Low", "Medium"]);
      expect(body.matrix.some((c) => c.level === "High")).toBe(false);
      expect(body.matrix).toHaveLength(25);
    },
    LONG_TEST_MS,
  );

  it("shows a refused level's name beside it and a refused matrix with the form", async () => {
    const name = "Two levels have this name. Name each its own.";
    const cells = "Give every cell of severity and probability its level.";
    stubApi({
      ...stubs,
      "PUT /projects/CHK/risk-scheme": {
        status: 422,
        body: { error: { code: "validation_failed", message: "Check the scheme.", fields: { "levels[1].name": name, matrix: cells } } },
      },
    });
    await renderAt("/projects/CHK/settings/risks");
    await userEvent.click(await screen.findByRole("button", { name: "Save the scheme" }));
    expect(await screen.findByLabelText("Name of level 2")).toHaveAccessibleDescription(name);
    expect(screen.getByLabelText("Name of level 1")).not.toHaveAccessibleDescription(name);
    expect(screen.getByRole("alert")).toHaveTextContent(cells);
  });
});

describe("the risk matrix's controls", () => {
  it("asks for the side picked and closes the cell open", async () => {
    const asked: string[] = [];
    stubApi({
      ...stubs,
      "GET /projects/CHK/risk-matrix": (request) => {
        const side = new URL(request.url).searchParams.get("side") ?? "effective";
        asked.push(side);
        return { status: 200, body: { matrix: matrix(side as "before" | "effective") } };
      },
    });
    await renderAt("/projects/CHK/risks?view=matrix");
    const grid = await screen.findByRole("table", { name: "Risk matrix" });
    await userEvent.click(within(grid).getByRole("button", { name: "1 risk at severity 4 and probability 3" }));
    expect(await screen.findByRole("region", { name: "Risks at severity 4 and probability 3" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Before mitigation" }));
    await waitFor(() => expect(asked).toContain("before"));
    expect(screen.queryByRole("region", { name: "Risks at severity 4 and probability 3" })).toBeNull();
    expect(screen.getByRole("button", { name: "Before mitigation" })).toHaveAttribute("aria-pressed", "true");
  });

  it("shows why a query does not parse", async () => {
    const refusal = "The query stops at bogus. Name a field, then compare it.";
    stubApi({
      ...stubs,
      "GET /projects/CHK/risk-matrix": (request) =>
        new URL(request.url).searchParams.get("query")
          ? { status: 400, body: { error: { code: "bad_query", message: refusal, position: 0 } } }
          : { status: 200, body: { matrix: matrix("effective") } },
    });
    await renderAt("/projects/CHK/risks?view=matrix");
    await screen.findByRole("table", { name: "Risk matrix" });
    const box = screen.getByRole("combobox", { name: "Query" });
    await userEvent.type(box, "bogus{Enter}");
    expect(await screen.findByText(refusal)).toBeInTheDocument();
  });
});

describe("the FMEA table's pages", () => {
  it("asks for the next offset and back", async () => {
    const offsets: string[] = [];
    const total = RISK_PAGE_SIZE * 2 + 1;
    stubApi({
      ...stubs,
      "GET /projects/CHK/risks": (request) => {
        offsets.push(new URL(request.url).searchParams.get("offset") ?? "0");
        return { status: 200, body: { risks: [row], total, unrated: 0 } };
      },
    });
    await renderAt("/projects/CHK/risks");
    await screen.findByRole("table", { name: "FMEA table" });
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(offsets).toContain(String(RISK_PAGE_SIZE)));
    expect(await screen.findByText(`${RISK_PAGE_SIZE + 1} to ${RISK_PAGE_SIZE * 2} of ${total}`)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByText(`${RISK_PAGE_SIZE * 2 + 1} to ${total} of ${total}`)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Previous" }));
    expect(await screen.findByText(`${RISK_PAGE_SIZE + 1} to ${RISK_PAGE_SIZE * 2} of ${total}`)).toBeInTheDocument();
  });
});
