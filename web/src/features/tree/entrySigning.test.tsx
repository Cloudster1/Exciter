import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Review } from "@/api/reviews";
import type { SpecEntry } from "@/api/tree";
import { renderAt } from "@/test/app";
import { aProject } from "@/test/projects";
import { entry, entryOf, pay, ship, stubDocument } from "@/test/spec";

afterEach(() => {
  vi.unstubAllGlobals();
  window.history.replaceState(null, "", "/");
  sessionStorage.clear();
});

const signed = aProject({
  settings: { defaultTypeId: null, workflowId: null, requireRevisionComment: false, signedApprovals: true, library: false, homeDashboardId: null },
});
const meanings = { approve: "I approve this revision.", reject: "I reject this revision." };

const pending = (id: string): NonNullable<SpecEntry["review"]> => ({
  id,
  revision: 1,
  required: 1,
  approvals: 0,
  rejections: 0,
  approver: true,
  mayDecide: true,
  decision: null,
});

const aReview = (id: string, key: string, title: string): Review => ({
  id,
  via: null,
  requirement: { id: `${id}-requirement`, key, projectKey: "CHK", kind: "requirement", title, revision: 1, status: null, trashed: false },
  revision: 1,
  state: "pending",
  required: 1,
  approvals: 0,
  approvers: [],
  decisions: [],
  comment: "",
  requestedByName: "Bob Builder",
  requestedAt: "2026-10-01T08:00:00Z",
  finishedAt: null,
  authorName: "Ada Lovelace",
  can: { decide: true, withdraw: false },
  signed: true,
  meanings,
  session: null,
});

function stubSigned(more: Parameters<typeof stubDocument>[1] = {}) {
  return stubDocument([entry(pay, "Pay at once.", { review: pending("rv-pay") }), entry(ship, "Ship in a day.", { review: pending("rv-ship") })], {
    "GET /projects/CHK": { status: 200, body: { project: signed } },
    "GET /reviews/rv-pay": { status: 200, body: { review: aReview("rv-pay", "CHK-R2", "Pay") } },
    "GET /reviews/rv-ship": { status: 200, body: { review: aReview("rv-ship", "CHK-R3", "Ship") } },
    ...more,
  });
}

describe("a signed approval in the document", () => {
  it("opens the signature instead of deciding", async () => {
    const sent = stubSigned();
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    await userEvent.click(within(entryOf("CHK-R2")).getByRole("button", { name: "Approve" }));
    const dialog = await screen.findByRole("dialog", { name: "Sign: approve" });
    expect(within(dialog).getByText(meanings.approve)).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Your password")).toBeInTheDocument();
    expect(sent.filter((each) => each.method === "POST")).toEqual([]);
  });

  it("finishes a signature the identity provider interrupted for that review only", async () => {
    sessionStorage.setItem("exciter.signing", JSON.stringify({ reviewId: "rv-ship", decision: "approve", comment: "Ships on time." }));
    window.history.replaceState(null, "", "/p/CHK/spec?sign=approve&review=rv-ship&signature=proof");
    const sent = stubSigned({ "POST /reviews/rv-ship/decisions": { status: 200, body: { review: aReview("rv-ship", "CHK-R3", "Ship") } } });
    await renderAt("/p/CHK/spec");
    const dialog = await screen.findByRole("dialog", { name: "Sign: approve" });
    expect(within(dialog).getByRole("status")).toHaveTextContent("You signed in again.");
    expect(screen.getAllByRole("dialog")).toHaveLength(1);
    expect(within(entryOf("CHK-R3")).getByLabelText("Your comment")).toHaveValue("Ships on time.");
    expect(within(entryOf("CHK-R2")).getByLabelText("Your comment")).toHaveValue("");
    expect(window.location.search).toBe("");
    await userEvent.click(within(dialog).getByRole("button", { name: "Sign and approve" }));
    await waitFor(() => expect(sent.filter((each) => each.method === "POST").map((each) => each.path)).toEqual(["/reviews/rv-ship/decisions"]));
    expect(sent.find((each) => each.method === "POST")?.body).toEqual({
      decision: "approve",
      revision: 1,
      comment: "Ships on time.",
      reauthentication: "proof",
    });
  });
});
