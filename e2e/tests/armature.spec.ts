import type { Page } from "@playwright/test";
import { expect, ORG_SLUG } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { openShowing } from "../fixtures/replica";
import { expectAccessible, menuItem, openAccountMenu, startInScheme, type ColourScheme } from "../fixtures/shell";
import { armatureURL } from "../fixtures/stack";

// Stator's spec of the same flow (#26), with the account on a page of its own.

const SETTINGS_PATH = "/settings/armature";
const ACCOUNT_PATH = "/settings/armature-account";
// The stub makes a person of any name on first use, in the Armature organization its token names; this spec's is a
// tenant of its own, and the connection is the installation's, so the spec runs alone.
const TENANT = "e2e-armature";
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;
const REVOKED = "armature_pat_revoked";
const WEBHOOK_SECRET = "armature_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG";
const REFUSED = "Armature did not accept this token. Make a new one under Tokens in Armature and paste it here.";
const SCHEMES: ColourScheme[] = ["light", "dark"];

/** Pastes a token on the Armature account page and connects it. */
async function paste(page: Page, token: string): Promise<void> {
  await page.getByLabel(/^(Armature token|Paste a new token)$/).fill(token);
  await page.locator('[data-action="connect-armature"]').click();
}

test.describe("connecting Armature", { tag: "@desktop" }, () => {
  // A test that stopped half way leaves its connection to the next one.
  test.beforeEach(async ({ api }) => {
    await api.DELETE("/armature/connection");
  });
  test.afterEach(async ({ api }) => {
    await api.DELETE("/armature/connection");
  });

  for (const scheme of SCHEMES) {
    test(`an administrator connects, alice connects her account and disconnects, in ${scheme}`, async ({ page }) => {
      await startInScheme(page, scheme);
      const tenant = TENANT;

      await page.goto(SETTINGS_PATH);
      await expect(page.locator("main").getByRole("heading", { level: 1, name: "Armature" })).toBeVisible();
      await page.getByLabel("Armature address", { exact: true }).fill(armatureURL());
      await page.getByLabel("Armature organization", { exact: true }).fill(tenant);
      await page.getByLabel("Webhook secret", { exact: true }).fill(WEBHOOK_SECRET);
      await page.locator('[data-action="save-armature"]').click();
      await expect(page.getByText("Saved.", { exact: true })).toBeVisible();
      await expect(page.getByLabel("Webhook address", { exact: true })).toHaveValue(new RegExp(`/api/v1/armature/webhook/${ORG_SLUG}$`));
      await expect(page.getByLabel("Webhook secret", { exact: true })).toHaveValue("");
      await expect(page.locator("body")).not.toContainText(WEBHOOK_SECRET);
      // The disconnect button is the danger variant, so axe judges its colours too.
      await expect(page.locator('[data-action="disconnect-armature"]')).toBeVisible();
      await expectAccessible(page);

      await openAccountMenu(page);
      await menuItem(page, "armature-account").click();
      await expect(page).toHaveURL(new RegExp(`${ACCOUNT_PATH}$`));
      const section = page.locator("[data-armature-account]");
      await expect(page.locator("main").getByRole("heading", { level: 1, name: "Armature account" })).toBeVisible();

      await paste(page, REVOKED);
      await expect(section.getByText(REFUSED)).toBeVisible();
      await expectAccessible(page);

      await paste(page, patFor(tenant, "reader"));
      await expect(section.locator("[data-armature-read-only]")).toBeVisible();
      await paste(page, patFor(tenant, "alice"));
      await expect(section.locator("[data-armature-user]")).toHaveText(`Connected as Alice (alice@${tenant}.armature.test).`);
      await expect(section.locator("[data-armature-read-only]")).toHaveCount(0);
      await expect(section.getByLabel("Paste a new token", { exact: true })).toHaveCount(0);
      await expect(page.locator("body")).not.toContainText(patFor(tenant, "alice"));
      await expectAccessible(page);

      await section.locator('[data-action="check-armature"]').click();
      await expect(section.getByText("Armature accepts your token.")).toBeVisible();

      page.once("dialog", (dialog) => dialog.accept());
      await section.locator('[data-action="disconnect-armature-account"]').click();
      await expect(section.getByText("Your Armature account is disconnected.")).toBeVisible();
      await expect(section.getByLabel("Armature token", { exact: true })).toBeVisible();

      await page.goto(SETTINGS_PATH);
      page.once("dialog", (dialog) => dialog.accept());
      await page.locator('[data-action="disconnect-armature"]').click();
      await expect(page.getByText("Armature is disconnected.")).toBeVisible();
    });
  }

  test("a member is told to ask an administrator until Armature is connected", async ({ pageAs, api }) => {
    const bob = await pageAs("bob");
    await openShowing(bob, ACCOUNT_PATH, bob.locator('[data-armature-status="not_configured"]'));
    await expect(bob.locator('[data-armature-status="not_configured"]')).toContainText("Ask an administrator to connect it under Settings, Armature.");

    expect((await api.PUT("/armature/connection", { body: { baseUrl: armatureURL(), orgSlug: TENANT } })).response.status).toBe(200);
    await openShowing(bob, ACCOUNT_PATH, bob.getByLabel("Armature token", { exact: true }));
    await bob.goto(SETTINGS_PATH);
    await expect(bob.getByText("Only an administrator of this organization can connect Armature.")).toBeVisible();
  });
});
