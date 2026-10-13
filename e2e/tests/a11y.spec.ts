import { ANONYMOUS, expect, LOGIN_PATH, test } from "../fixtures/auth";
import { expectAccessible, openShell, startInScheme, type ColourScheme } from "../fixtures/shell";

// Contrast needs real layout and real colours, which is why these run here and not in the jsdom suite. Every
// place of web/src/features/shell/places.ts, a new theme's editor and a path that leads nowhere, as alice, an admin.
const PATHS = [
  "/",
  "/projects",
  "/projects/new",
  "/search",
  "/settings/members",
  "/settings/sso",
  "/settings/default-theme",
  "/settings/audit",
  "/settings/themes",
  "/settings/themes/new",
  "/no-such-page",
];
const SCHEMES: ColourScheme[] = ["light", "dark"];

for (const scheme of SCHEMES) {
  for (const path of PATHS) {
    test(`${path} passes axe in ${scheme}`, async ({ page }) => {
      await startInScheme(page, scheme);
      await openShell(page, path);
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await expect(page.locator("main").locator("h1, [data-not-found]").first()).toBeVisible();
      await expect(page.locator("[data-skeleton]")).toHaveCount(0);
      await expectAccessible(page);
    });
  }

  test.describe(() => {
    test.use({ storageState: ANONYMOUS });

    test(`${LOGIN_PATH} passes axe in ${scheme}`, async ({ page }) => {
      await startInScheme(page, scheme);
      await page.goto(LOGIN_PATH);
      await expect(page.locator("[data-login]")).toBeVisible();
      await expectAccessible(page);
    });
  });
}
