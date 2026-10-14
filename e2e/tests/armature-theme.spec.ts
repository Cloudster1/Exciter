import type { Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { armatureURL } from "../fixtures/stack";

// Following the Armature theme (#30), Stator's spec: the stub shows each person one of the example themes, as
// Armature would show the theme they picked there.

// The connection is the installation's, so the spec runs alone, with a tenant of the stub's of its own.
const TENANT = "e2e-armature-theme";
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;
const SCHEMES: ColourScheme[] = ["light", "dark"];
// The light accents of the two examples, as the stylesheet sets them.
const DEEP_TECH_ACCENT = "#1f5fc5";
const CONSTELLATION_ACCENT = "#00699e";

const customStyle = (page: Page) => page.locator("style#exciter-theme");
const rootVar = (page: Page, name: string) => page.evaluate((n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim(), name);
const followSwitch = (page: Page) => page.getByRole("switch", { name: "Follow my Armature theme" });
const note = (page: Page) => page.locator("[data-armature-theme-note]");

async function showTheme(theme: string, broken = false) {
  const answer = await fetch(`${armatureURL()}/_stub/${TENANT}/people/alice/theme`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ theme, broken }),
  });
  expect(answer.ok, `the stub answered ${answer.status}`).toBe(true);
}

test.describe("following the Armature theme", { tag: "@desktop" }, () => {
  test.beforeEach(async ({ api }) => {
    await fetch(`${armatureURL()}/_stub/${TENANT}`, { method: "DELETE" });
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL() } }));
    must(await api.PUT("/armature/account/token", { body: { token: patFor(TENANT, "alice") } }));
    await showTheme("deep-tech");
  });

  test.afterEach(async ({ api }) => {
    await api.DELETE("/armature/theme");
    await api.DELETE("/armature/connection");
  });

  test("alice follows her Armature theme, and a change there reaches the page", async ({ page }) => {
    await startInScheme(page, "light");
    await page.goto("/settings/themes");
    await expect(followSwitch(page)).toHaveAttribute("aria-checked", "false");
    await expect(customStyle(page)).toHaveCount(0);

    await followSwitch(page).click();
    await expect(followSwitch(page)).toHaveAttribute("aria-checked", "true");
    await expect(note(page)).toContainText("Exciter shows the theme you use in Armature");
    await expect(page.locator("main")).toContainText("You are using Deep-Tech, the theme you use in Armature.");
    await expect.poll(() => rootVar(page, "--color-accent")).toBe(DEEP_TECH_ACCENT);

    // The theme stays on across a reload, painted before the server answers.
    await page.reload();
    await expect.poll(() => rootVar(page, "--color-accent")).toBe(DEEP_TECH_ACCENT);

    // The themes page asks Armature now, so the change shows without waiting for the cache.
    await showTheme("constellation");
    await page.reload();
    await expect(page.locator("main")).toContainText("You are using Constellation, the theme you use in Armature.");
    await expect.poll(() => rootVar(page, "--color-accent")).toBe(CONSTELLATION_ACCENT);

    await followSwitch(page).click();
    await expect(followSwitch(page)).toHaveAttribute("aria-checked", "false");
    await expect(customStyle(page)).toHaveCount(0);
  });

  for (const scheme of SCHEMES) {
    test(`the follow setting is accessible in ${scheme}`, async ({ page, api }) => {
      // Deep-Tech, which the stub shows alice, so axe judges an example theme's colours as well as the setting.
      await startInScheme(page, scheme);
      must(await api.PUT("/armature/theme"));
      await page.goto("/settings/themes");
      await expect(followSwitch(page)).toHaveAttribute("aria-checked", "true");
      await expect(page.locator("main")).toContainText("You are using Deep-Tech, the theme you use in Armature.");
      await expect(customStyle(page)).toBeAttached();
      await expectAccessible(page);

      // A theme Exciter cannot use says why, and the page falls back.
      await showTheme("constellation", true);
      await page.reload();
      await expect(note(page)).toContainText("Armature's theme cannot be used in Exciter");
      await expect(customStyle(page)).toHaveCount(0);
      await expectAccessible(page);
    });
  }
});
