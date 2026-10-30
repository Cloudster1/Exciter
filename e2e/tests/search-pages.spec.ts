import type { Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { withDatabase } from "../fixtures/db";
import type { Person } from "../fixtures/people";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("SRPG");
const project = { params: { path: { projectKey: KEY } } };
/** A word no other spec writes, so a search for it finds this spec's requirements alone. */
const WORD = `lumen${KEY.toLowerCase()}`;
const HITS = 25;
/** Requirements 1 to OLD were last changed days ago, a minute apart, past the trigger that stamps a change. */
const OLD = 5;
/** Requirements after OLD up to OWNED are alice's. */
const OWNED = 10;

async function removeProject(api: ExciterApi) {
  const found = await api.GET("/projects/{projectKey}", project);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", project);
  await api.DELETE("/projects/{projectKey}", project);
}

const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

/** Twenty-five requirements that share the word: the first has it in its title too, so it is the best match. */
async function makeProject(api: ExciterApi, alice: Person) {
  must(await api.POST("/projects", { body: { key: KEY, name: "Search pages" } }));
  for (let n = 1; n <= HITS; n++) {
    const title = n === 1 ? `The ${WORD} lamp` : `Lamp ${n}`;
    const owned = n > OLD && n <= OWNED ? { ownerId: alice.id } : {};
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title, statement: paragraph(`It holds a ${WORD}.`), ...owned } }));
  }
  await withDatabase(async (db) => {
    await db.query("BEGIN");
    await db.query("SET LOCAL session_replication_role = replica");
    await db.query(
      `UPDATE requirement SET updated_at = now() - interval '10 days' + number * interval '1 minute'
       WHERE number <= $2 AND project_id = (SELECT id FROM project WHERE key = $1)`,
      [KEY, OLD],
    );
    await db.query("COMMIT");
  });
}

const count = (page: Page) => page.locator("[data-search-count]");
const firstHit = (page: Page) => page.locator("[data-search-hit]").first();

// The project's key is this worker's own, so it meets no other spec.
test.describe("the search's filters and pages", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice narrows by owner and day, orders by the latest change, clears the filters and turns the pages", async ({ page, api, people }) => {
    await makeProject(api, people.alice);
    const today = new Date().toISOString().slice(0, 10);
    await page.goto("/search");
    await page.locator("[data-search-query]").fill(WORD);
    await page.locator('[data-action="run-search"]').click();
    await expect(page).toHaveURL(new RegExp(`q=${WORD}`));
    await expect(count(page)).toHaveText(`1 to 20 of ${HITS} results`);
    await expect(firstHit(page)).toHaveAttribute("data-search-hit", `${KEY}-R1`);

    await page.locator('[data-filter="owner"]').selectOption({ label: `You (${people.alice.name})` });
    await expect(page).toHaveURL(new RegExp(`owner=${people.alice.id}`));
    await expect(count(page)).toHaveText(`${OWNED - OLD} results`);
    await page.locator('[data-filter="owner"]').selectOption({ label: "Anyone" });

    await page.locator('[data-filter="updated-after"]').fill(today);
    await expect(page).toHaveURL(new RegExp(`updatedAfter=${today}`));
    await expect(count(page)).toHaveText(`${HITS - OLD} results`);
    await page.locator('[data-filter="updated-after"]').fill("");
    await page.locator('[data-filter="updated-before"]').fill(today);
    await expect(page).toHaveURL(new RegExp(`updatedBefore=${today}`));
    await expect(count(page)).toHaveText(`${OLD} results`);

    await page.locator('[data-filter="sort"]').selectOption({ label: "Latest change first" });
    await expect(page).toHaveURL(/sort=updated/);
    await expect(firstHit(page)).toHaveAttribute("data-search-hit", `${KEY}-R${OLD}`);
    await expectAccessible(page);

    await page.locator('[data-action="clear-filters"]').click();
    await expect(page).not.toHaveURL(/updatedBefore/);
    await expect(page).toHaveURL(new RegExp(`q=${WORD}.*sort=updated|sort=updated.*q=${WORD}`));
    await expect(count(page)).toHaveText(`1 to 20 of ${HITS} results`);
    await expect(firstHit(page)).toHaveAttribute("data-search-hit", `${KEY}-R${HITS}`);
    await expect(page.locator('[data-action="clear-filters"]')).toHaveCount(0);

    await page.locator('[data-action="next-results"]').click();
    await expect(page).toHaveURL(/page=2/);
    await expect(count(page)).toHaveText(`21 to ${HITS} of ${HITS} results`);
    await expect(page.locator("[data-search-hit]")).toHaveCount(HITS - 20);
    await page.locator('[data-action="previous-results"]').click();
    await expect(page).not.toHaveURL(/page=/);
    await expect(count(page)).toHaveText(`1 to 20 of ${HITS} results`);
    await page.locator('[data-action="next-results"]').click();
    await expect(count(page)).toHaveText(`21 to ${HITS} of ${HITS} results`);
    await page.goBack();
    await expect(page).not.toHaveURL(/page=/);
    await expect(count(page)).toHaveText(`1 to 20 of ${HITS} results`);
  });

  test("alice takes a query from the help, runs it, and completes a field with Tab", async ({ page, api, people }) => {
    await makeProject(api, people.alice);
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { kind: "section", title: "Lamps" } }));
    await page.goto(`/projects/${KEY}/grid`);
    await expect(page.locator("[data-grid-count]")).toHaveText(`${HITS + 1} requirements`);
    await page.locator('[data-mode="query"]').click();
    await page.locator('[data-action="query-help"]').click();
    const help = page.getByRole("dialog", { name: "What a query can say" });
    await expectAccessible(page);
    await help.getByRole("button", { name: "kind = requirement AND verifiedBy IS EMPTY" }).click();
    await expect(help).toHaveCount(0);
    const box = page.getByRole("combobox", { name: "Query" });
    await expect(box).toHaveValue("kind = requirement AND verifiedBy IS EMPTY");
    await page.locator('[data-action="run-query"]').click();
    await expect(page).toHaveURL(/query=kind/);
    await expect(page.locator("[data-grid-count]")).toHaveText(`${HITS} requirements`);
    await expect(page.locator(`[data-grid-row="${KEY}-R${HITS + 1}"]`)).toHaveCount(0);

    await box.fill("");
    await box.pressSequentially("prio");
    await expect(page.locator('[data-suggestion="completion"][data-suggestion-text="priority"]')).toBeVisible();
    await box.press("Tab");
    await expect(box).toHaveValue("priority ");
    await expect(box).toBeFocused();
  });
});
