import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, openProjectSettings } from "../fixtures/shell";

const KEY = uniqueKey("SET");

/** Every section of a project's settings, in the order web/src/features/projects/settingsSections.ts lists them. */
const SECTIONS = [
  "details",
  "types",
  "attributes",
  "fields",
  "workflow",
  "approval",
  "links",
  "trace",
  "quality",
  "risks",
  "templates",
  "permissions",
  "import",
  "reqif",
  "word",
  "trash",
];
const ADMINS_ONLY = ["permissions", "import", "reqif", "word"];

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

// The project's key is this worker's own, so it meets no other spec.
test.describe("a project's settings", () => {
  test.beforeEach(async ({ api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Settled" } }));
  });
  test.afterEach(async ({ api }) => removeProject(api));

  test("every section opens at its own address, with its tab chosen and the project then Settings above it", { tag: "@desktop" }, async ({ page }) => {
    await page.goto(`/projects/${KEY}`);
    await openProjectSettings(page, KEY);
    await expect(page).toHaveURL(`/projects/${KEY}/settings/details`);
    await expect(page.locator("[data-settings-tab]")).toHaveCount(SECTIONS.length);
    await expectAccessible(page);

    for (const section of SECTIONS) {
      await page.goto(`/projects/${KEY}/settings/${section}`);
      await expect(page.locator(`[data-settings-section="${section}"]`)).toBeVisible();
      await expect(page.locator(`[data-settings-tab="${section}"]`)).toHaveAttribute("aria-selected", "true");
    }

    const crumbs = page.locator("[data-shell-strip]").getByRole("navigation", { name: "Breadcrumb" });
    await expect(crumbs.getByRole("link", { name: "Settled" })).toHaveAttribute("href", `/projects/${KEY}`);
    await expect(crumbs.getByText("Settings")).toHaveAttribute("aria-current", "page");

    await page.locator('[data-settings-tab="links"]').click();
    await expect(page).toHaveURL(`/projects/${KEY}/settings/links`);
    await page.goBack();
    await expect(page).toHaveURL(`/projects/${KEY}/settings/trash`);
  });

  test("old addresses lead to where their pages are now", { tag: "@desktop" }, async ({ page }) => {
    await page.goto(`/p/${KEY}`);
    await expect(page).toHaveURL(`/projects/${KEY}`);
    await page.goto(`/p/${KEY}/settings?tab=links`);
    await expect(page).toHaveURL(`/projects/${KEY}/settings/links`);
    await expect(page.locator('[data-settings-section="links"]')).toBeVisible();
    await page.goto(`/projects/${KEY}/settings?tab=quality`);
    await expect(page).toHaveURL(`/projects/${KEY}/settings/quality`);
    await page.goto(`/p/${KEY}/permissions`);
    await expect(page).toHaveURL(`/projects/${KEY}/settings/permissions`);
    await page.goto(`/p/${KEY}/trash`);
    await expect(page).toHaveURL(`/projects/${KEY}/settings/trash`);
    await page.goto(`/p/${KEY}/settings/workflows/new`);
    await expect(page).toHaveURL(`/projects/${KEY}/settings/workflows/new`);
    await expect(page.locator("[data-workflow-design-page]")).toBeVisible();
  });

  test("the palette leads to the settings of the project open", { tag: "@desktop" }, async ({ page }) => {
    await page.goto(`/projects/${KEY}`);
    await expect(page.locator(`[data-project-nav="${KEY}"]`)).toBeVisible();
    await page.keyboard.press("ControlOrMeta+k");
    await page.getByRole("combobox").fill("project settings");
    await page.locator(`[data-quick-switcher-option="place:project-settings:${KEY}"]`).click();
    await expect(page).toHaveURL(`/projects/${KEY}/settings/details`);
  });

  test("a member reads the settings, is shown no section only administrators use, and is told who to ask", async ({ pageAs }) => {
    const bob = await pageAs("bob");
    // Alice made the project, so bob's read may come from a replica a moment behind.
    await openUntil(bob, `/projects/${KEY}`, () => expect(bob.getByText("No requirements yet")).toBeVisible(ONE_LOOK));
    await openProjectSettings(bob, KEY);
    await expect(bob).toHaveURL(`/projects/${KEY}/settings/details`);
    await expect(bob.locator("[data-settings-read-only]")).toHaveText(
      "Only the project's administrators change its settings. Ask one of them, or ask for the role.",
    );
    await expect(bob.locator("[data-settings-tab]")).toHaveCount(SECTIONS.length - ADMINS_ONLY.length);
    for (const section of ADMINS_ONLY) await expect(bob.locator(`[data-settings-tab="${section}"]`)).toHaveCount(0);
    await expect(bob.locator('[data-action="save-project"]')).toHaveCount(0);

    for (const section of ADMINS_ONLY) {
      await bob.goto(`/projects/${KEY}/settings/${section}`);
      const refused = bob.locator(`[data-settings-refused="${section}"]`);
      await expect(refused).toContainText("Only the project's administrators change its settings");
      await expect(refused).toContainText("Ask one of them, or ask for the role.");
    }
    await expectAccessible(bob);
  });
});
