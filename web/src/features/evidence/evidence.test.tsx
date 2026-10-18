import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { TestResult } from "@/api/testResults";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

// Test results from CI on a requirement and the branch whose results count (#200).

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function aResult(over: Partial<TestResult> = {}): TestResult {
  return {
    test: "engage within 100 ms",
    suite: "brakes",
    repository: "acme/checkout",
    branch: "main",
    job: "",
    outcome: "passed",
    message: "",
    durationMs: 4,
    revision: 1,
    outdated: false,
    counts: true,
    commit: "4f2c9a1e7b3d",
    runId: "0195f000-0000-7000-8000-0000000000e1",
    runUrl: "https://ci.example/runs/7",
    at: "2026-10-17T10:00:00Z",
    armature: null,
    ...over,
  };
}

function stubRequirement(results: TestResult[]) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement: aRequirement() } },
    "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
    "GET /requirements/CHK-R1/test-results": { status: 200, body: { results, countingBranch: "main" } },
  });
}

describe("a requirement's test results", () => {
  it("lists the counting branch first, each result with its commit and run, an outdated one marked", async () => {
    stubRequirement([
      aResult({ armature: { issueKey: "CP-1", url: "https://armature.example/commit/4f2c9a1" } }),
      aResult({ test: "lights follow the pedal", outcome: "failed", message: "lights stayed off", revision: 1, outdated: true }),
      aResult({ branch: "feature/lights", outcome: "failed", counts: false, job: "browser", runUrl: "" }),
    ]);
    await renderAt("/p/CHK/r/CHK-R1");
    const panel = (await screen.findByRole("heading", { name: "Test results" })).closest("[data-test-results]") as HTMLElement;
    const branches = (await within(panel).findAllByRole("heading", { level: 3 })).map((h) => h.textContent);
    expect(branches).toEqual(["On main, which counts", "On feature/lights"]);
    const main = panel.querySelector('[data-test-branch="main"]') as HTMLElement;
    expect(within(main).getByRole("link", { name: "Commit 4f2c9a1" })).toHaveAttribute("href", "https://armature.example/commit/4f2c9a1");
    expect(within(main).getAllByRole("link", { name: "Run" })[0]).toHaveAttribute("href", "https://ci.example/runs/7");
    expect(within(main).getByText("Outdated: run against revision 1")).toBeInTheDocument();
    expect(within(main).getByText("lights stayed off")).toBeInTheDocument();
    const feature = panel.querySelector('[data-test-branch="feature/lights"]') as HTMLElement;
    expect(within(feature).getByText("Job browser")).toBeInTheDocument();
    expect(within(feature).queryByRole("link", { name: "Run" })).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("says how a test names a requirement when none does yet", async () => {
    stubRequirement([]);
    await renderAt("/p/CHK/r/CHK-R1");
    expect(await screen.findByText(/A test names it with \[verifies KEY\] in its name/)).toBeInTheDocument();
  });
});

describe("the counting branch", () => {
  function stubSettings(can = aProject().can) {
    return stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject({ can }) } },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
      "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [] } },
      "GET /projects/CHK/trace-model": { status: 200, body: { model: { levels: [], updatedAt: null, updatedBy: null, can: { edit: true } } } },
      "GET /projects/CHK/evidence-settings": { status: 200, body: { settings: { countingBranch: "main", updatedAt: null } } },
      "PUT /projects/CHK/evidence-settings": { status: 200, body: { settings: { countingBranch: "release", updatedAt: "2026-10-17T10:00:00Z" } } },
    });
  }

  it("is named by an administrator in the project's trace settings", async () => {
    const sent = stubSettings();
    await renderAt("/p/CHK/settings?tab=trace");
    const branch = await screen.findByRole("textbox", { name: "Counting branch" });
    expect(branch).toHaveValue("main");
    expect(await axeViolations()).toEqual([]);
    await userEvent.clear(branch);
    await userEvent.type(branch, "release");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.find((each) => each.method === "PUT")?.body).toEqual({ countingBranch: "release" }));
  });

  it("is only read by somebody who does not administer the project", async () => {
    stubSettings({ view: true, comment: false, edit: false, approve: false, administer: false, purge: false });
    await renderAt("/p/CHK/settings?tab=trace");
    expect(await screen.findByRole("textbox", { name: "Counting branch" })).toBeDisabled();
    expect(screen.getByText("Only the project's administrators name the counting branch.")).toBeInTheDocument();
  });
});
