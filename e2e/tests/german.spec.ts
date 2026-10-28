import type { Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, openShell, scrollsSideways } from "../fixtures/shell";

// The interface in German because the browser asks for it, as alice, the spec's own administrator, who has chosen
// no language: the settings pages she reads are an administrator's.

test.use({ locale: "de-DE", timezoneId: "UTC" });

const KEY = uniqueKey("DE");
const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

async function heading(page: Page, path: string, words: string) {
  await openShell(page, path);
  await expect(page.locator("main").getByRole("heading", { level: 1, name: words, exact: true })).toBeVisible();
}

test.describe("the interface in German", () => {
  test("follows the browser's language until one is chosen, and back again", { tag: "@desktop" }, async ({ page, api }) => {
    try {
      await openShell(page, "/");
      await expect(page.locator("html")).toHaveAttribute("lang", "de");
      await expect(page.getByRole("link", { name: "Start", exact: true }).first()).toBeVisible();
      await heading(page, "/settings", "Einstellungen");
      await heading(page, "/settings/themes", "Designs");
      await heading(page, "/settings/members", "Mitglieder");

      await heading(page, "/settings/audit", "Protokoll");
      const when = page.locator("[data-audit-row] time").first();
      await expect(when).toBeVisible();
      const at = new Date((await when.getAttribute("datetime"))!);
      const two = (n: number) => String(n).padStart(2, "0");
      // Day, month and year with dots and the time on a 24 hour clock, where English writes the month's name first.
      await expect(when).toHaveText(new RegExp(`^${two(at.getUTCDate())}\\.${two(at.getUTCMonth() + 1)}\\.${at.getUTCFullYear()}, \\d{2}:\\d{2}:\\d{2}$`));

      await heading(page, "/settings/profile", "Ihr Profil");
      await expect(page.locator('[data-language="browser"]')).toBeChecked();
      await page.getByRole("radio", { name: "English" }).click();
      await expect(page.locator("main").getByRole("heading", { level: 1, name: "Your profile" })).toBeVisible();
      await expect(page.locator("html")).toHaveAttribute("lang", "en");
      await page.locator('[data-language="browser"]').click();
      await expect(page.locator("main").getByRole("heading", { level: 1, name: "Ihr Profil" })).toBeVisible();
      await expect(page.locator("html")).toHaveAttribute("lang", "de");
    } finally {
      await api.PATCH("/auth/me", { body: { locale: "" } });
    }
  });

  test("passes axe and keeps to a phone's width, as German words are longer", { tag: "@mobile" }, async ({ page, api }) => {
    // One page after another, each with its own axe run; expectAccessible adds the runs' time.
    test.slow();
    const path = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Bremsen" } }));
    try {
      must(
        await api.POST("/projects/{projectKey}/requirements", {
          ...path,
          body: { title: "Bremsweg unter 40 m", statement: paragraph("Das Fahrzeug hält an.") },
        }),
      );
      for (const place of ["/", "/projects", "/settings/members", "/settings/themes", `/p/${KEY}/r/${KEY}-R1`]) {
        await test.step(place, async () => {
          await openShell(page, place);
          await expect(page.locator("html")).toHaveAttribute("lang", "de");
          await expect(page.locator("main").locator("h1").first()).toBeVisible();
          await expect(page.locator("[data-skeleton]")).toHaveCount(0);
          await expectAccessible(page);
          expect(await scrollsSideways(page), `${place} scrolls sideways`).toBe(false);
        });
      }
    } finally {
      await api.PUT("/projects/{projectKey}/archive", path);
      await api.DELETE("/projects/{projectKey}", path);
    }
  });
});
