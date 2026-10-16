import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { openShowing } from "../fixtures/replica";
import { expectAccessible, menuItem, openAccountMenu, startInScheme, type ColourScheme } from "../fixtures/shell";
import { statorURL } from "../fixtures/stack";

// The Armature spec of the same flow, for Stator (#40).

const SETTINGS_PATH = "/settings/stator";
const ACCOUNT_PATH = "/settings/stator-account";
// The stub makes a person of any name on first use, in the tenant its token names; this spec's is a tenant of its
// own, and the connection is the installation's, so the spec runs alone.
const TENANT = "e2e-stator";
const patFor = (tenant: string, person: string) => `stator_pat_${tenant}_${person}`;
const REVOKED = "stator_pat_revoked";
const WEBHOOK_SECRET = "stator_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG";
const REFUSED = "Stator did not accept this token. Make a new one under Your account, Access tokens in Stator and paste it here.";
const SCHEMES: ColourScheme[] = ["light", "dark"];

/** Pastes a token on the Stator account page and connects it. */
async function paste(page: Page, token: string): Promise<void> {
  await page.getByLabel(/^(Stator token|Paste a new token)$/).fill(token);
  await page.locator('[data-action="connect-stator"]').click();
}

test.describe("connecting Stator", { tag: "@desktop" }, () => {
  // A test that stopped half way leaves its connection to the next one.
  test.beforeEach(async ({ api }) => {
    await api.DELETE("/stator/connection");
  });
  test.afterEach(async ({ api }) => {
    await api.DELETE("/stator/connection");
  });

  for (const scheme of SCHEMES) {
    test(`an administrator connects, alice connects her account and disconnects, in ${scheme}`, async ({ page }) => {
      await startInScheme(page, scheme);

      await page.goto(SETTINGS_PATH);
      await expect(page.locator("main").getByRole("heading", { level: 1, name: "Stator" })).toBeVisible();
      await page.getByLabel("Stator address", { exact: true }).fill(statorURL());
      await page.getByLabel("Webhook secret", { exact: true }).fill(WEBHOOK_SECRET);
      await page.locator('[data-action="save-stator"]').click();
      await expect(page.getByText("Saved.", { exact: true })).toBeVisible();
      await expect(page.getByLabel("Webhook address", { exact: true })).toHaveValue(/\/api\/v1\/stator\/webhook$/);
      await expect(page.getByLabel("Webhook secret", { exact: true })).toHaveValue("");
      await expect(page.locator("[data-stator-connected]")).toContainText("Stator shows the requirements linked to each page.");
      await expect(page.locator("body")).not.toContainText(WEBHOOK_SECRET);
      // The disconnect button is the danger variant, so axe judges its colours too.
      await expect(page.locator('[data-action="disconnect-stator"]')).toBeVisible();
      await expectAccessible(page);

      await openAccountMenu(page);
      await menuItem(page, "stator-account").click();
      await expect(page).toHaveURL(new RegExp(`${ACCOUNT_PATH}$`));
      const section = page.locator("[data-stator-account]");
      await expect(page.locator("main").getByRole("heading", { level: 1, name: "Stator account" })).toBeVisible();

      await paste(page, REVOKED);
      await expect(section.getByText(REFUSED)).toBeVisible();
      await expectAccessible(page);

      await paste(page, patFor(TENANT, "alice"));
      await expect(section.locator("[data-stator-user]")).toHaveText(`Connected as Alice (alice@${TENANT}.stator.test).`);
      await expect(section.getByLabel("Paste a new token", { exact: true })).toHaveCount(0);
      await expect(page.locator("body")).not.toContainText(patFor(TENANT, "alice"));
      await expectAccessible(page);

      await section.locator('[data-action="check-stator"]').click();
      await expect(section.getByText("Stator accepts your token.")).toBeVisible();

      page.once("dialog", (dialog) => dialog.accept());
      await section.locator('[data-action="disconnect-stator-account"]').click();
      await expect(section.getByText("Your Stator account is disconnected.")).toBeVisible();
      await expect(section.getByLabel("Stator token", { exact: true })).toBeVisible();

      await page.goto(SETTINGS_PATH);
      page.once("dialog", (dialog) => dialog.accept());
      await page.locator('[data-action="disconnect-stator"]').click();
      await expect(page.getByText("Stator is disconnected.")).toBeVisible();
    });
  }

  test("a member is told to ask an administrator until Stator is connected", async ({ pageAs, api }) => {
    const bob = await pageAs("bob");
    await openShowing(bob, ACCOUNT_PATH, bob.locator('[data-stator-status="not_configured"]'));
    await expect(bob.locator('[data-stator-status="not_configured"]')).toContainText("Ask an administrator to connect it under Settings, Stator.");

    expect((await api.PUT("/stator/connection", { body: { baseUrl: statorURL() } })).response.status).toBe(200);
    await openShowing(bob, ACCOUNT_PATH, bob.getByLabel("Stator token", { exact: true }));
    await bob.goto(SETTINGS_PATH);
    await expect(bob.getByText("Only an administrator can connect Stator.")).toBeVisible();
  });
});
