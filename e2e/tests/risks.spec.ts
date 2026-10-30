import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, toast, dismissToasts } from "../fixtures/shell";

// Risks and their ratings (#259): alice sets the project's risk scheme, marks a type as risks, rates a hazard and
// reads its rating's history value by value.

const KEY = uniqueKey("RSK");

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("risks", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice sets the risk scheme, rates a hazard and reads its history", async ({ page, api }) => {
    // It sets a scheme, a type, writes and revises a risk over four pages, each checked for accessibility.
    test.setTimeout(60_000);
    must(await api.POST("/projects", { body: { key: KEY, name: "Brakes" } }));

    await page.goto(`/projects/${KEY}/settings/risks`);
    await page.locator('[data-action="start-risk-scheme"]').click();
    await page.getByRole("table", { name: "Matrix" }).getByRole("combobox", { name: "Level at severity 5 and probability 1" }).selectOption("High");
    await expectAccessible(page);
    await page.locator('[data-action="save-risk-scheme"]').click();
    await expect(toast(page, "The risk scheme is saved.")).toBeVisible();
    await dismissToasts(page);

    await page.goto(`/projects/${KEY}/settings/types`);
    await page.getByRole("button", { name: "New type" }).click();
    const form = page.locator("[data-type-form]");
    await form.getByLabel("Name", { exact: true }).fill("Hazard");
    await form.getByLabel("Its requirements are").selectOption({ label: "Risks" });
    await form.locator('[data-action="save-type"]').click();
    await expect(page.locator('[data-type="Hazard"] [data-risk-type]')).toBeVisible();

    await page.goto(`/projects/${KEY}/new`);
    await page.getByLabel("Title").fill("Brakes fail on ice");
    await page.locator("[data-type-select]").selectOption({ label: "Hazard" });
    const rating = page.getByRole("group", { name: "Risk rating" });
    await rating.getByRole("combobox", { name: "Severity before mitigation" }).selectOption("5");
    await rating.getByRole("combobox", { name: "Probability before mitigation" }).selectOption("1");
    await expect(rating.locator('[data-rating-side="before"] [data-risk-level-tag="High"]')).toBeVisible();
    await expectAccessible(page);
    await page.locator('[data-action="create-requirement"]').click();

    const view = page.getByRole("table", { name: "Risk rating" });
    await expect(view.getByRole("row", { name: /Before mitigation 5 1 5 High/ })).toBeVisible();
    await expectAccessible(page);

    await page.getByRole("button", { name: "Edit" }).click();
    const edit = page.getByRole("group", { name: "Risk rating" });
    await edit.getByRole("combobox", { name: "Severity after mitigation" }).selectOption("2");
    await edit.getByRole("combobox", { name: "Probability after mitigation" }).selectOption("1");
    await page.locator('[data-action="save-requirement"]').click();
    await expect(page.getByText("Saved as revision 2.")).toBeVisible();
    await expect(view.getByRole("row", { name: /After mitigation 2 1 2 Low/ })).toBeVisible();

    await page.locator("[data-history-link]").click();
    await page.locator('[data-revision-row="2"] [data-action="compare-previous"]').click();
    await expect(page.locator('[data-diff-of="risk"] [data-risk-change="after.severity"]')).toBeVisible();
    await expectAccessible(page);
  });
});
