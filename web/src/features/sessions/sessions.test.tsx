import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReviewSession, SessionSummary } from "@/api/sessions";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";

// Review sessions (#204): an editor opens one with its participants, the session page says where each requirement
// and each person stands, the moderator closes it with a baseline, and the inbox lists the sessions that wait.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const sessionId = "0195f000-0000-7000-8000-0000000005e1";
const dave = { id: "0195f000-0000-7000-8000-0000000000b1", name: "Dave Decider", email: "dave@exciter.test" };
const section = {
  id: "0195f000-0000-7000-8000-0000000000a2",
  key: "CHK-R2",
  kind: "section" as const,
  title: "Braking",
  parentId: null,
  depth: 0,
  outline: "2",
  typeId: null,
  status: null,
  revision: 1,
  children: 1,
};

function aSession(over: Partial<ReviewSession> = {}): ReviewSession {
  return {
    id: sessionId,
    projectKey: "CHK",
    title: "Braking review",
    description: "",
    scope: { kind: "section", rootKey: "CHK-R2" },
    since: null,
    state: "open",
    round: 1,
    quorum: 100,
    deadline: null,
    moderatorName: "Ada Lovelace",
    participants: [{ subjectType: "user", userId: dave.id, name: dave.name, role: "approver" }],
    items: [
      {
        requirement: {
          id: "r3",
          key: "CHK-R3",
          projectKey: "CHK",
          kind: "requirement",
          title: "Brake lights follow the pedal",
          revision: 1,
          status: null,
          trashed: false,
        },
        outline: "2.1",
        revision: 1,
        round: 1,
        openThreads: 1,
        review: {
          id: "v3",
          via: null,
          requirement: {
            id: "r3",
            key: "CHK-R3",
            projectKey: "CHK",
            kind: "requirement",
            title: "Brake lights follow the pedal",
            revision: 1,
            status: null,
            trashed: false,
          },
          revision: 1,
          state: "approved",
          required: 1,
          approvals: 1,
          approvers: [],
          decisions: [],
          comment: "",
          requestedByName: "Ada Lovelace",
          requestedAt: "2026-10-01T08:00:00Z",
          finishedAt: null,
          authorName: "Ada Lovelace",
          can: { decide: false, withdraw: false },
          signed: false,
          meanings: {},
          session: { id: sessionId, title: "Braking review", round: 1 },
        },
      },
    ],
    excluded: [],
    progress: [{ person: { id: dave.id, name: dave.name }, role: "approver", decided: 1, commented: 0, of: 1 }],
    counts: { approved: 1, rejected: 0, pending: 0, total: 1 },
    baselineId: null,
    openedAt: "2026-10-01T08:00:00Z",
    closedAt: null,
    can: { edit: true, round: true, close: true },
    ...over,
  };
}

describe("a review session", () => {
  it("is opened over a section with its approvers", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/review-sessions": { status: 200, body: { sessions: [], total: 0, limit: 20, offset: 0 } },
      "GET /projects/CHK/tree": { status: 200, body: { nodes: [section] } },
      "GET /projects/CHK/baselines": { status: 200, body: { baselines: [] } },
      "GET /people": { status: 200, body: { people: [dave] } },
      "GET /groups": { status: 200, body: { groups: [] } },
      "POST /projects/CHK/review-sessions": { status: 201, body: { session: aSession() } },
      [`GET /review-sessions/${sessionId}`]: { status: 200, body: { session: aSession() } },
    });
    await renderAt("/p/CHK/sessions");
    await userEvent.click(await screen.findByRole("button", { name: "Open review session" }));
    const dialog = await screen.findByRole("dialog", { name: "Open a review session" });
    await userEvent.type(within(dialog).getByLabelText("Title"), "Braking review");
    await userEvent.selectOptions(await within(dialog).findByLabelText("Section"), "CHK-R2");
    await userEvent.click(within(dialog).getByRole("button", { name: "Open session" }));
    expect(await within(dialog).findByText("Name at least one approver.")).toBeInTheDocument();
    await userEvent.type(within(dialog).getByRole("combobox", { name: "Add a person or group" }), "dav");
    await userEvent.click(await screen.findByRole("option", { name: /Dave Decider/ }));
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "Open session" }));
    await waitFor(() =>
      expect(sent.find((each) => each.method === "POST")?.body).toMatchObject({
        title: "Braking review",
        scope: { kind: "section", rootKey: "CHK-R2" },
        participants: [{ subjectType: "user", userId: dave.id, role: "approver" }],
        quorum: 100,
      }),
    );
    expect(await screen.findByRole("heading", { name: "Braking review" })).toBeInTheDocument();
  });

  it("says where each requirement and each person stands, and closes with a baseline", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      [`GET /review-sessions/${sessionId}`]: { status: 200, body: { session: aSession() } },
      [`POST /review-sessions/${sessionId}/close`]: { status: 200, body: { session: aSession({ state: "closed", closedAt: "2026-10-02T08:00:00Z" }) } },
    });
    await renderAt(`/p/CHK/sessions/${sessionId}`);
    const item = (await screen.findByText("Brake lights follow the pedal")).closest("tr")!;
    expect(item).toHaveAttribute("data-session-outcome", "approved");
    expect(item).toHaveTextContent("1 open thread");
    expect(screen.getByText("1 approved, 0 rejected, 0 pending of 1")).toBeInTheDocument();
    expect(document.querySelector('[data-session-progress="Dave Decider"]')).toHaveTextContent("1 of 1");
    expect(screen.getByRole("link", { name: "Review in the document" })).toHaveAttribute("href", expect.stringContaining(`session=${sessionId}`));
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Close session" }));
    const dialog = await screen.findByRole("dialog", { name: "Close the session" });
    await userEvent.click(within(dialog).getByLabelText("Take a baseline of what was approved"));
    await userEvent.clear(within(dialog).getByLabelText("Baseline name"));
    await userEvent.type(within(dialog).getByLabelText("Baseline name"), "Braking agreed");
    await userEvent.click(within(dialog).getByRole("button", { name: "Close session" }));
    await waitFor(() => expect(sent.find((each) => each.method === "POST")?.body).toEqual({ baseline: { name: "Braking agreed" } }));
    expect(await screen.findByText("The session is closed.")).toBeInTheDocument();
  });

  it("waits in the inbox of whoever takes part", async () => {
    const waiting: SessionSummary = {
      id: sessionId,
      projectKey: "CHK",
      title: "Braking review",
      state: "open",
      round: 1,
      deadline: "2026-10-20T08:00:00Z",
      moderatorName: "Ada Lovelace",
      counts: { approved: 0, rejected: 0, pending: 2, total: 2 },
      waiting: 2,
      openedAt: "2026-10-01T08:00:00Z",
      closedAt: null,
    };
    stubApi({
      "GET /reviews/sessions": { status: 200, body: { sessions: [waiting], total: 1, limit: 20, offset: 0 } },
      "GET /reviews/waiting": { status: 200, body: { reviews: [], total: 0, limit: 20, offset: 0 } },
    });
    await renderAt("/reviews");
    const mine = await screen.findByRole("list", { name: "Your review sessions" });
    expect(within(mine).getByRole("link", { name: "Braking review" })).toHaveAttribute("href", `/p/CHK/sessions/${sessionId}`);
    expect(mine).toHaveTextContent("2 wait for you");
    expect(await axeViolations()).toEqual([]);
  });
});
