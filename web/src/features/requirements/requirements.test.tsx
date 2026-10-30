import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { arrival, renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

afterEach(() => vi.unstubAllGlobals());

const checkout = aProject();
const reader = aProject({ can: { view: true, comment: false, edit: false, approve: false, administer: false, purge: false } });
const pay = aRequirement();
const row = { ...pay, outline: "", updatedAt: pay.updatedAt, attributes: {}, labels: [], trashed: false };
const list = (rows: unknown[]) => ({ status: 200, body: { rows, total: rows.length, limit: 50, offset: 0 } });

describe("a project's requirements", () => {
  it("lists them by key, each a link to its page, and offers editors a new one", async () => {
    stubApi({ "GET /projects/CHK": { status: 200, body: { project: checkout } }, "GET /projects/CHK/requirements": list([row]) });
    await renderAt("/projects/CHK");
    const link = await screen.findByRole("link", { name: "Pay the basket" });
    expect(link).toHaveAttribute("href", "/projects/CHK/r/CHK-R1");
    expect(link.closest("tr")).toHaveAttribute("data-requirement-row", "CHK-R1");
    expect(screen.getByRole("button", { name: "New requirement" })).toBeInTheDocument();
  });

  it("tells a reader nothing is written yet, and offers no button", async () => {
    stubApi({ "GET /projects/CHK": { status: 200, body: { project: reader } }, "GET /projects/CHK/requirements": list([]) });
    await renderAt("/projects/CHK");
    expect(await screen.findByText("Nothing is written in this project yet.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "New requirement" })).toBeNull();
  });
});

describe("a new requirement", () => {
  it("writes a section without a rationale, and opens it", async () => {
    const section = aRequirement({ kind: "section", title: "Checkout", key: "CHK-R2", number: 2 });
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "POST /projects/CHK/requirements": { status: 201, body: { requirement: section } },
      "GET /requirements/CHK-R2": { status: 200, body: { requirement: section } },
    });
    const router = await renderAt("/projects/CHK/new");
    await userEvent.click(await screen.findByRole("button", { name: "Section" }));
    expect(screen.queryByLabelText("Rationale")).toBeNull();
    await userEvent.type(screen.getByLabelText("Title"), "Checkout");
    await userEvent.click(screen.getByRole("button", { name: "New requirement" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/projects/CHK/r/CHK-R2"));
    await arrival(router, "/projects/CHK/r/CHK-R2");
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ kind: "section", title: "Checkout", comment: "" });
  });
});

describe("a requirement's page", () => {
  it("shows what it says, and saves a change over the revision it was opened at", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "GET /requirements/CHK-R1": { status: 200, body: { requirement: pay } },
      "PATCH /requirements/CHK-R1": async (request) => {
        const body = (await request.json()) as { title: string };
        return { status: 200, body: { requirement: aRequirement({ title: body.title, revision: 2 }) } };
      },
    });
    await renderAt("/projects/CHK/r/CHK-R1");
    expect(await screen.findByText("The shopper pays at once.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Edit" }));
    const title = screen.getByLabelText("Title");
    await userEvent.clear(title);
    await userEvent.type(title, "Pay the whole basket");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Saved as revision 2.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PATCH")?.body).toMatchObject({ title: "Pay the whole basket", baseRevision: 1, rationale: null });
  });

  it("keeps the draft when somebody saved first, and saves over their revision when asked", async () => {
    let current = pay;
    let saves = 0;
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "GET /requirements/CHK-R1": () => ({ status: 200, body: { requirement: current } }),
      "PATCH /requirements/CHK-R1": () => {
        saves++;
        if (saves === 1) {
          current = aRequirement({ title: "Somebody else's", revision: 2 });
          return {
            status: 409,
            body: {
              error: {
                code: "revision_conflict",
                message: "Somebody saved a newer revision of CHK-R1 while you were editing. Compare it with your changes, then save again over it.",
              },
            },
          };
        }
        return { status: 200, body: { requirement: aRequirement({ title: "Mine", revision: 3 }) } };
      },
    });
    await renderAt("/projects/CHK/r/CHK-R1");
    await userEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const title = screen.getByLabelText("Title");
    await userEvent.clear(title);
    await userEvent.type(title, "Mine");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText(/Somebody saved a newer revision of CHK-R1/)).toBeInTheDocument();
    expect(screen.getByLabelText("Title")).toHaveValue("Mine");
    await userEvent.click(await screen.findByRole("button", { name: "Save over revision 2" }));
    expect(await screen.findByText("Saved as revision 3.")).toBeInTheDocument();
    expect(sent.filter((r) => r.method === "PATCH").map((r) => (r.body as { baseRevision: number }).baseRevision)).toEqual([1, 2]);
  });

  it("moves to the current key when opened by one it had", async () => {
    const moved = aRequirement({ key: "PAY-R1", projectKey: "PAY", formerKeys: ["CHK-R1"] });
    stubApi({
      "GET /requirements/CHK-R1": { status: 200, body: { requirement: moved } },
      "GET /projects/PAY": { status: 200, body: { project: aProject({ key: "PAY", formerKeys: ["CHK"] }) } },
    });
    const router = await renderAt("/projects/CHK/r/CHK-R1");
    await waitFor(() => expect(router.state.location.pathname).toBe("/projects/PAY/r/PAY-R1"));
    await arrival(router, "/projects/PAY/r/PAY-R1");
    expect(await screen.findByText("Former keys, which still work: CHK-R1")).toBeInTheDocument();
  });

  it("offers a reader nothing to change", async () => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: reader } },
      "GET /requirements/CHK-R1": {
        status: 200,
        body: { requirement: aRequirement({ can: { edit: false, transition: false, delete: false, link: false, comment: false } }) },
      },
    });
    await renderAt("/projects/CHK/r/CHK-R1");
    expect(await screen.findByText("You may read this project but not change its requirements.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Move to trash" })).toBeNull();
  });
});

describe("a permalink", () => {
  it("opens the requirement under its current key", async () => {
    stubApi({
      [`GET /requirements/by-id/${pay.id}`]: { status: 200, body: { requirement: pay } },
      "GET /requirements/CHK-R1": { status: 200, body: { requirement: pay } },
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
    });
    const router = await renderAt(`/r/${pay.id}`);
    await waitFor(() => expect(router.state.location.pathname).toBe("/projects/CHK/r/CHK-R1"));
    await arrival(router, "/projects/CHK/r/CHK-R1");
  });
});

describe("the trash", () => {
  it("restores for an editor, and deletes for good only after asking an administrator", async () => {
    const item = {
      id: pay.id,
      key: "CHK-R1",
      kind: "requirement",
      title: "Pay the basket",
      trashedAt: "2026-10-12T08:00:00Z",
      trashedByName: "Ada",
      requirements: 1,
      parentInTree: true,
    };
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "GET /projects/CHK/trash": { status: 200, body: { items: [item] } },
      [`POST /projects/CHK/trash/${pay.id}/restore`]: { status: 200, body: { requirement: pay } },
      [`DELETE /projects/CHK/trash/${pay.id}`]: { status: 204 },
    });
    await renderAt("/projects/CHK/settings/trash");
    const entry = (await screen.findByText("Pay the basket")).closest("tr")!;
    await userEvent.click(within(entry).getByRole("button", { name: "Restore" }));
    expect(await screen.findByText("CHK-R1 is back.")).toBeInTheDocument();
    await userEvent.click(within(entry).getByRole("button", { name: "Delete for good" }));
    const dialog = screen.getByRole("dialog", { name: "Delete this requirement for good?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete for good" }));
    await waitFor(() => expect(sent.some((r) => r.method === "DELETE" && r.path === `/projects/CHK/trash/${pay.id}`)).toBe(true));
  });
});
