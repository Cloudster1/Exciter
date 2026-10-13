import { uniqueKey } from "../fixtures/seed";
import type { Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("FIL");
// The smallest PNG there is, one pixel, so a picture is a real one the browser draws.
const PIXEL = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", "base64");

/** Removes the project the test made, so each test starts without it. */
async function cleanUp(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (found.data) {
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
}

/** Whether a picture on the page was fetched and drawn, not left broken. */
async function drawn(page: Page, selector: string) {
  return page.locator(selector).evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0);
}

// The projects' keys are this worker's own, so they meet no other spec's.
test.describe("attachments", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api));

  test("alice attaches files, shows a picture in the statement, and deletes both", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Files" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Show the basket" } }));
    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    const panel = page.locator("[data-attachments]");
    await expect(panel).toContainText("No files are attached.");

    await page.locator("[data-attachment-input]").setInputFiles([
      { name: "notes.txt", mimeType: "text/plain", buffer: Buffer.from("Pay at once.") },
      { name: "basket.png", mimeType: "image/png", buffer: PIXEL },
    ]);
    await expect(panel.locator('[data-attachment="notes.txt"]')).toBeVisible();
    await expect(panel.locator('[data-attachment="basket.png"]')).toBeVisible();
    await expectAccessible(page);
    const download = page.waitForEvent("download");
    await panel.locator('[data-attachment="notes.txt"] [data-action="download-attachment"]').click();
    expect((await download).suggestedFilename()).toBe("notes.txt");

    await page.getByRole("button", { name: "Edit" }).click();
    const frame = page.locator("[data-editor-frame]").filter({ has: page.locator("#edit-statement") });
    await frame.locator("[data-editor-file-input]").setInputFiles({ name: "diagram.png", mimeType: "image/png", buffer: PIXEL });
    await expect(frame.locator("figure[data-image] img")).toBeVisible();
    await page.locator('[data-action="save-requirement"]').click();
    await expect(page.getByText("Saved as revision 2.")).toBeAttached();
    const shown = "[data-statement] figure[data-image] img";
    await expect(page.locator(shown)).toBeVisible();
    await expect.poll(() => drawn(page, shown)).toBe(true);
    await expect(panel.locator('[data-attachment="diagram.png"]')).toContainText("Shown in the statement");

    page.on("dialog", (dialog) => void dialog.accept());
    await panel.getByRole("button", { name: "Delete notes.txt" }).click();
    await expect(panel.locator('[data-attachment="notes.txt"]')).toHaveCount(0);
    await panel.getByRole("button", { name: "Delete diagram.png" }).click();
    await expect(panel.locator('[data-attachment="diagram.png"]')).toHaveCount(0);

    // Taken off the list, the picture the revision shows is still there to draw.
    await page.reload();
    await expect(page.locator(shown)).toBeVisible();
    await expect.poll(() => drawn(page, shown)).toBe(true);
    await page.goto(`/p/${KEY}/r/${KEY}-R1/history?tab=activity`);
    await expect(page.locator('[data-activity-entry="change"]').first()).toContainText("diagram.png");
  });
});
