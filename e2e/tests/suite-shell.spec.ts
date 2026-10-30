import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, openAccountMenu, openShell } from "../fixtures/shell";

// The shell as the suite's look-and-feel standard draws it (#473): the account menu past the rail, the narrow
// strip, and the palette's groups.

const KEY = uniqueKey("SUI");

test.describe("the suite's shell", () => {
  test("opens the account menu past the rail, with the person at its head and its groups set apart", { tag: "@desktop" }, async ({ page, people }) => {
    await openShell(page, "/");
    await expect(page.locator('[data-rail] [data-action="account"]')).toHaveClass(/\brounded-full\b/);
    await openAccountMenu(page);
    const rail = (await page.locator("[data-rail]").boundingBox())!;
    const menu = (await page.getByRole("menu", { name: "Your account" }).boundingBox())!;
    expect(menu.x).toBeGreaterThanOrEqual(rail.x + rail.width);
    const head = page.locator("[data-account-header]");
    await expect(head).toContainText(people.alice.name);
    await expect(head.locator("[data-account-role]")).toBeVisible();
    await expect(page.getByRole("menu", { name: "Your account" }).getByRole("separator")).not.toHaveCount(0);
    await expectAccessible(page);
  });

  test("names the installation on the phone's strip, between the menu button and the bell", { tag: "@mobile" }, async ({ page, api }) => {
    const me = must(await api.GET("/auth/me"));
    await openShell(page, "/");
    const strip = page.locator("[data-narrow-strip]");
    // The suite's strip is h-12, the rail's width; the root type is 14px, so that is 42px here as in Armature.
    const { height, rail } = await strip.evaluate((el) => ({
      height: el.getBoundingClientRect().height,
      rail: 3 * Number.parseFloat(getComputedStyle(document.documentElement).fontSize),
    }));
    expect(height).toBe(rail);
    await expect(strip.locator('[data-action="drawer"]')).toBeVisible();
    await expect(strip.locator("[data-installation-name]")).toHaveText(me.installation.name);
    await expect(strip.locator('[data-action="notifications"]')).toBeVisible();
    await expectAccessible(page);
  });

  test("finds a project and what can be done in the palette, each in a group of its own", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Palette groups" } }));
    try {
      await openShell(page, "/");
      await page.keyboard.press("ControlOrMeta+k");
      const input = page.locator("[data-quick-switcher-input]");
      await input.fill(KEY.toLowerCase());
      const projects = page.getByRole("listbox", { name: "Projects" });
      await expect(projects.locator(`[data-quick-switcher-option="project:${KEY}"]`)).toBeVisible();
      await expectAccessible(page);
      await projects.locator(`[data-quick-switcher-option="project:${KEY}"]`).click();
      await expect(page).toHaveURL(new RegExp(`/projects/${KEY}$`));

      await page.keyboard.press("ControlOrMeta+k");
      await page.locator("[data-quick-switcher-input]").fill("new project");
      await expect(page.getByRole("listbox", { name: "Actions" }).locator('[data-quick-switcher-option="action:new-project"]')).toBeVisible();
    } finally {
      const path = { params: { path: { projectKey: KEY } } };
      await api.PUT("/projects/{projectKey}/archive", path);
      await api.DELETE("/projects/{projectKey}", path);
    }
  });
});
