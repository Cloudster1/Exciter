import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import {
  ARMATURE_ACCOUNT_PATH,
  ARMATURE_SETTINGS_PATH,
  AUDIT_PATH,
  INSTALLATION_PATH,
  LINK_TYPES_PATH,
  MEMBERS_PATH,
  NOTIFICATION_SETTINGS_PATH,
  NOTIFICATIONS_PATH,
  PROFILE_PATH,
  ALL_TOKENS_PATH,
  ALL_CONNECTED_APPS_PATH,
  CONNECTED_APPS_PATH,
  PROJECTS_PATH,
  REVIEWS_PATH,
  SEARCH_PATH,
  SSO_PATH,
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

const member: Me = { ...signedIn, role: "member" };
const membersList = { "GET /users": { status: 200, body: { members: [] } } };

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
    ).toEqual(["Home", "Search", "Notifications", "Reviews", "Projects"]);
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

  it("puts the breadcrumbs in the page's strip, the Settings crumb leading to the settings area's hub", async () => {
    stubApi(membersList);
    await renderAt(MEMBERS_PATH);
    const strip = document.querySelector<HTMLElement>("[data-shell-strip]")!;
    const crumbs = within(strip).getByRole("navigation", { name: "Breadcrumb" });
    expect(
      within(crumbs)
        .getAllByRole("listitem")
        .map((item) => item.textContent?.replace("/", "").trim()),
    ).toEqual(["Demo", "Settings", "Members"]);
    expect(within(crumbs).getByRole("link", { name: "Demo" })).toHaveAttribute("href", "/");
    expect(within(crumbs).getByRole("link", { name: "Settings" })).toHaveAttribute("href", SETTINGS_PATH);
    expect(within(crumbs).getByText("Members")).toHaveAttribute("aria-current", "page");
  });
});

describe("the settings area", () => {
  it("swaps the sidebar for its own, You for everyone and Settings for an administrator, in order", async () => {
    stubApi(membersList);
    await renderAt(MEMBERS_PATH);
    const sidebar = document.querySelector<HTMLElement>("[data-settings-sidebar]")!;
    expect(sidebar).not.toBeNull();
    expect(sidebar.querySelector("[data-sidebar-org]")).toBeNull();
    const links = (group: string) =>
      within(sidebar.querySelector<HTMLElement>(`[data-sidebar-group="${group}"]`)!)
        .getAllByRole("link")
        .map((link) => link.getAttribute("href"));
    expect(links("you")).toEqual([
      PROFILE_PATH,
      NOTIFICATION_SETTINGS_PATH,
      WATCHING_PATH,
      THEMES_PATH,
      TOKENS_PATH,
      CONNECTED_APPS_PATH,
      ARMATURE_ACCOUNT_PATH,
      STATOR_ACCOUNT_PATH,
    ]);
    expect(links("settings")).toEqual([
      INSTALLATION_PATH,
      MEMBERS_PATH,
      SSO_PATH,
      LINK_TYPES_PATH,
      ALL_TOKENS_PATH,
      ALL_CONNECTED_APPS_PATH,
      ARMATURE_SETTINGS_PATH,
      STATOR_SETTINGS_PATH,
      WEBHOOKS_PATH,
      AUDIT_PATH,
    ]);
    expect(within(sidebar).getByRole("link", { name: "Members" })).toHaveAttribute("data-status", "active");
  });

  it("keeps the installation's settings from a member", async () => {
    await renderAt(PROFILE_PATH, { me: member });
    const sidebar = document.querySelector<HTMLElement>("[data-settings-sidebar]")!;
    expect(sidebar.querySelector('[data-sidebar-group="you"]')).not.toBeNull();
    expect(sidebar.querySelector('[data-sidebar-group="settings"]')).toBeNull();
  });

  it("goes back to the last page outside it", async () => {
    stubApi(membersList);
    const router = await renderAt(SEARCH_PATH);
    await router.navigate({ to: MEMBERS_PATH });
    await arrival(router, MEMBERS_PATH);
    await router.navigate({ to: PROFILE_PATH });
    await arrival(router, PROFILE_PATH);
    const back = screen.getByRole("link", { name: "Back to Exciter" });
    expect(back).toHaveAttribute("href", SEARCH_PATH);
    await userEvent.click(back);
    await waitFor(() => expect(router.state.location.pathname).toBe(SEARCH_PATH));
    expect(document.querySelector("[data-settings-sidebar]")).toBeNull();
  });

  it("opens on a hub of the person's pages, and the installation's for an administrator", async () => {
    await renderAt(SETTINGS_PATH);
    expect(screen.getByRole("heading", { level: 1, name: "Settings" })).toBeInTheDocument();
    expect(within(document.querySelector<HTMLElement>('[data-hub-card="you"]')!).getByRole("link", { name: "Your profile" })).toHaveAttribute(
      "href",
      PROFILE_PATH,
    );
    expect(within(document.querySelector<HTMLElement>('[data-hub-card="settings"]')!).getByRole("link", { name: "Members" })).toHaveAttribute(
      "href",
      MEMBERS_PATH,
    );
  });

  it("shows a member only the You card of the hub", async () => {
    await renderAt(SETTINGS_PATH, { me: member });
    expect(document.querySelector('[data-hub-card="you"]')).not.toBeNull();
    expect(document.querySelector('[data-hub-card="settings"]')).toBeNull();
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
      `place:${MEMBERS_PATH}`,
      `place:${SSO_PATH}`,
      `place:${LINK_TYPES_PATH}`,
      `place:${ALL_TOKENS_PATH}`,
      `place:${ALL_CONNECTED_APPS_PATH}`,
      `place:${ARMATURE_SETTINGS_PATH}`,
      `place:${STATOR_SETTINGS_PATH}`,
      `place:${WEBHOOKS_PATH}`,
      `place:${AUDIT_PATH}`,
    ]);
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
    expect(header.querySelector("[data-account-role]")).toHaveAttribute("data-account-role", "admin");
    // The avatar beside who it is.
    expect(header.firstElementChild).not.toBe(header.querySelector("[data-account-name]"));
    const menu = screen.getByRole("menu", { name: "Your account" });
    expect(
      within(menu)
        .getAllByRole("menuitem")
        .map((item) => item.textContent),
    ).toEqual(["Your profile", "Notification settings", "Watching", "Themes", "Access tokens", "Connected apps", "Settings", "Sign out"]);
    expect(within(menu).queryByRole("menuitemradio")).toBeNull();
    await userEvent.click(within(menu).getByRole("menuitem", { name: "Sign out" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(sent.some((r) => r.method === "POST" && r.path === "/auth/logout")).toBe(true);
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("takes an administrator to the settings area", async () => {
    const router = await renderAt("/");
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    await userEvent.click(within(screen.getByRole("menu", { name: "Your account" })).getByRole("menuitem", { name: "Settings" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(SETTINGS_PATH));
  });

  it("offers a member no Settings", async () => {
    await renderAt("/", { me: member });
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    expect(within(screen.getByRole("menu", { name: "Your account" })).queryByRole("menuitem", { name: "Settings" })).toBeNull();
  });
});

describe("the other tools in the account menu", () => {
  const account = (baseUrl: string | null) => ({ status: 200, body: { account: { configured: baseUrl !== null, baseUrl: baseUrl ?? "" } } });
  const guest: Me = { ...signedIn, role: "guest" };

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
    const router = await renderAt(MEMBERS_PATH);
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(router.state.location.search).toEqual({ next: MEMBERS_PATH });
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
