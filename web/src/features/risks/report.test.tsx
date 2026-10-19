import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import type { RiskReport, RiskScheme } from "@/api/risks";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { defaultScheme } from "./level";
import { RiskMark } from "./RiskMark";

afterEach(() => {
  vi.unstubAllGlobals();
  document.documentElement.removeAttribute("data-print-ready");
});

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

const report: RiskReport = {
  projectKey: "CHK",
  projectName: "Checkout",
  scheme: scheme(),
  levels: [
    { level: { name: "Low", colour: "#0f9d8a", acceptable: true }, before: 0, effective: 0 },
    { level: medium, before: 0, effective: 1 },
    { level: high, before: 1, effective: 0 },
  ],
  unrated: 1,
  risks: [
    {
      risk: {
        id: "0195f000-0000-7000-8000-00000000f0a1",
        key: "CHK-R2",
        title: "Brakes fail on ice",
        revision: 1,
        type: { id: "0195f000-0000-7000-8000-00000000f0b1", name: "Hazard", icon: "risk", colour: "#c2410c" },
      },
      attributes: {},
      rating: {
        before: { severity: 4, probability: 3, detection: null },
        after: { severity: 2, probability: 3, detection: null },
        beforeRpn: 12,
        afterRpn: 6,
        beforeLevel: high,
        afterLevel: medium,
        state: "mitigated",
        level: medium,
      },
      mitigations: [
        {
          linkId: "0195f000-0000-7000-8000-00000000f0c1",
          source: { id: "0195f000-0000-7000-8000-00000000f0d1", key: "CHK-R1", title: "Stop on ice", projectKey: "CHK", hidden: false },
          implemented: "na",
          verified: "full",
          suspect: false,
          inEffect: true,
        },
      ],
      can: { edit: true },
    },
  ],
};

describe("the risk report", () => {
  it("prints the levels and every risk with its ratings and mitigations", async () => {
    stubApi({ "GET /projects/CHK/risk-report": { status: 200, body: { report } } });
    await renderAt("/print/risks/CHK", { ready: "[data-print-view], [data-print-failed]" });
    const risk = await screen.findByRole("heading", { name: "CHK-R2 Brakes fail on ice" });
    expect(risk).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "CHK-R1 Stop on ice" })).toBeInTheDocument();
    expect(screen.getByText("1 risk is not rated yet.")).toBeInTheDocument();
    await waitFor(() => expect(document.documentElement).toHaveAttribute("data-print-ready"), { timeout: 3000 });
    expect(await axeViolations()).toEqual([]);
  });
});

describe("a risk's mark", () => {
  it("shows the level it stands at and its state", () => {
    render(<RiskMark mark={{ level: high, state: "claimed" }} />);
    const mark = document.querySelector("[data-risk-mark]") as HTMLElement;
    expect(within(mark).getByText("High")).toBeInTheDocument();
    expect(within(mark).getByText("Claimed")).toBeInTheDocument();
  });
});
