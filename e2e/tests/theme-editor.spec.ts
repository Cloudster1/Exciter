import type { Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { createTheme, listThemes, themeSpec, uniqueName } from "../fixtures/seed";
import { confirm, dismissToasts, startInScheme } from "../fixtures/shell";

// The editor and the Themes page's row menu, clicked as alice and bob, the spec's own. Nothing here names the
// default for everyone, which themes.spec.ts does alone, so these run beside the rest.

const THEMES_PATH = "/settings/themes";
// The smallest PNG there is, one pixel, so a picture is a real one the browser draws.
const PIXEL = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", "base64");
// web/src/config.ts THEME_ASSET_MAX_BYTES, which the editor holds a file to before it sends it.
const ASSET_MAX_BYTES = 12 * 1024 * 1024;
// Handing a file past that limit to the page through the browser's protocol outlasts an action's 5 s on a busy shard (#452).
const HAND_OVER_HUGE_FILE = { timeout: 30_000 } as const;
const MAGENTA = "#ff0066";
const CHOSEN_ACCENT = "#00aa00";

const customStyle = (page: Page) => page.locator("style#exciter-theme");
const row = (page: Page, name: string) => page.locator(`[data-theme-row="${name}"]`);
// What the page said last, in the toast in the corner.
const notice = (page: Page) => page.locator("[data-toast]").last().locator("[data-toast-message]");
const editorNotice = notice;
const rootVar = (page: Page, name: string) => page.evaluate((n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim(), name);
const accent = (page: Page) => rootVar(page, "--color-accent");
const menuItem = (page: Page, action: string) => page.locator(`[role="menu"] [data-action="${action}"]`);

async function openMenu(page: Page, name: string) {
  await dismissToasts(page);
  await row(page, name).locator('[data-action="theme-menu"]').click();
  await expect(page.getByRole("menu")).toBeVisible();
}

async function rowAction(page: Page, name: string, action: string) {
  await openMenu(page, name);
  await menuItem(page, action).click();
}

/** Opens the Themes page on the Shared with me view until the row shows, as another person's share may lag. */
async function openShared(page: Page, name: string) {
  await openUntil(page, THEMES_PATH, async () => {
    await page.locator('[data-themes-view="shared"]').click();
    await expect(row(page, name)).toBeVisible(ONE_LOOK);
  });
}

async function openEditor(page: Page, id: string, name: string) {
  await page.goto(`${THEMES_PATH}/${id}`);
  await expect(page.locator("#field-theme-name")).toHaveValue(name);
}

/** Leaves the editor the way a person does, by the breadcrumb back to Themes, so the page is not loaded again. */
async function leaveEditor(page: Page) {
  await page.locator("[data-shell-strip]").getByRole("link", { name: "Themes" }).click();
  await expect(page).toHaveURL(new RegExp(`${THEMES_PATH}$`));
}

test.describe("the theme editor and the row menu", { tag: "@desktop" }, () => {
  test.beforeEach(async ({ page }) => {
    await startInScheme(page, "light");
  });

  test.afterEach(async ({ api, apiAs }) => {
    for (const each of [api, await apiAs("bob")]) await each.PUT("/themes/active", { body: { themeId: null, builtIn: true } });
  });

  test("a theme is made from an example, listed as mine, and used from the editor", async ({ page }, testInfo) => {
    const name = uniqueName(testInfo, "made");
    await page.goto(THEMES_PATH);
    await page.locator('[data-action="new-theme"]').click();
    await expect(page).toHaveURL(new RegExp(`${THEMES_PATH}/new$`));
    await page.locator('[data-theme-example="constellation"]').click();
    await expect(page.locator("#field-theme-name")).toHaveValue("Constellation");
    await page.locator("#field-theme-name").fill(name);
    await page.locator('[data-action="save-theme"]').click();
    await expect(page).toHaveURL(/\/settings\/themes\/[0-9a-f-]{36}$/);
    const id = page.url().split("/").pop()!;
    await expect(page.locator("main h1")).toHaveText(name);

    await leaveEditor(page);
    await expect(page.locator('[data-themes-view="mine"]')).toHaveAttribute("aria-pressed", "true");
    await expect(row(page, name)).toBeVisible();
    await expect(row(page, name)).toHaveAttribute("data-theme-active", "false");
    await row(page, name).getByRole("link", { name }).click();
    await expect(page).toHaveURL(new RegExp(`${THEMES_PATH}/${id}$`));
    await page.locator('[data-action="use-theme"]').click();
    await expect(editorNotice(page)).toHaveText(`Now using ${name}.`);
    await expect(customStyle(page)).toBeAttached();
    // Constellation's moving picture is drawn by the shell, which no stylesheet can do.
    await expect(page.locator('[data-backdrop-effect="constellation"]')).toBeAttached();
    await expect(page.locator('[data-action="use-theme"]')).toHaveCount(0);
  });

  test("a colour previews on the page before it is saved, a reset or leaving takes it back, and saved it applies", async ({ page, api }, testInfo) => {
    await page.goto("/");
    await expect(customStyle(page)).toHaveCount(0);
    const stock = await accent(page);
    const chosen = await createTheme(api, uniqueName(testInfo, "chosen"), themeSpec({ colors: { light: { accent: CHOSEN_ACCENT }, dark: {} } }));
    must(await api.PUT("/themes/active", { body: { themeId: chosen.id } }));
    const name = uniqueName(testInfo, "draft");
    const draft = await createTheme(api, name);
    const links = page.locator("#field-token-accent");

    await openEditor(page, draft.id, name);
    await expect.poll(() => accent(page)).toBe(CHOSEN_ACCENT);
    await links.fill(MAGENTA);
    expect(await accent(page), "nothing shows before the preview is asked for").toBe(CHOSEN_ACCENT);
    await page.locator('[data-action="preview-theme"]').click();
    await expect.poll(() => accent(page)).toBe(MAGENTA);
    await page.getByRole("button", { name: "Reset Links, the current item" }).click();
    // The draft overrides nothing now, and it is the draft that shows, not the chosen theme.
    await expect.poll(() => accent(page)).toBe(stock);
    await links.fill(MAGENTA);
    await expect.poll(() => accent(page)).toBe(MAGENTA);
    await leaveEditor(page);
    await expect.poll(() => accent(page)).toBe(CHOSEN_ACCENT);
    expect((await listThemes(api)).find((each) => each.id === draft.id)?.spec.colors.light, "leaving saved nothing").toEqual({});

    await openEditor(page, draft.id, name);
    await links.fill(MAGENTA);
    await page.locator('[data-action="save-theme"]').click();
    await expect(editorNotice(page)).toHaveText(`Saved ${name}.`);
    await page.locator('[data-action="use-theme"]').click();
    await expect(editorNotice(page)).toHaveText(`Now using ${name}.`);
    await page.goto("/");
    await expect.poll(() => accent(page)).toBe(MAGENTA);
  });

  test("a file is added, drawn as the backdrop, kept while in use and then removed; a scripted or huge one is not", async ({ page, api }, testInfo) => {
    // The huge file's hand-over may take most of its own budget, which the test's must hold beside the rest.
    test.setTimeout(60_000);
    const name = uniqueName(testInfo, "files");
    const made = await createTheme(api, name);
    const files = page.locator("[data-theme-file-input]");
    const pixel = page.locator('[data-theme-asset="pixel.png"]');
    const remove = pixel.locator('[data-action="remove-theme-file"]');
    const tab = (which: string) => page.locator(`[data-theme-tab="${which}"]`).click();
    const save = async () => {
      await dismissToasts(page);
      await page.locator('[data-action="save-theme"]').click();
      await expect(editorNotice(page)).toHaveText(`Saved ${name}.`);
    };
    await openEditor(page, made.id, name);
    await tab("files");
    const chooser = page.waitForEvent("filechooser");
    await page.locator('[data-action="add-theme-file"]').click();
    await (await chooser).setFiles({ name: "pixel.png", mimeType: "image/png", buffer: PIXEL });
    await expect(editorNotice(page)).toHaveText("Added pixel.png.");
    await expect(pixel).toContainText("image/png");
    await expect(pixel).toContainText("1 KB");

    await tab("backdrop");
    const backdrop = page.getByRole("combobox", { name: "Backdrop picture" });
    await backdrop.selectOption({ label: "pixel.png" });
    const assetID = await backdrop.inputValue();
    await save();
    await page.locator('[data-action="use-theme"]').click();
    await expect(editorNotice(page)).toHaveText(`Now using ${name}.`);
    await expect
      .poll(() =>
        page
          .locator(".bg-backdrop")
          .first()
          .evaluate((el) => getComputedStyle(el).backgroundImage),
      )
      .toContain(`/api/v1/themes/${made.id}/assets/${assetID}`);

    await tab("files");
    await expect(pixel).toContainText("In use");
    await expect(remove).toBeDisabled();
    await tab("backdrop");
    await page.getByRole("combobox", { name: "Backdrop picture" }).selectOption({ index: 0 });
    await save();
    await tab("files");
    await expect(remove).toBeEnabled();
    await dismissToasts(page);
    await remove.click();
    await confirm(page);
    await expect(editorNotice(page)).toHaveText("Removed pixel.png.");
    await expect(pixel).toHaveCount(0);

    await files.setInputFiles({
      name: "evil.svg",
      mimeType: "image/svg+xml",
      buffer: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>'),
    });
    await expect(page.getByRole("alert")).toHaveText("That SVG carries script, event handlers or links to elsewhere, which a theme cannot use.");
    const uploads: string[] = [];
    page.on("request", (request) => {
      if (request.method() === "POST" && request.url().includes("/assets")) uploads.push(request.url());
    });
    await files.setInputFiles({ name: "huge.png", mimeType: "image/png", buffer: Buffer.alloc(ASSET_MAX_BYTES + 1) }, HAND_OVER_HUGE_FILE);
    await expect(page.getByRole("alert").filter({ hasText: "That file is over 12 MB. Make it smaller and try again." })).toBeVisible();
    expect(uploads, "a file over the limit is never sent").toEqual([]);
    // The removal was the page's, so the API's read may see it a replica later.
    await expect.poll(async () => (await listThemes(api)).find((each) => each.id === made.id)?.assets).toEqual([]);
  });

  test("shared from the row menu a theme reaches bob, unshared it leaves him, and deleted it is gone", async ({ page, api, pageAs }, testInfo) => {
    const name = uniqueName(testInfo, "shared");
    await createTheme(api, name, themeSpec({ colors: { light: { canvas: "#101820" }, dark: { canvas: "#101820" } } }));
    await page.goto(THEMES_PATH);
    await rowAction(page, name, "share-theme");
    await expect(notice(page)).toHaveText(`${name} is shared with everyone.`);
    await expect(row(page, name).getByText("Shared", { exact: true })).toBeVisible();

    const bob = await pageAs("bob");
    await startInScheme(bob, "light");
    await openShared(bob, name);
    await expect(row(bob, name).getByText("Shared", { exact: true })).toBeVisible();
    await rowAction(bob, name, "use-theme");
    await expect(notice(bob)).toHaveText(`Now using ${name}.`);
    await expect(customStyle(bob)).toBeAttached();
    expect(await rootVar(bob, "--color-canvas")).toBe("#101820");

    await openMenu(page, name);
    await expect(menuItem(page, "share-theme")).toHaveText("Stop sharing");
    await menuItem(page, "share-theme").click();
    await expect(notice(page)).toHaveText(`${name} is yours alone again.`);
    await openUntil(bob, "/", () => expect(customStyle(bob)).toHaveCount(0, ONE_LOOK));

    // Bob's choice is kept while the theme is private, so he still counts; his write may reach alice a replica later.
    await openUntil(page, THEMES_PATH, () => expect(row(page, name)).toContainText("1 person", ONE_LOOK));
    await rowAction(page, name, "delete-theme");
    await expect(page.locator("[data-confirm]")).toContainText(`Delete theme ${name}?`);
    await expect(page.locator("[data-confirm]")).toContainText("Its files go with it for good. The one person using it goes back to the built-in theme.");
    await confirm(page);
    await expect(notice(page)).toHaveText(`Deleted ${name}.`);
    await expect(row(page, name)).toHaveCount(0);
  });

  test("bob may use alice's shared theme but neither edit, unshare nor delete it, and a stray file imports nothing", async ({ api, pageAs }, testInfo) => {
    const name = uniqueName(testInfo, "theirs");
    await createTheme(api, name, themeSpec(), true);
    const bob = await pageAs("bob");
    await openShared(bob, name);
    await expect(row(bob, name).getByRole("link")).toHaveCount(0);
    await expect(row(bob, name).getByText(name, { exact: true })).toBeVisible();
    await openMenu(bob, name);
    await expect(bob.getByRole("menu")).toHaveAccessibleName(`Actions for ${name}`);
    for (const action of ["edit-theme", "share-theme", "delete-theme"]) await expect(menuItem(bob, action)).toHaveAttribute("aria-disabled", "true");
    await expect(menuItem(bob, "share-theme")).toHaveText("Stop sharing");
    await expect(menuItem(bob, "use-theme")).not.toHaveAttribute("aria-disabled");
    await bob.keyboard.press("Escape");

    await bob.locator('[data-themes-view="mine"]').click();
    const mine = await bob.locator("[data-theme-row]").count();
    await bob.locator("[data-theme-file]").setInputFiles({ name: "notes.json", mimeType: "application/json", buffer: Buffer.from('{"hello": "world"}') });
    await expect(bob.getByRole("alert")).toContainText("That is not a theme file");
    await expect(bob.locator("[data-theme-row]")).toHaveCount(mine);
  });
});
