import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { expectAccessible } from "../fixtures/shell";

const KEY = "SYSSPEC";

async function cleanUp(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (found.data) {
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
}

// The organization is this file's alone, so its projects meet no other spec's.
test.describe("project templates", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api));

  test("an administrator starts a project from the system specification template", async ({ page, api }) => {
    await page.goto("/projects/new");
    await page.getByLabel("Name", { exact: true }).fill("Braking system");
    await page.getByLabel("Key", { exact: true }).fill(KEY);
    await page.locator('[data-template="system"]').click();
    await expect(page.locator('[data-template="system"]')).toHaveAttribute("aria-checked", "true");
    await expectAccessible(page);
    await page.locator('[data-action="create-project"]').click();
    await expect(page).toHaveURL(new RegExp(`/p/${KEY}$`));

    for (const section of ["Introduction", "Stakeholder needs", "System requirements", "Interfaces", "Constraints"]) {
      await expect(page.getByRole("link", { name: section, exact: true })).toBeVisible();
    }
    const types = must(await api.GET("/projects/{projectKey}/types", { params: { path: { projectKey: KEY } } })).types.map((t) => t.name);
    expect(types).toEqual(["Stakeholder need", "System requirement", "Interface", "Constraint"]);
    await page.goto(`/p/${KEY}/settings?tab=attributes`);
    await expect(page.locator('[data-attribute="Verification method"]')).toBeVisible();
  });
});
