import { ANONYMOUS, expect, LOGIN_PATH } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible, openShell, startInScheme, type ColourScheme } from "../fixtures/shell";
import { NEW_PROJECT_PATH, PLACE_PATHS, THEMES_PATH } from "../../web/src/config";

// Contrast needs real layout and real colours, which is why these run here and not in the jsdom suite. Every place
// of web/src/features/shell/places.ts by PLACE_PATHS, a new project, a new theme's editor and a path that leads
// nowhere, as the spec's own alice, an admin on the built-in theme whatever default another spec sets.
const PATHS = [...PLACE_PATHS, "/settings", NEW_PROJECT_PATH, `${THEMES_PATH}/new`, "/no-such-page"];
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
