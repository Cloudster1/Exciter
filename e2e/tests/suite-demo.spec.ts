import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { withDatabase } from "../fixtures/db";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible } from "../fixtures/shell";
import { armatureURL } from "../fixtures/stack";

const INSTALLATION_PATH = "/admin/installation";
const DEMO_KEYS = ["ARM", "EXC", "STA", "ROT"];
const BASELINE = "Demo baseline";
// The stub's EXC project holds Exciter's issue numbers for its demo person alone (#270), in a tenant of this spec's.
const TENANT = "e2e-suite-demo";
const DEMO_TOKEN = `armature_pat_${TENANT}_demo`;
// The stack's worker builds the four specifications and their test runs a step at a time beside every other spec's work.
const BUILD_TIMEOUT_MS = 8 * 60_000;
// Linking again asks Armature and Stator about every requirement once more, a step at a time.
const RELINK_TIMEOUT_MS = 3 * 60_000;
const SPEC_TIMEOUT_MS = BUILD_TIMEOUT_MS + RELINK_TIMEOUT_MS + 60_000;

// The demo is the installation's one, so the spec runs alone; one it added is taken away again, one it found stays.
async function takeAway(api: ExciterApi) {
  for (const projectKey of DEMO_KEYS) {
    const path = { params: { path: { projectKey } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (found.data) {
      if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
      await api.DELETE("/projects/{projectKey}", path);
    }
  }
  // No endpoint takes the demo away: it is added once, and an installation that wants it gone deletes its projects.
  await withDatabase((db) => db.query("DELETE FROM suite_demo"));
}

/** The key of a requirement of the project the query names, as the demo filed it; the content is written by hand. */
async function demoRequirement(sql: string): Promise<{ key: string; issue: string }> {
  const { rows } = await withDatabase((db) => db.query<{ key: string; issue: string }>(sql));
  expect(rows.length, "the demo filed such a requirement").toBeGreaterThan(0);
  return rows[0]!;
}

test.describe("the suite demo", { tag: "@desktop" }, () => {
  let addedHere = false;
  // Armature is the stub, so linking the demo puts its requirements on the stub's issues.
  test.beforeEach(async ({ api }) => {
    await fetch(`${armatureURL()}/_stub/${TENANT}`, { method: "DELETE" });
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL() } }));
    must(await api.PUT("/armature/account/token", { body: { token: DEMO_TOKEN } }));
  });
  test.afterEach(async ({ api }) => {
    if (addedHere) await takeAway(api);
    await api.DELETE("/armature/connection");
  });

  test("an administrator adds it, watches it built, and opens one of its projects", async ({ page, api }) => {
    test.setTimeout(SPEC_TIMEOUT_MS);
    const before = must(await api.GET("/installation/suite-demo")).demo;
    await page.goto(INSTALLATION_PATH);
    const card = page.locator("[data-suite-demo]");
    if (!before) {
      await page.locator('[data-action="add-suite-demo"]').click();
      await expect(page.getByRole("dialog", { name: "Add the suite demo?" })).toBeVisible();
      await expectAccessible(page);
      await page.locator('[data-action="confirm-add-suite-demo"]').click();
      addedHere = true;
      await expect(card.locator("[data-suite-demo-status]")).toContainText("The demo is being built");
    }
    await expect(card).toHaveAttribute("data-suite-demo", "done", { timeout: BUILD_TIMEOUT_MS });
    await expect(card.locator("[data-suite-demo-status]")).toContainText("The demo holds");
    await expect(page.locator('[data-action="add-suite-demo"]')).toHaveCount(0);
    await expectAccessible(page);

    await card.getByRole("link", { name: "EXC", exact: true }).click();
    await expect(page).toHaveURL(/\/projects\/EXC$/);
    for (const section of ["Introduction", "Functional requirements"]) {
      await expect(page.getByRole("link", { name: section, exact: true })).toBeVisible();
    }
    const baselines = must(await api.GET("/projects/{projectKey}/baselines", { params: { path: { projectKey: "EXC" } } })).baselines;
    expect(baselines.map((b) => b.name)).toContain(BASELINE);

    // Linked again with nothing new to reach, the demo runs to done and holds what it held.
    const built = must(await api.GET("/installation/suite-demo")).demo!;
    await page.goto(INSTALLATION_PATH);
    await page.locator('[data-action="link-suite-demo"]').click();
    // Asking queues a run of the links, which the card shows at once and follows until the worker is done.
    await expect(card).toHaveAttribute("data-suite-demo", /^(queued|running)$/);
    await expect(card.locator("[data-suite-demo-status]")).toHaveText("The demo is being linked to Armature and Stator.");
    await expect(card).toHaveAttribute("data-suite-demo", "done", { timeout: RELINK_TIMEOUT_MS });
    const relinked = must(await api.GET("/installation/suite-demo")).demo!;
    expect(relinked).toMatchObject({ state: "done", kind: "links", requirements: built.requirements, links: built.links });
    expect(relinked.finishedAt).not.toBe(built.finishedAt);
    await expect(card.locator("[data-suite-demo-status]")).toContainText("The demo holds");

    // A requirement implements the issue that built it, which the side panel shows from Armature.
    const implementing = await demoRequirement(`SELECT p.key || '-R' || r.number AS key, l.issue_key AS issue
      FROM armature_issue_link l JOIN requirement r ON r.id = l.requirement_id JOIN project p ON p.id = r.project_id
      WHERE p.key = 'EXC' AND l.relation = 'implements' ORDER BY r.number LIMIT 1`);
    await page.goto(`/projects/EXC/r/${implementing.key}`);
    const issues = page.getByRole("region", { name: "Armature issues" });
    await expect(issues.locator(`[data-armature-link="${implementing.issue}"]`)).toBeVisible();
    await expectAccessible(page);

    // The coverage counts the test cases that verify a requirement, which the demo's test run ran.
    const verified = await demoRequirement(`SELECT p.key || '-R' || r.number AS key, '' AS issue
      FROM trace_link l JOIN link_type t ON t.id = l.type_id JOIN requirement r ON r.id = l.target_id JOIN project p ON p.id = r.project_id
      WHERE p.key = 'EXC' AND t.key = 'verifies' AND r.trashed_at IS NULL ORDER BY r.number LIMIT 1`);
    await page.goto("/projects/EXC/coverage");
    await expect(page.locator(`[data-coverage-row="${verified.key}"]`)).toContainText("Verified");
    await expectAccessible(page);
  });
});
