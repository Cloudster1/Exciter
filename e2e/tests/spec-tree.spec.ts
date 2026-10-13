import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("SPC");
const OTHER = uniqueKey("OTH");

/** Archives and deletes the projects the test made, so each test starts without them. */
async function removeProjects(api: ExciterApi) {
  for (const projectKey of [KEY, OTHER]) {
    const path = { params: { path: { projectKey } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (!found.data) continue;
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
}

const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

// The projects' keys are this worker's own, so they meet no other spec.
test.describe("the specification", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProjects(api));

  test("reads as a numbered document beside its tree, which moves by dragging and edits in place", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Specified" } }));
    const write = async (body: Record<string, unknown>) =>
      must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: body as never })).requirement;
    const scope = await write({ kind: "section", title: "Scope" });
    await write({ title: "Pay", statement: paragraph("The shopper pays at once."), place: { parentId: scope.id, afterId: null } });
    const pay = (await api.GET("/requirements/{reqKey}", { params: { path: { reqKey: `${KEY}-R2` } } })).data!.requirement;
    await write({ title: "Ship", place: { parentId: scope.id, afterId: pay.id } });
    await write({ title: "Extra" });

    await page.goto(`/p/${KEY}/spec`);
    const tree = page.getByRole("tree");
    await expect(tree.getByRole("treeitem", { name: "1.2 Ship" })).toBeVisible();
    await expect(page.locator(`[data-spec-entry="${KEY}-R2"] [data-spec-outline]`)).toHaveText("1.1");
    await expect(page.getByText("The shopper pays at once.")).toBeVisible();
    await expectAccessible(page);

    await tree.locator(`[data-tree-key="${KEY}-R4"] [data-tree-row]`).dragTo(tree.locator(`[data-tree-key="${KEY}-R2"] [data-tree-row]`));
    await expect(page.locator("[data-tree-status]")).toHaveText(`${KEY}-R4 moved to 1.1.1.`);
    await expect(tree.getByRole("treeitem", { name: "1.1.1 Extra" })).toBeVisible();

    await page.locator(`[data-spec-entry="${KEY}-R3"]`).locator('[data-action="edit-entry"]').click();
    await page.getByLabel("Title", { exact: true }).fill("Ship the basket");
    await page.locator('[data-action="save-requirement"]').click();
    await expect(page.locator(`[data-spec-entry="${KEY}-R3"]`).getByRole("heading")).toHaveText("1.2 Ship the basket");
  });

  test("a move to another project gives a new key, and the old one still finds it", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Specified" } }));
    must(await api.POST("/projects", { body: { key: OTHER, name: "Other" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Travel" } }));

    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    await page.locator('[data-action="move-requirement"]').click();
    const dialog = page.locator('[data-place-dialog="move"]');
    await dialog.getByLabel("Project", { exact: true }).selectOption(OTHER);
    await expectAccessible(page);
    await dialog.locator('[data-action="confirm-move"]').click();
    await expect(page).toHaveURL(`/p/${OTHER}/r/${OTHER}-R1`);

    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    await expect(page).toHaveURL(`/p/${OTHER}/r/${OTHER}-R1`);
    await expect(page.locator("[data-former-keys]")).toContainText(`${KEY}-R1`);
  });
});
