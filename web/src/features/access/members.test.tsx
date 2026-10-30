import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ACCESS_PATH } from "@/config";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";

afterEach(() => vi.unstubAllGlobals());

const MEMBERS_TAB = `${ACCESS_PATH}/members`;
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

describe("the Members tab", () => {
  it("is where the members' page of before leads", async () => {
    api();
    const router = await renderAt("/settings/members");
    await waitFor(() => expect(router.state.location.pathname).toBe(MEMBERS_TAB));
    expect(await screen.findByRole("tab", { name: "Members" })).toHaveAttribute("aria-selected", "true");
  });

  it("lists everybody with their standing and roles, by name or through a group, and keeps the reader's own standing", async () => {
    api();
    await renderAt(MEMBERS_TAB);
    await screen.findByText("Bob Builder");
    expect(screen.getByText("3 members")).toBeInTheDocument();
    expect(row(olive.email)!.querySelector('[data-member-holds="administrator"]')).toHaveTextContent("Administrator");
    expect(row(olive.email)!.querySelector('[data-member-holds="viewer"]')).toHaveTextContent("Viewer through Everyone");
    expect(within(row(bob.email)!).getByText("None")).toBeInTheDocument();
    expect(within(row(self.email)!).getByText("(you)")).toBeInTheDocument();
    expect(within(row(self.email)!).queryByRole("combobox")).toBeNull();
    expect(screen.queryByRole("button", { name: /^Switch / })).toBeNull();
    expect(screen.getByRole("combobox", { name: "Standing of Bob Builder" })).toHaveValue("member");
    expect(within(screen.getByRole("combobox", { name: "Standing of Bob Builder" })).queryByRole("option", { name: "Administrator" })).toBeNull();
  });

  it("makes somebody a guest and says so", async () => {
    const sent = api();
    await renderAt(MEMBERS_TAB);
    await userEvent.selectOptions(await screen.findByRole("combobox", { name: "Standing of Bob Builder" }), "guest");
    await waitFor(() => expect(sent.find((r) => r.method === "PATCH")).toEqual({ method: "PATCH", path: `/users/${bob.userId}`, body: { role: "guest" } }));
    expect(await screen.findByText("Bob Builder is a guest now.")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("combobox", { name: "Standing of Bob Builder" })).toHaveValue("guest"));
  });

  it("shows why a role change was refused", async () => {
    api({
      [`PATCH /users/${bob.userId}`]: {
        status: 409,
        body: { error: { code: "last_administrator", message: "An administrator stays a member. Take the administrator's grant away first." } },
      },
    });
    await renderAt(MEMBERS_TAB);
    await userEvent.selectOptions(await screen.findByRole("combobox", { name: "Standing of Bob Builder" }), "guest");
    expect(await screen.findByRole("alert")).toHaveTextContent("An administrator stays a member.");
  });

  it("tells a member the screen is the administrators'", async () => {
    const sent = api();
    await renderAt(MEMBERS_TAB, { me: { ...signedIn, administers: false } });
    expect(await screen.findByText("Only administrators can open this page")).toBeInTheDocument();
    expect(sent.filter((request) => request.path.startsWith("/users"))).toHaveLength(0);
  });
});
