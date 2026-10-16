import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Reuse and variants (#49): alice reuses a library's requirement in her product, the library revises it, and the
// product takes the new revision.

const LIB = uniqueKey("LIB");
const APP = uniqueKey("APP");

async function cleanUp(api: ExciterApi) {
  for (const key of [APP, LIB]) {
    const path = { params: { path: { projectKey: key } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (!found.data) continue;
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
}

test.describe("reuse and variants", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api));

  test("alice reuses a library requirement in her product, and takes its new revision", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: LIB, name: "Brake library" } }));
    must(await api.POST("/projects", { body: { key: APP, name: "Scooter" } }));
    must(await api.PATCH("/projects/{projectKey}", { params: { path: { projectKey: LIB } }, body: { settings: { library: true } } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: LIB } }, body: { title: "Brakes engage within 100 ms" } }));

    await page.goto(`/p/${LIB}/r/${LIB}-R1`);
    await page.locator('[data-action="reuse-requirement"]').click();
    const dialog = page.getByRole("dialog", { name: `Reuse ${LIB}-R1` });
    await dialog.getByLabel("Project").selectOption(APP);
    await expectAccessible(page);
    await dialog.getByRole("button", { name: "Reuse", exact: true }).click();

    await expect(page).toHaveURL(new RegExp(`/p/${APP}/r/${APP}-R1$`));
    const banner = page.locator("[data-reuse]");
    await expect(banner).toContainText(`Follows ${LIB}-R1 from the library, at its revision 1.`);
    await page.locator('[data-action="edit-requirement"]').click();
    await expect(page.locator("[data-reuse-locked]")).toBeVisible();
    await page.getByRole("button", { name: "Cancel" }).click();

    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: `${LIB}-R1` } },
        body: { title: "Brakes engage within 80 ms", baseRevision: 1 },
      }),
    );
    await page.reload();
    await expect(banner).toHaveAttribute("data-reuse-behind", "true");
    await expect(banner).toContainText(`${LIB}-R1 has revision 2 now.`);
    await expectAccessible(page);
    await page.locator('[data-action="take-source"]').click();
    await expect(page.locator("[data-requirement-notice]")).toHaveText("Took revision 2 of the source.");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Brakes engage within 80 ms");
    await expect(banner).toHaveAttribute("data-reuse-behind", "false");
  });
});
