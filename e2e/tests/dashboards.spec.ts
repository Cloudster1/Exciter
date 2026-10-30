import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Dashboards (#212): alice builds a dashboard from a template, moves a widget, and follows a bar into the grid,
// which keeps the same requirements the bar counted.

const KEY = uniqueKey("DASH");

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("dashboards", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice builds a dashboard and follows a bar to the grid", async ({ page, api }) => {
    const path = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Watched" } }));
    for (const [title, priority] of [
      ["Stop within 40 m", "must"],
      ["Sound a horn", "should"],
      ["Brakes engage within 100 ms", "must"],
    ] as const) {
      must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title, priority } }));
    }

    await page.goto(`/projects/${KEY}/dashboards`);
    await page.getByRole("button", { name: "New dashboard" }).click();
    await page.getByLabel("Name", { exact: true }).fill("Brakes");
    await page.getByRole("button", { name: "Make the dashboard" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Brakes" })).toBeVisible();
    await expect(page.locator("[data-widget-card]")).toHaveCount(6);
    await expectAccessible(page);

    await page.getByRole("button", { name: "Arrange" }).click();
    await page.getByRole("button", { name: "Add a widget" }).click();
    const form = page.locator("[data-widget-form]");
    await form.getByLabel("Title").fill("By priority");
    await form.getByLabel("Count by").selectOption("priority");
    await expect(form.locator("[data-widget-preview] [data-bar]")).toHaveCount(2);
    await form.getByRole("button", { name: "Add it" }).click();
    const added = page.locator('[data-widget="By priority"]');
    await expect(added).toBeVisible();
    await added.getByRole("button", { name: "Move By priority earlier" }).click();
    await expect(page.locator("[data-dashboard-grid] > li").nth(5)).toHaveAttribute("data-widget", "By priority");
    await page.getByRole("button", { name: "Done" }).click();
    await expectAccessible(page);

    await added
      .locator("[data-chart=bars]")
      .getByRole("link", { name: /Open the 2 with must/ })
      .click();
    await expect(page).toHaveURL(new RegExp(`/projects/${KEY}/grid`));
    await expect(page.locator("[data-grid-count]")).toContainText("2");
  });
});
