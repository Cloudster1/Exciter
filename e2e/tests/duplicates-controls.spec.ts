import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, toast, dismissToasts } from "../fixtures/shell";

// Duplicates decided in the browser (#320): a pair linked and one merged with its links moved, the pairs narrowed,
// paged and read again after a side changed elsewhere, and the threshold that decides which pairs are listed.

const KEY = uniqueKey("DCTL");
// Low enough that titles a word apart are listed however the trigrams of the database weigh them.
const THRESHOLD = 0.4;
/** As many alike titles as make more pairs than a page of web/src/config.ts DUPLICATES_PAGE_SIZE holds. */
const ALIKE = 7;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

const inProject = { params: { path: { projectKey: KEY } } };

async function project(api: ExciterApi, threshold = THRESHOLD) {
  must(await api.POST("/projects", { body: { key: KEY, name: "Said twice" } }));
  must(await api.PUT("/projects/{projectKey}/duplicate-settings", { ...inProject, body: { threshold } }));
}

async function write(api: ExciterApi, title: string, parentId: string | null = null) {
  return must(await api.POST("/projects/{projectKey}/requirements", { ...inProject, body: { title, place: { parentId, afterId: null } } })).requirement;
}

/** Each test walks several pages, which outlasts the default budget on a busy machine. */
const FLOW_TIMEOUT_MS = 90_000;

test.describe("duplicates decided", { tag: "@desktop" }, () => {
  test.describe.configure({ timeout: FLOW_TIMEOUT_MS });
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice links one pair and merges another, whose links move to the requirement kept", async ({ page, api }) => {
    await project(api);
    const short = await write(api, "Brakes engage within 100 ms");
    const long = await write(api, "Brakes engage within 100 milliseconds");
    const lamp = await write(api, "Headlamps dim at dusk");
    const lamps = await write(api, "Headlamps dim at dusk automatically");
    const gauge = await write(api, "Dashboard shows the light level");
    must(
      await api.POST("/requirements/{reqKey}/links", {
        params: { path: { reqKey: gauge.key } },
        body: { typeKey: "derives", otherKey: lamps.key },
      }),
    );

    await page.goto(`/projects/${KEY}/duplicates`);
    const brakes = page.locator("[data-pair]", { has: page.locator(`[data-pair-side="${short.key}"]`) });
    await expect(brakes.locator(`[data-pair-side="${long.key}"]`)).toBeVisible();
    await brakes.locator('[data-action="link-pair"]').click();
    const linking = page.getByRole("dialog", { name: "Link the two" });
    const way = linking.locator("[data-link-way]");
    const first = (await brakes.getAttribute("data-pair"))!.split(" ")[0]!;
    const second = first === short.key ? long.key : short.key;
    await way.selectOption({ label: `${first} relates to ${second}` });
    await expectAccessible(page);
    await linking.locator('[data-action="confirm-link-pair"]').click();
    await expect(linking).toBeHidden();
    await expect(toast(page, `${first} and ${second} are linked.`)).toHaveText(`${first} and ${second} are linked.`);
    await dismissToasts(page);
    await expect(brakes).toHaveCount(0);

    const headlamps = page.locator("[data-pair]", { has: page.locator(`[data-pair-side="${lamp.key}"]`) });
    await headlamps.locator('[data-action="merge-pair"]').click();
    const merging = page.getByRole("dialog", { name: "Merge the two" });
    await merging.locator(`[data-keep="${lamp.key}"]`).click();
    await expect(merging.locator(`[data-merge-trashes="${lamps.key}"]`)).toBeVisible();
    await merging.getByRole("textbox", { name: "Comment" }).fill("Dusk is when they dim either way.");
    await merging.locator('[data-action="confirm-merge-pair"]').click();
    const result = page.locator("[data-merge-result]");
    await expect(result.locator(`[data-moved-link="${gauge.key}"]`)).toContainText("Made again on the one kept.");
    await expectAccessible(page);
    await result.locator('[data-action="close-merge"]').click();
    await expect(result).toBeHidden();
    await expect(toast(page, `${lamps.key} is in the trash, and ${lamp.key} is kept.`)).toHaveText(`${lamps.key} is in the trash, and ${lamp.key} is kept.`);
    await dismissToasts(page);
    await expect(page.locator("[data-pair]")).toHaveCount(0);

    const links = page.locator("[data-requirement-links]");
    await page.goto(`/projects/${KEY}/r/${short.key}`);
    await expect(links.locator(`[data-link="${long.key}"]`)).toBeVisible();
    await page.goto(`/projects/${KEY}/r/${long.key}`);
    await expect(links.locator(`[data-link="${short.key}"]`)).toBeVisible();
    await page.goto(`/projects/${KEY}/r/${lamp.key}`);
    await expect(links.locator(`[data-link-group="derives:incoming"] [data-link="${gauge.key}"]`)).toBeVisible();
    const trashed = must(await api.GET("/requirements/{reqKey}", { params: { path: { reqKey: lamps.key } } })).requirement;
    expect(trashed.trashedAt).toBeTruthy();
  });

  test("alice narrows the pairs, reads the next page, and reads them again after another tab changed a side", async ({ page, api }) => {
    await project(api);
    for (let i = 1; i <= ALIKE; i++) await write(api, `Brakes engage within 100 ms on wet road number ${i}`);
    const lighting = must(await api.POST("/projects/{projectKey}/requirements", { ...inProject, body: { kind: "section", title: "Lighting" } })).requirement;
    const lamp = await write(api, "Headlamps dim at dusk", lighting.id);
    const lamps = await write(api, "Headlamps dim at dusk automatically", lighting.id);

    await page.goto(`/projects/${KEY}/duplicates`);
    const pairs = page.locator("[data-pair]");
    const more = page.locator('[data-action="more-pairs"]');
    await expect(more).toBeVisible();
    const firstPage = await pairs.count();
    await more.click();
    await expect.poll(() => pairs.count()).toBeGreaterThan(firstPage);
    await expect(more).toBeHidden();
    const all = await pairs.count();
    expect(new Set(await pairs.evaluateAll((each) => each.map((pair) => pair.getAttribute("data-pair")))).size).toBe(all);

    const filters = page.locator("[data-duplicates-filters]");
    await filters.getByLabel("Section").fill(lighting.key);
    await filters.locator('[data-action="filter-pairs"]').click();
    await expect(pairs).toHaveCount(1);
    await expect(pairs.locator(`[data-pair-side="${lamp.key}"]`)).toBeVisible();
    await expect(pairs.locator(`[data-pair-side="${lamps.key}"]`)).toBeVisible();
    await expectAccessible(page);
    await filters.getByLabel("Section").fill("");
    await filters.locator('[data-action="filter-pairs"]').click();
    await page.getByRole("combobox", { name: "Query either side matches" }).fill('title ~ "Headlamps"');
    await page.getByRole("combobox", { name: "Query either side matches" }).press("Enter");
    await expect(pairs).toHaveCount(1);
    await expect(pairs.locator(`[data-pair-side="${lamp.key}"]`)).toBeVisible();

    const other = await page.context().newPage();
    try {
      await other.goto(`/projects/${KEY}/r/${lamp.key}`);
      await other.locator('[data-action="edit-requirement"]').click();
      await other.getByLabel("Title", { exact: true }).fill("Headlamps dim at dusk on their own");
      await other.locator('[data-action="save-requirement"]').click();
      await expect(other.getByText("Saved as revision 2.")).toBeVisible();
    } finally {
      await other.close();
    }

    await pairs.locator('[data-action="link-pair"]').click();
    const linking = page.getByRole("dialog", { name: "Link the two" });
    await linking.locator('[data-action="confirm-link-pair"]').click();
    const stale = linking.locator("[data-stale-pair]");
    await expect(stale).toContainText("One of the two changed since the pairs were read.");
    await expectAccessible(page);
    await stale.locator('[data-action="read-pairs-again"]').click();
    await expect(linking).toBeHidden();
    await expect(pairs.locator(`[data-pair-side="${lamp.key}"]`)).toContainText("Headlamps dim at dusk on their own");
    await expect(pairs.locator(`[data-pair-side="${lamp.key}"]`)).toContainText("Revision 2");
  });

  test("alice raises the threshold in the quality settings, and the pair is no longer listed", async ({ page, api }) => {
    await project(api);
    const short = await write(api, "Brakes engage within 100 ms");
    await write(api, "Brakes engage within 100 milliseconds");
    await page.goto(`/projects/${KEY}/duplicates`);
    await expect(page.locator("[data-pair]", { has: page.locator(`[data-pair-side="${short.key}"]`) })).toBeVisible();
    await expect(page.locator("[data-duplicates-threshold]")).toHaveText("Listed from 40% alike, as the project's quality settings set it.");

    await page.goto(`/projects/${KEY}/settings/quality`);
    const card = page.getByRole("form", { name: "Duplicates" });
    const threshold = card.getByRole("spinbutton", { name: "Similarity threshold in percent" });
    await expect(threshold).toHaveValue("40");
    await threshold.fill("95");
    await card.locator('[data-action="save-duplicate-settings"]').click();
    await expect(toast(page, "The threshold is saved.")).toBeVisible();
    await dismissToasts(page);
    await expectAccessible(page);

    await page.goto(`/projects/${KEY}/duplicates`);
    await expect(page.locator("[data-duplicates-threshold]")).toHaveText("Listed from 95% alike, as the project's quality settings set it.");
    await expect(page.locator("[data-pair]")).toHaveCount(0);
    await expect(page.getByText("No likely duplicates.")).toBeVisible();
  });
});
