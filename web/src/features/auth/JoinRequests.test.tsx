import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MEMBERS_PATH, SSO_PATH } from "@/config";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => vi.unstubAllGlobals());

const carol = { userId: "0195f000-0000-7000-8000-00000000c0c0", email: "carol@suite.test", name: "Carol New", requestedAt: "2026-09-29T08:00:00Z" };
const dave = { userId: "0195f000-0000-7000-8000-00000000da7e", email: "dave@suite.test", name: "", requestedAt: "2026-09-29T08:05:00Z" };
const provider = { provider: null, callbackUrl: "http://localhost:5173/api/v1/auth/oidc/callback" };

describe("the people waiting to be let in", () => {
  it("are listed on the single sign-on page, and let in as members with one click", async () => {
    let waiting = [carol, dave];
    const sent = stubApi({
      "GET /users/requests": () => ({ status: 200, body: { requests: waiting } }),
      "GET /oidc-provider": { status: 200, body: provider },
      [`POST /users/requests/${carol.userId}/admit`]: () => {
        waiting = [dave];
        return { status: 200, body: { membership: { orgId: "o-demo", orgSlug: "demo", orgName: "Demo", role: "member" } } };
      },
    });
    await renderAt(SSO_PATH);
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
      "GET /oidc-provider": { status: 200, body: provider },
      [`DELETE /users/requests/${carol.userId}`]: () => {
        waiting = [];
        return { status: 204 };
      },
    });
    await renderAt(SSO_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Turn away" }));
    await waitFor(() => expect(sent.some((r) => r.method === "DELETE" && r.path === `/users/requests/${carol.userId}`)).toBe(true));
    expect(await screen.findByText("Carol New was turned away.")).toBeInTheDocument();
  });

  it("take no room when there are none", async () => {
    stubApi({ "GET /users/requests": { status: 200, body: { requests: [] } }, "GET /oidc-provider": { status: 200, body: provider } });
    await renderAt(SSO_PATH);
    await screen.findByLabelText("Issuer");
    expect(document.querySelector("[data-join-requests]")).toBeNull();
  });

  it("show a refusal as it came", async () => {
    stubApi({
      "GET /users/requests": { status: 200, body: { requests: [carol] } },
      "GET /oidc-provider": { status: 200, body: provider },
      [`POST /users/requests/${carol.userId}/admit`]: {
        status: 404,
        body: { error: { code: "not_found", message: "That person is not waiting to be let in. Reload the list; somebody may have answered already." } },
      },
    });
    await renderAt(SSO_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Let in as member" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("That person is not waiting to be let in.");
  });

  it("pass axe", async () => {
    stubApi({ "GET /users/requests": { status: 200, body: { requests: [carol, dave] } }, "GET /oidc-provider": { status: 200, body: provider } });
    await renderAt(SSO_PATH);
    await screen.findByText("Carol New");
    expect(await axeViolations()).toEqual([]);
  });
});

describe("the members page", () => {
  it("shows the people waiting above the list, to be let in from there too", async () => {
    stubApi({
      "GET /users/requests": { status: 200, body: { requests: [carol] } },
      "GET /users": { status: 200, body: { members: [] } },
    });
    await renderAt(MEMBERS_PATH);
    const heading = await screen.findByRole("heading", { name: "Waiting to be let in" });
    expect(within(heading.closest("[data-join-requests]") as HTMLElement).getByRole("button", { name: "Let in as member" })).toBeInTheDocument();
  });
});
