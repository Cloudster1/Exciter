import { request } from "@playwright/test";
import { ANONYMOUS, BOOTSTRAP_OWNER, expect, LOGIN_PATH, ME_PATH, signInWithPassword, test } from "../fixtures/auth";
import { menuItem, openAccountMenu } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

const THEMES_PATH = "/settings/themes";
const WRONG_PASSWORD = "That email and password do not match an account. Check both and try again, or sign in with single sign-on.";

// The way in for somebody given a password here, from a browser nobody has signed in to.
test.describe("signing in with a password", () => {
  test.use({ storageState: ANONYMOUS });

  test("the bootstrap owner signs in, sees the shell and signs out again", async ({ page }) => {
    expect((await page.request.get(ME_PATH)).status()).toBe(401);
    await signInWithPassword(page);
    await expect(page).toHaveURL(`${WEB_URL}/`);
    await expect(page.locator("main#main")).toBeVisible();
    await expect(page.locator("[data-top-bar]")).toBeVisible();
    await expect(page.locator("[data-home]")).toBeVisible();

    await openAccountMenu(page);
    await menuItem(page, "sign-out").click();
    await expect(page).toHaveURL(new RegExp(`${LOGIN_PATH}(\\?|$)`));
    expect((await page.request.get(ME_PATH)).status()).toBe(401);
    await page.goto("/");
    await expect(page).toHaveURL(new RegExp(`${LOGIN_PATH}\\?next=`));
  });

  test("a wrong password is refused with what to do, and nobody is signed in", async ({ page }) => {
    await page.goto(LOGIN_PATH);
    await page.getByLabel("Email", { exact: true }).fill(BOOTSTRAP_OWNER.email);
    await page.getByLabel("Password", { exact: true }).fill("not the password at all");
    await page.locator('[data-action="password-sign-in"]').click();
    await expect(page.locator("[data-login]").getByRole("alert")).toHaveText(WRONG_PASSWORD);
    expect((await page.request.get(ME_PATH)).status()).toBe(401);
  });

  test("a visitor opening the themes page signs in with a password and comes back to it", async ({ page }) => {
    await page.goto(THEMES_PATH);
    await expect(page).toHaveURL(/\/login\?next=%2Fsettings%2Fthemes$/);
    await page.getByLabel("Email", { exact: true }).fill(BOOTSTRAP_OWNER.email);
    await page.getByLabel("Password", { exact: true }).fill(BOOTSTRAP_OWNER.password);
    await page.locator('[data-action="password-sign-in"]').click();
    await expect(page).toHaveURL(new RegExp(`${THEMES_PATH}$`));
    await expect(page.getByRole("heading", { level: 1, name: "Themes" })).toBeVisible();
  });

  test("a session that ends on the server sends the next step to sign in, and back", async ({ page, context }) => {
    await signInWithPassword(page);
    await expect(page.locator("[data-home]")).toBeVisible();

    // Signed out behind the browser's back: its cookie now names a session the api has dropped.
    const elsewhere = await request.newContext({ baseURL: WEB_URL, storageState: await context.storageState(), extraHTTPHeaders: { Origin: WEB_URL } });
    expect((await elsewhere.post("/api/v1/auth/logout")).ok()).toBe(true);
    await elsewhere.dispose();

    await openAccountMenu(page);
    await menuItem(page, "themes").click();
    await expect(page).toHaveURL(/\/login\?next=%2Fsettings%2Fthemes$/);
  });
});
