import { randomBytes } from "node:crypto";
import type { Page, TestInfo } from "@playwright/test";
import { must } from "../fixtures/api";
import { anonymousContext, expect, ME_PATH, signInWithPassword } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { findGrants, grantInForm, grantRow, revokeInTable } from "../fixtures/grants";
import { expectAccessible, toast, dismissToasts } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

const MEMBERS_PATH = "/settings/access/members";
const ROLES_PATH = "/settings/access/roles";
const SETTINGS_PATH = "/settings";
const LOGIN_API = "/api/v1/auth/login";
// Enough random bytes for a password past the minimum once written out.
const PASSWORD_BYTES = 18;
// auth.MinPasswordLength: a made password is at least this long, and a typed one has to be.
const MIN_PASSWORD_LENGTH = 12;

const member = (page: Page, email: string) => page.locator(`[data-member="${email}"]`);

/** An address no other test, worker or earlier run has used. */
function freshEmail(testInfo: TestInfo, label: string): string {
  return `${label}-${testInfo.workerIndex}-${Date.now().toString(36)}@exciter.test`;
}

// The people added here have addresses of their own, so they meet no other spec.
test.describe("members", { tag: "@desktop" }, () => {
  const added: string[] = [];

  test.afterEach(async ({ api }) => {
    const members = must(await api.GET("/users")).members;
    for (const email of added.splice(0)) {
      const made = members.find((each) => each.email === email);
      if (made) await api.DELETE("/users/{userID}", { params: { path: { userID: made.userId } } });
    }
  });

  test("an administrator adds somebody, who signs in with the password shown once", async ({ browser, page }, testInfo) => {
    const email = freshEmail(testInfo, "added");
    added.push(email);

    await page.goto(MEMBERS_PATH);
    await page.locator('[data-action="add-member"]').click();
    const dialog = page.locator("[data-add-member]");
    await dialog.getByLabel("Email address", { exact: true }).fill(email);
    await expectAccessible(page);
    await dialog.locator('[data-action="create-member"]').click();

    const shown = dialog.locator("[data-new-password]");
    await expect(shown).toBeVisible();
    const password = (await shown.textContent())!.trim();
    expect(password.length).toBeGreaterThanOrEqual(MIN_PASSWORD_LENGTH);
    await dialog.locator('[data-action="done"]').click();
    await expect(member(page, email)).toBeVisible();

    const context = await anonymousContext(browser);
    const fresh = await context.newPage();
    await signInWithPassword(fresh, email, password);
    expect((await fresh.request.get(ME_PATH)).status()).toBe(200);
    await context.close();

    // Shown once: a reload has nothing to show.
    await page.reload();
    await expect(page.locator("[data-new-password]")).toHaveCount(0);
  });

  test("a password that is too short is refused beside its field and nobody is added", async ({ page, api }, testInfo) => {
    const email = freshEmail(testInfo, "short");
    added.push(email);

    await page.goto(MEMBERS_PATH);
    await page.locator('[data-action="add-member"]').click();
    const dialog = page.locator("[data-add-member]");
    await dialog.getByLabel("Email address", { exact: true }).fill(email);
    await dialog.getByLabel("Password", { exact: true }).fill("short");
    await dialog.locator('[data-action="create-member"]').click();
    await expect(dialog.getByText(/must be at least 12 characters/)).toBeVisible();
    expect(must(await api.GET("/users")).members.some((each) => each.email === email)).toBe(false);
  });

  test("an administrator makes somebody a guest, switches them off after saying so, and on again", async ({ page, api }, testInfo) => {
    const email = freshEmail(testInfo, "roles");
    added.push(email);
    must(await api.POST("/users", { body: { email, name: "Rolf Roles", role: "member", password: "" } }));

    await page.goto(MEMBERS_PATH);
    await member(page, email).getByLabel("Standing of Rolf Roles").selectOption("guest");
    await expect(toast(page, "Rolf Roles is a guest now.")).toHaveText("Rolf Roles is a guest now.");
    await dismissToasts(page);
    await expect(member(page, email).locator("[data-member-role]")).toHaveAttribute("data-member-role", "guest");

    await member(page, email).locator('[data-action="switch-off-member"]').click();
    await page.locator('[data-action="confirm-switch-off-member"]').click();
    await expect(toast(page, "Rolf Roles was switched off.")).toHaveText("Rolf Roles was switched off.");
    await dismissToasts(page);
    await expect(member(page, email)).toHaveAttribute("data-member-active", "false");
    await expect(member(page, email).locator("[data-switched-off]")).toHaveText("Switched off");

    await member(page, email).locator('[data-action="switch-on-member"]').click();
    await expect(toast(page, "Rolf Roles was switched on and can sign in again.")).toHaveText("Rolf Roles was switched on and can sign in again.");
    await dismissToasts(page);
    await expect(member(page, email)).toHaveAttribute("data-member-active", "true");
  });

  test("the administrator's role granted and revoked shows on the person's own pages, and switching them off ends their session", async ({
    browser,
    page,
    api,
  }, testInfo) => {
    const email = freshEmail(testInfo, "standing");
    added.push(email);
    const password = randomBytes(PASSWORD_BYTES).toString("base64url");
    must(await api.POST("/users", { body: { email, name: "Stan Standing", role: "member", password } }));
    const context = await anonymousContext(browser);
    try {
      const theirs = await context.newPage();
      // The account was just made, so a replica may not hold it at the first try.
      await expect.poll(async () => (await theirs.request.post(LOGIN_API, { data: { email, password }, headers: { Origin: WEB_URL } })).status()).toBe(200);

      await page.goto(ROLES_PATH);
      await grantInForm(page, "Stan Standing", "administrator");
      await expect(toast(page, "Stan Standing holds Administrator now.")).toBeVisible();
      await dismissToasts(page);
      await page.goto(MEMBERS_PATH);
      await expect(member(page, email).locator('[data-member-holds="administrator"]')).toBeVisible();
      await openUntil(theirs, SETTINGS_PATH, () => expect(theirs.locator('[data-hub-card="settings"]')).toBeVisible(ONE_LOOK));
      await theirs.goto(MEMBERS_PATH);
      await expect(theirs.locator("[data-members]")).toBeVisible();

      await page.goto(ROLES_PATH);
      await findGrants(page, "Stan Standing");
      await revokeInTable(page, "Stan Standing", "administrator");
      await expect(toast(page, "Stan Standing no longer holds Administrator.")).toBeVisible();
      await dismissToasts(page);
      await expect(grantRow(page, "Stan Standing", "administrator")).toHaveCount(0);
      await openUntil(theirs, SETTINGS_PATH, async () => {
        await expect(theirs.locator('[data-hub-card="you"]')).toBeVisible(ONE_LOOK);
        await expect(theirs.locator('[data-hub-card="settings"]')).toHaveCount(0, ONE_LOOK);
      });
      await theirs.goto(MEMBERS_PATH);
      await expect(theirs.locator("[data-admins-only]")).toBeVisible();
      await expect(theirs.locator("[data-members]")).toHaveCount(0);

      await page.goto(MEMBERS_PATH);
      await member(page, email).locator('[data-action="switch-off-member"]').click();
      await page.locator('[data-action="confirm-switch-off-member"]').click();
      await expect(toast(page, "Stan Standing was switched off.")).toHaveText("Stan Standing was switched off.");
      await dismissToasts(page);
      await theirs.goto("/");
      await expect(theirs).toHaveURL(/\/login(\?|$)/);
      expect((await theirs.request.get(ME_PATH)).status()).toBe(401);
    } finally {
      await context.close();
    }
  });

  test("the password shown once is copied, and the copy signs the new person in", async ({ browser, page }, testInfo) => {
    const email = freshEmail(testInfo, "copied");
    added.push(email);
    await page.context().grantPermissions(["clipboard-read", "clipboard-write"], { origin: WEB_URL });

    await page.goto(MEMBERS_PATH);
    await page.locator('[data-action="add-member"]').click();
    const dialog = page.locator("[data-add-member]");
    await dialog.getByLabel("Email address", { exact: true }).fill(email);
    await dialog.locator('[data-action="create-member"]').click();
    const shown = (await dialog.locator("[data-new-password]").textContent())!.trim();
    await dialog.locator('[data-action="copy-password"]').click();
    await expect(dialog.locator('[data-action="copy-password"]')).toHaveText("Copied");
    const copied = await page.evaluate(() => navigator.clipboard.readText());
    expect(copied).toBe(shown);
    expect(copied.length).toBeGreaterThanOrEqual(MIN_PASSWORD_LENGTH);
    await dialog.locator('[data-action="done"]').click();

    const context = await anonymousContext(browser);
    try {
      const fresh = await context.newPage();
      await signInWithPassword(fresh, email, copied);
      expect((await fresh.request.get(ME_PATH)).status()).toBe(200);
    } finally {
      await context.close();
    }
  });
});
