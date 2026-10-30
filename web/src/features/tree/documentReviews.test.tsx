import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SpecEntry } from "@/api/tree";
import { renderAt } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aRequirement } from "@/test/requirements";
import { entry, entryOf, node, pay, scope, ship, stubDocument } from "@/test/spec";
import { byOutline, progressOf, withDecided } from "./DocumentReviews";

afterEach(() => vi.unstubAllGlobals());

const pending = (over: Partial<NonNullable<SpecEntry["review"]>> = {}) => ({
  id: "rv-pay",
  revision: 1,
  required: 1,
  approvals: 0,
  rejections: 0,
  approver: true,
  mayDecide: true,
  decision: null,
  ...over,
});

describe("reviews in the document", () => {
  it("orders outlines as the document reads, keeps what was decided in place, and counts it", () => {
    expect(["1.10", "1.9", "2", "1"].sort(byOutline)).toEqual(["1", "1.9", "1.10", "2"]);
    const decided = new Map([["CHK-R2", entry(pay, "", { review: pending({ decision: "approve" }) })]]);
    const shown = withDecided([entry(scope, ""), entry(ship, "", { review: pending({ id: "rv-ship" }) })], decided);
    expect(shown.map((each) => each.node.key)).toEqual(["CHK-R1", "CHK-R2", "CHK-R3"]);
    expect(progressOf(shown)).toEqual({ decided: 1, total: 2 });
  });

  it("asks for what waits for the reader, approves in place and keeps the decided entry in view", async () => {
    let decidedPay = false;
    const asked: URLSearchParams[] = [];
    const sent = stubDocument([], {
      "GET /projects/CHK/spec": (request) => {
        asked.push(new URL(request.url).searchParams);
        const waiting = [entry(scope, "What is in scope."), entry(ship, "Ship in a day.", { review: pending({ id: "rv-ship" }) })];
        if (!decidedPay) waiting.splice(1, 0, entry(pay, "Pay at once.", { review: pending() }));
        return {
          status: 200,
          body: { entries: waiting, total: waiting.length, limit: 50, offset: 0, changes: { base: "approved", baselineId: null, changed: 0, removed: [] } },
        };
      },
      "POST /reviews/rv-pay/decisions": () => {
        decidedPay = true;
        return { status: 200, body: { review: { id: "rv-pay" } } };
      },
    });
    const router = await renderAt("/projects/CHK/spec");
    await userEvent.click(await screen.findByRole("button", { name: "Review what waits for me" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ waiting: true, changes: "approved" }));
    await waitFor(() => expect(asked.some((query) => query.get("waiting") === "true" && query.get("changes") === "approved")).toBe(true));
    expect(await screen.findByText("0 of 2 decided")).toBeInTheDocument();
    const reject = within(entryOf("CHK-R2")).getByRole("button", { name: "Reject" });
    expect(reject).toBeDisabled();
    await userEvent.click(within(entryOf("CHK-R2")).getByRole("button", { name: "Approve" }));
    await waitFor(() => expect(sent.find((r) => r.path === "/reviews/rv-pay/decisions")?.body).toEqual({ decision: "approve", revision: 1 }));
    expect(await within(entryOf("CHK-R2")).findByText("You approved it.")).toBeInTheDocument();
    expect(screen.getByText("1 of 2 decided")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Leave review" }));
    await waitFor(() => expect(router.state.location.search).toEqual({}));
  });

  it("says a review a newer revision superseded takes no decision", async () => {
    stubDocument([entry(pay, "Pay at once.", { review: pending() })], {
      "POST /reviews/rv-pay/decisions": { status: 409, body: { error: { code: "review_closed", message: "This review is no longer waiting." } } },
    });
    await renderAt("/projects/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    await userEvent.type(within(entryOf("CHK-R2")).getByLabelText("Your comment"), "Not like this.");
    await userEvent.click(within(entryOf("CHK-R2")).getByRole("button", { name: "Reject" }));
    expect(await within(entryOf("CHK-R2")).findByText(/A newer revision superseded this review/)).toBeInTheDocument();
    expect(within(entryOf("CHK-R2")).queryByRole("button", { name: "Approve" })).toBeNull();
  });

  it("asks for the review of the picked requirements, and says which already wait", async () => {
    const sent = stubDocument([entry(scope, ""), entry(node({ ...pay, revision: 3 }), "Pay at once."), entry(ship, "Ship in a day.")], {
      "GET /projects/CHK/transitions": { status: 200, body: { requirements: [] } },
      "POST /projects/CHK/reviews": {
        status: 200,
        body: {
          results: [
            { key: "CHK-R2", outcome: "requested", review: { id: "rv-new" }, error: null },
            { key: "CHK-R3", outcome: "refused", review: null, error: { code: "review_pending", message: "A review of this requirement is already waiting." } },
          ],
        },
      },
    });
    await renderAt("/projects/CHK/spec");
    await userEvent.click(await screen.findByRole("checkbox", { name: "Select everything in Scope" }));
    await userEvent.click(within(document.body.querySelector<HTMLElement>("[data-selection-bar]")!).getByRole("button", { name: "Ask for review" }));
    await waitFor(() =>
      expect(sent.find((r) => r.path === "/projects/CHK/reviews")?.body).toEqual({
        items: [
          { key: "CHK-R2", revision: 3 },
          { key: "CHK-R3", revision: 1 },
        ],
      }),
    );
    expect(await screen.findByText(/A review of it is already waiting\./)).toBeInTheDocument();
    expect(document.body.querySelector("[data-spec-announced]")).toHaveTextContent("1 review asked for, 1 already waiting.");
  });

  it("says how a section's review went though its button went away while it was asked", async () => {
    const added = { kind: "added" as const, from: 0, to: 1, movedFrom: null, comparison: null };
    let answer: () => void = () => {};
    const answered = new Promise<void>((resolve) => {
      answer = resolve;
    });
    stubDocument([entry(scope, ""), entry(pay, "Pay at once.", { change: added }), entry(ship, "Ship in a day.", { change: added })], {
      // The answer waits until the button is gone, as a refetch showing both in review takes it away.
      "POST /projects/CHK/reviews": async () => {
        await answered;
        return {
          status: 200,
          body: { results: ["CHK-R2", "CHK-R3"].map((key) => ({ key, outcome: "requested", review: { id: "rv" }, error: null })) },
        };
      },
    });
    await renderAt("/projects/CHK/spec?changes=approved");
    await userEvent.click(await screen.findByRole("button", { name: "Ask for review of what changed here" }));
    await userEvent.click(screen.getByRole("switch", { name: "Show changes" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Ask for review of what changed here" })).toBeNull());
    answer();
    await waitFor(() => expect(document.body.querySelector("[data-spec-announced]")).toHaveTextContent("2 reviews asked for."));
  });

  it("opens an entry's comments inside it", async () => {
    stubDocument([entry(pay, "Pay at once.", { comments: { threads: 1, open: 1 } })], {
      "GET /requirements/CHK-R2": { status: 200, body: { requirement: aRequirement({ key: "CHK-R2", title: "Pay" }) } },
      "GET /requirements/CHK-R2/comments": { status: 200, body: { threads: [] } },
    });
    await renderAt("/projects/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    await userEvent.click(within(entryOf("CHK-R2")).getByRole("button", { name: "Comments (1 open)" }));
    expect(await within(entryOf("CHK-R2")).findByRole("button", { name: "Hide the comments" })).toHaveAttribute("aria-expanded", "true");
    await waitFor(() => expect(entryOf("CHK-R2").querySelector("[data-entry-comments] [data-comments-empty]")).not.toBeNull());
  });
});
