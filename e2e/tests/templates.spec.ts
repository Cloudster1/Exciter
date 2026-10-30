import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("SYS");

async function cleanUp(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (found.data) {
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
}

// The projects' keys are this worker's own, so they meet no other spec's.
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
    await expect(page).toHaveURL(new RegExp(`/projects/${KEY}$`));

    for (const section of ["Introduction", "Stakeholder needs", "System requirements", "Interfaces", "Constraints"]) {
      await expect(page.getByRole("link", { name: section, exact: true })).toBeVisible();
    }
    const types = must(await api.GET("/projects/{projectKey}/types", { params: { path: { projectKey: KEY } } })).types.map((t) => t.name);
    expect(types).toEqual(["Stakeholder need", "System requirement", "Interface", "Constraint"]);
    await page.goto(`/projects/${KEY}/settings/attributes`);
    await expect(page.locator('[data-attribute="Verification method"]')).toBeVisible();
  });
});
