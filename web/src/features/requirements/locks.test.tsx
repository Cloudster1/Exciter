import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Requirement } from "@/api/requirements";
import { answerConfirm, renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { aWorkflow, approved, draft, rejected, review } from "@/test/workflows";

// Locked requirements (#205): a requirement locked by its status or by hand says why and offers no edit, and the
// project's administrators lock and unlock it by hand and make a status lock.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const released = { id: draft.id, name: "Released", category: "done" as const };
const byStatus: Requirement["lock"] = { by: "status", status: released, reason: "", lockedByName: "", lockedAt: null };
const byHand: Requirement["lock"] = { by: "hand", status: released, reason: "Released in 2.0", lockedByName: "Ada Lovelace", lockedAt: "2026-10-02T08:00:00Z" };
const lockedCan = { edit: false, transition: true, delete: false, link: false, comment: true };

function stubRequirement(requirement: Requirement, more: Record<string, Answer> = {}, administer = true) {
  return stubApi({
    "GET /projects/CHK": {
      status: 200,
      body: { project: aProject(administer ? {} : { can: { view: true, comment: true, edit: true, approve: false, administer: false, purge: false } }) },
    },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement } },
    "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
    "GET /requirements/CHK-R1/comments": { status: 200, body: { threads: [] } },
    "GET /requirements/CHK-R1/reviews": { status: 200, body: { reviews: [], total: 0, limit: 20, offset: 0 } },
    ...more,
  });
}

describe("a locked requirement", () => {
  it("says which status locks it, still takes comments, and offers no edit", async () => {
    stubRequirement(aRequirement({ status: released, lock: byStatus, can: lockedCan }), {}, false);
    await renderAt("/p/CHK/r/CHK-R1");
    const banner = await screen.findByText("CHK-R1 is locked in the status Released. Take it out of Released to change it.");
    expect(banner.closest("[data-lock]")).toHaveAttribute("data-lock", "status");
    expect(screen.getByText("It still takes comments, reviews and transitions.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Lock" })).toBeNull();
    expect(screen.queryByText("You may read this project but not change its requirements.")).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("is locked by hand by an administrator, with a reason", async () => {
    const sent = stubRequirement(aRequirement(), {
      "POST /requirements/CHK-R1/lock": { status: 200, body: { requirement: aRequirement({ lock: byHand, can: lockedCan }) } },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(await screen.findByRole("button", { name: "Lock" }));
    const dialog = await screen.findByRole("dialog", { name: "Lock CHK-R1" });
    expect(within(dialog).getByRole("button", { name: "Lock" })).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText("Why"), "Released in 2.0");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "Lock" }));
    await waitFor(() => expect(sent.find((each) => each.method === "POST")?.body).toEqual({ reason: "Released in 2.0" }));
    expect(await screen.findByText("CHK-R1 is locked.")).toBeInTheDocument();
    expect(await screen.findByText("CHK-R1 is locked by Ada Lovelace: Released in 2.0")).toBeInTheDocument();
  });

  it("is unlocked by hand by an administrator", async () => {
    const sent = stubRequirement(aRequirement({ lock: byHand, can: lockedCan }), {
      "DELETE /requirements/CHK-R1/lock": { status: 200, body: { requirement: aRequirement() } },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(await screen.findByRole("button", { name: "Unlock" }));
    expect(await answerConfirm(true)).toHaveTextContent("Unlock this requirement?");
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE")).toBe(true));
    expect(await screen.findByText("CHK-R1 is unlocked.")).toBeInTheDocument();
  });

  it("offers nobody an unlock of a lock by its status", async () => {
    stubRequirement(aRequirement({ status: released, lock: byStatus, can: lockedCan }));
    await renderAt("/p/CHK/r/CHK-R1");
    expect(await screen.findByText(/is locked in the status Released/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Unlock" })).toBeNull();
  });
});

describe("a status that locks", () => {
  it("is set by an administrator and marked in the list", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/workflows": { status: 200, body: { workflows: [aWorkflow()] } },
      "GET /projects/CHK/statuses": { status: 200, body: { statuses: [draft, review, { ...approved, locks: true }, rejected] } },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
      "GET /workflow-rule-types": { status: 200, body: { ruleTypes: [] } },
      [`PATCH /projects/CHK/statuses/${draft.id}`]: { status: 200, body: { status: { ...draft, locks: true } } },
    });
    await renderAt("/p/CHK/settings?tab=workflow");
    const approvedRow = await waitFor(() => {
      const row = document.querySelector('[data-status-row="Approved"]');
      expect(row).not.toBeNull();
      return row as HTMLElement;
    });
    expect(approvedRow.querySelector("[data-status-locks]")).toHaveTextContent("Locks");
    const draftRow = document.querySelector('[data-status-row="Draft"]') as HTMLElement;
    await userEvent.click(within(draftRow).getByRole("button", { name: "Edit" }));
    await userEvent.click(screen.getByLabelText("Locks its requirements"));
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Save status" }));
    await waitFor(() => expect(sent.find((each) => each.method === "PATCH")?.body).toMatchObject({ locks: true }));
  });
});
