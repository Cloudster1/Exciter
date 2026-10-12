import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import { AUDIT_PATH, MEMBERS_PATH } from "@/config";
import { renderAt, signedIn, stubApi } from "@/test/app";
import { GROUPS_KEY, SIDEBAR_KEY } from "./state";

beforeEach(() => localStorage.clear());
afterEach(() => vi.unstubAllGlobals());

const member: Me = { ...signedIn, organization: { ...signedIn.organization!, role: "member" } };
const twoOrgs: Me = {
  ...signedIn,
  organizations: [...signedIn.organizations, { orgId: "0195f000-0000-7000-8000-0000000000d1", orgSlug: "acme", orgName: "Acme Rockets", role: "member" }],
};
const membersList = { "GET /users": { status: 200, body: { members: [] } } };

describe("the shell", () => {
  it("draws the rail, the sidebar and the top bar around the page", async () => {
    await renderAt("/");
    const rail = document.querySelector<HTMLElement>("[data-rail]")!;
    const sidebar = document.querySelector<HTMLElement>('[data-sidebar="open"]')!;
    expect(sidebar).not.toBeNull();
    expect(within(rail).getByRole("link", { name: "Home" })).toHaveAttribute("href", "/");
    expect(within(rail).getByRole("link", { name: "Settings" })).toHaveAttribute("href", "/settings");
    expect(within(rail).getAllByRole("link")).toHaveLength(2);
    expect(within(sidebar).getByRole("link", { name: "Home" })).toHaveAttribute("href", "/");
    expect(within(sidebar).getByRole("link", { name: "Members" })).toHaveAttribute("href", MEMBERS_PATH);
    expect(within(sidebar).getByRole("link", { name: "Audit log" })).toHaveAttribute("href", AUDIT_PATH);
    expect(sidebar.querySelector("[data-sidebar-org]")).toHaveTextContent("Demo");
    expect(document.querySelector("[data-top-bar]")).not.toBeNull();
    expect(screen.getByRole("button", { name: "Your account" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1, name: "Home" })).toBeInTheDocument();
    expect(screen.getByText("No projects yet")).toBeInTheDocument();
  });

  it("keeps the settings from somebody who does not administer the organization", async () => {
    await renderAt("/", { me: member });
    expect(document.querySelector("[data-rail]")?.querySelectorAll("a")).toHaveLength(1);
    expect(document.querySelector('[data-sidebar-group="settings"]')).toBeNull();
  });

  it("folds the sidebar to the rail and remembers it", async () => {
    await renderAt("/");
    await userEvent.click(screen.getByRole("button", { name: "Collapse the sidebar" }));
    expect(document.querySelector("[data-sidebar]")).toBeNull();
    expect(document.querySelector("[data-rail]")).not.toBeNull();
    expect(localStorage.getItem(SIDEBAR_KEY)).toBe("rail");

    await userEvent.click(screen.getByRole("button", { name: "Expand the sidebar" }));
    expect(document.querySelector('[data-sidebar="open"]')).not.toBeNull();
    expect(localStorage.getItem(SIDEBAR_KEY)).toBe("open");
  });

  it("folds a sidebar group by its title and remembers it", async () => {
    await renderAt("/");
    await userEvent.click(screen.getByRole("button", { name: "Requirements" }));
    expect(document.querySelector('[data-sidebar-group="requirements"]')).toHaveAttribute("data-open", "false");
    expect(JSON.parse(localStorage.getItem(GROUPS_KEY) ?? "{}")).toEqual({ requirements: false });
  });

  it("marks where the reader is, in the sidebar, the rail and the breadcrumbs", async () => {
    stubApi(membersList);
    await renderAt(MEMBERS_PATH);
    const sidebar = document.querySelector<HTMLElement>("[data-sidebar]")!;
    expect(within(sidebar).getByRole("link", { name: "Members" })).toHaveAttribute("data-status", "active");
    expect(within(sidebar).getByRole("link", { name: "Home" })).not.toHaveAttribute("data-status", "active");
    const rail = document.querySelector<HTMLElement>("[data-rail]")!;
    expect(within(rail).getByRole("link", { name: "Settings" })).toHaveAttribute("data-status", "active");
    const crumbs = screen.getByRole("navigation", { name: "Breadcrumb" });
    expect(
      within(crumbs)
        .getAllByRole("listitem")
        .map((item) => item.textContent?.replace("/", "").trim()),
    ).toEqual(["Demo", "Settings", "Members"]);
    expect(within(crumbs).getByRole("link", { name: "Demo" })).toHaveAttribute("href", "/");
    expect(within(crumbs).getByText("Members")).toHaveAttribute("aria-current", "page");
  });

  it("takes the settings entry of the rail to the members", async () => {
    stubApi(membersList);
    const router = await renderAt("/");
    await userEvent.click(within(document.querySelector<HTMLElement>("[data-rail]")!).getByRole("link", { name: "Settings" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(MEMBERS_PATH));
  });
});

describe("the organization switcher", () => {
  it("is not there for somebody in one organization", async () => {
    await renderAt("/");
    expect(document.querySelector('[data-action="switch-org"]')).toBeNull();
  });

  it("moves the session to another organization", async () => {
    const sent = stubApi({
      "POST /auth/switch-org": {
        status: 200,
        body: { organization: { id: "0195f000-0000-7000-8000-0000000000d1", slug: "acme", name: "Acme Rockets", role: "member" } },
      },
      "GET /auth/me": {
        status: 200,
        body: { ...twoOrgs, organization: { id: "0195f000-0000-7000-8000-0000000000d1", slug: "acme", name: "Acme Rockets", role: "member" } },
      },
    });
    await renderAt("/", { me: twoOrgs });
    await userEvent.click(screen.getByRole("button", { name: "Switch organization, now Demo" }));
    const menu = screen.getByRole("menu", { name: "Your organizations" });
    expect(within(menu).getByRole("menuitemradio", { name: "Demo" })).toHaveAttribute("aria-checked", "true");
    await userEvent.click(within(menu).getByRole("menuitemradio", { name: "Acme Rockets" }));
    await waitFor(() => expect(sent.find((r) => r.path === "/auth/switch-org")?.body).toEqual({ slug: "acme" }));
    await waitFor(() => expect(document.querySelector("[data-sidebar-org]")).toHaveTextContent("Acme Rockets"));
  });
});

describe("the quick switcher", () => {
  it("opens from the top bar and from Ctrl K, and hands focus back on Escape", async () => {
    await renderAt("/");
    const box = document.querySelector<HTMLButtonElement>('[data-action="switcher"]')!;
    await userEvent.click(box);
    expect(screen.getByRole("dialog", { name: "Go to" })).toBeInTheDocument();
    expect(box).toHaveAttribute("aria-expanded", "true");
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "Go to" })).toBeNull();
    expect(box).toHaveFocus();

    await userEvent.keyboard("{Control>}k{/Control}");
    expect(screen.getByRole("combobox")).toHaveFocus();
  });

  it("lists the places and the organizations, and moves through them from the keyboard", async () => {
    stubApi({ "GET /audit": { status: 200, body: { entries: [], next: null } }, "GET /audit/facets": { status: 404 } });
    const router = await renderAt("/", { me: twoOrgs });
    await userEvent.keyboard("{Control>}k{/Control}");
    const input = screen.getByRole("combobox");
    const options = () => within(screen.getByRole("listbox")).getAllByRole("option");
    expect(options().map((o) => o.getAttribute("data-quick-switcher-option"))).toEqual([
      "place:/",
      `place:${MEMBERS_PATH}`,
      `place:${AUDIT_PATH}`,
      "org:demo",
      "org:acme",
    ]);
    expect(options()[0]).toHaveAttribute("aria-selected", "true");
    expect(input).toHaveAttribute("aria-activedescendant", options()[0]!.id);

    await userEvent.keyboard("{ArrowDown}{ArrowDown}");
    expect(options()[2]).toHaveAttribute("aria-selected", "true");
    expect(input).toHaveAttribute("aria-activedescendant", options()[2]!.id);
    await userEvent.keyboard("{End}");
    expect(options()[4]).toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{ArrowDown}");
    expect(options()[4]).toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{Home}{ArrowUp}");
    expect(options()[0]).toHaveAttribute("aria-selected", "true");

    await userEvent.type(input, "aud");
    expect(options()).toHaveLength(1);
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe(AUDIT_PATH));
    expect(screen.queryByRole("dialog", { name: "Go to" })).toBeNull();
  });

  it("offers only the places a member may go, and says when nothing matches", async () => {
    await renderAt("/", { me: member });
    await userEvent.keyboard("{Control>}k{/Control}");
    expect(within(screen.getByRole("listbox")).getAllByRole("option")).toHaveLength(2);
    await userEvent.type(screen.getByRole("combobox"), "zzz");
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(document.querySelector("[data-quick-switcher-empty]")).not.toBeNull();
    expect(screen.getByRole("status")).toHaveTextContent("0 places");
  });
});

describe("a path that leads nowhere", () => {
  it("says so inside the shell, with a heading, and offers the way home", async () => {
    const router = await renderAt("/no/such/page");
    expect(await screen.findByRole("heading", { level: 1, name: "Page not found" })).toBeInTheDocument();
    expect(document.querySelector("[data-not-found]")).not.toBeNull();
    expect(document.querySelector("[data-sidebar]")).not.toBeNull();

    await userEvent.click(screen.getByRole("button", { name: "Go home" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(await screen.findByText("No projects yet")).toBeInTheDocument();
  });
});

describe("the account menu", () => {
  it("shows who is signed in and signs them out to the sign-in page", async () => {
    const sent = stubApi({
      "POST /auth/logout": { status: 204 },
      "GET /auth/me": { status: 401, body: { error: { code: "unauthorized", message: "Sign in to continue." } } },
    });
    const router = await renderAt("/");
    expect(document.querySelector("[data-account-name]")).toHaveTextContent("Ada Lovelace");
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    await userEvent.click(within(screen.getByRole("menu", { name: "Your account" })).getByRole("menuitem", { name: "Sign out" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(sent.some((r) => r.method === "POST" && r.path === "/auth/logout")).toBe(true);
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });
});

describe("a session that ends while the page is open", () => {
  it("sends the reader to sign in, and back to the page afterwards", async () => {
    const unauthorized = { status: 401, body: { error: { code: "unauthorized", message: "Sign in to continue." } } };
    stubApi({ "GET /users": unauthorized, "GET /auth/me": unauthorized });
    const router = await renderAt(MEMBERS_PATH);
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(router.state.location.search).toEqual({ next: MEMBERS_PATH });
  });
});
