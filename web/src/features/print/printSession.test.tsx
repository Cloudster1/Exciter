import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import type { ReviewSession, SessionReport } from "@/api/sessions";
import { PRINT_SETTLE_MS } from "@/config";
import { type Answer, renderAt, stubApi } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  document.documentElement.removeAttribute("data-print-ready");
  document.title = "";
});

// A print view has no shell; it has arrived once it shows the report, why it cannot, or that it is loading.
const arrived = "[data-print-view], [data-print-failed], main[aria-busy]";
const sessionId = "0195f000-0000-7000-8000-0000000005e2";
const reportPath = `GET /review-sessions/${sessionId}/report`;
const lights = {
  id: "r3",
  key: "CHK-R3",
  projectKey: "CHK",
  kind: "requirement" as const,
  title: "Brake lights follow the pedal",
  revision: 2,
  status: null,
  trashed: false,
};

function decision(reviewerName: string, verdict: "approve" | "reject", comment: string) {
  return { reviewerId: `id-${reviewerName}`, reviewerName, decision: verdict, comment, decidedAt: "2026-10-02T08:00:00Z", signature: null };
}

function aReport(): SessionReport {
  const session: ReviewSession = {
    id: sessionId,
    projectKey: "CHK",
    title: "Braking review",
    description: "",
    scope: { kind: "section", rootKey: "CHK-R2" },
    since: null,
    state: "closed",
    round: 2,
    quorum: 100,
    deadline: null,
    moderatorName: "Ada Lovelace",
    participants: [],
    items: [],
    excluded: [],
    progress: [
      { person: { id: "dave", name: "Dave Decider" }, role: "approver", decided: 1, commented: 0, of: 1 },
      { person: { id: "carol", name: "Carol Reader" }, role: "reviewer", decided: 0, commented: 1, of: 1 },
    ],
    counts: { approved: 1, rejected: 0, pending: 0, total: 1 },
    baselineId: "0195f000-0000-7000-8000-0000000008b1",
    openedAt: "2026-10-01T08:00:00Z",
    closedAt: "2026-10-03T08:00:00Z",
    can: { edit: false, round: false, close: false },
  };
  return {
    session,
    baselineName: "Braking agreed",
    rounds: [
      { number: 1, comment: "", startedAt: "2026-10-01T08:00:00Z" },
      { number: 2, comment: "Say how fast.", startedAt: "2026-10-02T08:00:00Z" },
    ],
    items: [
      {
        requirement: lights,
        outline: "2.1",
        openThreads: 1,
        rounds: [
          { round: 1, revision: 1, state: "rejected", decisions: [decision("Dave Decider", "reject", "How fast?")] },
          { round: 2, revision: 2, state: "approved", decisions: [decision("Dave Decider", "approve", "")] },
        ],
      },
    ],
  };
}

describe("the print view of a review session's report", () => {
  it("draws the session, who took part and each requirement round by round, and is ready only once loaded", async () => {
    let answer: (value: Answer) => void = () => {};
    stubApi({ [reportPath]: () => new Promise<Answer>((resolve) => (answer = resolve)) });
    await renderAt(`/print/sessions/${sessionId}`, { ready: arrived });
    expect(document.querySelector("main[aria-busy]")).not.toBeNull();
    expect(document.documentElement).not.toHaveAttribute("data-print-ready");

    answer({ status: 200, body: { report: aReport() } });
    expect(await screen.findByRole("heading", { level: 1, name: "Braking review" })).toBeInTheDocument();
    expect(screen.getByText("Moderated by Ada Lovelace")).toBeInTheDocument();
    expect(screen.getByText("1 approved, 0 rejected, 0 pending of 1")).toBeInTheDocument();
    expect(screen.getByText("Baseline taken: Braking agreed")).toBeInTheDocument();
    expect(screen.getByRole("row", { name: "Dave Decider Approver 1 of 1 0 of 1" })).toBeInTheDocument();
    expect(screen.getByRole("row", { name: "Carol Reader Reviewer 0 of 1 1 of 1" })).toBeInTheDocument();
    expect(screen.getByText(/Round 2, .*: Say how fast\./)).toBeInTheDocument();

    const item = document.querySelector('[data-report-item="CHK-R3"]') as HTMLElement;
    expect(within(item).getByRole("heading", { name: "2.1 CHK-R3 Brake lights follow the pedal" })).toBeInTheDocument();
    expect(within(item).getByText("Round 1, Revision 1: Rejected (Dave Decider: reject, How fast?)")).toBeInTheDocument();
    expect(within(item).getByText("Round 2, Revision 2: Approved (Dave Decider: approve)")).toBeInTheDocument();
    expect(within(item).getByText("1 open thread")).toBeInTheDocument();

    const meta = document.querySelector("[data-print-meta]") as HTMLElement;
    expect(meta.dataset.header).toBe("CHK");
    expect(meta.dataset.title).toBe("Braking review");
    expect(document.title).toBe("Braking review");
    await waitFor(() => expect(document.documentElement).toHaveAttribute("data-print-ready"), { timeout: 3000 });
  });

  it("shows why the report cannot be printed and never says it is ready", async () => {
    stubApi({ [reportPath]: { status: 404, body: { error: { code: "not_found", message: "That review session was not found." } } } });
    await renderAt(`/print/sessions/${sessionId}`, { ready: "[data-print-failed]" });
    expect(screen.getByRole("alert")).toHaveTextContent("That review session was not found.");
    expect(document.querySelector("[data-print-view]")).toBeNull();
    // Past the wait a loaded page settles in, a refusal is still not ready.
    await new Promise((resolve) => setTimeout(resolve, 2 * PRINT_SETTLE_MS));
    expect(document.documentElement).not.toHaveAttribute("data-print-ready");
    expect(document.title).not.toBe("Braking review");
  });
});
