import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// A long history read a page at a time (#309): the pages of revisions, one revision as it was, two chosen
// revisions compared, and the older activity loaded on asking.

const KEY = uniqueKey("HPG");
/** One more revision than a page of the history holds, and one more again, so the second page holds two. */
const REVISIONS = 22;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

/** Writes a requirement and revises it until it has `revisions`, each saying its own number. */
async function revised(api: ExciterApi, revisions: number) {
  const made = must(
    await api.POST("/projects/{projectKey}/requirements", {
      params: { path: { projectKey: KEY } },
      body: { title: "Pay at revision 1", statement: paragraph("The shopper pays at revision 1.") },
    }),
  ).requirement;
  for (let n = 2; n <= revisions; n++) {
    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: made.key } },
        body: { title: `Pay at revision ${n}`, statement: paragraph(`The shopper pays at revision ${n}.`), baseRevision: n - 1 },
      }),
    );
  }
  return made.key;
}

/** Each test walks several pages, some as two people, which outlasts the default budget on a busy machine. */
const FLOW_TIMEOUT_MS = 60_000;

test.describe("history pages", { tag: "@desktop" }, () => {
  test.describe.configure({ timeout: FLOW_TIMEOUT_MS });
  test.afterEach(async ({ api }) => removeProject(api));

  test("a reader walks the pages of a long history, opens one revision and loads the older activity", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Long history" } }));
    const key = await revised(api, REVISIONS);
    const history = `/projects/${KEY}/r/${key}/history`;

    await page.goto(history);
    await expect(page.locator(`[data-revision-row="${REVISIONS}"]`)).toBeVisible();
    await expect(page.locator("[data-revision-row]")).toHaveCount(20);
    await expect(page.locator('[data-revision-row="2"]')).toHaveCount(0);
    await page.getByRole("button", { name: "Next" }).click();
    await expect(page).toHaveURL(/[?&]offset=20/);
    await expect(page.locator("[data-revision-row]")).toHaveCount(2);
    await expect(page.locator('[data-revision-row="1"]')).toBeVisible();
    await expect(page.getByRole("button", { name: "Next" })).toBeDisabled();
    await expectAccessible(page);

    await page.reload();
    await expect(page.locator('[data-revision-row="1"]')).toBeVisible();
    await page.locator('[data-revision-row="1"] [data-action="view-revision"]').click();
    await expect(page).toHaveURL(/[?&]revision=1/);
    const revision = page.locator('[data-revision="1"]');
    await expect(revision.getByRole("heading", { level: 2 })).toHaveText("Pay at revision 1");
    await expect(revision).toContainText("The shopper pays at revision 1.");
    await expectAccessible(page);

    await page.getByRole("button", { name: "Back to the history" }).click();
    await expect(page.locator(`[data-revision-row="${REVISIONS}"]`)).toBeVisible();
    await page.getByRole("button", { name: "Next" }).click();
    await page.getByRole("button", { name: "Previous" }).click();
    await expect(page.locator(`[data-revision-row="${REVISIONS}"]`)).toBeVisible();
    await expect(page.getByRole("button", { name: "Previous" })).toBeDisabled();

    await page.getByRole("tab", { name: "Activity" }).click();
    const entries = page.locator('[data-activity-entry="revision"]');
    await expect(entries).toHaveCount(20);
    await expect(entries.first()).toContainText(`wrote revision ${REVISIONS}`);
    await page.getByRole("button", { name: "Show more" }).click();
    await expect(entries).toHaveCount(REVISIONS);
    await expect(entries.last()).toContainText("wrote revision 1");
    await expect(page.getByRole("button", { name: "Show more" })).toHaveCount(0);
  });

  test("a reader compares the two revisions they select", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Long history" } }));
    const key = await revised(api, 3);

    await page.goto(`/projects/${KEY}/r/${key}/history`);
    const compare = page.locator('[data-action="compare-selected"]');
    await expect(compare).toBeDisabled();
    await page.getByRole("checkbox", { name: "Select Revision 1 to compare" }).check();
    await expect(compare).toBeDisabled();
    await page.getByRole("checkbox", { name: "Select Revision 3 to compare" }).check();
    await compare.click();
    await expect(page).toHaveURL(/[?&]from=1&to=3/);
    await expect(page.locator('[data-compare-side="1"]')).toContainText("Pay at revision 1");
    await expect(page.locator('[data-compare-side="3"]')).toContainText("Pay at revision 3");
    const statement = page.locator('[data-diff-of="statement"]');
    await expect(statement.locator('del[data-diff="delete"]')).toContainText("1");
    await expect(statement.locator('ins[data-diff="insert"]')).toContainText("3");
    await expectAccessible(page);
  });
});
