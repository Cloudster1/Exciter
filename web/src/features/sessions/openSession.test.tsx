import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { LONG_TEST_MS, renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";

// Opening a review session over a baseline or a query (#317): each scope asks for its own field, since a baseline
// asks for one, and the server's refusal shows beside the field it is about.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const dave = { id: "0195f000-0000-7000-8000-0000000000b1", name: "Dave Decider", email: "dave@exciter.test" };
const release = { id: "0195f000-0000-7000-8000-0000000008c1", name: "Release 1" };

function stubbed(answers: Record<string, { status: number; body?: unknown }> = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /projects/CHK/review-sessions": { status: 200, body: { sessions: [], total: 0, limit: 20, offset: 0 } },
    "GET /projects/CHK/tree": { status: 200, body: { nodes: [] } },
    "GET /projects/CHK/baselines": { status: 200, body: { baselines: [release] } },
    "GET /people": { status: 200, body: { people: [dave] } },
    "GET /groups": { status: 200, body: { groups: [] } },
    ...answers,
  });
}

async function openDialog() {
  await renderAt("/p/CHK/sessions");
  await userEvent.click(await screen.findByRole("button", { name: "Open review session" }));
  return screen.findByRole("dialog", { name: "Open a review session" });
}

async function addDave(dialog: HTMLElement) {
  await userEvent.type(within(dialog).getByRole("combobox", { name: "Add a person or group" }), "dav");
  await userEvent.click(await screen.findByRole("option", { name: /Dave Decider/ }));
}

describe("opening a review session", () => {
  it("asks for the field of each scope, and for a baseline when since names one", { timeout: LONG_TEST_MS }, async () => {
    stubbed();
    const dialog = await openDialog();
    const send = within(dialog).getByRole("button", { name: "Open session" });
    expect(send).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText("Title"), "   ");
    expect(send).toBeDisabled();

    const scope = within(dialog).getByLabelText("Review");
    expect(within(dialog).getByLabelText("Section")).toBeInTheDocument();
    await userEvent.selectOptions(scope, "baseline");
    expect(within(dialog).queryByLabelText("Section")).toBeNull();
    expect(within(within(dialog).getByLabelText("Baseline")).getByRole("option", { name: "Release 1" })).toBeInTheDocument();
    await userEvent.selectOptions(scope, "query");
    expect(within(dialog).queryByLabelText("Baseline")).toBeNull();
    expect(within(dialog).getByLabelText("Query")).toHaveAccessibleDescription(/In the query language/);

    expect(within(dialog).queryByLabelText("A baseline")).toBeNull();
    await userEvent.selectOptions(within(dialog).getByLabelText("Only what changed since"), "baseline");
    const since = within(dialog).getByLabelText("A baseline");
    expect(since).toHaveValue("");
    expect(within(since).getByRole("option", { name: "Release 1" })).toBeInTheDocument();
  });

  it("sends a query and what changed since, and shows each refusal beside its field", { timeout: LONG_TEST_MS }, async () => {
    const sent = stubbed({
      "POST /projects/CHK/review-sessions": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Some fields need attention.",
            fields: {
              scope: "Nothing in this scope needs a review. Widen it, or drop since.",
              since: "Keep what changed since the last approval, or since a baseline of the project.",
              quorum: "Ask for 1 to 100 percent of the approvers.",
            },
          },
        },
      },
    });
    const dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText("Title"), "Lights review");
    await userEvent.selectOptions(within(dialog).getByLabelText("Review"), "query");
    await userEvent.type(within(dialog).getByLabelText("Query"), "title ~ lights");
    await userEvent.selectOptions(within(dialog).getByLabelText("Only what changed since"), "approved");
    await userEvent.clear(within(dialog).getByLabelText("Approvers each requirement needs"));
    await userEvent.type(within(dialog).getByLabelText("Approvers each requirement needs"), "50");
    await addDave(dialog);
    await userEvent.click(within(dialog).getByRole("button", { name: "Open session" }));
    await waitFor(() =>
      expect(sent.find((each) => each.method === "POST")?.body).toMatchObject({
        title: "Lights review",
        scope: { kind: "query", query: "title ~ lights" },
        since: { base: "approved" },
        quorum: 50,
        participants: [{ subjectType: "user", userId: dave.id, role: "approver" }],
      }),
    );
    expect(await within(dialog).findByLabelText("Review")).toHaveAccessibleDescription(/Nothing in this scope needs a review/);
    expect(within(dialog).getByLabelText("Only what changed since")).toHaveAccessibleDescription(/Keep what changed since the last approval/);
    expect(within(dialog).getByLabelText("Approvers each requirement needs")).toHaveAccessibleDescription(/Ask for 1 to 100 percent/);
    expect(within(dialog).queryByText("Some fields need attention.")).toBeNull();
  });
});
