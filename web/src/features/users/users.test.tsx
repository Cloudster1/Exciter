import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { USERS_PATH } from "@/config";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";

afterEach(() => vi.unstubAllGlobals());

const everyone = "0195f000-0000-7000-8000-00000000e0e0";

const olive = {
  userId: "0195f000-0000-7000-8000-000000000001",
  email: "olive@exciter.test",
  name: "Olive Other",
  role: "member",
  administers: true,
  joinedAt: "2026-09-01T08:00:00Z",
  active: true,
  roles: [
    { role: "administrator", name: "Administrator", groupId: null, groupName: null, groupBuiltin: null },
    { role: "viewer", name: "Viewer", groupId: everyone, groupName: "Everyone", groupBuiltin: "everyone" },
  ],
};
const self = {
  userId: signedIn.user.id,
  email: signedIn.user.email,
  name: signedIn.user.name,
  role: "member",
  administers: true,
  joinedAt: "2026-09-02T08:00:00Z",
  active: true,
  roles: [{ role: "administrator", name: "Administrator", groupId: null, groupName: null, groupBuiltin: null }],
};
const bob = {
  userId: "0195f000-0000-7000-8000-000000000002",
  email: "bob@exciter.test",
  name: "Bob Builder",
  role: "member",
  administers: false,
  joinedAt: "2026-09-03T08:00:00Z",
  active: true,
  roles: [],
};

/** The members list as the API keeps it, changed by what the screen sends. */
function api(extra: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}) {
  let members = [olive, self, bob];
  const sent = stubApi({
    "GET /users": () => ({ status: 200, body: { members } }),
    [`PATCH /users/${bob.userId}`]: async (request) => {
      const change = (await request.json()) as { role?: string; active?: boolean };
      members = members.map((m) => (m.userId === bob.userId ? { ...m, ...change } : m));
      return { status: 200, body: { member: members.find((m) => m.userId === bob.userId) } };
    },
    ...extra,
  });
  return sent;
}

const row = (email: string) => document.querySelector<HTMLElement>(`[data-member="${email}"]`);

describe("the Users page", () => {
  it("lets an administrator switch another administrator off", async () => {
    api();
    await renderAt(USERS_PATH);
    expect(await screen.findByRole("button", { name: "Switch Olive Other off" })).toBeInTheDocument();
  });

  it("asks before switching somebody off, and does nothing when the answer is no", async () => {
    const sent = api();
    await renderAt(USERS_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Switch Bob Builder off" }));
    const dialog = screen.getByRole("dialog", { name: "Switch Bob Builder off?" });
    expect(dialog).toHaveTextContent("They are signed out at once, on every device");
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(sent.some((r) => r.method === "PATCH")).toBe(false);
    expect(screen.getByRole("button", { name: "Switch Bob Builder off" })).toHaveFocus();
  });

  it("switches somebody off once confirmed and keeps them listed, then on again", async () => {
    const sent = api();
    await renderAt(USERS_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Switch Bob Builder off" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Switch off" }));
    await waitFor(() => expect(sent.find((r) => r.method === "PATCH")).toEqual({ method: "PATCH", path: `/users/${bob.userId}`, body: { active: false } }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(await screen.findByText("Bob Builder was switched off.")).toBeInTheDocument();
    await waitFor(() => expect(within(row(bob.email)!).getByText("Switched off")).toBeInTheDocument());
    await waitFor(() => expect(screen.getByRole("button", { name: "Add person" })).toHaveFocus());

    await userEvent.click(screen.getByRole("button", { name: "Switch Bob Builder on" }));
    expect(await screen.findByText("Bob Builder was switched on and can sign in again.")).toBeInTheDocument();
    await waitFor(() => expect(row(bob.email)).toHaveAttribute("data-member-active", "true"));
  });

  it("shows why switching off was refused inside the question", async () => {
    api({
      [`PATCH /users/${bob.userId}`]: {
        status: 409,
        body: {
          error: {
            code: "last_administrator",
            message: "The last active administrator cannot be switched off. Make somebody else an administrator first.",
          },
        },
      },
    });
    await renderAt(USERS_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Switch Bob Builder off" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Switch off" }));
    expect(await within(screen.getByRole("dialog")).findByRole("alert")).toHaveTextContent("Make somebody else an administrator first.");
  });

  it("lists every account with whether it can sign in, and offers the reader no switch for their own", async () => {
    api();
    await renderAt(USERS_PATH);
    await screen.findByText("Bob Builder");
    expect(screen.getByText("3 members")).toBeInTheDocument();
    expect(within(row(bob.email)!).getByText("Can sign in")).toBeInTheDocument();
    expect(within(row(self.email)!).queryByRole("button")).toBeNull();
    expect(screen.getByRole("button", { name: "Add person" })).toBeInTheDocument();
  });

  it("tells a member the page is the administrators' and asks nothing", async () => {
    const sent = api();
    await renderAt(USERS_PATH, { me: { ...signedIn, administers: false } });
    expect(await screen.findByText("Only administrators can open this page")).toBeInTheDocument();
    expect(sent.filter((request) => request.path.startsWith("/users"))).toHaveLength(0);
  });
});
