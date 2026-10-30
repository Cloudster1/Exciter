import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// The trace graph (#211): alice opens a requirement's graph, follows a node to centre it there, and reads it as a
// list and an impact she can download.

const KEY = uniqueKey("GRPH");

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("trace graph", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice opens a requirement's graph and follows a node", async ({ page, api }) => {
    const projectKey = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Graphed" } }));
    for (const title of ["Stop within 40 m", "Brakes engage within 100 ms", "Brake bench run"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title } }));
    }
    const link = (from: string, typeKey: string, to: string) =>
      api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: `${KEY}-${from}` } }, body: { typeKey, otherKey: `${KEY}-${to}` } });
    must(await link("R2", "derives", "R1"));
    must(await link("R3", "verifies", "R2"));

    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    await page.getByRole("link", { name: "Trace graph" }).click();
    await expect(page.getByRole("heading", { level: 1, name: `Trace graph of ${KEY}-R1` })).toBeVisible();
    const canvas = page.locator("[data-graph-canvas]");
    await expect(canvas.locator("[data-graph-node]")).toHaveCount(3);
    await expect(page.locator(`[data-impact-item="${KEY}-R3"]`)).toBeVisible();
    await expectAccessible(page);

    await canvas.locator("[data-graph-node]", { hasText: `${KEY}-R2` }).click();
    const chosen = page.locator(`[data-graph-chosen="${KEY}-R2"]`);
    await expect(chosen).toContainText("Brakes engage within 100 ms");
    await chosen.getByRole("button", { name: "Centre the graph here" }).click();
    await expect(page).toHaveURL(new RegExp(`/projects/${KEY}/r/${KEY}-R2/trace`));
    await expect(page.getByRole("heading", { level: 1, name: `Trace graph of ${KEY}-R2` })).toBeVisible();
    await expect(page.locator(`[data-impact-item="${KEY}-R1"]`)).toBeVisible();
    await expect(page.locator(`[data-impact-item="${KEY}-R3"]`)).toBeVisible();

    await page.getByRole("button", { name: "List" }).click();
    const list = page.locator("[data-graph-list]");
    await expect(list.locator(`[data-graph-item="${KEY}-R3"]`)).toBeVisible();
    await expectAccessible(page);

    const download = page.waitForEvent("download");
    await page.getByRole("link", { name: "Download as CSV" }).click();
    expect((await download).suggestedFilename()).toBe(`${KEY}-R2-impact.csv`);
  });
});
