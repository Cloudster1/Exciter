import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible, openAdmin, openShell } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

// The Admin area (#470): every page of the installation reached the way an administrator finds it, the Admin entry
// and then Admin's own section nav, and every address the settings area had for them sent on.

/** Each page of web/src/features/shell/places.ts adminPlaces, by its id, with its address and its heading. */
const PAGES = [
  { id: "installation", path: "/admin/installation", heading: "Installation" },
  { id: "access", path: "/admin/access/roles", heading: "Access" },
  { id: "users", path: "/admin/users", heading: "Users" },
  { id: "link-types", path: "/admin/link-types", heading: "Link types" },
  { id: "armature", path: "/admin/armature", heading: "Armature" },
  { id: "stator", path: "/admin/stator", heading: "Stator" },
  { id: "connected-apps", path: "/admin/connected-apps", heading: "Connected apps" },
  { id: "webhooks", path: "/admin/webhooks", heading: "Webhooks" },
  { id: "audit", path: "/admin/audit", heading: "Audit log" },
];

/** The settings area's addresses of before, and where each leads now. */
const MOVED = [
  { from: "/settings/installation", to: "/admin/installation" },
  { from: "/settings/default-theme", to: "/admin/installation" },
  { from: "/settings/access", to: "/admin/access/roles" },
  { from: "/settings/access/groups", to: "/admin/access/groups" },
  { from: "/settings/members", to: "/admin/access/members" },
  { from: "/settings/sso", to: "/admin/access/sso" },
  { from: "/settings/link-types", to: "/admin/link-types" },
  { from: "/settings/all-tokens", to: "/admin/connected-apps" },
  { from: "/settings/all-connected-apps", to: "/admin/connected-apps" },
  { from: "/settings/armature", to: "/admin/armature" },
  { from: "/settings/stator", to: "/admin/stator" },
  { from: "/settings/webhooks", to: "/admin/webhooks" },
  { from: "/settings/audit", to: "/admin/audit" },
];

test.describe("the Admin area", { tag: "@desktop" }, () => {
  for (const { id, path, heading } of PAGES) {
    test(`an administrator reaches ${heading} through the Admin entry and its section nav`, async ({ page }) => {
      await openAdmin(page, id);
      await expect(page).toHaveURL(`${WEB_URL}${path}`);
      await expect(page.locator("main").getByRole("heading", { level: 1, name: heading, exact: true })).toBeVisible();
      const crumbs = page.locator("[data-shell-strip]").getByRole("navigation", { name: "Breadcrumb" });
      await expect(crumbs.getByRole("link", { name: "Admin" })).toHaveAttribute("href", "/admin");
      await expect(page.locator("[data-admin-refused]")).toHaveCount(0);
    });
  }

  test("the Admin page lists every page with what it is for, and leads to each", async ({ page }) => {
    await openShell(page, "/admin");
    await expect(page.locator("[data-admin-card]")).toHaveCount(PAGES.length);
    await expect(page.locator('[data-admin-card="users"]')).toContainText("switching an account off or back on");
    await expect(page.locator('[data-admin-card="connected-apps"]')).toContainText("every token and grant they hold");
    await expectAccessible(page);
    await page.locator('[data-admin-card="users"]').click();
    await expect(page).toHaveURL(`${WEB_URL}/admin/users`);
    await page.locator('[data-admin-nav] [data-nav="admin"]').getByRole("link").click();
    await expect(page).toHaveURL(`${WEB_URL}/admin`);
  });

  for (const { from, to } of MOVED) {
    test(`${from} leads to ${to}`, async ({ page }) => {
      await page.goto(from);
      await expect(page).toHaveURL(`${WEB_URL}${to}`);
      await expect(page.locator("main h1")).toBeVisible();
    });
  }

  test("bob, a member, is refused once for the whole of Admin", async ({ pageAs }) => {
    const bob = await pageAs("bob");
    for (const { path } of PAGES) {
      await openShell(bob, path);
      await expect(bob.locator("[data-admin-refused]")).toHaveCount(1);
      await expect(bob.locator("main").getByRole("heading", { level: 1, name: "Admin" })).toBeVisible();
    }
    await expectAccessible(bob);
  });
});
