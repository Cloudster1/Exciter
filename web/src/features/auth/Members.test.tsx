import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MEMBERS_PATH } from "@/config";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";

afterEach(() => vi.unstubAllGlobals());

const owner = {
  userId: "0195f000-0000-7000-8000-000000000001",
  email: "owner@exciter.test",
  name: "Olive Owner",
  role: "owner",
  joinedAt: "2026-09-01T08:00:00Z",
  active: true,
};
const self = { userId: signedIn.user.id, email: signedIn.user.email, name: signedIn.user.name, role: "admin", joinedAt: "2026-09-02T08:00:00Z", active: true };
const bob = {
  userId: "0195f000-0000-7000-8000-000000000002",
  email: "bob@exciter.test",
  name: "Bob Builder",
  role: "member",
  joinedAt: "2026-09-03T08:00:00Z",
  active: true,
};

/** The members list as the API keeps it, changed by what the screen sends. */
function api(extra: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}) {
  let members = [owner, self, bob];
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

describe("the members screen", () => {
  it("lists everybody with their role, and lets no administrator change an owner or themselves", async () => {
    api();
    await renderAt(MEMBERS_PATH);
    await screen.findByText("Bob Builder");
    expect(screen.getByText("3 members")).toBeInTheDocument();
    expect(within(row(owner.email)!).getByText("Owner")).toBeInTheDocument();
    expect(within(row(owner.email)!).queryByRole("combobox")).toBeNull();
    expect(within(row(owner.email)!).queryByRole("button")).toBeNull();
    expect(within(row(self.email)!).getByText("(you)")).toBeInTheDocument();
    expect(within(row(self.email)!).queryByRole("button")).toBeNull();
    expect(screen.getByRole("combobox", { name: "Role of Bob Builder" })).toHaveValue("member");
  });

  it("changes a role and says so", async () => {
    const sent = api();
    await renderAt(MEMBERS_PATH);
    await userEvent.selectOptions(await screen.findByRole("combobox", { name: "Role of Bob Builder" }), "admin");
    await waitFor(() => expect(sent.find((r) => r.method === "PATCH")).toEqual({ method: "PATCH", path: `/users/${bob.userId}`, body: { role: "admin" } }));
    expect(await screen.findByRole("status")).toHaveTextContent("Bob Builder is now Administrator.");
    await waitFor(() => expect(screen.getByRole("combobox", { name: "Role of Bob Builder" })).toHaveValue("admin"));
  });

  it("shows why a role change was refused", async () => {
    api({
      [`PATCH /users/${bob.userId}`]: {
        status: 409,
        body: { error: { code: "conflict", message: "An owner's role does not change. Change somebody else's instead." } },
      },
    });
    await renderAt(MEMBERS_PATH);
    await userEvent.selectOptions(await screen.findByRole("combobox", { name: "Role of Bob Builder" }), "guest");
    expect(await screen.findByRole("alert")).toHaveTextContent("An owner's role does not change.");
  });

  it("lets an owner switch another owner off", async () => {
    api();
    await renderAt(MEMBERS_PATH, { me: { ...signedIn, role: "owner" } });
    expect(await screen.findByRole("button", { name: "Switch Olive Owner off" })).toBeInTheDocument();
    expect(within(row(owner.email)!).queryByRole("combobox")).toBeNull();
  });

  it("asks before switching somebody off, and does nothing when the answer is no", async () => {
    const sent = api();
    await renderAt(MEMBERS_PATH);
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
    await renderAt(MEMBERS_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Switch Bob Builder off" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Switch off" }));
    await waitFor(() => expect(sent.find((r) => r.method === "PATCH")).toEqual({ method: "PATCH", path: `/users/${bob.userId}`, body: { active: false } }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("status")).toHaveTextContent("Bob Builder was switched off.");
    await waitFor(() => expect(within(row(bob.email)!).getByText("Switched off")).toBeInTheDocument());
    await waitFor(() => expect(screen.getByRole("button", { name: "Add person" })).toHaveFocus());

    await userEvent.click(screen.getByRole("button", { name: "Switch Bob Builder on" }));
    expect(await screen.findByRole("status")).toHaveTextContent("Bob Builder was switched on and can sign in again.");
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
    await renderAt(MEMBERS_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Switch Bob Builder off" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Switch off" }));
    expect(await within(screen.getByRole("dialog")).findByRole("alert")).toHaveTextContent("Make somebody else an administrator first.");
  });

  it("tells a member the screen is the administrators'", async () => {
    const sent = api();
    await renderAt(MEMBERS_PATH, { me: { ...signedIn, role: "member" } });
    expect(await screen.findByText(/Only an administrator manages the members/)).toBeInTheDocument();
    expect(sent.filter((request) => request.path.startsWith("/users"))).toHaveLength(0);
  });
});
