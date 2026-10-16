import { readFile } from "node:fs/promises";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// The matrix's axes (#147): alice counts only the verifying links, only those pointing at each row, and finds the
// same matrix after a reload and in its file. Its gaps (#148): what nothing links to, as rows and as columns.

const KEY = uniqueKey("MTRX");
// Tests run side by side, so each makes its own project.
const GAP_KEY = uniqueKey("MGAP");

async function removeProject(api: ExciterApi, projectKey: string) {
  const path = { params: { path: { projectKey } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("the traceability matrix", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => {
    await removeProject(api, KEY);
    await removeProject(api, GAP_KEY);
  });

  test("alice picks the link types and the direction that count, and the address and the file keep them", async ({ page, api }) => {
    const projectKey = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Matrix" } }));
    for (const title of ["Brakes engage within 100 ms", "Brake pedal reads within 10 ms", "Brake bench run"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title } }));
    }
    for (const [from, typeKey] of [
      ["R2", "derives"],
      ["R3", "verifies"],
    ] as const) {
      must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: `${KEY}-${from}` } }, body: { typeKey, otherKey: `${KEY}-R1` } }));
    }

    await page.goto(`/p/${KEY}/matrix`);
    const matrix = page.getByRole("table", { name: "Matrix" });
    await expect(matrix.locator(`[data-cell="${KEY}-R1:${KEY}-R2"]`)).toHaveAttribute("title", `${KEY}-R1 to ${KEY}-R2: Derives`);

    await page.getByRole("group", { name: "Link types" }).getByRole("button", { name: "Verifies" }).click();
    await expect(matrix.getByRole("columnheader", { name: `${KEY}-R2` })).toHaveCount(0);
    await expect(matrix.locator(`[data-cell="${KEY}-R1:${KEY}-R3"]`)).toHaveAttribute("title", `${KEY}-R1 to ${KEY}-R3: Verifies`);
    await page.getByRole("button", { name: "Incoming" }).click();
    await expect(matrix.getByRole("columnheader", { name: `${KEY}-R1` })).toHaveCount(0);
    await expect(matrix.locator(`[data-cell="${KEY}-R1:${KEY}-R3"]`)).toBeVisible();
    await expectAccessible(page);

    await page.reload();
    await expect(page.getByRole("group", { name: "Link types" }).getByRole("button", { name: "Verifies" })).toHaveAttribute("aria-pressed", "true");
    await expect(page.getByRole("button", { name: "Incoming" })).toHaveAttribute("aria-pressed", "true");
    await expect(matrix.locator(`[data-cell="${KEY}-R1:${KEY}-R3"]`)).toBeVisible();
    const download = page.waitForEvent("download");
    await page.getByRole("link", { name: "Download CSV" }).click();
    const file = await readFile((await (await download).path()) ?? "", "utf8");
    expect(file).toContain(`Key,Title,Links,${KEY}-R3`);
    expect(file).not.toContain(`,${KEY}-R2`);
  });

  test("alice finds what nothing links to, in the rows and in the columns of a column filter", async ({ page, api }) => {
    const projectKey = { params: { path: { projectKey: GAP_KEY } } };
    must(await api.POST("/projects", { body: { key: GAP_KEY, name: "Gaps" } }));
    for (const title of ["Brakes engage within 100 ms", "Brake pedal reads within 10 ms", "Horn sounds"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title } }));
    }
    must(
      await api.POST("/requirements/{reqKey}/links", {
        params: { path: { reqKey: `${GAP_KEY}-R2` } },
        body: { typeKey: "derives", otherKey: `${GAP_KEY}-R1` },
      }),
    );

    await page.goto(`/p/${GAP_KEY}/matrix`);
    const matrix = page.getByRole("table", { name: "Matrix" });
    await expect(matrix.locator(`[data-cell="${GAP_KEY}-R1:${GAP_KEY}-R2"]`)).toBeVisible();
    await page.getByLabel("Only gaps").selectOption("rows");
    await expect(matrix.getByRole("rowheader", { name: new RegExp(`${GAP_KEY}-R3`) })).toBeVisible();
    await expect(matrix.getByRole("rowheader", { name: new RegExp(`${GAP_KEY}-R1 `) })).toHaveCount(0);

    await page.getByRole("combobox", { name: "Column query" }).fill("kind = requirement");
    await page.getByRole("combobox", { name: "Column query" }).press("Enter");
    await page.getByLabel("Only gaps").selectOption("columns");
    await expect(matrix.getByRole("columnheader", { name: `${GAP_KEY}-R3` })).toBeVisible();
    await expect(matrix.getByRole("columnheader", { name: `${GAP_KEY}-R1` })).toHaveCount(0);
    await expect(matrix.locator("[data-gap]").first()).toBeVisible();
    await expectAccessible(page);
  });
});
