import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ApprovalRule, Review } from "@/api/reviews";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  window.history.replaceState(null, "", "/");
  sessionStorage.clear();
});

const reviewId = "0195f000-0000-7000-8000-0000000000d1";
const dave = { id: "0195f000-0000-7000-8000-0000000000b1", name: "Dave Decider", email: "dave@exciter.test" };

function aReview(over: Partial<Review> = {}): Review {
  return {
    id: reviewId,
    via: null,
    requirement: {
      id: "0195f000-0000-7000-8000-0000000000a1",
      key: "CHK-R1",
      projectKey: "CHK",
      kind: "requirement",
      title: "Pay the basket",
      revision: 1,
      status: null,
      trashed: false,
    },
    revision: 1,
    state: "pending",
    required: 2,
    approvals: 1,
    approvers: [
      { subjectType: "user", userId: dave.id, name: dave.name },
      { subjectType: "group", groupId: "0195f000-0000-7000-8000-0000000000c1", name: "Safety board" },
    ],
    decisions: [
      {
        reviewerId: "0195f000-0000-7000-8000-0000000000b2",
        reviewerName: "Carol Clerk",
        decision: "approve",
        comment: "Fine.",
        decidedAt: "2026-10-02T08:00:00Z",
        signature: null,
      },
    ],
    comment: "Ready to sign off.",
    requestedByName: "Bob Builder",
    requestedAt: "2026-10-01T08:00:00Z",
    finishedAt: null,
    authorName: "Ada Lovelace",
    can: { decide: true, withdraw: false },
    signed: false,
    meanings: {},
    session: null,
    ...over,
  };
}

function stubRequirement(reviews: Review[], more: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}, approval = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /requirements/CHK-R1": {
      status: 200,
      body: { requirement: aRequirement({ approval: { pendingReviewId: null, approvedRevision: null, current: false, ...approval } }) },
    },
    "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
    "GET /requirements/CHK-R1/comments": { status: 200, body: { threads: [] } },
    "GET /requirements/CHK-R1/reviews": { status: 200, body: { reviews, total: reviews.length, limit: 20, offset: 0 } },
    ...more,
  });
}

const panel = () => screen.findByRole("region", { name: "Approval" });

describe("a requirement's approval", () => {
  it("says nothing is approved and lets an editor ask for the revision they read", async () => {
    const sent = stubRequirement([], { "POST /requirements/CHK-R1/reviews": { status: 201, body: { review: aReview() } } });
    await renderAt("/p/CHK/r/CHK-R1");
    const region = await panel();
    expect(within(region).getByText("No revision is approved yet.")).toBeInTheDocument();
    await userEvent.click(await within(region).findByRole("button", { name: "Ask for review" }));
    await userEvent.type(within(region).getByLabelText("Note to the approvers"), "Ready.");
    await userEvent.click(within(region).getByRole("button", { name: "Ask" }));
    await waitFor(() => expect(sent.find((each) => each.method === "POST")?.body).toEqual({ revision: 1, comment: "Ready." }));
  });

  it("shows the refusal when nobody approves this type", async () => {
    stubRequirement([], {
      "POST /requirements/CHK-R1/reviews": {
        status: 409,
        body: {
          error: {
            code: "no_approval_rule",
            message: "Nobody approves requirements of this type yet. Ask one of the project's administrators to set an approval rule.",
          },
        },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    const region = await panel();
    await userEvent.click(await within(region).findByRole("button", { name: "Ask for review" }));
    await userEvent.click(within(region).getByRole("button", { name: "Ask" }));
    expect(await within(region).findByText(/Nobody approves requirements of this type yet/)).toBeInTheDocument();
  });

  it("shows the review waiting and lets an approver approve with a comment", async () => {
    const sent = stubRequirement([aReview()], {
      [`POST /reviews/${reviewId}/decisions`]: { status: 200, body: { review: aReview({ state: "approved", approvals: 2 }) } },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    const region = await panel();
    const pending = await within(region).findByText("Revision 1 is under review, asked by Bob Builder.");
    const box = pending.closest("[data-pending-review]") as HTMLElement;
    expect(box).toHaveTextContent("1 of 2 approvals");
    expect(box).toHaveTextContent("Dave Decider, Safety board");
    expect(within(box).getByRole("list", { name: "Decisions" })).toHaveTextContent("Carol Clerk approved: Fine.");
    expect(within(region).queryByRole("button", { name: "Ask for review" })).toBeNull();
    expect(await axeViolations()).toEqual([]);
    await userEvent.type(within(box).getByLabelText("Your comment"), "Agreed.");
    await userEvent.click(within(box).getByRole("button", { name: "Approve" }));
    await waitFor(() => expect(sent.find((each) => each.method === "POST")?.body).toEqual({ decision: "approve", revision: 1, comment: "Agreed." }));
  });

  it("lets whoever asked withdraw, and offers no decision to somebody not named", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const sent = stubRequirement([aReview({ can: { decide: false, withdraw: true } })], {
      [`POST /reviews/${reviewId}/withdraw`]: { status: 200, body: { review: aReview({ state: "withdrawn" }) } },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    const region = await panel();
    await within(region).findByText(/is under review/);
    expect(within(region).queryByRole("button", { name: "Approve" })).toBeNull();
    await userEvent.click(within(region).getByRole("button", { name: "Withdraw" }));
    await waitFor(() => expect(sent.some((each) => each.method === "POST" && each.path === `/reviews/${reviewId}/withdraw`)).toBe(true));
  });

  it("says which revision was approved, and when it is no longer the current one", async () => {
    stubRequirement([aReview({ state: "approved", approvals: 2, finishedAt: "2026-10-02T09:00:00Z" })], {}, { approvedRevision: 1, current: true });
    await renderAt("/p/CHK/r/CHK-R1");
    const region = await panel();
    expect(within(region).getByText("Revision 1 is approved.")).toBeInTheDocument();
    expect(await within(region).findByRole("list", { name: "Earlier reviews" })).toHaveTextContent("Approved");
  });
});

describe("a project's approval rules", () => {
  const rule: ApprovalRule = {
    id: "0195f000-0000-7000-8000-0000000000e1",
    typeId: null,
    typeName: "",
    required: 1,
    authorMayApprove: false,
    approvers: [{ subjectType: "user", userId: dave.id, name: dave.name }],
    updatedAt: "2026-10-01T08:00:00Z",
  };

  it("adds a rule for every type with an approver from the picker", async () => {
    let rules: ApprovalRule[] = [];
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
      "GET /projects/CHK/approval-rules": () => ({ status: 200, body: { rules } }),
      "GET /people": { status: 200, body: { people: [dave] } },
      "GET /groups": { status: 200, body: { groups: [] } },
      "PUT /projects/CHK/approval-rules": () => {
        rules = [rule];
        return { status: 200, body: { rule } };
      },
    });
    await renderAt("/p/CHK/settings?tab=approval");
    expect(await screen.findByText("No rule yet, so no requirement of this project can be reviewed.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Add a rule" }));
    await userEvent.click(screen.getByRole("button", { name: "Save rule" }));
    expect(await screen.findByText("Name at least one person or group who approves.")).toBeInTheDocument();
    await userEvent.type(screen.getByRole("combobox", { name: "Add a person or a group" }), "dav");
    await userEvent.click(await screen.findByRole("option", { name: /Dave Decider/ }));
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Save rule" }));
    await waitFor(() =>
      expect(sent.find((each) => each.method === "PUT")?.body).toEqual({
        required: 1,
        authorMayApprove: false,
        approvers: [{ subjectType: "user", userId: dave.id, name: dave.name }],
      }),
    );
    expect(await screen.findByText(/The rule is saved/)).toBeInTheDocument();
    expect(await screen.findByRole("list", { name: "Approval rules" })).toHaveTextContent("1 of Dave Decider");
  });

  it("are read only to a project's readers", async () => {
    stubApi({
      "GET /projects/CHK": {
        status: 200,
        body: { project: aProject({ can: { view: true, comment: false, edit: false, approve: false, administer: false, purge: false } }) },
      },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
      "GET /projects/CHK/approval-rules": { status: 200, body: { rules: [rule] } },
    });
    await renderAt("/p/CHK/settings?tab=approval");
    expect(await screen.findByText("Only the project's administrators set who approves.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add a rule" })).toBeNull();
    expect(await screen.findByRole("list", { name: "Approval rules" })).toHaveTextContent("Every type without its own rule");
  });
});

describe("the reviews waiting on the reader", () => {
  it("are counted on the sidebar's Reviews and listed oldest first", async () => {
    stubApi({ "GET /reviews/waiting": { status: 200, body: { reviews: [aReview()], total: 1, limit: 20, offset: 0 } } });
    const router = await renderAt("/projects");
    await userEvent.click(await screen.findByRole("link", { name: /^Reviews.*1 review waits for you$/ }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/reviews"));
    const list = await screen.findByRole("list", { name: "Reviews" });
    expect(within(list).getByRole("link", { name: "CHK-R1 Pay the basket" })).toHaveAttribute("href", "/p/CHK/r/CHK-R1");
    expect(list).toHaveTextContent("Asked by Bob Builder");
    expect(list).toHaveTextContent("1 of 2 approvals");
    expect(within(list).getByRole("link", { name: "Review in the CHK document" })).toHaveAttribute("href", "/p/CHK/spec?waiting=true&changes=approved");
    expect(await axeViolations()).toEqual([]);
  });

  it("say so when nothing waits", async () => {
    stubApi({});
    await renderAt("/reviews");
    expect(await screen.findByText("Nothing waits for your decision.")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /waits for you/ })).toBeNull();
  });
});

const proof = "0195f000-0000-7000-8000-0000000000e1";
const meanings = {
  approve: "I approve revision 1 of CHK-R1, Pay the basket, as one of its approvers.",
  reject: "I reject revision 1 of CHK-R1, Pay the basket, as one of its approvers.",
};

describe("a signed approval", () => {
  it("shows what is signed and signs it with the password", async () => {
    const sent = stubRequirement([aReview({ signed: true, meanings })], {
      [`POST /reviews/${reviewId}/decisions`]: { status: 200, body: { review: aReview({ state: "approved", approvals: 2 }) } },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    const region = await panel();
    await userEvent.type(await within(region).findByLabelText("Your comment"), "Agreed.");
    await userEvent.click(within(region).getByRole("button", { name: "Approve" }));
    const dialog = await screen.findByRole("dialog", { name: "Sign: approve" });
    expect(within(dialog).getByText(meanings.approve)).toBeInTheDocument();
    expect(sent.some((each) => each.method === "POST")).toBe(false);
    expect(await axeViolations()).toEqual([]);
    await userEvent.type(within(dialog).getByLabelText("Your password"), "correct horse");
    await userEvent.click(within(dialog).getByRole("button", { name: "Sign and approve" }));
    await waitFor(() =>
      expect(sent.find((each) => each.method === "POST")?.body).toEqual({ decision: "approve", revision: 1, comment: "Agreed.", password: "correct horse" }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("says when the password is wrong, and keeps the dialog open", async () => {
    stubRequirement([aReview({ signed: true, meanings })], {
      [`POST /reviews/${reviewId}/decisions`]: {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Check the highlighted fields.",
            fields: { password: "That is not your password. Type it again to sign." },
          },
        },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(await within(await panel()).findByRole("button", { name: "Reject" }));
    const dialog = await screen.findByRole("dialog", { name: "Sign: reject" });
    expect(within(dialog).getByText(meanings.reject)).toBeInTheDocument();
    await userEvent.type(within(dialog).getByLabelText("Your password"), "wrong");
    await userEvent.click(within(dialog).getByRole("button", { name: "Sign and reject" }));
    expect(await within(dialog).findByText("That is not your password. Type it again to sign.")).toBeInTheDocument();
  });

  it("sends somebody who signs in at the provider there, keeping their comment", async () => {
    window.history.replaceState(null, "", "/p/CHK/r/CHK-R1");
    stubRequirement([aReview({ signed: true, meanings })]);
    await renderAt("/p/CHK/r/CHK-R1", { me: { ...signedIn, signsWith: "provider" } });
    const region = await panel();
    await userEvent.type(await within(region).findByLabelText("Your comment"), "Agreed.");
    await userEvent.click(within(region).getByRole("button", { name: "Approve" }));
    const dialog = await screen.findByRole("dialog", { name: "Sign: approve" });
    expect(within(dialog).queryByLabelText("Your password")).toBeNull();
    const link = within(dialog).getByRole("link", { name: "Sign in again to sign" });
    const next = new URL(link.getAttribute("href") ?? "", window.location.origin).searchParams.get("next");
    expect(next).toBe(`/p/CHK/r/CHK-R1?sign=approve&review=${reviewId}`);
    // jsdom cannot follow the link; what matters is what the click keeps for the way back.
    link.addEventListener("click", (event) => event.preventDefault());
    await userEvent.click(link);
    expect(JSON.parse(sessionStorage.getItem("exciter.signing") ?? "null")).toEqual({ reviewId, decision: "approve", comment: "Agreed." });
  });

  it("finishes the signature with the proof the provider brought back", async () => {
    sessionStorage.setItem("exciter.signing", JSON.stringify({ reviewId, decision: "approve", comment: "Agreed." }));
    window.history.replaceState(null, "", `/p/CHK/r/CHK-R1?sign=approve&review=${reviewId}&signature=${proof}`);
    const sent = stubRequirement([aReview({ signed: true, meanings })], {
      [`POST /reviews/${reviewId}/decisions`]: { status: 200, body: { review: aReview({ state: "approved", approvals: 2 }) } },
    });
    await renderAt("/p/CHK/r/CHK-R1", { me: { ...signedIn, signsWith: "provider" } });
    const dialog = await screen.findByRole("dialog", { name: "Sign: approve" });
    expect(within(dialog).getByRole("status")).toHaveTextContent("You signed in again.");
    expect(window.location.search).toBe("");
    await userEvent.click(within(dialog).getByRole("button", { name: "Sign and approve" }));
    await waitFor(() =>
      expect(sent.find((each) => each.method === "POST")?.body).toEqual({ decision: "approve", revision: 1, comment: "Agreed.", reauthentication: proof }),
    );
  });

  it("says why the provider brought no proof back", async () => {
    window.history.replaceState(null, "", `/p/CHK/r/CHK-R1?sign=reject&review=${reviewId}&signature_error=someone_else`);
    stubRequirement([aReview({ signed: true, meanings })]);
    await renderAt("/p/CHK/r/CHK-R1", { me: { ...signedIn, signsWith: "provider" } });
    const dialog = await screen.findByRole("dialog", { name: "Sign: reject" });
    expect(within(dialog).getByText("The identity provider signed in somebody else. Sign in again as yourself.")).toBeInTheDocument();
    expect(within(dialog).getByRole("link", { name: "Sign in again to sign" })).toBeInTheDocument();
  });

  it("tells somebody with no way to prove who they are that they cannot sign", async () => {
    stubRequirement([aReview({ signed: true, meanings })]);
    await renderAt("/p/CHK/r/CHK-R1", { me: { ...signedIn, signsWith: "" } });
    await userEvent.click(await within(await panel()).findByRole("button", { name: "Approve" }));
    const dialog = await screen.findByRole("dialog", { name: "Sign: approve" });
    expect(within(dialog).getByText(/so you cannot sign/)).toBeInTheDocument();
    expect(within(dialog).queryByRole("button", { name: "Sign and approve" })).toBeNull();
  });

  it("shows each signature under its decision", async () => {
    const signature = {
      meaning: meanings.approve,
      method: "password",
      authenticatedAt: "2026-10-02T08:00:00Z",
      signedAt: "2026-10-02T08:00:00Z",
      revision: 1,
      contentHash: "abc",
    };
    const signedDecisions = aReview().decisions.map((each) => ({ ...each, signature }));
    stubRequirement([aReview({ signed: true, meanings, decisions: signedDecisions })]);
    await renderAt("/p/CHK/r/CHK-R1");
    const decisions = await within(await panel()).findByRole("list", { name: "Decisions" });
    expect(decisions).toHaveTextContent(`with the password: ${meanings.approve}`);
  });
});

describe("an earlier review", () => {
  it("keeps who decided it and why, so the author reads why it was rejected", async () => {
    const rejected = aReview({
      state: "rejected",
      approvals: 0,
      finishedAt: "2026-10-02T09:00:00Z",
      can: { decide: false, withdraw: false },
      decisions: [
        { reviewerId: dave.id, reviewerName: dave.name, decision: "reject", comment: "Say how fast.", decidedAt: "2026-10-02T09:00:00Z", signature: null },
      ],
    });
    stubRequirement([rejected]);
    await renderAt("/p/CHK/r/CHK-R1");
    const past = await within(await panel()).findByRole("list", { name: "Earlier reviews" });
    const row = past.querySelector('[data-past-review="rejected"]') as HTMLElement;
    expect(within(row).getByRole("list", { name: "Decisions" })).toHaveTextContent("Dave Decider rejected: Say how fast.");
  });
});
