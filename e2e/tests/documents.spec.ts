import { uniqueKey } from "../fixtures/seed";
import { readFileSync } from "node:fs";
import type { Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("DOC");

async function cleanUp(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (found.data) {
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
}

/** Exports from the menu the page shows, and answers the file the browser saved. */
async function exportAs(page: Page, format: "PDF" | "Word", within = page.locator("main")) {
  const download = page.waitForEvent("download");
  await within.locator('[data-action="export-menu"]').first().click();
  await page.getByRole("menuitem", { name: format }).click();
  const saved = await download;
  return { name: saved.suggestedFilename(), data: readFileSync(await saved.path()) };
}

// The project's key is this worker's own, so it meets no other spec's.
test.describe("exporting documents", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api));

  test("alice exports the specification as PDF and Word, and a baseline as it was taken", async ({ page, api }) => {
    test.setTimeout(60_000);
    must(await api.POST("/projects", { body: { key: KEY, name: "Brakes" } }));
    const path = { params: { path: { projectKey: KEY } } };
    const section = must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { kind: "section", title: "Braking" } })).requirement;
    must(
      await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Stop within 40 m", place: { parentId: section.id, afterId: null } } }),
    );
    const baseline = must(await api.POST("/projects/{projectKey}/baselines", { ...path, body: { name: "Release 1" } })).baseline;

    await page.goto(`/p/${KEY}/spec`);
    await expect(page.getByRole("heading", { name: /Stop within 40 m/ })).toBeVisible();
    const pdf = await exportAs(page, "PDF");
    expect(pdf.name).toMatch(new RegExp(`^${KEY}-\\d{4}-\\d{2}-\\d{2}\\.pdf$`));
    expect(pdf.data.subarray(0, 5).toString()).toBe("%PDF-");
    const word = await exportAs(page, "Word");
    expect(word.name).toMatch(/\.docx$/);
    expect(word.data.subarray(0, 2).toString()).toBe("PK");
    await expectAccessible(page);

    await page.goto(`/p/${KEY}/baselines/${baseline.id}`);
    await expect(page.getByRole("heading", { name: "Release 1" })).toBeVisible();
    const taken = await exportAs(page, "Word");
    expect(taken.name).toMatch(new RegExp(`^${KEY}-release-1-.*\\.docx$`));
  });
});
