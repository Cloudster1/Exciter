import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("HIS");

/** Archives and deletes the project the test made, so each test starts without it. */
async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

// The project's key is this worker's own, so it meets no other spec.
test.describe("history", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("a reader compares two revisions, and an editor restores the older one as the next", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "History" } }));
    must(
      await api.POST("/projects/{projectKey}/requirements", {
        params: { path: { projectKey: KEY } },
        body: { title: "Pay", statement: paragraph("The shopper pays at once."), comment: "First." },
      }),
    );
    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: `${KEY}-R1` } },
        body: { statement: paragraph("The shopper pays later."), baseRevision: 1, comment: "Later." },
      }),
    );

    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    await page.locator("[data-history-link]").click();
    await expect(page).toHaveURL(new RegExp(`/projects/${KEY}/r/${KEY}-R1/history`));
    await expect(page.locator('[data-revision-row="2"]')).toContainText("Later.");
    await expectAccessible(page);

    await page.locator('[data-revision-row="2"] [data-action="compare-previous"]').click();
    const statement = page.locator('[data-diff-of="statement"]');
    await expect(statement.locator('del[data-diff="delete"]')).toContainText("at once");
    await expect(statement.locator('ins[data-diff="insert"]')).toContainText("later");
    await expectAccessible(page);

    await page.getByRole("button", { name: "Back to the history" }).click();
    await page.locator('[data-revision-row="1"] [data-action="restore-revision"]').click();
    await page.locator('[data-action="confirm-restore"]').click();
    await expect(page.getByText("Revision 1 restored as revision 3.")).toBeVisible();
    await expect(page.locator('[data-revision-row="3"]')).toContainText("Restores revision 1");

    await page.getByRole("tab", { name: "Activity" }).click();
    await expect(page.locator('[data-activity-entry="revision"]').first()).toContainText("wrote revision 3");

    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    await expect(page.locator("[data-statement]")).toHaveText("The shopper pays at once.");
  });
});
