import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ExecutionCase, ExecutionReport, TestExecution, TestPlan } from "@/api/tests";
import { LONG_TEST_MS, renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";

// The dialogs that start an execution and file a failure, and the print view of a report (#324).

afterEach(() => {
  vi.unstubAllGlobals();
  document.documentElement.removeAttribute("data-print-ready");
});

const doc = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
const planId = "0195f000-0000-7000-8000-00000000f0a1";
const executionId = "0195f000-0000-7000-8000-00000000f0b1";
const caseId = "0195f000-0000-7000-8000-00000000f0c1";
const ada = { id: "0195f000-0000-7000-8000-00000000f0d1", name: "Ada Tester", email: "ada.tester@exciter.test" };
const bob = { id: "0195f000-0000-7000-8000-00000000f0d2", name: "Bob Tester", email: "bob.tester@exciter.test" };
const project = { "GET /projects/CHK": { status: 200, body: { project: aProject() } } };

function aPlan(): TestPlan {
  return {
    id: planId,
    projectKey: "CHK",
    title: "Wet road",
    description: "",
    scope: "query",
    query: 'type = "Test case"',
    cases: [{ id: caseId, key: "CHK-R2", title: "Brake on a wet road", trashed: false }],
    createdBy: { id: ada.id, name: ada.name },
    createdAt: "2026-10-18T08:00:00Z",
    updatedAt: "2026-10-18T08:00:00Z",
    can: { edit: true },
  } as TestPlan;
}

const failedRun = {
  case: { id: caseId, key: "CHK-R2", title: "Brake on a wet road", trashed: false },
  iteration: 1,
  revision: 1,
  values: {},
  outcome: "failed" as const,
  note: "",
  source: "tester" as const,
  recordedBy: { id: ada.id, name: ada.name },
  recordedAt: "2026-10-18T10:00:00Z",
  ci: null,
};

function anExecution(over: Partial<TestExecution> = {}): TestExecution {
  return {
    id: executionId,
    projectKey: "CHK",
    title: "Release 1 on wet roads",
    planId,
    planTitle: "Wet road",
    baseline: null,
    version: "2.3",
    configuration: "Winter tyres",
    manager: { id: ada.id, name: ada.name },
    dueAt: null,
    state: "open",
    testers: [{ subjectType: "user", userId: ada.id, name: ada.name }],
    excluded: [],
    progress: { total: 1, passed: 0, failed: 1, blocked: 0, skipped: 0, notRun: 0 },
    cases: [failedRun],
    closeComment: "",
    openedAt: "2026-10-18T08:00:00Z",
    closedAt: null,
    can: { manage: true, record: true },
    ...over,
  } as TestExecution;
}

function aFailedCase(): ExecutionCase {
  return {
    run: failedRun,
    preconditions: null,
    steps: [
      { action: doc("Drive at 50 km/h."), expected: null },
      { action: doc("Press the pedal."), expected: doc("The car stops.") },
      { action: doc("Look at the dashboard."), expected: null },
    ],
    results: [
      { step: 1, outcome: "passed", note: "", recordedBy: null, recordedAt: null },
      { step: 2, outcome: "failed", note: "It slid on.", recordedBy: null, recordedAt: null },
    ],
    files: [],
    issues: [],
    iterations: 1,
    parameters: [],
  } as ExecutionCase;
}

const refusal = (status: number, code: string, message: string, fields?: Record<string, string>) => ({
  status,
  body: { error: { code, message, ...(fields ? { fields } : {}) } },
});

async function openStartDialog() {
  await renderAt(`/projects/CHK/tests/plans/${planId}`);
  await userEvent.click(await screen.findByRole("button", { name: "Start an execution" }));
  return screen.findByRole("dialog", { name: "Start a test execution" });
}

async function addTester(dialog: HTMLElement, typed: string, name: RegExp) {
  await userEvent.type(within(dialog).getByRole("combobox", { name: "Add a tester" }), typed);
  await userEvent.click(await screen.findByRole("option", { name }));
}

describe("starting an execution", () => {
  const stubs = {
    ...project,
    [`GET /test-plans/${planId}`]: { status: 200, body: { plan: aPlan() } },
    "GET /projects/CHK/baselines": { status: 200, body: { baselines: [] } },
    "GET /people": { status: 200, body: { people: [ada, bob] } },
    "GET /groups": { status: 200, body: { groups: [] } },
  };

  it("sends nothing without a tester, and only the testers left after one is removed", { timeout: LONG_TEST_MS }, async () => {
    const sent = stubApi({ ...stubs, "POST /projects/CHK/test-executions": { status: 201, body: { execution: anExecution() } } });
    const dialog = await openStartDialog();
    const send = within(dialog).getByRole("button", { name: "Start" });
    // The button stays enabled, so pressing it says what is missing rather than nothing at all.
    expect(send).toBeEnabled();
    await userEvent.click(send);
    expect(await within(dialog).findByText("Name at least one tester.")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "POST")).toBe(false);

    await addTester(dialog, "ada", /Ada Tester/);
    await addTester(dialog, "bob", /Bob Tester/);
    expect(within(dialog).queryByText("Name at least one tester.")).not.toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Remove Ada Tester" }));
    expect(dialog.querySelector('[data-tester="Ada Tester"]')).toBeNull();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(send);
    await waitFor(() => expect(sent.some((r) => r.method === "POST")).toBe(true));
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ title: "Wet road", planId, testers: [{ subjectType: "user", userId: bob.id }] });
  });

  it("shows each refused field beside it", { timeout: LONG_TEST_MS }, async () => {
    stubApi({
      ...stubs,
      "POST /projects/CHK/test-executions": refusal(422, "validation_failed", "Some fields need attention.", {
        dueAt: "Set the due date ahead, within a year, or leave it out.",
        testers: "Name each tester once.",
        planId: "The plan holds no test case to run here. Add cases to it, or pick another baseline.",
      }),
    });
    const dialog = await openStartDialog();
    await addTester(dialog, "ada", /Ada Tester/);
    await userEvent.type(within(dialog).getByLabelText("Due"), "2020-01-01T10:00");
    await userEvent.click(within(dialog).getByRole("button", { name: "Start" }));
    expect(await within(dialog).findByLabelText("Due")).toHaveAccessibleDescription(/Set the due date ahead, within a year/);
    expect(within(dialog).getByRole("group", { name: "Testers" })).toHaveAccessibleDescription(/Name each tester once\./);
    expect(within(dialog).getByLabelText("Due")).not.toHaveAccessibleDescription(/Name each tester once/);
    // The plan is the page's own and has no control here, so its sentence is the dialog's one banner, and no field reads it.
    expect(within(dialog).getByRole("alert")).toHaveTextContent(/^The plan holds no test case to run here\. Add cases to it, or pick another baseline\.$/);
    expect(within(dialog).getByLabelText("Baseline")).not.toHaveAccessibleDescription(/The plan holds/);
    expect(within(dialog).getByRole("group", { name: "Testers" })).not.toHaveAccessibleDescription(/The plan holds/);
    expect(within(dialog).queryByText("Some fields need attention.")).not.toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });
});

describe("filing a failure", () => {
  const stubs = {
    ...project,
    [`GET /test-executions/${executionId}`]: { status: 200, body: { execution: anExecution() } },
    [`GET /test-executions/${executionId}/cases/CHK-R2`]: { status: 200, body: { case: aFailedCase() } },
    "GET /armature/projects": {
      status: 200,
      body: {
        status: "ok",
        projects: [
          { key: "CP", name: "Car platform", canCreate: true },
          { key: "RO", name: "Read only", canCreate: false },
          { key: "BR", name: "Brakes", canCreate: true },
        ],
      },
    },
  };

  it("offers the projects the tester may create in and every step, the failed one first, and says why it was refused", {
    timeout: LONG_TEST_MS,
  }, async () => {
    const sent = stubApi({
      ...stubs,
      [`POST /test-executions/${executionId}/cases/CHK-R2/issues`]: refusal(
        409,
        "armature_not_configured",
        "No Armature is connected, so no issue can be filed. Ask an administrator to connect one.",
      ),
    });
    await renderAt(`/projects/CHK/tests/executions/${executionId}/run/CHK-R2?iteration=1`);
    await userEvent.click(await screen.findByRole("button", { name: "File an issue" }));
    const dialog = await screen.findByRole("dialog", { name: "File the failure as an Armature issue" });
    const projects = await within(dialog).findByRole("combobox", { name: "Armature project" });
    expect(
      within(projects)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["CP Car platform", "BR Brakes"]);
    const step = within(dialog).getByRole("combobox", { name: "Failed step" });
    expect(
      within(step)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["The case as a whole", "Step 1", "Step 2", "Step 3"]);
    expect(step).toHaveValue("2");
    await userEvent.selectOptions(projects, "BR");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "File it" }));
    expect(await within(dialog).findByText("No Armature is connected, so no issue can be filed. Ask an administrator to connect one.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ iteration: 1, armatureProject: "BR", step: 2 });
  });

  it("says Armature is not set up when the projects cannot be listed, and files nothing", async () => {
    stubApi({ ...stubs, "GET /armature/projects": { status: 200, body: { status: "not_configured", projects: [] } } });
    await renderAt(`/projects/CHK/tests/executions/${executionId}/run/CHK-R2?iteration=1`);
    await userEvent.click(await screen.findByRole("button", { name: "File an issue" }));
    const dialog = await screen.findByRole("dialog", { name: "File the failure as an Armature issue" });
    await waitFor(() => expect(within(dialog).queryByRole("combobox", { name: "Armature project" })).not.toBeInTheDocument());
    expect(within(dialog).getByRole("button", { name: "File it" })).toBeDisabled();
  });
});

describe("the print view of an execution", () => {
  const report: ExecutionReport = {
    execution: anExecution({
      state: "closed",
      closedAt: "2026-10-19T08:00:00Z",
      closeComment: "Wet roads done.",
      progress: { total: 1, passed: 0, failed: 1, blocked: 0, skipped: 0, notRun: 0 },
      cases: undefined,
    }),
    cases: [{ run: failedRun, steps: aFailedCase().steps, results: aFailedCase().results, files: ["slide.png"], issues: ["CP-7"] }],
  } as ExecutionReport;

  it("prints a closed execution with every case, its steps and notes, and says when it is ready", async () => {
    stubApi({ [`GET /test-executions/${executionId}/report`]: { status: 200, body: { report } } });
    await renderAt(`/print/executions/${executionId}`, { ready: "[data-print-view], [data-print-failed]" });
    expect(await screen.findByRole("heading", { level: 1, name: "Release 1 on wet roads" })).toBeInTheDocument();
    for (const line of ["Version: 2.3", "Configuration: Winter tyres", "Wet roads done.", "Files: slide.png", "Issues: CP-7"]) {
      expect(screen.getByText(line)).toBeInTheDocument();
    }
    const entry = document.querySelector('[data-print-case="CHK-R2#1"]') as HTMLElement;
    expect(within(entry).getByRole("heading", { name: /CHK-R2 Brake on a wet road/ })).toBeInTheDocument();
    expect(within(entry).getByText("Failed, Ada Tester")).toBeInTheDocument();
    const rows = within(entry).getAllByRole("row");
    expect(rows[2]).toHaveTextContent("Press the pedal.The car stops.FailedIt slid on.");
    expect(rows[3]).toHaveTextContent("Not run");
    await waitFor(() => expect(document.documentElement).toHaveAttribute("data-print-ready"), { timeout: 3000 });
    expect(document.title).toBe("Test report: Release 1 on wet roads");
  });

  it("says what went wrong when the report cannot be read, and is never ready", async () => {
    stubApi({ [`GET /test-executions/${executionId}/report`]: refusal(404, "not_found", "That test execution was not found. It may have been deleted.") });
    await renderAt(`/print/executions/${executionId}`, { ready: "[data-print-view], [data-print-failed]" });
    expect(await screen.findByRole("alert")).toHaveTextContent("That test execution was not found.");
    expect(document.documentElement).not.toHaveAttribute("data-print-ready");
  });
});
