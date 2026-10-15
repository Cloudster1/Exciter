import { uniqueKey } from "../fixtures/seed";
import { readFileSync } from "node:fs";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible } from "../fixtures/shell";

const SOURCE = uniqueKey("RQX");
const TARGET = uniqueKey("RQY");
// The smallest PNG there is, one pixel, so a picture is a real one the browser draws.
const PIXEL = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", "base64");

async function cleanUp(api: ExciterApi) {
  for (const key of [SOURCE, TARGET]) {
    const path = { params: { path: { projectKey: key } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (found.data) {
      if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
      await api.DELETE("/projects/{projectKey}", path);
    }
  }
}

// The projects' keys are this worker's own, so they meet no other spec's.
test.describe("ReqIF exchange", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api));

  test("alice exports a project with a picture as ReqIF and imports it into another project", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: SOURCE, name: "Exported" } }));
    must(await api.POST("/projects", { body: { key: TARGET, name: "Imported" } }));
    const path = { params: { path: { projectKey: SOURCE } } };
    const section = must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { kind: "section", title: "Braking" } })).requirement;
    must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Stop within 40 m", place: { parentId: section.id, afterId: null } } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Warn the driver" } }));
    must(
      await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: `${SOURCE}-R3` } }, body: { typeKey: "derives", otherKey: `${SOURCE}-R2` } }),
    );
    await page.goto(`/p/${SOURCE}`);
    const uploaded = await page.request.post(`/api/v1/requirements/${SOURCE}-R2/attachments`, {
      multipart: { file: { name: "curve.png", mimeType: "image/png", buffer: PIXEL } },
    });
    expect(uploaded.status()).toBe(201);
    const picture = (await uploaded.json()).attachment.id as string;
    const statement = {
      type: "doc",
      content: [
        { type: "paragraph", content: [{ type: "text", text: "The vehicle stops." }] },
        { type: "image", attrs: { attachmentId: picture, alt: "Braking curve" } },
      ],
    };
    must(await api.PATCH("/requirements/{reqKey}", { params: { path: { reqKey: `${SOURCE}-R2` } }, body: { statement, baseRevision: 1 } }));

    await page.goto(`/p/${SOURCE}/settings?tab=reqif`);
    const download = page.waitForEvent("download");
    await page.locator('[data-action="export-reqifz"]').click();
    const file = await (await download).path();

    await page.goto(`/p/${TARGET}/settings?tab=reqif`);
    await page.getByLabel("ReqIF file").setInputFiles({ name: `${SOURCE}.reqifz`, mimeType: "application/zip", buffer: readFileSync(file) });
    await page.locator('[data-action="upload-reqif"]').click();
    await expect(page.getByText("Nothing keeps the import from starting.")).toBeVisible();
    await expect(page.getByText("Everything in the file comes across.")).toBeVisible();
    await expectAccessible(page);
    await page.locator('[data-action="start-reqif"]').click();
    await expect(page.locator('[data-reqif-step="done"]')).toContainText("3 requirements made", { timeout: 30_000 });

    const tree = must(await api.GET("/projects/{projectKey}/tree", { params: { path: { projectKey: TARGET } } })).nodes;
    expect(tree.map((node) => `${node.outline} ${node.title}`)).toEqual(["1 Braking", "1.1 Stop within 40 m", "2 Warn the driver"]);
    const links = must(await api.GET("/requirements/{reqKey}/links", { params: { path: { reqKey: `${TARGET}-R3` } } })).links;
    expect(links.map((link) => link.other.key)).toEqual([`${TARGET}-R2`]);
    await page.goto(`/p/${TARGET}/r/${TARGET}-R2`);
    const shown = "[data-statement] figure[data-image] img";
    await expect(page.locator(shown)).toBeVisible();
    await expect
      .poll(() => page.locator(shown).evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0))
      .toBe(true);
  });
});
