import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Test cases (#250): alice marks a type as test cases, writes one with steps and a parameter, and reads its
// history step by step.

const KEY = uniqueKey("TCS");

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("test cases", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice writes a test case with steps and reads its history", async ({ page, api }) => {
    // It marks a type, writes a case with steps and parameters, revises it and reads its history: over 20 s on CI.
    test.setTimeout(60_000);
    must(await api.POST("/projects", { body: { key: KEY, name: "Brakes" } }));

    await page.goto(`/projects/${KEY}/settings/types`);
    await page.getByRole("button", { name: "New type" }).click();
    const form = page.locator("[data-type-form]");
    await form.getByLabel("Name", { exact: true }).fill("Test case");
    await form.getByLabel("Its requirements are").selectOption({ label: "Test cases" });
    await form.locator('[data-action="save-type"]').click();
    await expect(page.locator('[data-type="Test case"] [data-test-case-type]')).toBeVisible();

    await page.goto(`/projects/${KEY}/new`);
    await page.getByLabel("Title").fill("Brake on a wet road");
    await page.locator("[data-type-select]").selectOption({ label: "Test case" });
    const steps = page.getByRole("group", { name: "Steps" });
    await steps.locator('[data-action="add-step"]').click();
    await steps.getByRole("textbox", { name: "Step 1: Action" }).fill("Press the pedal.");
    await steps.getByRole("textbox", { name: "Step 1: Expected result" }).fill("The car stops within 40 m.");
    await steps.locator('[data-action="add-step"]').click();
    await steps.getByRole("textbox", { name: "Step 2: Action" }).fill("Release the pedal.");
    const parameters = page.getByRole("group", { name: "Parameters" });
    await parameters.locator('[data-action="add-parameter"]').click();
    await parameters.getByLabel("Name", { exact: true }).fill("Speed");
    await parameters.getByLabel("Values, apart by semicolons").fill("30; 50");
    await expectAccessible(page);
    await page.locator('[data-action="create-requirement"]').click();

    const table = page.getByRole("table", { name: "Steps" });
    await expect(table.getByRole("row", { name: /1 Press the pedal\. The car stops within 40 m\./ })).toBeVisible();
    await expect(page.getByRole("table", { name: "Parameters" }).getByRole("row", { name: "2 50" })).toBeVisible();
    await expectAccessible(page);

    await page.getByRole("button", { name: "Edit" }).click();
    await page.getByRole("group", { name: "Steps" }).getByRole("button", { name: "Move step 2 up" }).click();
    await page.locator('[data-action="save-requirement"]').click();
    await expect(page.getByText("Saved as revision 2.")).toBeVisible();
    await expect(table.getByRole("row", { name: /1 Release the pedal\./ })).toBeVisible();

    await page.locator("[data-history-link]").click();
    await page.locator('[data-revision-row="2"] [data-action="compare-previous"]').click();
    await expect(page.locator('[data-diff-of="steps"] [data-step-change="moved"]').first()).toBeVisible();
    await expectAccessible(page);
  });
});
