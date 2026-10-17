import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("DOC");

/** Archives and deletes the project the test made, so each test starts without it. */
async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

// The project's key is this worker's own, so it meets no other spec.
test.describe("the document", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("writes a requirement where it is read, adds one after it, and saves over a revision made meanwhile", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Document" } }));
    const write = async (body: Record<string, unknown>) =>
      must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: body as never })).requirement;
    const scope = await write({ kind: "section", title: "Scope" });
    const pay = await write({ title: "Pay", statement: paragraph("The shopper pays at once."), place: { parentId: scope.id, afterId: null } });
    await write({ title: "Ship", place: { parentId: scope.id, afterId: pay.id } });

    await page.goto(`/p/${KEY}/spec`);
    const entry = (key: string) => page.locator(`[data-spec-entry="${key}"]`);
    await expect(entry(pay.key).getByRole("heading")).toHaveText("1.1 Pay");

    // From the keyboard: down to Pay, edit, save, and back on the entry.
    await entry(scope.key).focus();
    await page.keyboard.press("ArrowDown");
    await expect(entry(pay.key)).toBeFocused();
    await page.keyboard.press("Enter");
    const title = entry(pay.key).getByLabel("Title", { exact: true });
    await expect(title).toBeFocused();
    await title.fill("Pay by card");
    await expectAccessible(page);
    await page.keyboard.press("ControlOrMeta+s");
    await expect(entry(pay.key).getByRole("heading")).toHaveText("1.1 Pay by card");
    await expect(page.locator("[data-spec-announced]")).toHaveText(`${pay.key}: Saved as revision 2.`);
    await expect(entry(pay.key)).toBeFocused();

    // Added after Pay, it stands between Pay and Ship.
    await entry(pay.key).locator('[data-action="add-entry"]').click();
    await page.locator('[data-action="add-after"]').click();
    const adding = page.locator("[data-spec-adding]");
    await adding.getByLabel("Title", { exact: true }).fill("Refund");
    await adding.locator('[data-action="create-requirement"]').click();
    await expect(page.getByRole("heading", { name: "1.2 Refund" })).toBeVisible();
    await expect(entry(`${KEY}-R3`).getByRole("heading")).toHaveText("1.3 Ship");

    // Somebody else saves Ship in a second tab while this one edits it.
    await entry(`${KEY}-R3`).locator('[data-action="edit-entry"]').click();
    await entry(`${KEY}-R3`).getByLabel("Title", { exact: true }).fill("Ship in a day");
    const other = await page.context().newPage();
    await other.goto(`/p/${KEY}/spec`);
    await other.locator(`[data-spec-entry="${KEY}-R3"] [data-action="edit-entry"]`).click();
    await other.locator(`[data-spec-entry="${KEY}-R3"]`).getByLabel("Title", { exact: true }).fill("Ship in two days");
    await other.locator(`[data-spec-entry="${KEY}-R3"] [data-action="save-requirement"]`).click();
    await expect(other.locator(`[data-spec-entry="${KEY}-R3"]`).getByRole("heading")).toHaveText("1.3 Ship in two days");
    await other.close();

    await entry(`${KEY}-R3`).locator('[data-action="save-requirement"]').click();
    await expect(entry(`${KEY}-R3`).locator("[data-newer-revision]")).toContainText("Ship in two days");
    await expect(entry(`${KEY}-R3`).getByLabel("Title", { exact: true })).toHaveValue("Ship in a day");
    await entry(`${KEY}-R3`).locator('[data-action="save-over-latest"]').click();
    await expect(entry(`${KEY}-R3`).getByRole("heading")).toHaveText("1.3 Ship in a day");
  });

  test("asks before an unsaved draft is left behind", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Document" } }));
    for (const title of ["Pay", "Ship"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title } }));
    }
    await page.goto(`/p/${KEY}/spec`);
    await page.locator(`[data-spec-entry="${KEY}-R1"] [data-action="edit-entry"]`).click();
    await page.locator(`[data-spec-entry="${KEY}-R1"]`).getByLabel("Title", { exact: true }).fill("Pay later");
    await page.getByRole("link", { name: `${KEY}-R2` }).click();
    const dialog = page.locator("[data-discard-dialog]");
    await expect(dialog).toBeVisible();
    await expectAccessible(page);
    await dialog.locator('[data-action="keep-editing"]').click();
    await expect(page).toHaveURL(`/p/${KEY}/spec`);
    await expect(page.locator(`[data-spec-entry="${KEY}-R1"]`).getByLabel("Title", { exact: true })).toHaveValue("Pay later");

    await page.getByRole("link", { name: `${KEY}-R2` }).click();
    await dialog.locator('[data-action="discard-changes"]').click();
    await expect(page).toHaveURL(`/p/${KEY}/r/${KEY}-R2`);
  });
});
