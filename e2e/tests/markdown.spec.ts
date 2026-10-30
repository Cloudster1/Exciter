import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect, test } from "../fixtures/auth";
import { openShowing } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("MKD");

/** Archives and deletes the project the test made, so each test starts without it. */
async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

const WRITTEN = ["Brakes **engage** within `100 ms`.", "- front\n- rear", "| Signal | ms |\n| --- | --: |\n| pedal | 100 |"].join("\n\n");

// The project's key is this worker's own, so it meets no other spec.
test.describe("Markdown", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("a requirement written in Markdown through the API reads as written, and the editor keeps it so", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Markdown" } }));
    const made = must(
      await api.POST("/projects/{projectKey}/requirements", {
        params: { path: { projectKey: KEY } },
        body: { title: "Brakes", statementMarkdown: WRITTEN },
      }),
    ).requirement;

    await openShowing(page, `/projects/${KEY}/r/${made.key}`, page.locator("[data-statement] strong"));
    const statement = page.locator("[data-statement]");
    await expect(statement.locator("strong")).toHaveText("engage");
    await expect(statement.locator("code")).toHaveText("100 ms");
    await expect(statement.locator("li")).toHaveText(["front", "rear"]);
    await expect(statement.locator("th")).toHaveText(["Signal", "ms"]);
    await expect(statement.locator("td")).toHaveText(["pedal", "100"]);
    await expectAccessible(page);

    // The editor opens what Markdown made and saves it untouched beside a new title.
    await page.locator('[data-action="edit-requirement"]').click();
    await page.getByLabel("Title", { exact: true }).fill("Brakes engage");
    await page.locator('[data-action="save-requirement"]').click();
    await expect(page.getByText("Saved as revision 2.")).toBeVisible();
    const read = must(await api.GET("/requirements/{reqKey}", { params: { path: { reqKey: made.key }, query: { format: "markdown" } } })).requirement;
    expect(read.title).toBe("Brakes engage");
    expect(read.statementMarkdown).toBe(WRITTEN);
  });
});
