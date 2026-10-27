import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";

afterEach(() => vi.unstubAllGlobals());

// The trash as each role meets it (#309): its administrators empty it after saying so, its editors restore only,
// and a reader is told they may not open it.

const ADMINS_ONLY = "Only the project's administrators delete from the trash for good.";
const READ_ONLY = "You may read this project but not change its requirements.";

const item = {
  id: "0195f000-0000-7000-8000-0000000000a1",
  key: "CHK-R1",
  kind: "section",
  title: "Ways to pay",
  trashedAt: "2026-10-12T08:00:00Z",
  trashedByName: "Ada",
  requirements: 3,
  parentInTree: true,
};

const editor = aProject({ can: { view: true, comment: true, edit: true, approve: false, administer: false, purge: false } });
const reader = aProject({ can: { view: true, comment: true, edit: false, approve: false, administer: false, purge: false } });

describe("emptying the trash", () => {
  it("asks first, and empties it only once confirmed", async () => {
    let items = [item];
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/trash": () => ({ status: 200, body: { items } }),
      "DELETE /projects/CHK/trash": () => {
        items = [];
        return { status: 204 };
      },
    });
    await renderAt("/p/CHK/trash");
    await screen.findByText("Ways to pay");
    await userEvent.click(screen.getByRole("button", { name: "Empty the trash" }));
    const dialog = screen.getByRole("dialog", { name: "Empty the trash for good?" });
    expect(dialog).toHaveTextContent("Everything in the trash and its history will be gone. This cannot be undone.");
    expect(sent.some((r) => r.method === "DELETE")).toBe(false);

    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(sent.some((r) => r.method === "DELETE")).toBe(false);

    await userEvent.click(screen.getByRole("button", { name: "Empty the trash" }));
    await userEvent.click(within(screen.getByRole("dialog", { name: "Empty the trash for good?" })).getByRole("button", { name: "Delete for good" }));
    await waitFor(() => expect(sent.filter((r) => r.method === "DELETE").map((r) => r.path)).toEqual(["/projects/CHK/trash"]));
    expect(await screen.findByText("The trash is empty.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Empty the trash" })).toBeNull();
  });

  it("is not offered to an editor, who is told who deletes for good", async () => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: editor } },
      "GET /projects/CHK/trash": { status: 200, body: { items: [item] } },
    });
    await renderAt("/p/CHK/trash");
    const entry = (await screen.findByText("Ways to pay")).closest("tr")!;
    expect(screen.getByText(ADMINS_ONLY)).toBeInTheDocument();
    expect(within(entry).getByRole("button", { name: "Restore" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Empty the trash" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete for good" })).toBeNull();
  });

  it("is not opened for a reader, who is told why", async () => {
    const sent = stubApi({ "GET /projects/CHK": { status: 200, body: { project: reader } } });
    await renderAt("/p/CHK/trash");
    expect(await screen.findByText(READ_ONLY)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Empty the trash" })).toBeNull();
    expect(screen.queryByText(ADMINS_ONLY)).toBeNull();
    expect(sent.some((r) => r.path === "/projects/CHK/trash")).toBe(false);
  });
});
