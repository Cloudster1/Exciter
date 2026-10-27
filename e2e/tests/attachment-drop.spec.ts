import { writeFile } from "node:fs/promises";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";

// Attaching files (#312): dropped on the panel, chosen through the button, refused when too large, and offered to
// a reader only to read.

const KEY = uniqueKey("DROP");
const REQUIREMENT = `/p/${KEY}/r/${KEY}-R1`;
// The stack's EXCITER_UPLOAD_LIMIT, 50 MB, and a byte past it.
const TOO_LARGE = 50 * 1024 * 1024 + 1;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("attaching files", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice drops a file and chooses another, a file too large is refused, and bob, a reader, only reads them", async ({
    page,
    api,
    pageAs,
    people,
  }, testInfo) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Dropped on" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Show the basket" } }));
    await page.goto(REQUIREMENT);
    const panel = page.locator("[data-attachments]");
    await expect(panel).toContainText("No files are attached.");

    const dropped = await page.evaluateHandle(() => {
      const transfer = new DataTransfer();
      transfer.items.add(new File(["Pay at once."], "dropped.txt", { type: "text/plain" }));
      return transfer;
    });
    await panel.dispatchEvent("dragenter", { dataTransfer: dropped });
    await expect(panel.getByText("Drop the files to attach them.")).toBeVisible();
    await expect(panel).toHaveAttribute("data-drop-active", "true");
    await panel.dispatchEvent("dragover", { dataTransfer: dropped });
    await panel.dispatchEvent("drop", { dataTransfer: dropped });
    await expect(panel.locator('[data-attachment="dropped.txt"]')).toBeVisible();
    await expect(panel.getByText("Drop the files to attach them.")).toHaveCount(0);

    const chooser = page.waitForEvent("filechooser");
    await panel.getByRole("button", { name: "Attach files" }).click();
    await (await chooser).setFiles({ name: "chosen.txt", mimeType: "text/plain", buffer: Buffer.from("Chosen.") });
    await expect(panel.locator('[data-attachment="chosen.txt"]')).toBeVisible();

    const large = testInfo.outputPath("large.bin");
    await writeFile(large, Buffer.alloc(TOO_LARGE));
    const again = page.waitForEvent("filechooser");
    await panel.getByRole("button", { name: "Attach files" }).click();
    await (await again).setFiles(large);
    await expect(panel.locator("[data-attachment-errors]")).toContainText("large.bin (50 MB) was not attached.");
    await expect(panel.locator('[data-attachment="large.bin"]')).toHaveCount(0);

    const me = must(await api.GET("/auth/me")).user;
    must(
      await api.PUT("/projects/{projectKey}/grants", {
        params: { path: { projectKey: KEY } },
        body: {
          grants: [
            { subject: { type: "user", id: me.id }, role: "admin" },
            { subject: { type: "user", id: people.bob.id }, role: "viewer" },
          ],
        },
      }),
    );
    const bob = await pageAs("bob");
    const bobs = bob.locator("[data-attachments]");
    await openUntil(bob, REQUIREMENT, () => expect(bob.getByText("You may read this project but not change its requirements.")).toBeVisible(ONE_LOOK));
    await expect(bobs.locator('[data-attachment="dropped.txt"]')).toBeVisible();
    await expect(bobs.locator('[data-attachment="chosen.txt"]')).toBeVisible();
    await expect(bobs.getByRole("button", { name: "Attach files" })).toHaveCount(0);
    await expect(bobs.getByRole("button", { name: /^Delete / })).toHaveCount(0);
  });
});
