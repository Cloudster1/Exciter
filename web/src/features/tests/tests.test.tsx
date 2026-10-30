import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ExecutionCase, TestExecution } from "@/api/tests";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";

afterEach(() => vi.unstubAllGlobals());

const doc = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
const executionId = "0195f000-0000-7000-8000-00000000e0a1";
const caseId = "0195f000-0000-7000-8000-00000000e0b1";
const userId = "0195f000-0000-7000-8000-00000000e0c1";

const run = (iteration: number, outcome: "passed" | "failed" | null) => ({
  case: { id: caseId, key: "CHK-R2", title: "Brake on a wet road", trashed: false },
  iteration,
  revision: 1,
  values: { Speed: iteration === 1 ? "30" : "50" },
  outcome,
  note: "",
  source: outcome ? ("tester" as const) : null,
  recordedBy: outcome ? { id: userId, name: "Bob Builder" } : null,
  recordedAt: outcome ? "2026-10-18T10:00:00Z" : null,
  ci: null,
});

function anExecution(over: Partial<TestExecution> = {}): TestExecution {
  return {
    id: executionId,
    projectKey: "CHK",
    title: "Release 1 on wet roads",
    planId: "0195f000-0000-7000-8000-00000000e0d1",
    planTitle: "Wet road",
    baseline: null,
    version: "2.3",
    configuration: "Winter tyres",
    manager: { id: userId, name: "Ada Lovelace" },
    dueAt: null,
    state: "open",
    testers: [{ subjectType: "user", userId, name: "Bob Builder" }],
    excluded: [],
    progress: { total: 2, passed: 1, failed: 0, blocked: 0, skipped: 0, notRun: 1 },
    cases: [run(1, "passed"), run(2, null)],
    closeComment: "",
    openedAt: "2026-10-18T08:00:00Z",
    closedAt: null,
    can: { manage: true, record: true },
    ...over,
  } as TestExecution;
}

function aCase(): ExecutionCase {
  return {
    run: run(2, null),
    preconditions: doc("The car stands on a wet road."),
    steps: [
      { action: doc("Drive at the speed."), expected: null },
      { action: doc("Press the pedal."), expected: doc("The car stops.") },
    ],
    results: [],
    files: [],
    issues: [],
    iterations: 2,
    parameters: [{ name: "Speed", values: ["30", "50"] }],
  } as ExecutionCase;
}

const project = { "GET /projects/CHK": { status: 200, body: { project: aProject() } } };

describe("tests", () => {
  it("lists a project's executions with their progress, and makes a plan", async () => {
    const sent = stubApi({
      ...project,
      "GET /projects/CHK/test-executions": { status: 200, body: { executions: [anExecution({ cases: undefined })] } },
      "GET /projects/CHK/test-plans": { status: 200, body: { plans: [] } },
      "POST /projects/CHK/test-plans": {
        status: 201,
        body: { plan: { id: "p1", projectKey: "CHK", title: "Smoke", description: "", scope: "query", query: "type = x", cases: [], can: { edit: true } } },
      },
      "GET /test-plans/p1": {
        status: 200,
        body: { plan: { id: "p1", projectKey: "CHK", title: "Smoke", description: "", scope: "query", query: "type = x", cases: [], can: { edit: true } } },
      },
    });
    await renderAt("/projects/CHK/tests");
    const row = await screen.findByRole("row", { name: /Release 1 on wet roads/ });
    expect(within(row).getByText("1 of 2 run, 0 failed")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("tab", { name: "Plans" }));
    await userEvent.click(await screen.findByRole("button", { name: "New plan" }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.type(within(dialog).getByRole("textbox", { name: "Title" }), "Smoke");
    await userEvent.click(within(dialog).getByRole("radio", { name: /query finds/ }));
    await userEvent.type(within(dialog).getByRole("textbox", { name: "Query" }), "type = x");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "Make the plan" }));
    await waitFor(() => expect(sent.some((r) => r.method === "POST")).toBe(true));
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ title: "Smoke", scope: "query", query: "type = x" });
  });

  it("closes an execution, or aborts it", async () => {
    const sent = stubApi({
      ...project,
      [`GET /test-executions/${executionId}`]: { status: 200, body: { execution: anExecution() } },
      [`POST /test-executions/${executionId}/close`]: { status: 200, body: { execution: anExecution({ state: "aborted" }) } },
    });
    await renderAt(`/projects/CHK/tests/executions/${executionId}`);
    const table = await screen.findByRole("table", { name: "Test cases" });
    expect(within(table).getByRole("row", { name: /Speed = 30 Passed Bob Builder/ })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Close" }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("checkbox", { name: /Abort it/ }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Close it" }));
    await waitFor(() => expect(sent.some((r) => r.method === "POST")).toBe(true));
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ comment: "", abort: true });
  });

  it("runs a case step by step and saves what each step came to", async () => {
    const sent = stubApi({
      ...project,
      [`GET /test-executions/${executionId}`]: { status: 200, body: { execution: anExecution() } },
      [`GET /test-executions/${executionId}/cases/CHK-R2`]: { status: 200, body: { case: aCase() } },
      [`PUT /test-executions/${executionId}/cases/CHK-R2/result`]: { status: 200, body: { case: aCase() } },
    });
    await renderAt(`/projects/CHK/tests/executions/${executionId}/run/CHK-R2?iteration=2`);
    expect(await screen.findByText("The car stands on a wet road.")).toBeInTheDocument();
    expect(screen.getByText(/Iteration 2 of 2 · Speed = 50/)).toBeInTheDocument();
    const second = screen.getByRole("group", { name: "Outcome of step 2" });
    await userEvent.click(within(screen.getByRole("group", { name: "Outcome of step 1" })).getByRole("button", { name: "Passed" }));
    await userEvent.click(within(second).getByRole("button", { name: "Failed" }));
    expect(within(second).getByRole("button", { name: "Failed" })).toHaveAttribute("aria-pressed", "true");
    const notes = screen.getAllByRole("textbox", { name: "Actual result" });
    await userEvent.type(notes[1]!, "It slid.");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Save the results" }));
    await waitFor(() => expect(sent.some((r) => r.method === "PUT")).toBe(true));
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({
      iteration: 2,
      steps: [
        { step: 1, outcome: "passed" },
        { step: 2, outcome: "failed", note: "It slid." },
      ],
    });
    expect(await screen.findByRole("status")).toHaveTextContent("The results are saved.");
  });
});
