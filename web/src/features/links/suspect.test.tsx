import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Comparison } from "@/api/history";
import type { SuspectLink } from "@/api/links";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aLink, derives } from "@/test/links";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const sys = { key: "SYS-R2", title: "Keep a ledger" };
// CHK-R1 derives from SYS-R2, which moved from revision 1 to 2 since the link was confirmed.
const moved = {
  ...aLink(sys),
  other: { ...aLink(sys).other, revision: 2 },
  suspect: [{ end: "target" as const, reviewed: 1, current: 2 }],
};

const comparison: Comparison = {
  from: { number: 1, title: "Keep a ledger", type: null, authorName: "Ada", createdAt: "2026-10-01T08:00:00Z" },
  to: { number: 2, title: "Keep a ledger of every payment", type: null, authorName: "Ada", createdAt: "2026-10-02T08:00:00Z" },
  title: [{ change: "equal", node: { type: "paragraph", content: [{ type: "text", text: "Keep a ledger" }] } }],
  statement: [{ change: "modified", node: { type: "paragraph", content: [{ type: "text", text: "every payment", marks: [{ type: "diffInsert" }] }] } }],
  rationale: [],
  attributes: [],
};

function stubRequirement(more: Parameters<typeof stubApi>[0] = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement: aRequirement() } },
    "GET /requirements/CHK-R1/links": { status: 200, body: { links: [moved] } },
    "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [derives] } },
    "GET /requirements/SYS-R2/diff": { status: 200, body: { comparison } },
    [`GET /links/${moved.id}/clearances`]: {
      status: 200,
      body: {
        clearances: [
          {
            id: "0195f000-0000-7000-8000-0000000000c1",
            sourceRevision: 1,
            targetRevision: 1,
            comment: "Agreed at the start.",
            clearedByName: "Bob",
            clearedAt: "2026-09-01T08:00:00Z",
          },
        ],
      },
    },
    ...more,
  });
}

describe("a suspect link", () => {
  it("is marked, and checking it shows what changed before it is confirmed against both ends", async () => {
    const sent = stubRequirement({ [`POST /links/${moved.id}/clear`]: { status: 200, body: { link: { ...moved, suspect: [] } } } });
    await renderAt("/p/CHK/r/CHK-R1");
    const panel = await screen.findByRole("region", { name: "Trace links" });
    const row = (await within(panel).findByRole("link", { name: "SYS-R2 Keep a ledger" })).closest("li")!;
    expect(within(row).getByText("Suspect")).toBeInTheDocument();
    await userEvent.click(within(row).getByRole("button", { name: "Check the link to SYS-R2" }));

    const dialog = await screen.findByRole("dialog", { name: "Check: CHK-R1 derives from SYS-R2" });
    const change = await within(dialog).findByRole("region", { name: "SYS-R2, revision 1 to 2" });
    expect(await within(change).findByText("every payment")).toBeInTheDocument();
    expect(await within(dialog).findByRole("list", { name: "Confirmed before" })).toHaveTextContent("Agreed at the start.");
    expect(within(dialog).getByRole("button", { name: "Confirm link" })).toBeDisabled();
    expect(await axeViolations()).toEqual([]);

    await userEvent.type(within(dialog).getByLabelText("Why the link still holds"), "The ledger still covers it.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Confirm link" }));
    await waitFor(() =>
      expect(sent.find((each) => each.method === "POST")?.body).toEqual({ sourceRevision: 1, targetRevision: 2, comment: "The ledger still covers it." }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("says so when an end moved on again while it was read", async () => {
    stubRequirement({
      [`POST /links/${moved.id}/clear`]: {
        status: 409,
        body: {
          error: {
            code: "revision_moved_on",
            message: "A requirement of this link has a newer revision than the one you read. Reload it and read the change first.",
          },
        },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(await screen.findByRole("button", { name: "Check the link to SYS-R2" }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.type(within(dialog).getByLabelText("Why the link still holds"), "Fine.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Confirm link" }));
    expect(await within(dialog).findByText(/has a newer revision than the one you read/)).toBeInTheDocument();
  });

  it("offers a reader no check", async () => {
    stubRequirement({
      "GET /requirements/CHK-R1": {
        status: 200,
        body: { requirement: aRequirement({ can: { edit: false, transition: false, delete: false, link: false, comment: false } }) },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    expect(await screen.findByText("Suspect")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Check the link/ })).toBeNull();
  });
});

describe("a project's suspect links", () => {
  const waiting: SuspectLink = {
    id: moved.id,
    type: derives,
    source: { ...aLink({ key: "CHK-R1", title: "Pay the basket" }).other },
    target: moved.other,
    ends: moved.suspect,
    since: "2026-10-02T08:00:00Z",
  };

  it("are listed the longest waiting first, each checked in place", async () => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/suspects": { status: 200, body: { links: [waiting], total: 1, limit: 20, offset: 0 } },
      "GET /requirements/SYS-R2/diff": { status: 200, body: { comparison } },
      [`GET /links/${moved.id}/clearances`]: { status: 200, body: { clearances: [] } },
    });
    await renderAt("/p/CHK/suspects");
    const list = await screen.findByRole("list", { name: "Suspect links" });
    const item = within(list).getByRole("listitem");
    expect(within(item).getByRole("link", { name: "CHK-R1 Pay the basket" })).toHaveAttribute("href", "/p/CHK/r/CHK-R1");
    expect(within(item).getByRole("link", { name: "SYS-R2 Keep a ledger" })).toHaveAttribute("href", "/p/SYS/r/SYS-R2");
    expect(item).toHaveTextContent("SYS-R2, revision 1 to 2");
    expect(item).toHaveTextContent(/Waiting since/);
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(item).getByRole("button", { name: "Check: CHK-R1 derives from SYS-R2" }));
    expect(await screen.findByRole("dialog", { name: "Check: CHK-R1 derives from SYS-R2" })).toBeInTheDocument();
  });

  it("say so when none waits", async () => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/suspects": { status: 200, body: { links: [], total: 0, limit: 20, offset: 0 } },
    });
    await renderAt("/p/CHK/suspects");
    expect(await screen.findByText("No link waits to be checked.")).toBeInTheDocument();
  });
});
