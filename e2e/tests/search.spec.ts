import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("SRCH");

/** Archives and deletes the project the test made, so each test starts without it. */
async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

/** A project of three requirements, to find by their words and to narrow in the grid. */
async function makeProject(api: ExciterApi) {
  must(await api.POST("/projects", { body: { key: KEY, name: "Searching" } }));
  const write = async (body: { title: string; statement: object; priority?: "must" | "could"; labels?: string[] }) =>
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body }));
  await write({ title: "Pay the basket", statement: paragraph("The shopper pays for everything in the basket at once."), priority: "must" });
  await write({ title: "Refund a payment", statement: paragraph("A refund goes back to the card it came from."), priority: "could" });
  await write({ title: "Print the receipt", statement: paragraph("The till prints a receipt for every basket."), labels: ["paper"] });
}

// The project's key is this worker's own, so it meets no other spec.
test.describe("search", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice finds requirements by their words, and goes to one by its key from the palette", async ({ page, api }) => {
    await makeProject(api);
    await page.goto(`/search?q=basket&project=${KEY}`);
    await expect(page.locator("[data-search-count]")).toHaveText("2 results");
    const pay = page.locator(`[data-search-hit="${KEY}-R1"]`);
    await expect(pay.locator("mark").first()).toHaveText("basket");
    await expect(page.locator(`[data-search-hit="${KEY}-R3"] [data-search-snippet] mark`)).toHaveText("basket");
    await expectAccessible(page);

    await page.locator('[data-filter="label"] [data-label-input]').fill("paper");
    await page.locator('[data-filter="label"] [data-label-input]').press("Enter");
    await expect(page).toHaveURL(/label=paper/);
    await expect(page.locator("[data-search-hit]")).toHaveCount(1);

    await page.keyboard.press("Control+k");
    await page.locator("[data-quick-switcher-input]").fill(`${KEY.toLowerCase()}-r2`);
    await expect(page.locator(`[data-quick-switcher-option^="requirement:"]`)).toContainText(`${KEY}-R2 Refund a payment`);
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(`/p/${KEY}/r/${KEY}-R2`);
  });

  test("alice narrows and orders the grid, edits a priority in place, and saves the grid as a shared view", async ({ page, api }) => {
    await makeProject(api);
    await page.goto(`/p/${KEY}/grid`);
    const rows = page.locator("[data-grid-row]");
    await expect(rows).toHaveCount(3);
    await expectAccessible(page);

    await page.locator('[data-grid-column="title"] [data-action="sort-column"]').click();
    await expect(rows.first()).toHaveAttribute("data-grid-row", `${KEY}-R1`);
    await expect(rows.nth(1)).toHaveAttribute("data-grid-row", `${KEY}-R3`);
    await page.locator('[data-grid-filter="priority"]').selectOption("must");
    await expect(rows).toHaveCount(1);
    await page.locator('[data-action="clear-grid-filters"]').click();
    await expect(rows).toHaveCount(3);

    await page.locator(`[data-grid-row="${KEY}-R3"] [data-grid-edit="priority"]`).selectOption("should");
    await expect(page.locator("[data-grid-notice]")).toHaveText(`Saved ${KEY}-R3.`);
    await page.locator('[data-action="choose-columns"]').click();
    await page.locator('[data-column-option="labels"]').check();
    await expect(page.locator(`[data-grid-row="${KEY}-R3"] [data-label="paper"]`)).toBeVisible();

    await page.locator('[data-action="save-view"]').click();
    const dialog = page.getByRole("dialog", { name: "Save as a view" });
    await dialog.getByLabel("Name").fill("Ordered by title");
    await dialog.getByRole("checkbox", { name: "Share with everybody in the project" }).check();
    await expectAccessible(page);
    await dialog.locator('[data-action="confirm-save-view"]').click();
    await expect(page.locator("[data-grid-notice]")).toHaveText("Saved the view Ordered by title.");

    await page.goto(`/p/${KEY}/grid`);
    await page.locator("[data-view-select]").selectOption({ label: "Ordered by title (Shared)" });
    await expect(page).toHaveURL(/sort=title/);
    await expect(page.locator('[data-grid-column="labels"]')).toBeVisible();
  });
});
