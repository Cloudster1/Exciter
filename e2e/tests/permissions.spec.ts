import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, openProjectAdmin } from "../fixtures/shell";

const KEY = uniqueKey("PERM");
const PERMISSIONS_PATH = `/p/${KEY}/permissions`;

/** Archives and deletes the project the test made, so each test starts without it. */
async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

// The project's key is this worker's own, so it meets no other spec.
test.describe("permissions", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("an administrator narrows everyone to viewer, lets bob review, and the inspector and bob agree", async ({ page, api, pageAs, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Permitted" } }));
    await page.goto(`/p/${KEY}/settings`);
    await openProjectAdmin(page, KEY);
    await page.locator(`[data-project-nav="${KEY}"]`).getByRole("link", { name: "Permissions" }).click();
    await expect(page).toHaveURL(PERMISSIONS_PATH);
    await expect(page.locator('[data-grant="Everyone"]')).toBeVisible();
    await expect(page.locator(`[data-grant="${people.alice.name}"]`)).toBeVisible();

    await page.getByRole("combobox", { name: "Role of Everyone" }).selectOption("viewer");
    await page.getByRole("combobox", { name: "Add a person, a group or everyone" }).fill(people.bob.name);
    await page.getByRole("option", { name: new RegExp(people.bob.name) }).click();
    await page.getByRole("combobox", { name: `Role of ${people.bob.name}` }).selectOption("reviewer");
    await expectAccessible(page);
    await page.locator('[data-action="save-grants"]').click();
    await expect(page.getByText("Permissions saved.")).toBeVisible();

    await page.locator('[data-action="check-access"]').click();
    const dialog = page.getByRole("dialog", { name: "Access to Permitted" });
    await dialog.getByRole("combobox", { name: "Member" }).fill(people.bob.name);
    await dialog.getByRole("option", { name: new RegExp(people.bob.name) }).click();
    await expect(dialog.locator('[data-access-right="approve"]')).toHaveAttribute("data-allowed", "true");
    await expect(dialog.locator('[data-access-right="edit"]')).toHaveAttribute("data-allowed", "false");
    await expect(dialog.locator('[data-access-right="edit"] [data-decides]')).toHaveAttribute("data-access-step", "grant");
    await expectAccessible(page);

    // Alice made the project and its grants, so bob's read may come from a replica a moment behind.
    const bob = await pageAs("bob");
    await openUntil(bob, `/p/${KEY}/settings`, () => expect(bob.locator(`[data-project-nav="${KEY}"]`)).toBeVisible(ONE_LOOK));
    await openProjectAdmin(bob, KEY);
    await expect(bob.locator(`[data-project-nav="${KEY}"]`).getByRole("link", { name: "Project settings" })).toBeVisible();
    await expect(bob.locator(`[data-project-nav="${KEY}"]`).getByRole("link", { name: "Permissions" })).toHaveCount(0);
    await bob.goto(PERMISSIONS_PATH);
    await expect(bob.getByText("Only the project's administrators see and change who may do what in it.")).toBeVisible();
  });

  test("the last administrator cannot be removed", async ({ page, api, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Permitted" } }));
    await page.goto(PERMISSIONS_PATH);
    await page.getByRole("button", { name: `Remove ${people.alice.name}` }).click();
    await page.locator('[data-action="save-grants"]').click();
    await expect(page.getByText("Give at least one person or group the admin role")).toBeVisible();
  });
});
