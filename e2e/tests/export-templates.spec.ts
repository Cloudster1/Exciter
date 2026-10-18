import { readFileSync } from "node:fs";
import type { Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { wordTemplate } from "../fixtures/docx";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { unzipText } from "../fixtures/unzip";

const KEY = uniqueKey("TPL");

async function cleanUp(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (found.data) {
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
}

/** Opens the export menu's format, and answers the file the browser saved once choose has done its part. */
async function exportAs(page: Page, format: "Word" | "HTML site", choose: () => Promise<void>) {
  const download = page.waitForEvent("download");
  await page.locator("main").locator('[data-action="export-menu"]').first().click();
  await page.getByRole("menuitem", { name: format }).click();
  await choose();
  const saved = await download;
  return { name: saved.suggestedFilename(), data: readFileSync(await saved.path()) };
}

// The project's key is this worker's own, so it meets no other spec's.
test.describe("exporting through a template", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api));

  test("alice uploads a Word template, exports through it, and downloads the specification as a site", async ({ page, api }) => {
    test.setTimeout(90_000);
    must(await api.POST("/projects", { body: { key: KEY, name: "Brakes" } }));
    const path = { params: { path: { projectKey: KEY } } };
    const section = must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { kind: "section", title: "Braking" } })).requirement;
    must(
      await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Stop within 40 m", place: { parentId: section.id, afterId: null } } }),
    );

    await page.goto(`/p/${KEY}/settings?tab=templates`);
    const upload = page.getByRole("form", { name: "New template" });
    await upload.getByLabel("Word file").setInputFiles({
      name: "supplier.docx",
      mimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      buffer: wordTemplate("Delivered to the supplier: {{title}}", "Header of {{project}}"),
    });
    await upload.getByRole("textbox", { name: "Name" }).fill("Supplier");
    await upload.locator('[data-action="upload-template"]').click();
    const card = page.locator('[data-template="Supplier"]');
    await expect(card.getByText(/Uses \{\{title\}\}, \{\{project\}\}/)).toBeVisible();
    await card.getByRole("combobox", { name: "Add a field" }).selectOption({ label: "Status" });
    await card.locator('[data-action="add-template-field"]').click();
    await card.locator('[data-action="save-template"]').click();
    await expect(card.getByText("The template is saved.")).toBeVisible();
    await expectAccessible(page);

    await page.goto(`/p/${KEY}/spec`);
    await expect(page.getByRole("heading", { name: /Stop within 40 m/ })).toBeVisible();
    const word = await exportAs(page, "Word", async () => {
      const dialog = page.getByRole("dialog");
      await dialog.getByRole("combobox", { name: "Template" }).selectOption({ label: "Supplier" });
      await expectAccessible(page);
      await dialog.locator('[data-action="export-with-template"]').click();
    });
    const parts = unzipText(word.data);
    expect(parts["word/document.xml"]).toContain("Delivered to the supplier: Brakes");
    expect(parts["word/header1.xml"]).toContain(`Header of Brakes (${KEY})`);

    const site = await exportAs(page, "HTML site", async () => {
      await page.getByRole("dialog").locator('[data-action="export-with-template"]').click();
    });
    expect(site.name).toMatch(/\.html\.zip$/);
    const files = unzipText(site.data);
    expect(files["index.html"]).toContain(`part-1.html#entry-${KEY}-R1`);
    expect(files["part-1.html"]).toContain("Stop within 40 m");
    expect(files["search.js"]).toContain("window.EXCITER_SEARCH");
  });
});
