import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// The trace model and its chains (#151): alice sets two levels in the project's settings and reads where a chain
// from a need breaks.

const KEY = uniqueKey("CHNS");

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("trace chains", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice sets a trace model and reads where a chain breaks", async ({ page, api }) => {
    const projectKey = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Chains" } }));
    const types: Record<string, string> = {};
    for (const name of ["Need", "System"]) {
      types[name] = must(await api.POST("/projects/{projectKey}/types", { ...projectKey, body: { name, icon: "test", colour: "#2255aa" } })).type.id;
    }
    for (const [title, type] of [
      ["Stop within 40 m", "Need"],
      ["Sound a horn", "Need"],
      ["Brakes engage within 100 ms", "System"],
    ] as const) {
      must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title, typeId: types[type] } }));
    }
    must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: `${KEY}-R3` } }, body: { typeKey: "derives", otherKey: `${KEY}-R1` } }));

    await page.goto(`/p/${KEY}/settings?tab=trace`);
    await page.getByRole("button", { name: "Add a level" }).click();
    await page.getByLabel("Name").fill("Need");
    await page.getByRole("group", { name: "Types: Need" }).getByRole("button", { name: "Need" }).click();
    await page.getByRole("button", { name: "Add a level" }).click();
    await page.getByLabel("Name").nth(1).fill("System");
    await page.getByRole("group", { name: "Types: System" }).getByRole("button", { name: "System" }).click();
    await page.getByRole("group", { name: "Reached from the level before by: System" }).getByRole("button", { name: "Is derived into" }).click();
    await page.getByRole("button", { name: "Save the model" }).click();
    await expect(page.getByText("The trace model is saved.")).toBeVisible();
    await expectAccessible(page);

    await page.goto(`/p/${KEY}/chains`);
    const table = page.getByRole("table", { name: "Chains" });
    await expect(table.locator("[data-chain-line]")).toHaveCount(2);
    await expect(table.locator("[data-broken]")).toContainText(`${KEY}-R2`);
    await expect(table.locator("[data-break]")).toHaveCount(1);
    await expect(page.getByText("2 chains on this page, 1 broken")).toBeVisible();
    await expectAccessible(page);

    await page.getByLabel("Only broken chains").check();
    await expect(table.locator("[data-chain-line]")).toHaveCount(1);
  });
});
