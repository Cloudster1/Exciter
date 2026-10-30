import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { withDatabase } from "../fixtures/db";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible } from "../fixtures/shell";

const INSTALLATION_PATH = "/admin/installation";
const DEMO_KEYS = ["ARM", "EXC", "STA"];
// The stack's worker builds the whole demo, about 1,500 requirements, a step at a time beside every other spec's work.
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

test.describe("the suite demo", { tag: "@desktop" }, () => {
  let addedHere = false;
  test.afterEach(async ({ api }) => {
    if (addedHere) await takeAway(api);
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
    expect(baselines.map((b) => b.name)).toContain(`Snapshot ${must(await api.GET("/installation/suite-demo")).demo!.taken}`);

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
  });
});
