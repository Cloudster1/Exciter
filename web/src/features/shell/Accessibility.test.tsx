import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AUDIT_PATH, MEMBERS_PATH, SIDEBAR_DRAWER_BELOW_PX } from "@/config";
import { renderAt, setViewportWidth, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

// jsdom's own width, which is what every other shell test runs at.
const WIDE = 1024;
const PHONE = 360;

beforeEach(() => {
  localStorage.clear();
  setViewportWidth(WIDE);
});

// Unmounted first, so widening the window redraws nothing outside act.
afterEach(() => {
  cleanup();
  setViewportWidth(WIDE);
  vi.unstubAllGlobals();
});

const drawer = () => document.querySelector<HTMLElement>('[role="dialog"][data-sidebar="drawer"]');
const menuButton = () => screen.getByRole("button", { name: "Open the navigation" });

const member = {
  userId: "0195f000-0000-7000-8000-00000000000b",
  email: "bob@exciter.test",
  name: "Bob Builder",
  role: "member",
  joinedAt: "2026-09-01T08:00:00Z",
};
const settingsApi = () =>
  stubApi({
    "GET /users": { status: 200, body: { members: [member] } },
    "GET /audit": { status: 200, body: { entries: [], next: null } },
    "GET /audit/facets": { status: 200, body: { facets: { actions: [], actors: [], targetTypes: [], retentionDays: 365 } } },
  });

describe("the skip link", () => {
  it("is the first thing Tab reaches and moves focus to the content", async () => {
    await renderAt("/");
    await userEvent.tab();
    const skip = screen.getByRole("link", { name: "Skip to content" });
    expect(skip).toHaveFocus();
    expect(skip).toHaveAttribute("href", "#main");

    await userEvent.keyboard("{Enter}");
    expect(screen.getByRole("main")).toHaveFocus();
  });
});

describe("the landmarks", () => {
  it("names the banner, the main content and every navigation", async () => {
    await renderAt("/");
    // A header inside main is no banner to a browser, but Testing Library
    // counts the page's own head as one; axe below knows the difference.
    const banners = screen.getAllByRole("banner").filter((el) => !el.closest("main"));
    expect(banners).toHaveLength(1);
    expect(banners[0]).toHaveAttribute("data-top-bar");
    expect(screen.getByRole("main")).toHaveAttribute("id", "main");
    const navs = screen.getAllByRole("navigation");
    expect(navs.length).toBeGreaterThan(1);
    for (const nav of navs) expect(nav).toHaveAccessibleName();
  });

  it("gives every button a name", async () => {
    await renderAt("/");
    for (const button of screen.getAllByRole("button")) expect(button).toHaveAccessibleName();
  });
});

describe("the sidebar's switches", () => {
  it("say what they fold and whether it is open", async () => {
    await renderAt("/");
    const toggle = screen.getByRole("button", { name: "Collapse the sidebar" });
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(document.getElementById(toggle.getAttribute("aria-controls") ?? "")).toHaveAttribute("data-sidebar", "open");

    const group = screen.getByRole("button", { name: "Requirements" });
    const panel = document.getElementById(group.getAttribute("aria-controls") ?? "");
    expect(group).toHaveAttribute("aria-expanded", "true");
    expect(panel).toBeVisible();

    group.focus();
    await userEvent.keyboard("{Enter}");
    expect(group).toHaveAttribute("aria-expanded", "false");
    expect(panel).not.toBeVisible();
    await userEvent.keyboard(" ");
    expect(group).toHaveAttribute("aria-expanded", "true");
    expect(panel).toBeVisible();
  });
});

describe("the account menu", () => {
  it("opens, moves and closes from the keyboard and hands focus back", async () => {
    await renderAt("/");
    const trigger = screen.getByRole("button", { name: "Your account" });
    expect(trigger).toHaveAttribute("aria-haspopup", "menu");
    expect(trigger).toHaveAttribute("aria-expanded", "false");

    trigger.focus();
    await userEvent.keyboard("{Enter}");
    const menu = screen.getByRole("menu", { name: "Your account" });
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(trigger).toHaveAttribute("aria-controls", menu.id);
    const [browser, english, german] = within(menu).getAllByRole("menuitemradio");
    const signOut = within(menu).getByRole("menuitem", { name: "Sign out" });
    expect(browser).toHaveAttribute("aria-checked", "true");
    expect(english).toHaveAttribute("aria-checked", "false");
    expect(browser).toHaveFocus();
    await userEvent.keyboard("{ArrowDown}");
    expect(english).toHaveFocus();
    await userEvent.keyboard("{ArrowDown}");
    expect(german).toHaveFocus();
    await userEvent.keyboard("{ArrowDown}");
    expect(signOut).toHaveFocus();
    await userEvent.keyboard("{ArrowDown}");
    expect(browser).toHaveFocus();

    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("menu")).toBeNull();
    expect(trigger).toHaveFocus();
    expect(trigger).toHaveAttribute("aria-expanded", "false");

    await userEvent.keyboard("{Enter}");
    await userEvent.tab();
    expect(screen.queryByRole("menu")).toBeNull();
    expect(trigger).toHaveFocus();
  });
});

describe("on a narrow screen", () => {
  it("drops the rail and the sidebar for a menu button", async () => {
    setViewportWidth(PHONE);
    await renderAt("/");
    expect(document.querySelector("[data-rail]")).toBeNull();
    expect(document.querySelector("[data-sidebar]")).toBeNull();
    expect(menuButton()).toHaveAttribute("aria-expanded", "false");
    expect(menuButton()).toHaveAttribute("aria-haspopup", "dialog");
  });

  it("opens the drawer with focus inside and closes it on Escape, back to the button", async () => {
    setViewportWidth(PHONE);
    await renderAt("/");
    await userEvent.click(menuButton());

    const panel = drawer();
    expect(panel).not.toBeNull();
    expect(panel).toHaveAttribute("aria-modal", "true");
    expect(panel).toHaveAccessibleName("Navigation");
    expect(menuButton()).toHaveAttribute("aria-expanded", "true");
    expect(menuButton()).toHaveAttribute("aria-controls", panel?.id);
    expect(panel).toContainElement(document.activeElement as HTMLElement);
    const nav = within(panel as HTMLElement).getByRole("navigation", { name: "Where you are" });
    expect(within(nav).getByRole("link", { name: "Members" })).toBeInTheDocument();

    await userEvent.keyboard("{Escape}");
    expect(drawer()).toBeNull();
    expect(menuButton()).toHaveFocus();
    expect(menuButton()).toHaveAttribute("aria-expanded", "false");
  });

  it("holds Tab inside the drawer", async () => {
    setViewportWidth(PHONE);
    await renderAt("/");
    await userEvent.click(menuButton());
    const panel = drawer() as HTMLElement;
    const close = within(panel).getByRole("button", { name: "Close the navigation" });
    const last = within(panel).getByRole("link", { name: "Audit log" });

    last.focus();
    await userEvent.tab();
    expect(close).toHaveFocus();
    await userEvent.tab({ shift: true });
    expect(last).toHaveFocus();
  });

  it("closes on the close button and on a press beside it", async () => {
    setViewportWidth(PHONE);
    await renderAt("/");
    await userEvent.click(menuButton());
    await userEvent.click(screen.getByRole("button", { name: "Close the navigation" }));
    expect(drawer()).toBeNull();
    expect(menuButton()).toHaveFocus();

    await userEvent.click(menuButton());
    await userEvent.click(document.querySelector<HTMLElement>("[data-drawer-backdrop]") as HTMLElement);
    expect(drawer()).toBeNull();
    expect(menuButton()).toHaveFocus();
  });

  it("closes when a link in it is followed, even to the page already open", async () => {
    setViewportWidth(PHONE);
    settingsApi();
    const router = await renderAt("/");
    await userEvent.click(menuButton());
    await userEvent.click(within(drawer() as HTMLElement).getByRole("link", { name: "Members" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(MEMBERS_PATH));
    expect(drawer()).toBeNull();

    await userEvent.click(menuButton());
    await userEvent.click(within(drawer() as HTMLElement).getByRole("link", { name: "Members" }));
    expect(drawer()).toBeNull();
  });

  it("closes when the route changes from elsewhere", async () => {
    setViewportWidth(PHONE);
    settingsApi();
    const router = await renderAt("/");
    await userEvent.click(menuButton());
    await act(() => router.navigate({ to: AUDIT_PATH }));
    await waitFor(() => expect(router.state.location.pathname).toBe(AUDIT_PATH));
    expect(drawer()).toBeNull();
  });

  it("goes back to the rail and the sidebar when the window widens", async () => {
    setViewportWidth(PHONE);
    await renderAt("/");
    await userEvent.click(menuButton());
    act(() => setViewportWidth(WIDE));
    expect(drawer()).toBeNull();
    expect(document.querySelector("[data-rail]")).not.toBeNull();
    expect(screen.queryByRole("button", { name: "Open the navigation" })).toBeNull();
  });
});

describe("axe", () => {
  it("finds nothing on the wide shell", async () => {
    await renderAt("/");
    await screen.findByText("No projects yet");
    expect(await axeViolations()).toEqual([]);
  });

  it("finds nothing with the account menu open", async () => {
    await renderAt("/");
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    expect(await axeViolations()).toEqual([]);
  });

  it("finds nothing with the quick switcher open", async () => {
    await renderAt("/");
    await userEvent.keyboard("{Control>}k{/Control}");
    expect(await axeViolations()).toEqual([]);
  });

  it("finds nothing on the narrow shell, drawer shut or open", async () => {
    setViewportWidth(PHONE);
    settingsApi();
    await renderAt(MEMBERS_PATH);
    await screen.findByText("Bob Builder");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(menuButton());
    expect(await axeViolations()).toEqual([]);
  });

  it.each([MEMBERS_PATH, AUDIT_PATH])("finds nothing on %s", async (path) => {
    settingsApi();
    await renderAt(path);
    await screen.findByRole("heading", { level: 1 });
    await waitFor(() => expect(document.querySelector("[data-skeleton]")).toBeNull());
    expect(await axeViolations()).toEqual([]);
  });

  it("finds nothing on a settings page somebody may not read", async () => {
    await renderAt(AUDIT_PATH, { me: { ...signedIn, organization: { ...signedIn.organization!, role: "member" } } });
    await screen.findByRole("heading", { level: 1, name: "Audit log" });
    expect(await axeViolations()).toEqual([]);
  });

  it("finds nothing on a path that leads nowhere", async () => {
    await renderAt("/no/such/page");
    await screen.findByText("Page not found");
    expect(await axeViolations()).toEqual([]);
  });
});

describe("the drawer's breakpoint", () => {
  it("is the same width in the stylesheet and in config.ts", () => {
    const css = readFileSync(resolve(process.cwd(), "src/styles/index.css"), "utf8");
    const rem = css.match(/--breakpoint-shell:\s*([\d.]+)rem;/)?.[1];
    // A rem in a media query is always the browser's 16px.
    expect(Number(rem) * 16).toBe(SIDEBAR_DRAWER_BELOW_PX);
  });
});
