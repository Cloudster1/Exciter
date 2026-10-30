import { readFileSync } from "node:fs";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, toast } from "../fixtures/shell";
import { PRINTED } from "../fixtures/stack";

// Dashboards shared and arranged (#319): alice shares a dashboard bob then reads by its address, copies its link,
// prints it, renames it and changes, widens and removes its widgets, and makes a shared one the project's home.

const KEY = uniqueKey("DSH");
// A PDF is printed by the render service, which takes longer than a page.
const PRINTING_MS = 60_000;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

/** A project of three requirements, two of them musts. */
async function makeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  must(await api.POST("/projects", { body: { key: KEY, name: "Dashboarded" } }));
  for (const [title, priority] of [
    ["Stop within 40 m", "must"],
    ["Sound a horn", "should"],
    ["Brakes engage within 100 ms", "must"],
  ] as const) {
    must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title, priority } }));
  }
}

/** A dashboard of alice's with a count and a breakdown by priority. */
async function makeDashboard(api: ExciterApi, name: string, shared: boolean) {
  const made = must(await api.POST("/projects/{projectKey}/dashboards", { params: { path: { projectKey: KEY } }, body: { name, shared } })).dashboard;
  const widgets = { params: { path: { dashboardID: made.id } } };
  must(await api.POST("/dashboards/{dashboardID}/widgets", { ...widgets, body: { kind: "count", title: "Total", config: { source: {} } } }));
  must(
    await api.POST("/dashboards/{dashboardID}/widgets", {
      ...widgets,
      body: { kind: "breakdown", title: "By priority", config: { source: {}, groupBy: "priority" } },
    }),
  );
  return made;
}

test.describe("dashboards shared and arranged", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice shares a dashboard, bob reads it by its address, and loses it when she stops", async ({ page, api, pageAs }) => {
    await makeProject(api);
    await page.goto(`/projects/${KEY}/dashboards`);
    await page.locator('[data-action="new-dashboard"]').click();
    await page.getByLabel("Name", { exact: true }).fill("Brakes");
    await page.locator('[data-action="create-dashboard"]').click();
    await expect(page.getByRole("heading", { level: 1, name: "Brakes" })).toBeVisible();
    const address = new URL(page.url()).pathname;
    expect(address).toMatch(new RegExp(`^/projects/${KEY}/dashboards/[0-9a-f-]{36}$`));
    const titles = await page.locator("[data-dashboard-grid] > li").evaluateAll((items) => items.map((item) => item.getAttribute("data-widget")));
    expect(titles.length).toBeGreaterThan(0);

    await page.locator('[data-action="arrange-dashboard"]').click();
    await page.locator('[data-action="share-dashboard"]').click();
    await expect(page.locator('[data-action="share-dashboard"]')).toHaveText("Stop sharing");
    await expect(page.locator("main").getByText("Shared", { exact: true })).toBeVisible();
    await page.locator('[data-action="arrange-dashboard"]').click();

    const bob = await pageAs("bob");
    await openUntil(bob, address, () => expect(bob.getByRole("heading", { level: 1, name: "Brakes" })).toBeVisible(ONE_LOOK));
    await expect
      .poll(() => bob.locator("[data-dashboard-grid] > li").evaluateAll((items) => items.map((item) => item.getAttribute("data-widget"))))
      .toEqual(titles);
    await expect(bob.locator('[data-action="arrange-dashboard"]')).toHaveCount(0);
    await expect(bob.locator('[data-action="share-dashboard"]')).toHaveCount(0);
    await expectAccessible(bob);
    // While shared, Brakes is in bob's list, so its absence later is the unsharing and not a list that never shows it.
    const sharedWithBob = bob.getByRole("region", { name: "Shared with the project" }).locator('[data-dashboard-item="Brakes"]');
    await openUntil(bob, `/projects/${KEY}/dashboards`, () => expect(sharedWithBob).toBeVisible(ONE_LOOK));
    await expect(sharedWithBob).toContainText("By Alice");

    await page.locator('[data-action="arrange-dashboard"]').click();
    await page.locator('[data-action="share-dashboard"]').click();
    await expect(page.locator('[data-action="share-dashboard"]')).toHaveText("Share");
    // Bob keeps no dashboard of his own, so the list is empty once Brakes is no longer shared.
    await openUntil(bob, `/projects/${KEY}/dashboards`, () =>
      expect(bob.getByText("No dashboard yet. Make one, from a template or empty.")).toBeVisible(ONE_LOOK),
    );
    await expect(bob.locator('[data-dashboard-item="Brakes"]')).toHaveCount(0);
    await bob.goto(address);
    await expect(bob.getByText("That dashboard or widget was not found. It may have been deleted, or not shared with you.")).toBeVisible();
  });

  test("alice copies a shared dashboard's link and downloads it as a PDF", async ({ page, api }) => {
    test.setTimeout(PRINTING_MS);
    await makeProject(api);
    const dashboard = await makeDashboard(api, "Brake report", true);
    await page.context().grantPermissions(["clipboard-read", "clipboard-write"]);
    await page.goto(`/projects/${KEY}/dashboards/${dashboard.id}`);
    await expect(page.locator('[data-widget="By priority"] [data-bar]')).toHaveCount(2);

    await page.locator('[data-action="copy-dashboard-link"]').click();
    await expect(page.getByRole("status")).toHaveText("The link is copied. Whoever reads the project opens it.");
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(page.url());
    await expectAccessible(page);

    const download = page.waitForEvent("download", PRINTED);
    await page.locator('[data-action="dashboard-pdf"]').click();
    const saved = await download;
    expect(saved.suggestedFilename()).toBe(`${KEY}-brake-report.pdf`);
    expect(
      readFileSync(await saved.path())
        .subarray(0, 5)
        .toString(),
    ).toBe("%PDF-");
  });

  test("alice renames a dashboard and changes, widens and removes its widgets, then deletes it", async ({ page, api }) => {
    await makeProject(api);
    const dashboard = await makeDashboard(api, "Brakes", false);
    await page.goto(`/projects/${KEY}/dashboards/${dashboard.id}`);
    await page.locator('[data-action="arrange-dashboard"]').click();

    const settings = page.locator("[data-dashboard-settings]");
    await settings.getByRole("button", { name: "Rename" }).click();
    await settings.getByLabel("Name").fill("Pedals");
    await settings.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Pedals" })).toBeVisible();

    const total = page.locator('[data-widget="Total"]');
    await total.getByRole("button", { name: "Change Total" }).click();
    const form = total.locator("[data-widget-form]");
    await form.getByLabel("Title").fill("All of them");
    await form.locator('[data-action="save-widget"]').click();
    const renamed = page.locator('[data-widget="All of them"]');
    await expect(renamed.locator("h2")).toHaveText("All of them");
    await expect(total).toHaveCount(0);

    await expect(renamed).toHaveClass(/md:col-span-1/);
    await renamed.getByLabel("Width").selectOption("2");
    await expect(renamed).toHaveClass(/md:col-span-2/);
    await expectAccessible(page);

    await page.getByRole("button", { name: "Remove By priority" }).click();
    await expect(page.locator('[data-widget="By priority"]')).toHaveCount(0);
    await expect(page.locator("[data-dashboard-grid] > li")).toHaveCount(1);

    await page.reload();
    await expect(page.getByRole("heading", { level: 1, name: "Pedals" })).toBeVisible();
    await expect(page.locator('[data-widget="All of them"]')).toHaveClass(/md:col-span-2/);
    await page.getByRole("link", { name: "All dashboards" }).click();
    await expect(page.locator('[data-dashboard-item="Pedals"]')).toBeVisible();
    await page.locator('[data-dashboard-item="Pedals"]').getByRole("link", { name: "Pedals" }).click();

    await page.locator('[data-action="arrange-dashboard"]').click();
    await page.locator('[data-action="delete-dashboard"]').click();
    await expect(page).toHaveURL(new RegExp(`/projects/${KEY}/dashboards$`));
    await expect(page.locator('[data-dashboard-item="Pedals"]')).toHaveCount(0);
  });

  test("alice makes a shared dashboard the project's home, which shows it above the requirements", async ({ page, api }) => {
    await makeProject(api);
    const dashboard = await makeDashboard(api, "Overview", true);
    await page.goto(`/projects/${KEY}/settings/details`);
    await page.getByLabel("Home dashboard").selectOption({ label: "Overview" });
    const details = page.locator("[data-project-details]");
    await details.getByRole("button", { name: "Save" }).click();
    await expect(toast(page, "Saved.")).toBeVisible();

    await page.goto(`/projects/${KEY}`);
    const home = page.locator("[data-home-dashboard]");
    await expect(home.locator('[data-widget-card="breakdown"] [data-bar]')).toHaveCount(2);
    await expect(home.locator('[data-widget-card="count"]')).toBeVisible();
    const rows = page.locator("[data-requirement-row]");
    await expect(rows).toHaveCount(3);
    for (const n of [1, 2, 3]) await expect(page.locator(`[data-requirement-row="${KEY}-R${n}"]`)).toBeVisible();
    const dashboardBox = await home.boundingBox();
    const listBox = await page.getByRole("table", { name: "Requirements of the project" }).boundingBox();
    expect(dashboardBox && listBox && dashboardBox.y + dashboardBox.height <= listBox.y, "the dashboard ends above the requirements").toBe(true);
    await expectAccessible(page);
    await home.getByRole("link", { name: "Overview" }).click();
    await expect(page).toHaveURL(new RegExp(`/projects/${KEY}/dashboards/${dashboard.id}$`));
    await expect(page.locator("main").getByText("Home", { exact: true })).toBeVisible();
  });

  test("bob prints a shared dashboard, and is told why he cannot print a private one", async ({ api, pageAs }) => {
    await makeProject(api);
    const shared = await makeDashboard(api, "Printed", true);
    const hers = await makeDashboard(api, "Hers", false);
    const bob = await pageAs("bob");

    // The last widget alice added is the last a replica shows bob.
    await openUntil(bob, `/print/dashboards/${shared.id}`, () => expect(bob.getByRole("region", { name: "By priority" })).toBeVisible(ONE_LOOK));
    await expect(bob.getByRole("heading", { level: 1, name: "Printed" })).toBeVisible();
    await expect(bob.locator("html[data-print-ready]")).toBeAttached();
    await expect(bob.getByRole("region", { name: "Total" })).toContainText("3");
    await expect(bob.getByRole("region", { name: "By priority" }).locator("[data-bar]")).toHaveCount(2);
    await expect(bob.locator("[data-print-meta]")).toHaveAttribute("data-title", "Printed");
    await expectAccessible(bob);

    await bob.goto(`/print/dashboards/${hers.id}`);
    await expect(bob.locator("[data-print-failed]")).toHaveText("That dashboard or widget was not found. It may have been deleted, or not shared with you.");
    await expect(bob.locator("html[data-print-ready]")).toBeAttached();
  });
});
