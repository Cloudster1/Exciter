import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("ATT");

/** Archives and deletes the project the test made, so each test starts without it. */
async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

// The project's key is this worker's own, so it meets no other spec.
test.describe("types and attributes", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("an administrator shapes a project's types and attributes, and a requirement is held to them", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Attributed" } }));
    await page.goto(`/projects/${KEY}/settings`);

    await page.getByRole("tab", { name: "Types" }).click();
    await expect(page.locator('[data-type="Requirement"]')).toContainText("Default");
    await page.locator('[data-action="new-type"]').click();
    await page.getByLabel("Name", { exact: true }).fill("Interface");
    await page.getByLabel("Icon", { exact: true }).selectOption("interface");
    await page.locator('[data-action="save-type"]').click();
    await expect(page.locator('[data-type="Interface"]')).toBeVisible();

    await page.getByRole("tab", { name: "Attributes" }).click();
    await page.locator('[data-action="new-attribute"]').click();
    const form = page.locator("[data-attribute-form]");
    await form.getByLabel("Name", { exact: true }).fill("Verification");
    await form.getByLabel("Kind", { exact: true }).selectOption("select");
    await form.getByLabel("Required: a new revision needs a value").check();
    await form.getByLabel("Requirement", { exact: true }).check();
    for (const option of ["Test", "Analysis"]) {
      await form.getByLabel("New option").fill(option);
      await form.locator('[data-action="add-option"]').click();
    }
    await expectAccessible(page);
    await form.locator('[data-action="save-attribute"]').click();
    await expect(page.locator('[data-attribute="Verification"]')).toContainText("Required");

    await page.goto(`/projects/${KEY}/new`);
    await page.getByLabel("Title", { exact: true }).fill("Pay the basket");
    await page.locator('[data-action="create-requirement"]').click();
    await expect(page.getByText("Verification is required. Give it a value.")).toBeVisible();
    await page.getByLabel("Verification (required)").selectOption({ label: "Test" });
    await page.locator('[data-action="create-requirement"]').click();
    await expect(page).toHaveURL(`/projects/${KEY}/r/${KEY}-R1`);
    await expect(page.locator('[data-attribute-value="Verification"]')).toBeVisible();
    await expect(page.locator("[data-attribute-values]")).toContainText("Test");
    await expect(page.locator('[data-requirement-type="Requirement"]')).toBeVisible();
    await expectAccessible(page);

    await page.locator('[data-action="edit-requirement"]').click();
    await page.getByLabel("Verification (required)").selectOption({ label: "Analysis" });
    await page.locator('[data-action="save-requirement"]').click();
    await expect(page.getByText("Saved as revision 2.")).toBeVisible();
    await expect(page.locator("[data-attribute-values]")).toContainText("Analysis");
  });
});
