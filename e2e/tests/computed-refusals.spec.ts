import pg from "pg";
import type { Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { superuserURL } from "../fixtures/stack";

// Computed attributes (#319): a formula naming nobody or reading itself round is refused, an attribute a formula
// reads stays, and a changed formula is worked out again while the page counts, then the grid sorts by it.

const KEY = uniqueKey("CMPR");
// The worker works a value out a moment after a change; this is how long the page is given to show it.
const WORKED_OUT_MS = 30_000;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

async function openAttributes(page: Page) {
  await page.goto(`/p/${KEY}/settings`);
  await page.getByRole("tab", { name: "Attributes" }).click();
}

/** Holds one requirement's computed values, so the worker's batch over the project waits until released and the
 *  page's progress can be read before it ends. Arranging only: what the spec checks goes through the app. */
async function holdComputed(requirementId: string): Promise<() => Promise<void>> {
  const client = new pg.Client({ connectionString: superuserURL() });
  await client.connect();
  await client.query("BEGIN");
  const held = await client.query("SELECT 1 FROM requirement_computed WHERE requirement_id = $1 FOR UPDATE", [requirementId]);
  if (held.rowCount !== 1) throw new Error(`${requirementId} has no computed values to hold. Was it worked out?`);
  return async () => {
    await client.query("ROLLBACK");
    await client.end();
  };
}

async function rowKeys(page: Page): Promise<string[]> {
  return page.locator("[data-grid-row]").evaluateAll((rows) => rows.map((row) => row.getAttribute("data-grid-row") ?? ""));
}

test.describe("computed attributes refused and recomputed", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice is refused a formula that names nobody or reads itself round, and deleting what a formula reads", async ({ page, api }) => {
    const path = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Refused" } }));
    must(await api.POST("/projects/{projectKey}/attributes", { ...path, body: { name: "Cost", kind: "number" } }));
    must(await api.POST("/projects/{projectKey}/attributes", { ...path, body: { name: "Score", formula: `"Cost" * 2` } }));
    must(await api.POST("/projects/{projectKey}/attributes", { ...path, body: { name: "Rank", formula: `"Score" + 1` } }));

    await openAttributes(page);
    await page.locator('[data-action="new-attribute"]').click();
    const form = page.locator("[data-attribute-form]");
    await form.getByLabel("Name", { exact: true }).fill("Broken");
    await form.getByLabel("Work it out from a formula").check();
    await form.getByLabel("Formula", { exact: true }).fill(`"Weight" * 2`);
    await expect(form.locator("[data-formula-mistake]")).toContainText(`There is no attribute "Weight" in this project.`);
    await form.locator('[data-action="save-attribute"]').click();
    await expect(form.locator('p[id$="-error"]')).toHaveText(`There is no attribute "Weight" in this project. Name one of its attributes in double quotes.`);
    await expectAccessible(page);
    await expect(form).toBeVisible();
    await expect(page.locator('[data-attribute="Broken"]')).toHaveCount(0);
    await form.getByRole("button", { name: "Cancel" }).click();

    const score = page.locator('[data-attribute="Score"]');
    await score.locator('[data-action="edit-attribute"]').click();
    const editing = score.locator("[data-attribute-form]");
    await editing.getByLabel("Formula", { exact: true }).fill(`"Rank" * 2`);
    await editing.locator('[data-action="save-attribute"]').click();
    await expect(editing.locator('p[id$="-error"]')).toHaveText(
      /^(Score reads Rank, which reads Score|Rank reads Score, which reads Rank)\. Take one of them out of the other's formula, or read it of the children\.$/,
    );
    await editing.getByRole("button", { name: "Cancel" }).click();
    await expect(score.locator("code")).toHaveText(`"Cost" * 2`);

    const cost = page.locator('[data-attribute="Cost"]');
    await cost.locator('[data-action="delete-attribute"]').click();
    await expect(
      page.locator("[data-attribute-list]").getByText("The formula of Score reads this attribute. Take it out of that formula first."),
    ).toBeVisible();
    await openAttributes(page);
    await expect(page.locator('[data-attribute="Cost"]')).toBeVisible();
    const kept = must(await api.GET("/projects/{projectKey}/attributes", path)).attributes.map((a) => a.name);
    expect(kept).toEqual(expect.arrayContaining(["Cost", "Score", "Rank"]));
    expect(kept).not.toContain("Broken");
  });

  test("alice changes a formula, watches it worked out again, and the grid sorts by the new values", async ({ page, api }) => {
    test.setTimeout(WORKED_OUT_MS * 2);
    const path = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Recomputed" } }));
    const weight = must(await api.POST("/projects/{projectKey}/attributes", { ...path, body: { name: "Weight", kind: "number" } })).attribute;
    const score = must(await api.POST("/projects/{projectKey}/attributes", { ...path, body: { name: "Score", formula: `"Weight"` } })).attribute;
    const made = [];
    for (const [title, w] of [
      ["Light", 1],
      ["Middling", 2],
      ["Heavy", 3],
    ] as const) {
      made.push(must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title, attributes: { [weight.id]: w } } })).requirement);
    }
    const scoreOf = async (key: string) =>
      (must(await api.GET("/requirements/{reqKey}", { params: { path: { reqKey: key } } })).requirement.computed as Record<string, unknown>)[score.id];
    for (const [i, r] of made.entries()) {
      await expect.poll(() => scoreOf(r.key), { timeout: WORKED_OUT_MS }).toBe(i + 1);
    }

    await openAttributes(page);
    const row = page.locator('[data-attribute="Score"]');
    await row.locator('[data-action="edit-attribute"]').click();
    await row.getByLabel("Formula", { exact: true }).fill(`10 - "Weight"`);
    const release = await holdComputed(made[0]!.id);
    try {
      await row.locator('[data-action="save-attribute"]').click();
      const progress = page.locator("[data-recompute]");
      await expect(progress).toHaveText("Working the computed attributes out again: 0 of 3 requirements.");
      await expect(progress).toHaveAttribute("data-recompute", "0/3");
      await expectAccessible(page);
    } finally {
      await release();
    }
    await expect(page.locator("[data-recompute]")).toHaveCount(0, { timeout: WORKED_OUT_MS });
    await expect(page.locator('[data-attribute="Score"] code')).toHaveText(`10 - "Weight"`);

    await page.goto(`/p/${KEY}/grid?columns=key&columns=title&columns=${score.id}`);
    await page.locator(`[data-grid-column="${score.id}"] [data-action="sort-column"]`).click();
    await expect(page).toHaveURL(new RegExp(`sort=${score.id}`));
    await expect.poll(() => rowKeys(page)).toEqual([made[2]!.key, made[1]!.key, made[0]!.key]);
    await expect(page.locator(`[data-grid-row="${made[2]!.key}"]`)).toContainText("7");
    await expect(page.locator(`[data-grid-row="${made[0]!.key}"]`)).toContainText("9");
  });
});
