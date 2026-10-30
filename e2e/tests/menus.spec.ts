import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible, openAccountMenu, openShell, scrollsSideways } from "../fixtures/shell";

// The layout the three tools share (#280, #470): personal pages under the avatar and in Settings, the installation's
// pages under Admin, the sidebar for work.

const OWN_PAGES = ["Your profile", "Notification settings", "Watching", "Themes", "Access tokens", "Connected apps"];

/** The entries of the open account menu, in order. */
async function menuEntries(page: Page): Promise<string[]> {
  return (await page.getByRole("menu", { name: "Your account" }).getByRole("menuitem").allInnerTexts()).map((each) => each.trim());
}

/** The links of one group of the settings sidebar, in order. */
async function settingsGroup(page: Page, group: string): Promise<string[]> {
  return (await page.locator(`[data-settings-sidebar] [data-sidebar-group="${group}"]`).getByRole("link").allInnerTexts()).map((each) => each.trim());
}

test.describe("the avatar menu, the settings area and Admin", { tag: "@desktop" }, () => {
  test("an administrator finds their own pages and Admin in the menu, only their own in Settings, and the installation's in Admin", async ({
    page,
    people,
  }) => {
    await openShell(page, "/search");
    await expect(page.locator("[data-top-bar]")).toHaveCount(0);
    await expect(page.locator("[data-sidebar]").getByRole("link", { name: "Access" })).toHaveCount(0);
    await expect(page.locator('[data-sidebar] [data-nav="admin"]')).toBeVisible();

    await openAccountMenu(page);
    const header = page.locator("[data-account-header]");
    await expect(header).toContainText(people.alice.name);
    await expect(header).toContainText(people.alice.email);
    await expect(header).toContainText("Administrator");
    expect(await menuEntries(page)).toEqual([...OWN_PAGES, "Admin", "Sign out"]);
    await expectAccessible(page);

    await page.getByRole("menuitem", { name: "Your profile" }).click();
    await expect(page).toHaveURL(/\/settings\/profile$/);
    const crumbs = page.locator("[data-shell-strip]").getByRole("navigation", { name: "Breadcrumb" });
    await crumbs.getByRole("link", { name: "Settings" }).click();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator("main").getByRole("heading", { level: 1, name: "Settings" })).toBeVisible();
    await expect(page.locator('[data-hub-card="you"] [data-settings-link]')).toHaveCount(OWN_PAGES.length + 2);
    await expect(page.locator('[data-hub-card="you"] [data-settings-link="profile"]')).toContainText("Your name, language and time zone.");
    expect(await settingsGroup(page, "you")).toEqual([...OWN_PAGES, "Armature account", "Stator account"]);
    await expect(page.locator("[data-settings-sidebar] [data-sidebar-group]")).toHaveCount(1);
    await expectAccessible(page);

    await page.getByRole("link", { name: "Back to Exciter" }).click();
    await expect(page).toHaveURL(/\/search$/);
    await expect(page.locator("[data-settings-sidebar]")).toHaveCount(0);

    await openAccountMenu(page);
    await page.getByRole("menuitem", { name: "Admin", exact: true }).click();
    await expect(page).toHaveURL(/\/admin$/);
    await expect(page.locator("main").getByRole("heading", { level: 1, name: "Admin" })).toBeVisible();
    const pages = (section: string) => page.locator(`[data-admin-section="${section}"] [data-admin-card]`);
    expect((await pages("people").allInnerTexts()).map((each) => each.split("\n")[0])).toEqual(["Installation", "Access", "Users"]);
    expect((await pages("work").allInnerTexts()).map((each) => each.split("\n")[0])).toEqual(["Link types"]);
    expect((await pages("integrations").allInnerTexts()).map((each) => each.split("\n")[0])).toEqual(["Armature", "Stator", "Connected apps", "Webhooks"]);
    expect((await pages("records").allInnerTexts()).map((each) => each.split("\n")[0])).toEqual(["Audit log"]);
    await expectAccessible(page);

    await page.locator('[data-admin-card="access"]').click();
    await expect(page).toHaveURL(/\/admin\/access\/roles$/);
    await page.getByRole("tab", { name: "Members" }).click();
    await expect(page).toHaveURL(/\/admin\/access\/members$/);
    await expect(crumbs.getByText("Access")).toHaveAttribute("aria-current", "page");
    await crumbs.getByRole("link", { name: "Admin" }).click();
    await expect(page).toHaveURL(/\/admin$/);
  });

  test("a member finds their own pages, and no Admin in the menu, the sidebar or the palette, and is refused it once", async ({ pageAs }) => {
    const page = await pageAs("bob");
    await openShell(page, "/");
    await expect(page.locator('[data-sidebar] [data-nav="admin"]')).toHaveCount(0);
    await openAccountMenu(page);
    await expect(page.locator("[data-account-header]")).toContainText("Member");
    expect(await menuEntries(page)).toEqual([...OWN_PAGES, "Sign out"]);

    await page.getByRole("menuitem", { name: "Your profile" }).click();
    await expect(page).toHaveURL(/\/settings\/profile$/);
    expect(await settingsGroup(page, "you")).toEqual([...OWN_PAGES, "Armature account", "Stator account"]);
    await openShell(page, "/settings");
    await expect(page.locator('[data-hub-card="you"]')).toBeVisible();
    await expectAccessible(page);

    await page.keyboard.press("ControlOrMeta+k");
    await page.getByRole("combobox").fill("admin");
    await expect(page.locator('[data-quick-switcher-option^="place:/admin"]')).toHaveCount(0);
    await page.keyboard.press("Escape");

    await openShell(page, "/admin/webhooks");
    await expect(page.locator("[data-admin-refused]")).toHaveCount(1);
    await expect(page.getByText("Only administrators can open this page")).toBeVisible();
    await expect(page.locator("[data-admin-nav]")).toHaveCount(0);
  });

  test("the language and time zone are chosen on Your profile, saved together and kept with the account", async ({ pageAs, apiAs }) => {
    const page = await pageAs("bob");
    const api = await apiAs("bob");
    try {
      await openShell(page, "/settings/profile");
      await expect(page.locator("main").getByRole("heading", { level: 1, name: "Your profile" })).toBeVisible();
      await expectAccessible(page);
      await page.getByLabel("Interface language", { exact: true }).selectOption("de");
      await page.getByLabel("Find: Time zone", { exact: true }).fill("Tokyo");
      await page.getByLabel("Time zone", { exact: true }).selectOption("Asia/Tokyo");
      await page.getByRole("button", { name: "Save", exact: true }).click();
      await expect(page.locator("main").getByRole("heading", { level: 1, name: "Ihr Profil" })).toBeVisible();
      await expect(page.locator("[data-language-saved]")).toHaveText("Ihre Sprache und Zeitzone sind gespeichert.");
      await page.reload();
      await expect(page.locator("main").getByRole("heading", { level: 1, name: "Ihr Profil" })).toBeVisible();
      await expect(page.getByLabel("Sprache der Oberfläche", { exact: true })).toHaveValue("de");
      await expect(page.getByLabel("Zeitzone", { exact: true })).toHaveValue("Asia/Tokyo");
    } finally {
      await api.PATCH("/auth/me", { body: { locale: "", timezone: "" } });
    }
  });

  test("the palette offers Your profile, and every Admin page as Admin / <page>", async ({ page }) => {
    await openShell(page, "/");
    await page.keyboard.press("ControlOrMeta+k");
    await page.getByRole("combobox").fill("your profile");
    await page.locator(`[data-quick-switcher-option="place:/settings/profile"]`).click();
    await expect(page).toHaveURL(/\/settings\/profile$/);

    await page.keyboard.press("ControlOrMeta+k");
    await page.getByRole("combobox").fill("link types");
    await expect(page.locator(`[data-quick-switcher-option="place:/admin/link-types"]`)).toContainText("Admin / Link types");
    await page.locator(`[data-quick-switcher-option="place:/admin/link-types"]`).click();
    await expect(page).toHaveURL(/\/admin\/link-types$/);
  });
});

test.describe("the drawer at phone width", { tag: "@mobile" }, () => {
  test("opens from the strip with the work's groups and the person in its foot, Admin as a menu, and the settings sidebar in the settings area", async ({
    page,
  }) => {
    await openShell(page, "/");
    await expect(page.locator("[data-rail]")).toHaveCount(0);
    await page.locator('[data-action="drawer"]').click();
    const drawer = page.getByRole("dialog", { name: "Navigation" });
    await expect(drawer.getByRole("navigation", { name: "Where you are" }).getByRole("link", { name: "Reviews" })).toBeVisible();
    await expect(drawer.getByRole("link", { name: "Access" })).toHaveCount(0);
    await expectAccessible(page);

    await drawer.locator('[data-action="account"]').click();
    expect(await menuEntries(page)).toEqual([...OWN_PAGES, "Admin", "Sign out"]);
    await page.getByRole("menuitem", { name: "Admin", exact: true }).click();
    await expect(page).toHaveURL(/\/admin$/);
    expect(await scrollsSideways(page)).toBe(false);
    await expect(page.locator("[data-admin-nav]")).toHaveCount(0);
    await page.locator('[data-action="admin-sections"]').click();
    await page.getByRole("menu", { name: "Section" }).locator('[data-nav="users"]').click();
    await expect(page).toHaveURL(/\/admin\/users$/);
    await expect(page.locator('[data-action="admin-sections"]')).toHaveText("Users");
    await expectAccessible(page);

    await openShell(page, "/settings");
    await page.locator('[data-action="drawer"]').click();
    const settings = page.getByRole("dialog", { name: "Navigation" }).getByRole("navigation", { name: "Settings" });
    await expect(settings.getByRole("link", { name: "Your profile" })).toBeVisible();
    await expect(settings.getByRole("link", { name: "Access" })).toHaveCount(0);
    expect(await scrollsSideways(page)).toBe(false);
    await expectAccessible(page);
    // Admin is outside the settings area, so it is where the way back leads.
    await page.getByRole("dialog", { name: "Navigation" }).getByRole("link", { name: "Back to Exciter" }).click();
    await expect(page).toHaveURL(/\/admin\/users$/);
  });
});
