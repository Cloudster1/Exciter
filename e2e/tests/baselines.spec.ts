import { readFile } from "node:fs/promises";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, confirm } from "../fixtures/shell";

// Baselines (#37): alice takes a baseline, R1 changes and R3 is written, and comparing the baseline with now says so
// before she locks it.

const KEY = uniqueKey("BASE");
// Tests run side by side, so the matrix's (#153) makes its own project.
const MATRIX_KEY = uniqueKey("BMTX");

async function removeProject(api: ExciterApi, projectKey = KEY) {
  const path = { params: { path: { projectKey } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("baselines", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => {
    await removeProject(api);
    await removeProject(api, MATRIX_KEY);
  });

  test("alice takes a baseline, compares it with now after a change, and locks it", async ({ page, api }) => {
    const projectKey = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Baselined" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title: "Brakes engage within 100 ms" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title: "Brake lights follow the pedal" } }));

    await page.goto(`/p/${KEY}/baselines`);
    await page.getByRole("button", { name: "Take baseline" }).click();
    const form = page.getByRole("form", { name: "Take a baseline" });
    await form.getByLabel("Name").fill("Release 1.0");
    await expectAccessible(page);
    await form.getByRole("button", { name: "Take" }).click();
    await expect(page.getByText("The baseline Release 1.0 is taken.")).toBeVisible();

    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: `${KEY}-R1` } },
        body: { title: "Brakes engage within 80 ms", baseRevision: 1 },
      }),
    );
    must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title: "Brake fluid is checked" } }));

    await page.getByRole("list", { name: "Baselines" }).getByRole("link", { name: "Release 1.0" }).click();
    const spec = page.getByRole("list", { name: "Release 1.0" });
    await expect(spec.getByRole("listitem")).toHaveCount(2);
    await expect(spec).toContainText("Brakes engage within 100 ms");
    await page.getByRole("button", { name: "Compare" }).click();

    const items = page.getByRole("list", { name: "Requirements" });
    await expect(items.locator(`[data-item-change="${KEY}-R1"]`)).toContainText("Revision 1 to 2");
    await expect(items.locator(`[data-item-change="${KEY}-R3"]`)).toContainText("Added");
    await expectAccessible(page);
    await items.locator(`[data-item-change="${KEY}-R1"]`).getByRole("link", { name: "Show the change" }).click();
    await expect(page).toHaveURL(new RegExp(`/p/${KEY}/r/${KEY}-R1/history\\?from=1&to=2`));

    await page.goBack();
    await page.getByRole("link", { name: "Release 1.0" }).click();
    await page.getByRole("button", { name: "Lock" }).click();
    await confirm(page);
    await expect(page.getByText("A locked baseline keeps its name and is never deleted.")).toBeVisible();
    await expect(page.getByRole("button", { name: "Delete" })).toHaveCount(0);
  });

  test("alice reads the matrix a baseline recorded, and what changed in it since", async ({ page, api }) => {
    const projectKey = { params: { path: { projectKey: MATRIX_KEY } } };
    must(await api.POST("/projects", { body: { key: MATRIX_KEY, name: "Baseline matrix" } }));
    for (const title of ["Brakes engage within 100 ms", "Brake pedal reads within 10 ms", "Brake bench run"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title } }));
    }
    must(
      await api.POST("/requirements/{reqKey}/links", {
        params: { path: { reqKey: `${MATRIX_KEY}-R2` } },
        body: { typeKey: "derives", otherKey: `${MATRIX_KEY}-R1` },
      }),
    );
    const taken = must(await api.POST("/projects/{projectKey}/baselines", { ...projectKey, body: { name: "Signed off" } })).baseline;
    must(
      await api.POST("/requirements/{reqKey}/links", {
        params: { path: { reqKey: `${MATRIX_KEY}-R3` } },
        body: { typeKey: "verifies", otherKey: `${MATRIX_KEY}-R1` },
      }),
    );

    await page.goto(`/p/${MATRIX_KEY}/baselines/${taken.id}`);
    await page.getByRole("button", { name: "Matrix", exact: true }).click();
    const matrix = page.getByRole("table", { name: "Matrix" });
    await expect(matrix.locator(`[data-cell="${MATRIX_KEY}-R1:${MATRIX_KEY}-R2"]`)).toHaveAttribute("title", `${MATRIX_KEY}-R1 to ${MATRIX_KEY}-R2: Derives`);
    await expect(matrix.getByRole("columnheader", { name: `${MATRIX_KEY}-R3` })).toHaveCount(0);

    await page.getByLabel("Compare the matrix with").selectOption("now");
    await expect(matrix.locator(`[data-cell="${MATRIX_KEY}-R1:${MATRIX_KEY}-R3"] [data-change="added"]`)).toBeVisible();
    await expectAccessible(page);
    const download = page.waitForEvent("download");
    await page.getByRole("link", { name: "Download CSV" }).click();
    const file = await readFile((await (await download).path()) ?? "", "utf8");
    expect(file).toContain("Verifies (added)");
  });
});
