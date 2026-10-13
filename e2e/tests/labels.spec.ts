import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { expectAccessible } from "../fixtures/shell";

const KEY = "TAGS";

/** Archives and deletes the project the test made, so each test starts without it. */
async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

// The organization is this file's alone, so its labels meet no other spec's.
test.describe("labels", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice coins a label, puts it on a second requirement from the suggestions, and finds both by it", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Tagged" } }));
    for (const title of ["Pay the basket", "Refund the basket", "Print the receipt"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title } }));
    }

    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    const labels = page.locator("[data-requirement-labels]");
    await expect(labels).toContainText("No labels yet.");
    const box = labels.getByRole("combobox", { name: "Add a label" });
    await box.fill("Safety Critical");
    await box.press("Enter");
    await expect(labels.locator('[data-label="safety-critical"]')).toBeVisible();
    await expect(page.getByText("Added the label safety-critical.")).toBeAttached();
    await expect(page.locator("[data-revision]")).toHaveAttribute("data-revision", "1");
    await expectAccessible(page);

    await page.goto(`/p/${KEY}/r/${KEY}-R2`);
    const second = page.locator("[data-requirement-labels]").getByRole("combobox", { name: "Add a label" });
    await second.fill("saf");
    await page.locator('[data-label-option="safety-critical"]').click();
    await expect(page.locator('[data-requirement-labels] [data-label="safety-critical"]')).toBeVisible();

    await page.locator('[data-requirement-labels] [data-label="safety-critical"]').getByRole("link").click();
    await expect(page).toHaveURL(`/p/${KEY}?label=safety-critical`);
    await expect(page.locator("[data-label-filter]")).toContainText("Showing requirements labelled safety-critical");
    await expect(page.locator("[data-requirement-row]")).toHaveCount(2);
    await expect(page.locator(`[data-requirement-row="${KEY}-R3"]`)).toHaveCount(0);
    await expectAccessible(page);

    await page.locator('[data-label-filter] [data-action="label-everywhere"]').click();
    await expect(page).toHaveURL("/labels/safety-critical");
    await expect(page.locator("[data-label-count]")).toHaveText("2 requirements");
    await expect(page.locator(`[data-labelled-requirement="${KEY}-R1"]`)).toContainText("Pay the basket");
    await expectAccessible(page);

    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    await page.getByRole("button", { name: "Remove label safety-critical" }).click();
    await expect(page.locator("[data-requirement-labels]")).toContainText("No labels yet.");
    await page.goto(`/p/${KEY}/r/${KEY}-R1/history?tab=activity`);
    await expect(page.locator('[data-activity-entry="change"]').first()).toContainText("changed the labels from safety-critical to nothing");
  });
});
