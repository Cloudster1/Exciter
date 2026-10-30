import { afterEach, beforeEach, describe, expect, it, onTestFinished, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import {
  ACCESS_PATH,
  ADMIN_PATH,
  USERS_PATH,
  ARMATURE_ACCOUNT_PATH,
  ARMATURE_SETTINGS_PATH,
  AUDIT_PATH,
  INSTALLATION_PATH,
  LINK_TYPES_PATH,
  NOTIFICATION_SETTINGS_PATH,
  NOTIFICATIONS_PATH,
  PROFILE_PATH,
  ALL_CONNECTED_APPS_PATH,
  CONNECTED_APPS_PATH,
  PROJECTS_PATH,
  REVIEWS_PATH,
  SEARCH_PATH,
  STATOR_ACCOUNT_PATH,
  STATOR_SETTINGS_PATH,
  WEBHOOKS_PATH,
  THEMES_PATH,
  TOKENS_PATH,
  WATCHING_PATH,
} from "@/config";
import { arrival, renderAt, setViewportWidth, signedIn, stubApi } from "@/test/app";
import { SETTINGS_PATH } from "./places";
import { GROUPS_KEY, SIDEBAR_KEY } from "./state";

beforeEach(() => localStorage.clear());
afterEach(() => vi.unstubAllGlobals());

const member: Me = { ...signedIn, administers: false };
const membersList = { "GET /users": { status: 200, body: { members: [] } } };
const MEMBERS_TAB = `${ACCESS_PATH}/members`;

describe("the shell", () => {
  it("draws the rail and the sidebar around the page, with no top bar", async () => {
    await renderAt("/");
    const rail = document.querySelector<HTMLElement>("[data-rail]")!;
    const sidebar = document.querySelector<HTMLElement>('[data-sidebar="open"]')!;
    expect(sidebar).not.toBeNull();
    expect(
      within(rail)
        .getAllByRole("link")
        .map((link) => link.getAttribute("href")),
    ).toEqual(["/", SEARCH_PATH, PROJECTS_PATH]);
    const order = Array.from(rail.querySelectorAll("a, button")).map((each) => each.getAttribute("data-action") ?? each.getAttribute("href"));
    expect(order).toEqual(["sidebar", "/", SEARCH_PATH, "notifications", PROJECTS_PATH, "theme", "account"]);
    expect(
      within(sidebar)
        .getAllByRole("link")
        .map((link) => link.querySelector("span")?.textContent),
    ).toEqual(["Home", "Search", "Notifications", "Reviews", "Projects", "Admin"]);
    expect(within(sidebar).queryByRole("link", { name: "Members" })).toBeNull();
    expect(sidebar.querySelector("[data-sidebar-org]")).toHaveTextContent("Demo");
    expect(await within(sidebar).findByText("0.0.0-test (0123456)")).toHaveAttribute("title", "commit 0123456789abcdef");
    expect(document.querySelector("[data-top-bar]")).toBeNull();
    expect(document.querySelector('[data-action="switcher"]')).toBeNull();
    expect(screen.getByRole("heading", { level: 1, name: "Home" })).toBeInTheDocument();
    expect(await screen.findByText("No projects yet")).toBeInTheDocument();
  });

  it("counts the unread notifications and the waiting reviews on their entries", async () => {
    stubApi({
      "GET /notifications/unread-count": { status: 200, body: { unread: 3 } },
      "GET /reviews/waiting": { status: 200, body: { reviews: [], total: 2 } },
    });
    await renderAt("/");
    const sidebar = document.querySelector<HTMLElement>("[data-sidebar]")!;
    await waitFor(() => expect(sidebar.querySelector('[data-count="notifications"]')).toHaveTextContent("3"));
    expect(sidebar.querySelector('[data-count="reviews"]')).toHaveTextContent("2");
    expect(within(sidebar).getByRole("link", { name: /^Reviews.*2 reviews wait for you$/ })).toHaveAttribute("href", REVIEWS_PATH);
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

  it("puts the breadcrumbs in the page's strip, the Admin crumb leading to the Admin page", async () => {
    stubApi(membersList);
    await renderAt(MEMBERS_TAB);
    const strip = document.querySelector<HTMLElement>("[data-shell-strip]")!;
    const crumbs = within(strip).getByRole("navigation", { name: "Breadcrumb" });
    expect(
      within(crumbs)
        .getAllByRole("listitem")
        .map((item) => item.textContent?.replace("/", "").trim()),
    ).toEqual(["Demo", "Admin", "Access"]);
    expect(within(crumbs).getByRole("link", { name: "Demo" })).toHaveAttribute("href", "/");
    expect(within(crumbs).getByRole("link", { name: "Admin" })).toHaveAttribute("href", ADMIN_PATH);
    expect(within(crumbs).getByText("Access")).toHaveAttribute("aria-current", "page");
  });
});

describe("the settings area", () => {
  it("swaps the sidebar for its own, with the person's own pages only, in order", async () => {
    await renderAt(PROFILE_PATH);
    const sidebar = document.querySelector<HTMLElement>("[data-settings-sidebar]")!;
    expect(sidebar).not.toBeNull();
    expect(sidebar.querySelector("[data-sidebar-org]")).toBeNull();
    const links = within(sidebar.querySelector<HTMLElement>('[data-sidebar-group="you"]')!)
      .getAllByRole("link")
      .map((link) => link.getAttribute("href"));
    expect(links).toEqual([
      PROFILE_PATH,
      NOTIFICATION_SETTINGS_PATH,
      WATCHING_PATH,
      THEMES_PATH,
      TOKENS_PATH,
      CONNECTED_APPS_PATH,
      ARMATURE_ACCOUNT_PATH,
      STATOR_ACCOUNT_PATH,
    ]);
    expect(sidebar.querySelectorAll("[data-sidebar-group]")).toHaveLength(1);
    expect(within(sidebar).getByRole("link", { name: "Your profile" })).toHaveAttribute("data-status", "active");
  });

  it("goes back to the last page outside it", async () => {
    stubApi(membersList);
    const router = await renderAt(SEARCH_PATH);
    await router.navigate({ to: `${ACCESS_PATH}/$tab`, params: { tab: "members" } });
    await arrival(router, MEMBERS_TAB);
    // Admin is outside the settings area, so it keeps the main sidebar and is where the way back leads.
    expect(document.querySelector("[data-settings-sidebar]")).toBeNull();
    await router.navigate({ to: PROFILE_PATH });
    await arrival(router, PROFILE_PATH);
    const back = screen.getByRole("link", { name: "Back to Exciter" });
    expect(back).toHaveAttribute("href", MEMBERS_TAB);
    await userEvent.click(back);
    await waitFor(() => expect(router.state.location.pathname).toBe(MEMBERS_TAB));
    expect(document.querySelector("[data-settings-sidebar]")).toBeNull();
  });

  it("opens on a hub of the person's own pages, each with what it is for, for an administrator too", async () => {
    await renderAt(SETTINGS_PATH);
    expect(screen.getByRole("heading", { level: 1, name: "Settings" })).toBeInTheDocument();
    const hub = document.querySelector<HTMLElement>('[data-hub-card="you"]')!;
    const profile = within(hub).getByRole("link", { name: /Your profile/ });
    expect(profile).toHaveAttribute("href", PROFILE_PATH);
    expect(profile).toHaveTextContent("Your name, language and time zone.");
    expect(within(hub).getAllByRole("link")).toHaveLength(8);
    expect(hub.querySelector(`a[href="${ACCESS_PATH}"]`)).toBeNull();
    expect(document.querySelector('[data-hub-card="settings"]')).toBeNull();
  });
});

describe("the Admin area", () => {
  it("lists every page of the installation in its four sections, with the section nav beside it", async () => {
    await renderAt(ADMIN_PATH);
    expect(await screen.findByRole("heading", { level: 1, name: "Admin" })).toBeInTheDocument();
    const section = (id: string) =>
      [...document.querySelectorAll<HTMLElement>(`[data-admin-section="${id}"] [data-admin-card]`)].map((card) => card.getAttribute("href"));
    expect(section("people")).toEqual([INSTALLATION_PATH, ACCESS_PATH, USERS_PATH]);
    expect(section("work")).toEqual([LINK_TYPES_PATH]);
    expect(section("integrations")).toEqual([ARMATURE_SETTINGS_PATH, STATOR_SETTINGS_PATH, ALL_CONNECTED_APPS_PATH, WEBHOOKS_PATH]);
    expect(section("records")).toEqual([AUDIT_PATH]);
    expect(document.querySelector('[data-admin-card="users"]')).toHaveTextContent("switching an account off or back on");
    const nav = screen.getByRole("navigation", { name: "Admin sections" });
    expect(within(nav).getAllByRole("link")).toHaveLength(10);
    expect(document.querySelector("[data-settings-sidebar]")).toBeNull();
  });

  it("puts the section nav behind one menu on a narrow screen", async () => {
    setViewportWidth(500);
    onTestFinished(() => setViewportWidth(1280));
    const router = await renderAt(ADMIN_PATH);
    expect(screen.queryByRole("navigation", { name: "Admin sections" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Admin" }));
    await userEvent.click(within(screen.getByRole("menu", { name: "Section" })).getByRole("menuitem", { name: "Audit log" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(AUDIT_PATH));
  });

  it("refuses a member once, for the whole of it, and leads nowhere from the sidebar", async () => {
    const sent = stubApi({});
    await renderAt(WEBHOOKS_PATH, { me: member });
    expect(await screen.findByRole("heading", { level: 1, name: "Admin" })).toBeInTheDocument();
    expect(screen.getByText("Only administrators can open this page")).toBeInTheDocument();
    expect(document.querySelectorAll("[data-admin-refused]")).toHaveLength(1);
    expect(sent.some((r) => r.path === "/webhooks")).toBe(false);
    expect(document.querySelector('[data-nav="admin"]')).toBeNull();
  });

  it("is one entry at the foot of an administrator's sidebar", async () => {
    await renderAt("/");
    const entry = document.querySelector<HTMLElement>('[data-sidebar] [data-nav="admin"]')!;
    expect(within(entry).getByRole("link", { name: /Admin/ })).toHaveAttribute("href", ADMIN_PATH);
  });

  it("sends the old address of a moved page on to its new one, with its search", async () => {
    const router = await renderAt("/settings/webhooks?from=mail");
    await waitFor(() => expect(router.state.location.pathname).toBe(WEBHOOKS_PATH));
    expect(router.state.location.searchStr).toBe("?from=mail");
  });

  it("leaves an address of the settings area that never was a page not found", async () => {
    await renderAt("/settings/nowhere");
    expect(await screen.findByRole("heading", { level: 1, name: "Page not found" })).toBeInTheDocument();
  });
});

describe("the installation in the shell", () => {
  it("names the installation at the head of the sidebar and the start of the trail, with no switcher", async () => {
    await renderAt("/", { me: { ...signedIn, installation: { name: "Acme Rockets", oauthConnections: true } } });
    expect(document.querySelector("[data-sidebar-org]")).toHaveTextContent("Acme Rockets");
    expect(screen.getByRole("link", { name: "Acme Rockets" })).toHaveAttribute("href", "/");
    expect(document.querySelector('[data-action="switch-org"]')).toBeNull();
  });
});

describe("the quick switcher", () => {
  it("opens from Ctrl K, and hands focus back on Escape", async () => {
    await renderAt("/");
    const home = within(document.querySelector<HTMLElement>("[data-sidebar]")!).getByRole("link", { name: "Home" });
    home.focus();
    await userEvent.keyboard("{Control>}k{/Control}");
    expect(screen.getByRole("dialog", { name: "Go to" })).toBeInTheDocument();
    expect(screen.getByRole("combobox")).toHaveFocus();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "Go to" })).toBeNull();
    expect(home).toHaveFocus();
  });

  it("lists the places, and moves through them from the keyboard", async () => {
    stubApi({ "GET /audit": { status: 200, body: { entries: [], next: null } }, "GET /audit/facets": { status: 404 } });
    const router = await renderAt("/");
    await userEvent.keyboard("{Control>}k{/Control}");
    const input = screen.getByRole("combobox");
    const options = () => within(screen.getByRole("dialog", { name: "Go to" })).getAllByRole("option");
    expect(options().map((o) => o.getAttribute("data-quick-switcher-option"))).toEqual([
      "place:/",
      `place:${SEARCH_PATH}`,
      `place:${NOTIFICATIONS_PATH}`,
      `place:${REVIEWS_PATH}`,
      `place:${PROJECTS_PATH}`,
      `place:${PROFILE_PATH}`,
      `place:${NOTIFICATION_SETTINGS_PATH}`,
      `place:${WATCHING_PATH}`,
      `place:${THEMES_PATH}`,
      `place:${TOKENS_PATH}`,
      `place:${CONNECTED_APPS_PATH}`,
      `place:${ARMATURE_ACCOUNT_PATH}`,
      `place:${STATOR_ACCOUNT_PATH}`,
      `place:${INSTALLATION_PATH}`,
      `place:${ACCESS_PATH}`,
      `place:${USERS_PATH}`,
      `place:${LINK_TYPES_PATH}`,
      `place:${ARMATURE_SETTINGS_PATH}`,
      `place:${STATOR_SETTINGS_PATH}`,
      `place:${ALL_CONNECTED_APPS_PATH}`,
      `place:${WEBHOOKS_PATH}`,
      `place:${AUDIT_PATH}`,
      `place:${ADMIN_PATH}`,
    ]);
    expect(within(screen.getByRole("dialog", { name: "Go to" })).getByRole("option", { name: /Admin \/ Audit log/ })).toBeInTheDocument();
    expect(options()[0]).toHaveAttribute("aria-selected", "true");
    expect(input).toHaveAttribute("aria-activedescendant", options()[0]!.id);

    await userEvent.keyboard("{ArrowDown}{ArrowDown}");
    expect(options()[2]).toHaveAttribute("aria-selected", "true");
    expect(input).toHaveAttribute("aria-activedescendant", options()[2]!.id);
    await userEvent.keyboard("{End}");
    expect(options()[22]).toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{ArrowDown}");
    expect(options()[22]).toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{Home}{ArrowUp}");
    expect(options()[0]).toHaveAttribute("aria-selected", "true");

    await userEvent.type(input, "aud");
    // The audit log, and the full search for what was typed.
    expect(options().map((o) => o.getAttribute("data-quick-switcher-option"))).toEqual([`place:${AUDIT_PATH}`, "search"]);
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe(AUDIT_PATH));
    expect(screen.queryByRole("dialog", { name: "Go to" })).toBeNull();
  });

  it("offers only the places a member may go, and says when nothing matches", async () => {
    await renderAt("/", { me: member });
    await userEvent.keyboard("{Control>}k{/Control}");
    // Home, the search, the notifications, the reviews, the projects, and their own profile, notification settings,
    // watches, themes, tokens, connected apps, and Armature and Stator accounts.
    expect(within(screen.getByRole("listbox")).getAllByRole("option")).toHaveLength(13);
    await userEvent.type(screen.getByRole("combobox"), "prof");
    expect(within(screen.getByRole("dialog", { name: "Go to" })).getAllByRole("option")[0]).toHaveTextContent("Your profile");
    await userEvent.clear(screen.getByRole("combobox"));
    await userEvent.type(screen.getByRole("combobox"), "zzz");
    // Only the full search is left to offer.
    expect(within(screen.getByRole("dialog", { name: "Go to" })).getByRole("option")).toHaveTextContent("Search everything for zzz");
    expect(screen.getByRole("status")).toHaveTextContent("1 place");
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
  it("heads with who is signed in, lists their own pages, and signs them out to the sign-in page", async () => {
    const sent = stubApi({
      "POST /auth/logout": { status: 204 },
      "GET /auth/me": { status: 401, body: { error: { code: "unauthorized", message: "Sign in to continue." } } },
    });
    const router = await renderAt("/");
    await userEvent.click(within(document.querySelector<HTMLElement>("[data-rail]")!).getByRole("button", { name: "Your account" }));
    const header = document.querySelector("[data-account-header]")!;
    expect(header).toHaveTextContent("Ada Lovelace");
    expect(header).toHaveTextContent("ada@exciter.test");
    expect(header).toHaveTextContent("Administrator");
    expect(header.querySelector("[data-account-role]")).toHaveAttribute("data-account-role", "member");
    // The avatar beside who it is.
    expect(header.firstElementChild).not.toBe(header.querySelector("[data-account-name]"));
    const menu = screen.getByRole("menu", { name: "Your account" });
    expect(
      within(menu)
        .getAllByRole("menuitem")
        .map((item) => item.textContent),
    ).toEqual(["Your profile", "Notification settings", "Watching", "Themes", "Access tokens", "Connected apps", "Admin", "Sign out"]);
    expect(within(menu).queryByRole("menuitemradio")).toBeNull();
    await userEvent.click(within(menu).getByRole("menuitem", { name: "Sign out" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(sent.some((r) => r.method === "POST" && r.path === "/auth/logout")).toBe(true);
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("takes an administrator to Admin", async () => {
    const router = await renderAt("/");
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    await userEvent.click(within(screen.getByRole("menu", { name: "Your account" })).getByRole("menuitem", { name: "Admin" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(ADMIN_PATH));
  });

  it("offers a member no Admin", async () => {
    await renderAt("/", { me: member });
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    expect(within(screen.getByRole("menu", { name: "Your account" })).queryByRole("menuitem", { name: "Admin" })).toBeNull();
  });
});

describe("the other tools in the account menu", () => {
  const account = (baseUrl: string | null) => ({ status: 200, body: { account: { configured: baseUrl !== null, baseUrl: baseUrl ?? "" } } });
  const guest: Me = { ...signedIn, role: "guest", administers: false };

  async function openMenu(me: Me = signedIn) {
    await renderAt("/", { me });
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    return screen.getByRole("menu", { name: "Your account" });
  }

  it("links each connected tool's home as a plain link under Switch to, for a guest too", async () => {
    stubApi({ "GET /armature/account": account("https://armature.example/"), "GET /stator/account": account("https://stator.example") });
    const menu = await openMenu(guest);
    const group = await within(menu).findByRole("group", { name: "Switch to" });
    const links = within(group).getAllByRole("menuitem");
    expect(links.map((link) => link.textContent)).toEqual(["Armature", "Stator"]);
    expect(links[0]).toHaveAttribute("href", "https://armature.example/");
    expect(links[1]).toHaveAttribute("href", "https://stator.example/");
    expect(links[0]?.tagName).toBe("A");
    expect(links[0]?.querySelector("img")).toHaveAttribute("alt", "");
    // Each tool's own mark, all in the one format.
    expect(links.map((link) => link.querySelector("img")?.getAttribute("src"))).toEqual(["/tools/armature.webp", "/tools/stator.webp"]);
  });

  it("lists only the tool that is connected", async () => {
    stubApi({ "GET /armature/account": account(null), "GET /stator/account": account("https://stator.example") });
    const menu = await openMenu();
    const group = await within(menu).findByRole("group", { name: "Switch to" });
    expect(
      within(group)
        .getAllByRole("menuitem")
        .map((link) => link.textContent),
    ).toEqual(["Stator"]);
  });

  it("leaves the group out when no tool is connected", async () => {
    const sent = stubApi({ "GET /armature/account": account(null) });
    const menu = await openMenu();
    await waitFor(() => expect(sent.some((r) => r.path === "/stator/account")).toBe(true));
    expect(within(menu).queryByRole("group", { name: "Switch to" })).toBeNull();
    expect(within(menu).queryByText("Switch to")).toBeNull();
  });
});

describe("a session that ends while the page is open", () => {
  it("sends the reader to sign in, and back to the page afterwards", async () => {
    const unauthorized = { status: 401, body: { error: { code: "unauthorized", message: "Sign in to continue." } } };
    stubApi({ "GET /users": unauthorized, "GET /auth/me": unauthorized });
    const router = await renderAt(MEMBERS_TAB);
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(router.state.location.search).toEqual({ next: MEMBERS_TAB });
  });
});

describe("the suite's shell", () => {
  afterEach(() => setViewportWidth(1280));

  it("names the palette's shortcut beside Search, as the reader's keyboard says it", async () => {
    await renderAt("/");
    const search = within(document.querySelector<HTMLElement>("[data-sidebar]")!).getByRole("link", { name: "Search" });
    expect(search.querySelector("[data-shortcut]")).toHaveTextContent("Ctrl K");
  });

  it("says Cmd K on a Mac", async () => {
    vi.spyOn(navigator, "platform", "get").mockReturnValue("MacIntel");
    await renderAt("/");
    expect(document.querySelector("[data-sidebar] [data-shortcut]")).toHaveTextContent("Cmd K");
  });

  it("keeps the menu button, the installation's name and the bell in the narrow strip", async () => {
    setViewportWidth(500);
    await renderAt("/");
    const strip = await waitFor(() => document.querySelector<HTMLElement>("[data-narrow-strip]")!);
    expect(document.querySelectorAll("[data-shell]").length).toBe(1);
    expect(document.querySelectorAll("[data-narrow-strip]").length).toBe(1);
    expect(strip.tagName).toBe("HEADER");
    expect(strip).toHaveClass("h-12");
    expect(within(strip).getByRole("button", { name: "Open the navigation" })).toBeInTheDocument();
    expect(strip.querySelector("[data-installation-name]")).toHaveTextContent("Demo");
    expect(within(strip).getByRole("button", { name: /Notifications/ })).toBeInTheDocument();
  });

  it("lets the account menu's anchor span the rail, so the menu opens past its edge", async () => {
    await renderAt("/");
    const foot = document.querySelector<HTMLElement>("[data-rail-foot]")!;
    expect(foot).toHaveClass("w-full");
    expect(within(foot).getByRole("button", { name: "Your account" }).parentElement).toHaveClass("w-full");
  });
});
