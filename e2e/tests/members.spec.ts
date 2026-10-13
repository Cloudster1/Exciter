import type { Page, TestInfo } from "@playwright/test";
import { must } from "../fixtures/api";
import { ANONYMOUS, expect, ME_PATH, signInWithPassword } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

const MEMBERS_PATH = "/settings/members";
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

    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
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

  test("an administrator changes somebody's role, and removes them after saying so", async ({ page, api }, testInfo) => {
    const email = freshEmail(testInfo, "roles");
    added.push(email);
    must(await api.POST("/users", { body: { email, name: "Rolf Roles", role: "member", password: "" } }));

    await page.goto(MEMBERS_PATH);
    await member(page, email).getByLabel("Role of Rolf Roles").selectOption("admin");
    await expect(page.locator("[data-members-notice]")).toHaveText("Rolf Roles is now Administrator.");
    await expect(member(page, email).locator("[data-member-role]")).toHaveAttribute("data-member-role", "admin");

    await member(page, email).locator('[data-action="remove-member"]').click();
    await page.locator('[data-action="confirm-remove-member"]').click();
    await expect(page.locator("[data-members-notice]")).toHaveText("Rolf Roles was removed from the organization.");
    await expect(member(page, email)).toHaveCount(0);
  });
});
