import type { Page } from "@playwright/test";
import { ANONYMOUS, expect, LOGIN_PATH } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible, openShell, startInScheme, type ColourScheme } from "../fixtures/shell";

// Contrast needs real layout and colours, so these run here, not in jsdom. The places are read from the shell's own
// sidebars, so a new one is walked unnamed, as the spec's own alice, an admin on the built-in theme.

/** Below this width the shell is a drawer, as web/src/config.ts SIDEBAR_DRAWER_BELOW_PX says. */
const DRAWER_BELOW_PX = 768;

/** The pages no sidebar link leads to: the settings hub, the Admin page, the forms under a place, and a path that leads
 *  nowhere. */
// The Access page's link opens its first tab; the others are addresses of their own.
const OFF_THE_SIDEBAR = [
  "/settings",
  "/admin",
  "/projects/new",
  "/settings/themes/new",
  "/admin/access/matrix",
  "/admin/access/members",
  "/admin/access/groups",
  "/admin/access/sso",
  "/no-such-page",
];

/** Where each group of places is listed: the work's in "Where you are", the person's in the settings area, and the
 *  installation's on the Admin page, which lists them at every width. */
const GROUPS = [
  { group: "requirements", from: "/", nav: "Where you are", links: '[data-sidebar-group="requirements"] a' },
  { group: "you", from: "/settings", nav: "Settings", links: '[data-sidebar-group="you"] a' },
  { group: "admin", from: "/admin", nav: "", links: "[data-admin-card]" },
] as const;

/** Every place of web/src/features/shell/places.ts when this walk was written, so a shell that lists fewer fails. */
const AT_LEAST = {
  requirements: ["/", "/search", "/notifications", "/reviews", "/projects"],
  you: [
    "/settings/profile",
    "/settings/notifications",
    "/settings/watching",
    "/settings/themes",
    "/settings/tokens",
    "/settings/connected-apps",
    "/settings/armature-account",
    "/settings/stator-account",
  ],
  admin: [
    "/admin/installation",
    "/admin/access",
    "/admin/users",
    "/admin/link-types",
    "/admin/armature",
    "/admin/stator",
    "/admin/connected-apps",
    "/admin/webhooks",
    "/admin/audit",
  ],
};

const SCHEMES: ColourScheme[] = ["light", "dark"];

/** The paths a group of the shell's navigation links to, read from the drawer on a phone. */
async function placesOf(page: Page, from: string, nav: string, selector: string): Promise<string[]> {
  await openShell(page, from);
  const narrow = (page.viewportSize()?.width ?? DRAWER_BELOW_PX) < DRAWER_BELOW_PX;
  if (nav && narrow) await page.locator('[data-action="drawer"]').click();
  const links = (nav ? page.getByRole("navigation", { name: nav }) : page.locator("main")).locator(selector);
  await expect(links.first()).toBeVisible();
  return links.evaluateAll((each) => each.map((link) => link.getAttribute("href") ?? ""));
}

async function expectPageAccessible(page: Page, path: string, scheme: ColourScheme): Promise<void> {
  await openShell(page, path);
  await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
  await expect(page.locator("main").locator("h1, [data-not-found]").first()).toBeVisible();
  await expect(page.locator("[data-skeleton]")).toHaveCount(0);
  await expectAccessible(page);
}

for (const scheme of SCHEMES) {
  for (const { group, from, nav, links } of GROUPS) {
    test(`every place of the ${group} group passes axe in ${scheme}`, async ({ page }) => {
      // One page after another, each with its own axe run; expectAccessible adds the runs' time.
      test.slow();
      await startInScheme(page, scheme);
      const paths = await placesOf(page, from, nav, links);
      expect(paths).toEqual(expect.arrayContaining(AT_LEAST[group]));
      expect(new Set(paths).size).toBe(paths.length);
      for (const path of paths) {
        await test.step(path, () => expectPageAccessible(page, path, scheme));
      }
    });
  }

  for (const path of OFF_THE_SIDEBAR) {
    test(`${path} passes axe in ${scheme}`, async ({ page }) => {
      await startInScheme(page, scheme);
      await expectPageAccessible(page, path, scheme);
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
