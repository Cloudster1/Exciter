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
};
const self = { userId: signedIn.user.id, email: signedIn.user.email, name: signedIn.user.name, role: "admin", joinedAt: "2026-09-02T08:00:00Z" };
const bob = {
  userId: "0195f000-0000-7000-8000-000000000002",
  email: "bob@exciter.test",
  name: "Bob Builder",
  role: "member",
  joinedAt: "2026-09-03T08:00:00Z",
};

/** The members list as the API keeps it, changed by what the screen sends. */
function api(extra: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}) {
  let members = [owner, self, bob];
  const sent = stubApi({
    "GET /users": () => ({ status: 200, body: { members } }),
    [`PATCH /users/${bob.userId}`]: async (request) => {
      const { role } = (await request.json()) as { role: string };
      members = members.map((m) => (m.userId === bob.userId ? { ...m, role } : m));
      return { status: 200, body: { member: { ...bob, role } } };
    },
    [`DELETE /users/${bob.userId}`]: () => {
      members = members.filter((m) => m.userId !== bob.userId);
      return { status: 204 };
    },
    ...extra,
  });
  return sent;
}

const row = (email: string) => document.querySelector<HTMLElement>(`[data-member="${email}"]`);

describe("the members screen", () => {
  it("lists everybody with their role, and lets no one change an owner or themselves", async () => {
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
        body: { error: { code: "conflict", message: "An owner keeps their role. Make somebody else the owner first." } },
      },
    });
    await renderAt(MEMBERS_PATH);
    await userEvent.selectOptions(await screen.findByRole("combobox", { name: "Role of Bob Builder" }), "guest");
    expect(await screen.findByRole("alert")).toHaveTextContent("An owner keeps their role.");
  });

  it("asks before removing somebody, and does nothing when the answer is no", async () => {
    const sent = api();
    await renderAt(MEMBERS_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Remove Bob Builder" }));
    const dialog = screen.getByRole("dialog", { name: "Remove Bob Builder?" });
    expect(dialog).toHaveTextContent("They lose access to this organization at once");
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(sent.some((r) => r.method === "DELETE")).toBe(false);
    expect(screen.getByRole("button", { name: "Remove Bob Builder" })).toHaveFocus();
  });

  it("removes somebody once confirmed, and keeps the focus on the page", async () => {
    const sent = api();
    await renderAt(MEMBERS_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Remove Bob Builder" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(sent.some((r) => r.method === "DELETE" && r.path === `/users/${bob.userId}`)).toBe(true));
    await waitFor(() => expect(row(bob.email)).toBeNull());
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("status")).toHaveTextContent("Bob Builder was removed from the organization.");
    await waitFor(() => expect(screen.getByRole("button", { name: "Add person" })).toHaveFocus());
  });

  it("tells a member the screen is the administrators'", async () => {
    const sent = api();
    await renderAt(MEMBERS_PATH, { me: { ...signedIn, role: "member" } });
    expect(await screen.findByText(/Only an owner or administrator of this organization manages its members/)).toBeInTheDocument();
    expect(sent.filter((request) => request.path.startsWith("/users"))).toHaveLength(0);
  });
});
