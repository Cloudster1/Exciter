import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible, openAccountMenu, openShell, scrollsSideways } from "../fixtures/shell";

// The layout the three tools share (#280): personal pages under the avatar, the installation's settings in an area of
// their own, the sidebar for work.

const OWN_PAGES = ["Your profile", "Notification settings", "Watching", "Themes", "Access tokens", "Connected apps"];

/** The entries of the open account menu, in order. */
async function menuEntries(page: Page): Promise<string[]> {
  return (await page.getByRole("menu", { name: "Your account" }).getByRole("menuitem").allInnerTexts()).map((each) => each.trim());
}

/** The links of one group of the settings sidebar, in order. */
async function settingsGroup(page: Page, group: string): Promise<string[]> {
  return (await page.locator(`[data-settings-sidebar] [data-sidebar-group="${group}"]`).getByRole("link").allInnerTexts()).map((each) => each.trim());
}

test.describe("the avatar menu and the settings area", { tag: "@desktop" }, () => {
  test("an administrator finds their own pages, Settings and the settings area with both groups", async ({ page, people }) => {
    await openShell(page, "/search");
    await expect(page.locator("[data-top-bar]")).toHaveCount(0);
    await expect(page.locator("[data-sidebar]").getByRole("link", { name: "Members" })).toHaveCount(0);

    await openAccountMenu(page);
    const header = page.locator("[data-account-header]");
    await expect(header).toContainText(people.alice.name);
    await expect(header).toContainText(people.alice.email);
    await expect(header).toContainText("Administrator");
    expect(await menuEntries(page)).toEqual([...OWN_PAGES, "Settings", "Sign out"]);
    await expectAccessible(page);

    await page.getByRole("menuitem", { name: "Settings", exact: true }).click();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator("main").getByRole("heading", { level: 1, name: "Settings" })).toBeVisible();
    await expect(page.locator('[data-hub-card="settings"]')).toBeVisible();
    expect(await settingsGroup(page, "you")).toEqual([...OWN_PAGES, "Armature account", "Stator account"]);
    expect(await settingsGroup(page, "settings")).toEqual([
      "Installation",
      "Members",
      "Single sign-on",
      "Link types",
      "All tokens",
      "All connected apps",
      "Armature",
      "Stator",
      "Webhooks",
      "Audit log",
    ]);
    await expectAccessible(page);

    await page.locator("[data-settings-sidebar]").getByRole("link", { name: "Members" }).click();
    await expect(page).toHaveURL(/\/settings\/members$/);
    const crumbs = page.locator("[data-shell-strip]").getByRole("navigation", { name: "Breadcrumb" });
    await crumbs.getByRole("link", { name: "Settings" }).click();
    await expect(page).toHaveURL(/\/settings$/);

    await page.getByRole("link", { name: "Back to Exciter" }).click();
    await expect(page).toHaveURL(/\/search$/);
    await expect(page.locator("[data-settings-sidebar]")).toHaveCount(0);
  });

  test("a member finds their own pages, and no Settings in the menu, the hub or the area", async ({ pageAs }) => {
    const page = await pageAs("bob");
    await openShell(page, "/");
    await openAccountMenu(page);
    await expect(page.locator("[data-account-header]")).toContainText("Member");
    expect(await menuEntries(page)).toEqual([...OWN_PAGES, "Sign out"]);

    await page.getByRole("menuitem", { name: "Your profile" }).click();
    await expect(page).toHaveURL(/\/settings\/profile$/);
    expect(await settingsGroup(page, "you")).toEqual([...OWN_PAGES, "Armature account", "Stator account"]);
    await expect(page.locator('[data-settings-sidebar] [data-sidebar-group="settings"]')).toHaveCount(0);
    await openShell(page, "/settings");
    await expect(page.locator('[data-hub-card="you"]')).toBeVisible();
    await expect(page.locator('[data-hub-card="settings"]')).toHaveCount(0);
    await expectAccessible(page);
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

  test("the palette offers Your profile", async ({ page }) => {
    await openShell(page, "/");
    await page.keyboard.press("ControlOrMeta+k");
    await page.getByRole("combobox").fill("your profile");
    await page.locator(`[data-quick-switcher-option="place:/settings/profile"]`).click();
    await expect(page).toHaveURL(/\/settings\/profile$/);
  });
});

test.describe("the drawer at phone width", { tag: "@mobile" }, () => {
  test("opens from the strip with the work's groups and the person in its foot, and the settings sidebar in the settings area", async ({ page }) => {
    await openShell(page, "/");
    await expect(page.locator("[data-rail]")).toHaveCount(0);
    await page.locator('[data-action="drawer"]').click();
    const drawer = page.getByRole("dialog", { name: "Navigation" });
    await expect(drawer.getByRole("navigation", { name: "Where you are" }).getByRole("link", { name: "Reviews" })).toBeVisible();
    await expect(drawer.getByRole("link", { name: "Members" })).toHaveCount(0);
    await expectAccessible(page);

    await drawer.locator('[data-action="account"]').click();
    expect(await menuEntries(page)).toEqual([...OWN_PAGES, "Settings", "Sign out"]);
    await page.getByRole("menuitem", { name: "Settings", exact: true }).click();
    await expect(page).toHaveURL(/\/settings$/);
    expect(await scrollsSideways(page)).toBe(false);

    await page.locator('[data-action="drawer"]').click();
    const settings = page.getByRole("dialog", { name: "Navigation" }).getByRole("navigation", { name: "Settings" });
    await expect(settings.getByRole("link", { name: "Your profile" })).toBeVisible();
    await expect(settings.getByRole("link", { name: "Members" })).toBeVisible();
    await expectAccessible(page);
    await page.getByRole("dialog", { name: "Navigation" }).getByRole("link", { name: "Back to Exciter" }).click();
    await expect(page).toHaveURL(/\/$/);
  });
});
