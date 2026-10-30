import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openShowing, openUntil } from "../fixtures/replica";
import { chooseTheme, createTheme, themeSpec, uniqueName } from "../fixtures/seed";
import { dismissToasts, openAccountMenu } from "../fixtures/shell";

// Armature's own theme file, kept byte for byte beside the backend's theme tests.
const MINECRAFT = resolve(dirname(fileURLToPath(import.meta.url)), "../../backend/internal/theme/testdata/minecraft.armature-theme.json");
const MINECRAFT_NAME = JSON.parse(readFileSync(MINECRAFT, "utf8")).name as string;
// A theme exported from a running Stator (#59), kept the same way.
const MEADOW = resolve(dirname(fileURLToPath(import.meta.url)), "../../backend/internal/theme/testdata/meadow.armature-theme.json");
const MEADOW_SPEC = JSON.parse(readFileSync(MEADOW, "utf8")).spec as { colors: { light: Record<string, string> } };

const THEMES_PATH = "/settings/themes";
const DEFAULT_THEME_PATH = "/settings/default-theme";
const customStyle = (page: Page) => page.locator("style#exciter-theme");
const row = (page: Page, name: string) => page.locator(`[data-theme-row="${name}"]`);
// What the page said last, in the toast in the corner.
const notice = (page: Page) => page.locator("[data-toast]").last().locator("[data-toast-message]");
const rootVar = (page: Page, name: string) => page.evaluate((n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim(), name);

async function rowAction(page: Page, name: string, action: string) {
  await dismissToasts(page);
  await row(page, name).locator('[data-action="theme-menu"]').click();
  await page.locator(`[role="menu"] [data-action="${action}"]`).click();
}

/** Presses the rail's theme button until it shows the choice, which is how a person picks Auto, Light or Dark. */
async function pickScheme(page: Page, choice: "system" | "light" | "dark") {
  const button = page.locator('[data-rail] [data-action="theme"]');
  for (let i = 0; i < 3 && (await button.getAttribute("data-theme-choice")) !== choice; i++) await button.click();
  await expect(button).toHaveAttribute("data-theme-choice", choice);
}

// Alice and bob are this spec's own, so what they use is nobody else's; the default is everybody's, which is why
// the spec runs alone. Each test starts with nobody's choice and no default.
test.describe("themes", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api, apiAs }) => {
    for (const each of [api, await apiAs("bob")]) await chooseTheme(each, null).catch(() => {});
    await api.PUT("/themes/default", { body: { themeId: null } });
  });

  test("light and dark from the rail each apply their palette, and the choice survives a reload", async ({ page }) => {
    await page.goto("/");
    await pickScheme(page, "light");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    const light = await rootVar(page, "--color-canvas");
    await pickScheme(page, "dark");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    const dark = await rootVar(page, "--color-canvas");
    expect(dark, "dark has a canvas of its own").not.toBe(light);

    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    expect(await rootVar(page, "--color-canvas")).toBe(dark);

    await pickScheme(page, "system");
    await expect(page.locator("html")).not.toHaveAttribute("data-theme", /.*/);
    await page.emulateMedia({ colorScheme: "dark" });
    expect(await rootVar(page, "--color-canvas")).toBe(dark);
    await page.emulateMedia({ colorScheme: "light" });
    expect(await rootVar(page, "--color-canvas")).toBe(light);
  });

  test("the chosen theme applies in the shell and survives a reload without a flash", async ({ page, api }, testInfo) => {
    const name = uniqueName(testInfo, "canvas");
    await createTheme(api, name, themeSpec({ colors: { light: { canvas: "#123456" }, dark: { canvas: "#123456" } } }));
    await page.goto(THEMES_PATH);
    await rowAction(page, name, "use-theme");
    await expect(notice(page)).toHaveText(`Now using ${name}.`);

    await page.goto("/");
    await expect(customStyle(page)).toBeAttached();
    expect(await rootVar(page, "--color-canvas")).toBe("#123456");

    // Recorded the moment the app first draws anything, before the server has answered.
    await page.addInitScript(() => {
      new MutationObserver((_, observer) => {
        if (document.getElementById("root")?.firstChild) {
          (window as unknown as { themeAtFirstDraw: boolean }).themeAtFirstDraw = Boolean(document.getElementById("exciter-theme"));
          observer.disconnect();
        }
      }).observe(document, { childList: true, subtree: true });
    });
    await page.reload();
    await page.waitForFunction(() => "themeAtFirstDraw" in window);
    expect(await page.evaluate(() => (window as unknown as { themeAtFirstDraw?: boolean }).themeAtFirstDraw)).toBe(true);
    expect(await rootVar(page, "--color-canvas")).toBe("#123456");
  });

  test("Armature's Minecraft theme imports and gives square corners and hard shadows", async ({ page }) => {
    await page.goto(THEMES_PATH);
    await page.locator("[data-theme-file]").setInputFiles(MINECRAFT);
    await expect(notice(page)).toContainText(`Imported ${MINECRAFT_NAME}`);
    // A copy an earlier test left this alice makes this one "Minecraft (2)".
    const imported = (await notice(page).textContent())!.replace(/^Imported /, "").replace(/\.$/, "");
    await rowAction(page, imported, "use-theme");
    await expect(notice(page)).toHaveText(`Now using ${imported}.`);

    await page.goto("/");
    await expect(customStyle(page)).toBeAttached();
    await expect(page.locator('[data-action="sidebar"]')).toHaveCSS("border-radius", "0px");
    await openAccountMenu(page);
    // The shadow is the list's panel, which holds the menu and its header.
    const shadow = await page.getByRole("menu").evaluate((el) => getComputedStyle(el.parentElement!).boxShadow);
    expect(shadow).toMatch(/\b6px 6px 0px\b/);
  });

  // The three products share one file format (#59): what Exciter exports comes back the same, and Stator's file draws here.
  test("a theme exported from Exciter imports again and draws the same, and so does one exported from Stator", async ({ page, api }, testInfo) => {
    const name = uniqueName(testInfo, "export");
    await createTheme(
      api,
      name,
      themeSpec({ colors: { light: { canvas: "#fbf8f3", primary: "#b45309" }, dark: { canvas: "#17120c" } }, shape: { radiusControl: 3 } }),
    );
    await page.goto(THEMES_PATH);
    const download = page.waitForEvent("download");
    await rowAction(page, name, "export-theme");
    const file = await (await download).path();

    await page.locator("[data-theme-file]").setInputFiles({ name: `${name}.armature-theme.json`, mimeType: "application/json", buffer: readFileSync(file) });
    await expect(notice(page)).toHaveText(`Imported ${name} (2).`);
    await rowAction(page, `${name} (2)`, "use-theme");
    await expect(notice(page)).toHaveText(`Now using ${name} (2).`);
    await pickScheme(page, "light");
    await page.goto("/");
    await expect(customStyle(page)).toBeAttached();
    expect(await rootVar(page, "--color-canvas")).toBe("#fbf8f3");
    expect(await rootVar(page, "--color-primary")).toBe("#b45309");
    await expect(page.locator('[data-action="sidebar"]')).toHaveCSS("border-radius", "3px");

    await page.goto(THEMES_PATH);
    await page.locator("[data-theme-file]").setInputFiles(MEADOW);
    await expect(notice(page)).toContainText("Imported Meadow, from Stator");
    const meadow = (await notice(page).textContent())!.replace(/^Imported /, "").replace(/\.$/, "");
    await rowAction(page, meadow, "use-theme");
    await expect(notice(page)).toHaveText(`Now using ${meadow}.`);
    await page.goto("/");
    expect(await rootVar(page, "--color-primary")).toBe(MEADOW_SPEC.colors.light.primary);
  });

  test("the default, named where the themes are, reaches a member who has not chosen", async ({ page, api, apiAs, pageAs }, testInfo) => {
    // Each spec's people start on the built-in theme, so bob is made to have chosen nothing.
    await chooseTheme(await apiAs("bob"), null);
    const name = uniqueName(testInfo, "default");
    await createTheme(api, name, themeSpec({ colors: { light: { canvas: "#0f0f0f" }, dark: { canvas: "#0f0f0f" } } }), true);

    // The page of its own it once had sends a bookmark on to Themes.
    await openShowing(page, DEFAULT_THEME_PATH, row(page, name));
    await expect(page).toHaveURL(/\/settings\/themes$/);
    await rowAction(page, name, "default-theme");
    await expect(notice(page)).toHaveText(`${name} is what everybody sees until they choose.`);

    const bob = await pageAs("bob");
    await openShowing(bob, THEMES_PATH, bob.getByText(`You are using ${name}, everyone's default.`));
    await expect(customStyle(bob)).toBeAttached();
    expect(await rootVar(bob, "--color-canvas")).toBe("#0f0f0f");

    await bob.locator('[data-action="built-in-theme"]').click();
    await expect(bob.getByText(`You are using the built-in theme, over everyone's default ${name}.`)).toBeVisible();
    await expect(customStyle(bob)).toHaveCount(0);
  });

  test("the default is named and taken back from the Themes page, and bob steps between it, his choice and the built-in", async ({
    page,
    api,
    apiAs,
    pageAs,
  }, testInfo) => {
    const bobApi = await apiAs("bob");
    await chooseTheme(bobApi, null);
    const name = uniqueName(testInfo, "house");
    const house = await createTheme(api, name, themeSpec({ colors: { light: { canvas: "#0f0f0f" }, dark: { canvas: "#0f0f0f" } } }), true);
    const using = (who: Page, words: string) => openShowing(who, THEMES_PATH, who.getByText(words, { exact: true }));

    await page.goto(THEMES_PATH);
    await rowAction(page, name, "default-theme");
    await expect(notice(page)).toHaveText(`${name} is what everybody sees until they choose.`);
    await expect(row(page, name)).toHaveAttribute("data-theme-default", "true");
    const bob = await pageAs("bob");
    await using(bob, `You are using ${name}, everyone's default.`);
    await expect(customStyle(bob)).toBeAttached();

    await rowAction(page, name, "undefault-theme");
    await expect(notice(page)).toHaveText("Everyone sees the built-in theme again.");
    await expect(row(page, name)).toHaveAttribute("data-theme-default", "false");
    await using(bob, "You are using the built-in theme.");
    await expect(customStyle(bob)).toHaveCount(0);

    // Bob chooses it for himself while it is the default, then stops, and lands on the default.
    must(await api.PUT("/themes/default", { body: { themeId: house.id } }));
    await openUntil(bob, THEMES_PATH, async () => {
      await bob.locator('[data-themes-view="shared"]').click();
      await expect(row(bob, name)).toHaveAttribute("data-theme-default", "true", ONE_LOOK);
    });
    await rowAction(bob, name, "use-theme");
    await expect(notice(bob)).toHaveText(`Now using ${name}.`);
    await rowAction(bob, name, "stop-theme");
    await expect(notice(bob)).toHaveText("Back to everyone's default.");
    await expect(bob.getByText(`You are using ${name}, everyone's default.`, { exact: true })).toBeVisible();

    await bob.locator('[data-action="built-in-theme"]').click();
    await expect(bob.getByText(`You are using the built-in theme, over everyone's default ${name}.`, { exact: true })).toBeVisible();
    await expect(customStyle(bob)).toHaveCount(0);
    await bob.locator('[data-action="org-default-theme"]').click();
    await expect(notice(bob)).toHaveText(`Now using ${name}.`);
    await expect(bob.getByText(`You are using ${name}, everyone's default.`, { exact: true })).toBeVisible();
    await expect(customStyle(bob)).toBeAttached();

    // The page must know the default bob just used, or taking it back would look like no change.
    await openShowing(page, THEMES_PATH, page.locator(`[data-theme-row="${name}"][data-theme-default="true"]`));
    await rowAction(page, name, "undefault-theme");
    await expect(notice(page)).toHaveText("Everyone sees the built-in theme again.");
    await using(bob, "You are using the built-in theme.");
    await expect(customStyle(bob)).toHaveCount(0);
    await expect.poll(async () => must(await bobApi.GET("/themes/default")).theme).toBeNull();
  });
});
