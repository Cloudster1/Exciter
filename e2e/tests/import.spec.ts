import { uniqueKey } from "../fixtures/seed";
import { readFileSync } from "node:fs";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible } from "../fixtures/shell";

const SOURCE = uniqueKey("EXP");
const TARGET = uniqueKey("IMP");

async function cleanUp(api: ExciterApi) {
  for (const key of [SOURCE, TARGET]) {
    const path = { params: { path: { projectKey: key } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (found.data) {
      if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
      await api.DELETE("/projects/{projectKey}", path);
    }
  }
}

// The projects' keys are this worker's own, so they meet no other spec's.
test.describe("CSV import and export", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api));

  test("alice exports a project's grid and imports the file into another project", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: SOURCE, name: "Exported" } }));
    must(await api.POST("/projects", { body: { key: TARGET, name: "Imported" } }));
    const path = { params: { path: { projectKey: SOURCE } } };
    const section = must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { kind: "section", title: "Checkout" } })).requirement;
    must(
      await api.POST("/projects/{projectKey}/requirements", {
        ...path,
        body: { title: "Pay the basket", place: { parentId: section.id, afterId: null } },
      }),
    );
    must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Refund" } }));
    must(
      await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: `${SOURCE}-R3` } }, body: { typeKey: "derives", otherKey: `${SOURCE}-R2` } }),
    );

    await page.goto(`/p/${SOURCE}/grid?columns=key&columns=kind&columns=title`);
    await expect(page.locator(`[data-grid="${SOURCE}"]`)).toContainText("Pay the basket");
    const download = page.waitForEvent("download");
    await page.locator('[data-action="export-csv"]').click();
    const file = await (await download).path();

    await page.goto(`/p/${TARGET}/settings?tab=import`);
    await page.getByLabel("CSV file").setInputFiles({ name: "export.csv", mimeType: "text/csv", buffer: readFileSync(file) });
    await page.locator('[data-action="upload-import"]').click();
    await expect(page.getByText("Nothing keeps the import from starting.")).toBeVisible();
    await expect(page.locator('[data-import-target="parent"]')).toHaveValue("parent");
    await expectAccessible(page);
    await page.locator('[data-action="start-import"]').click();
    await expect(page.locator('[data-import-step="done"]')).toContainText("3 requirements made", { timeout: 30_000 });

    await page.goto(`/p/${TARGET}`);
    for (const title of ["Checkout", "Pay the basket", "Refund"]) await expect(page.getByRole("link", { name: title })).toBeVisible();
    const tree = must(await api.GET("/projects/{projectKey}/tree", { params: { path: { projectKey: TARGET } } })).nodes;
    expect(tree.map((node) => `${node.outline} ${node.title}`)).toEqual(["1 Checkout", "1.1 Pay the basket", "2 Refund"]);
  });
});
