import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Duplicates (#208): a draft is shown the requirements it is most like as its title is typed, and the project's
// duplicates page lists two that say the same thing until the pair is dismissed with a reason.

const KEY = uniqueKey("DUPL");
// Low enough that two titles a word apart are listed however the trigrams of the database weigh them.
const THRESHOLD = 0.4;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("duplicates", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("a draft is shown what it is like, and a pair of duplicates is dismissed with a reason", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Said twice" } }));
    const path = { params: { path: { projectKey: KEY } } };
    must(await api.PUT("/projects/{projectKey}/duplicate-settings", { ...path, body: { threshold: THRESHOLD } }));
    const short = must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Brakes engage within 100 ms" } })).requirement;
    const long = must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Brakes engage within 100 milliseconds" } })).requirement;

    await page.goto(`/projects/${KEY}/new`);
    await page.getByLabel("Title", { exact: true }).fill("Brakes engage within 100 ms");
    const hint = page.locator("[data-similar-hint]");
    await expect(hint.getByRole("region", { name: "Similar requirements" })).toBeVisible();
    await expect(hint.locator(`[data-similar="${short.key}"]`)).toBeVisible();
    await expect(hint.locator(`[data-similar="${long.key}"]`)).toBeVisible();
    await expect(hint.getByRole("status")).toHaveText("2 similar requirements.");
    await expectAccessible(page);

    await page.goto(`/projects/${KEY}/duplicates`);
    await expect(page.getByRole("heading", { level: 1, name: "Duplicates" })).toBeVisible();
    const pair = page.locator("[data-pair]");
    await expect(pair).toHaveCount(1);
    await expect(pair.locator(`[data-pair-side="${short.key}"]`)).toBeVisible();
    await expect(pair.locator(`[data-pair-side="${long.key}"]`)).toBeVisible();
    await expectAccessible(page);

    await pair.getByRole("button", { name: /^Dismiss / }).click();
    const dialog = page.getByRole("dialog", { name: "Dismiss the pair" });
    await dialog.getByRole("textbox", { name: "Reason" }).fill("One is the service brake, the other the parking brake.");
    await expectAccessible(page);
    await dialog.getByRole("button", { name: "Dismiss", exact: true }).click();
    await expect(dialog).toBeHidden();
    await expect(page.locator("[data-pair]")).toHaveCount(0);
    await expect(page.getByText("No likely duplicates.")).toBeVisible();
    await expectAccessible(page);
  });
});
