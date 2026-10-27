import { readFile } from "node:fs/promises";
import type { Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { passingTestName, uploadPassingResult } from "../fixtures/evidence";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// The trace graph's controls and its ways out (#314): alice narrows the graph by direction, depth, link types and
// relations, keeps it through a reload and in the file, and moves from node to node by keyboard and by the list.

const KEY = uniqueKey("GRCT");
// Enough to pass the zoom buttons and every node of a small graph before giving up.
const MOST_TABS = 12;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

/** R2 derives from R1, R3 verifies R2 and R4 derives from R3. */
async function chain(api: ExciterApi) {
  const projectKey = { params: { path: { projectKey: KEY } } };
  must(await api.POST("/projects", { body: { key: KEY, name: "Graph controls" } }));
  for (const title of ["Stop within 40 m", "Brakes engage within 100 ms", "Brake bench run", "Brake bench run on ice"]) {
    must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title } }));
  }
  for (const [from, typeKey, to] of [
    ["R2", "derives", "R1"],
    ["R3", "verifies", "R2"],
    ["R4", "derives", "R3"],
  ] as const) {
    must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: `${KEY}-${from}` } }, body: { typeKey, otherKey: `${KEY}-${to}` } }));
  }
}

/** What the impact list names, nearest first. */
async function impact(page: Page): Promise<string[]> {
  return page.locator("[data-impact-item]").evaluateAll((items) => items.map((item) => item.getAttribute("data-impact-item") ?? ""));
}

/** A list the page keeps in its address, which holds it as JSON. */
function searched(page: Page, name: string): string[] | undefined {
  const raw = new URL(page.url()).searchParams.get(name);
  return raw === null ? undefined : (JSON.parse(raw) as string[]);
}

test.describe("trace graph controls", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice narrows the graph by direction, depth, link types and relations, and keeps it", async ({ page, api }) => {
    await chain(api);
    await uploadPassingResult(api, KEY, `${KEY}-R2`);
    const result = passingTestName(`${KEY}-R2`);
    const [r1, r3, r4] = [`${KEY}-R1`, `${KEY}-R3`, `${KEY}-R4`];

    await page.goto(`/p/${KEY}/r/${KEY}-R2/trace`);
    const nodes = page.locator("[data-graph-canvas] [data-graph-node]");
    await expect(nodes).toHaveCount(5);
    await expect.poll(() => impact(page)).toEqual([r1, r3, result, r4]);

    await page.getByLabel("Direction").selectOption({ label: "Up" });
    await expect(page).toHaveURL(/direction=up/);
    await expect(nodes).toHaveCount(2);
    await expect.poll(() => impact(page)).toEqual([r1]);
    await page.reload();
    await expect(page.getByLabel("Direction")).toHaveValue("up");
    await expect(nodes).toHaveCount(2);

    await page.getByLabel("Direction").selectOption({ label: "Up and down" });
    await page.getByLabel("Depth").selectOption({ label: "1 level" });
    await expect(page).toHaveURL(/depth=1/);
    await expect(page).not.toHaveURL(/direction=/);
    await expect(nodes).toHaveCount(4);
    await expect.poll(() => impact(page)).toEqual([r1, r3, result]);

    const types = page.locator("[data-graph-types]");
    await types.locator("summary").click();
    await types.getByRole("checkbox", { name: "Verifies" }).uncheck();
    await expect.poll(() => searched(page, "type")).toEqual(expect.arrayContaining(["derives"]));
    expect(searched(page, "type")).not.toContain("verifies");
    await expect(nodes).toHaveCount(3);
    await expect.poll(() => impact(page)).toEqual([r1, result]);
    await types.locator("summary").click();

    const relations = page.locator("[data-graph-relations]");
    await relations.locator("summary").click();
    await relations.getByRole("checkbox", { name: "Verifies" }).uncheck();
    await expect.poll(() => searched(page, "relation")).toEqual(expect.arrayContaining(["implements"]));
    expect(searched(page, "relation")).not.toContain("verifies");
    await expect(nodes).toHaveCount(2);
    await expect.poll(() => impact(page)).toEqual([r1]);
    await relations.locator("summary").click();
    await expectAccessible(page);

    await page.reload();
    await expect(page.getByLabel("Depth")).toHaveValue("1");
    await expect(nodes).toHaveCount(2);
    await expect.poll(() => impact(page)).toEqual([r1]);

    const download = page.waitForEvent("download");
    await page.getByRole("link", { name: "Download as CSV" }).click();
    // trim drops the byte order mark the file starts with too.
    const file = (await readFile((await (await download).path()) ?? "", "utf8")).trim();
    const rows = file.split("\n");
    expect(rows).toHaveLength(3);
    expect(rows[1]).toContain(`${KEY}-R2`);
    expect(rows[2]).toContain(r1);
    expect(file).not.toContain(r3);
    expect(file).not.toContain(result);
  });

  test("alice chooses a node by keyboard, opens it, centres the list elsewhere and goes back to the requirement", async ({ page, api }) => {
    await chain(api);
    await page.goto(`/p/${KEY}/r/${KEY}-R2`);
    await page.getByRole("link", { name: "Trace graph" }).click();
    await expect(page.getByRole("heading", { level: 1, name: `Trace graph of ${KEY}-R2` })).toBeVisible();
    await expect(page.locator("[data-graph-canvas] [data-graph-node]")).toHaveCount(4);

    await page.getByRole("button", { name: "Fit the graph" }).focus();
    for (let tabs = 0; tabs < MOST_TABS; tabs++) {
      await page.keyboard.press("Tab");
      const label = await page.evaluate(() => document.activeElement?.getAttribute("aria-label") ?? "");
      if (label.startsWith(`Requirement, ${KEY}-R1,`)) break;
    }
    await page.keyboard.press("Enter");
    const chosen = page.locator(`[data-graph-chosen="${KEY}-R1"]`);
    await expect(chosen).toContainText("Stop within 40 m");
    await expectAccessible(page);

    await chosen.locator('[data-action="graph-open"]').click();
    await expect(page).toHaveURL(new RegExp(`/p/${KEY}/r/${KEY}-R1$`));
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Stop within 40 m");

    await page.goBack();
    await page.getByRole("button", { name: "List" }).click();
    const r3 = page.locator(`[data-graph-list] [data-graph-item="${KEY}-R3"]`);
    await r3.locator(":scope > div").getByRole("button", { name: "Centre the graph here" }).click();
    await expect(page).toHaveURL(new RegExp(`/p/${KEY}/r/${KEY}-R3/trace\\?view=list$`));
    await expect(page.getByRole("heading", { level: 1, name: `Trace graph of ${KEY}-R3` })).toBeVisible();
    await expect(page.locator(`[data-graph-list] [data-graph-item="${KEY}-R4"]`)).toBeVisible();

    await page.locator('[data-action="back-to-requirement"]').click();
    await expect(page).toHaveURL(new RegExp(`/p/${KEY}/r/${KEY}-R3$`));
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Brake bench run");
  });
});
