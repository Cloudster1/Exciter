import { readFile } from "node:fs/promises";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Coverage (#38): alice asks every requirement to be decomposed, sees half of them are, keeps the ones that are
// not, and finds the link in the matrix and its file.

const KEY = uniqueKey("COVR");

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("coverage", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice sets what coverage asks, keeps what is missing, and reads the matrix", async ({ page, api }) => {
    const projectKey = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Covered" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title: "Brakes engage within 100 ms" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title: "Brake pedal reads within 10 ms" } }));
    must(
      await api.POST("/requirements/{reqKey}/links", {
        params: { path: { reqKey: `${KEY}-R2` } },
        body: { typeKey: "derives", otherKey: `${KEY}-R1` },
      }),
    );

    await page.goto(`/p/${KEY}/coverage`);
    await expect(page.getByText("No rule yet, so coverage asks nothing.", { exact: false })).toBeVisible();
    const rules = page.getByRole("list", { name: "Rules" });
    // The box shows what the rule says once saved, so it turns when the answer comes, not on the click.
    const decomposed = rules.locator('[data-rule="Every other type"]').getByLabel("Decomposed");
    await decomposed.click();
    await expect(decomposed).toBeChecked();
    await expect(page.locator('[data-dimension="decomposed"]')).toContainText("50%");
    const list = page.getByRole("list", { name: "Requirements" });
    await expect(list.locator(`[data-coverage-row="${KEY}-R1"]`)).toContainText("Decomposed: Full");
    await expectAccessible(page);

    await page.getByRole("combobox", { name: "Decomposed" }).selectOption("none");
    await expect(list.getByRole("listitem")).toHaveCount(1);
    await expect(list.locator(`[data-coverage-row="${KEY}-R2"]`)).toBeVisible();

    await page.getByRole("button", { name: "Matrix" }).click();
    const matrix = page.getByRole("table", { name: "Matrix" });
    await expect(matrix.locator(`[data-cell="${KEY}-R1:${KEY}-R2"]`)).toHaveAttribute("title", `${KEY}-R1 to ${KEY}-R2: Derives`);
    await expectAccessible(page);
    const download = page.waitForEvent("download");
    await page.getByRole("link", { name: "Download CSV" }).click();
    const file = await readFile((await (await download).path()) ?? "", "utf8");
    expect(file).toContain(`${KEY}-R2,Brake pedal reads within 10 ms,Derives`);
  });
});
