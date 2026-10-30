import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { wordDocument } from "../fixtures/docx";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("WRD");
const REQUIREMENT = "The brake pedal shall return within 1 s.";
const EXPLAINING = "This text explains the pedal.";

const SPEC = wordDocument([
  { heading: 1, text: "Braking" },
  { heading: 2, text: "Pedal" },
  { paragraph: REQUIREMENT },
  { paragraph: EXPLAINING },
  {
    table: [
      ["Signal", "Meaning"],
      ["Pedal pressed", "The driver brakes"],
    ],
  },
]);

async function cleanUp(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (found.data) {
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
}

// The project's key is this worker's own, so it meets no other spec's.
test.describe("Word import", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api));

  test("alice imports a Word document as a specification and reads it there", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "From Word" } }));

    await page.goto(`/projects/${KEY}/settings/word`);
    await page.locator("[data-word-file]").setInputFiles({
      name: "brakes.docx",
      mimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      buffer: SPEC,
    });
    await expect(page.getByLabel("Import below")).toHaveValue("");
    await page.locator('[data-action="upload-word"]').click();

    // The keywords stay as the project's language has them, which leaves the explaining sentence out of the requirements.
    await expect(page.getByLabel("Keywords")).toHaveValue("shall, must");
    const tree = page.locator("[data-word-tree]");
    await expect(tree.locator('[data-word-node="section"]')).toHaveText([/Braking/, /Pedal/]);
    await expect(tree.locator('[data-word-node="requirement"]').first()).toContainText(REQUIREMENT);
    await expect(tree.locator('[data-word-node="informative"]')).toContainText(EXPLAINING);
    await expect(page.getByText("Nothing keeps the import from starting.")).toBeVisible();
    await expectAccessible(page);

    await page.locator('[data-action="start-word"]').click();
    await expect(page.locator('[data-reqif-step="done"]')).toBeVisible({ timeout: 30_000 });

    await page.goto(`/projects/${KEY}/spec`);
    const spec = page.locator("[data-spec-document]");
    // The specification numbers its sections, as the tree beside it does.
    await expect(spec.getByRole("heading", { name: "1 Braking" })).toBeVisible();
    await expect(spec.getByRole("heading", { name: "1.1 Pedal" })).toBeVisible();
    await expect(spec).toContainText(REQUIREMENT);
  });
});
