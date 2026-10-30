import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { openShowing } from "../fixtures/replica";
import { expectAccessible, startInScheme, type ColourScheme, confirm, toast, dismissToasts } from "../fixtures/shell";
import { armatureURL } from "../fixtures/stack";

// Stator's spec of the same flow (#26), with the account on a page of its own.

const SETTINGS_PATH = "/admin/armature";
const ACCOUNT_PATH = "/settings/armature-account";
// The stub makes a person of any name on first use, in the Armature organization its token names; this spec's is a
// tenant of its own, and the connection is the installation's, so the spec runs alone.
const TENANT = "e2e-armature";
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;
const REVOKED = "armature_pat_revoked";
const WEBHOOK_SECRET = "armature_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG";
const REFUSED = "Armature did not accept this token. Make a new one under Tokens in Armature and paste it here.";
const SCHEMES: ColourScheme[] = ["light", "dark"];

/** Sets what the stub's /meta lists, for every tenant; null puts back Armature's own list. */
async function armatureOffers(capabilities: string[] | null): Promise<void> {
  const res = await fetch(`${armatureURL()}/_stub/capabilities`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ capabilities }),
  });
  expect(res.status, "the stub took what it offers").toBe(204);
}

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
    await armatureOffers(null);
  });

  for (const scheme of SCHEMES) {
    test(`an administrator connects, alice connects her account and disconnects, in ${scheme}`, async ({ page }) => {
      await startInScheme(page, scheme);
      const tenant = TENANT;

      await page.goto(SETTINGS_PATH);
      await expect(page.locator("main").getByRole("heading", { level: 1, name: "Armature" })).toBeVisible();
      await page.getByLabel("Armature address", { exact: true }).fill(armatureURL());
      await page.getByLabel("Webhook secret", { exact: true }).fill(WEBHOOK_SECRET);
      await page.locator('[data-action="save-armature"]').click();
      await expect(page.getByText("Saved.", { exact: true })).toBeVisible();
      await expect(page.getByLabel("Webhook address", { exact: true })).toHaveValue(/\/api\/v1\/armature\/webhook$/);
      await expect(page.getByLabel("Webhook secret", { exact: true })).toHaveValue("");
      await expect(page.locator("body")).not.toContainText(WEBHOOK_SECRET);
      // The disconnect button is the danger variant, so axe judges its colours too.
      await expect(page.locator('[data-action="disconnect-armature"]')).toBeVisible();
      await expectAccessible(page);

      await page.goto("/settings");
      await page.locator('[data-settings-link="armature-account"]').click();
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
      await expect(toast(page, "Armature accepts your token.")).toBeVisible();
      await dismissToasts(page);

      await section.locator('[data-action="disconnect-armature-account"]').click();
      await confirm(page);
      await expect(toast(page, "Your Armature account is disconnected.")).toBeVisible();
      await dismissToasts(page);
      await expect(section.getByLabel("Armature token", { exact: true })).toBeVisible();

      await page.goto(SETTINGS_PATH);
      await page.locator('[data-action="disconnect-armature"]').click();
      await confirm(page);
      await expect(toast(page, "Armature is disconnected.")).toBeVisible();
      await dismissToasts(page);
    });
  }

  test("the settings say what an older Armature lacks, until saving again finds it updated", async ({ page }) => {
    await armatureOffers(["webhooks@1", "theme-export@1", "remote-links@1"]);
    await page.goto(SETTINGS_PATH);
    await page.getByLabel("Armature address", { exact: true }).fill(armatureURL());
    await page.locator('[data-action="save-armature"]').click();
    const note = page.getByRole("note", { name: "This Armature needs an update" });
    await expect(note).toContainText("does not offer issue-embed@1, which Exciter needs to show and find its issues on requirements.");
    await expect(note.getByRole("listitem")).toHaveCount(1);
    await expectAccessible(page);

    await armatureOffers(null);
    await page.locator('[data-action="save-armature"]').click();
    await expect(page.getByText("Saved.", { exact: true })).toBeVisible();
    await expect(note).toHaveCount(0);
  });

  test("a member is told to ask an administrator until Armature is connected", async ({ pageAs, api }) => {
    const bob = await pageAs("bob");
    await openShowing(bob, ACCOUNT_PATH, bob.locator('[data-armature-status="not_configured"]'));
    await expect(bob.locator('[data-armature-status="not_configured"]')).toContainText("Ask an administrator to connect it under Admin, Armature.");

    expect((await api.PUT("/armature/connection", { body: { baseUrl: armatureURL() } })).response.status).toBe(200);
    await openShowing(bob, ACCOUNT_PATH, bob.getByLabel("Armature token", { exact: true }));
    await bob.goto(SETTINGS_PATH);
    await expect(bob.getByText("Only administrators can open this page")).toBeVisible();
  });
});
