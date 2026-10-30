import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { USERS_PATH } from "@/config";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => vi.unstubAllGlobals());

const carol = { userId: "0195f000-0000-7000-8000-00000000c0c0", email: "carol@suite.test", name: "Carol New", requestedAt: "2026-09-29T08:00:00Z" };
const dave = { userId: "0195f000-0000-7000-8000-00000000da7e", email: "dave@suite.test", name: "", requestedAt: "2026-09-29T08:05:00Z" };
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
const members = { status: 200, body: { members: [bob] } };

describe("the people waiting to be let in", () => {
  it("are listed on the Members tab, and let in as members with one click", async () => {
    let waiting = [carol, dave];
    const sent = stubApi({
      "GET /users/requests": () => ({ status: 200, body: { requests: waiting } }),
      "GET /users": members,
      [`POST /users/requests/${carol.userId}/admit`]: () => {
        waiting = [dave];
        return { status: 200, body: { membership: { userId: carol.userId, role: "member" } } };
      },
    });
    await renderAt(USERS_PATH);
    const list = await screen.findByRole("heading", { name: "Waiting to be let in" });
    const card = list.closest("[data-join-requests]") as HTMLElement;
    const row = within(card).getByText("Carol New").closest("li") as HTMLElement;
    expect(row).toHaveTextContent("carol@suite.test, asked");
    // Somebody the provider named no one is shown by address.
    expect(within(card).getAllByText("dave@suite.test").length).toBeGreaterThan(0);

    await userEvent.click(within(row).getByRole("button", { name: "Let in as member" }));
    await waitFor(() => expect(sent.some((r) => r.method === "POST")).toBe(true));
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ role: "member" });
    expect(await screen.findByText("Carol New may sign in now.")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText("Carol New")).toBeNull());
  });

  it("are turned away, which asks the server to forget the request", async () => {
    let waiting = [carol];
    const sent = stubApi({
      "GET /users/requests": () => ({ status: 200, body: { requests: waiting } }),
      "GET /users": members,
      [`DELETE /users/requests/${carol.userId}`]: () => {
        waiting = [];
        return { status: 204 };
      },
    });
    await renderAt(USERS_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Turn away" }));
    await waitFor(() => expect(sent.some((r) => r.method === "DELETE" && r.path === `/users/requests/${carol.userId}`)).toBe(true));
    expect(await screen.findByText("Carol New was turned away.")).toBeInTheDocument();
  });

  it("take no room when there are none", async () => {
    stubApi({ "GET /users/requests": { status: 200, body: { requests: [] } }, "GET /users": members });
    await renderAt(USERS_PATH);
    await screen.findByText("Bob Builder");
    expect(document.querySelector("[data-join-requests]")).toBeNull();
  });

  it("show a refusal as it came", async () => {
    stubApi({
      "GET /users/requests": { status: 200, body: { requests: [carol] } },
      "GET /users": members,
      [`POST /users/requests/${carol.userId}/admit`]: {
        status: 404,
        body: { error: { code: "not_found", message: "That person is not waiting to be let in. Reload the list; somebody may have answered already." } },
      },
    });
    await renderAt(USERS_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Let in as member" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("That person is not waiting to be let in.");
  });

  it("pass axe", async () => {
    stubApi({ "GET /users/requests": { status: 200, body: { requests: [carol, dave] } }, "GET /users": members });
    await renderAt(USERS_PATH);
    await screen.findByText("Carol New");
    expect(await axeViolations()).toEqual([]);
  });
});

describe("the members' page of before", () => {
  it("leads to the Members tab, and the people waiting are under Users", async () => {
    stubApi({
      "GET /users/requests": { status: 200, body: { requests: [carol] } },
      "GET /users": { status: 200, body: { members: [] } },
    });
    const router = await renderAt("/settings/members");
    await waitFor(() => expect(router.state.location.pathname).toBe("/admin/access/members"));
    expect(document.querySelector("[data-join-requests]")).toBeNull();
    await router.navigate({ to: USERS_PATH });
    const heading = await screen.findByRole("heading", { name: "Waiting to be let in" });
    expect(within(heading.closest("[data-join-requests]") as HTMLElement).getByRole("button", { name: "Let in as member" })).toBeInTheDocument();
  });
});
