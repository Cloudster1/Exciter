import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { dismissToasts, expectAccessible, openProjectSettings, toast } from "../fixtures/shell";
import { grantInForm, grantRow, revokeInTable, setProjectGrants } from "../fixtures/grants";

const KEY = uniqueKey("PERM");
const PERMISSIONS_PATH = `/projects/${KEY}/settings/permissions`;
const PROJECTS_PATH = "/projects";

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

  test("an administrator narrows Everyone to viewer, lets bob review, and the inspector and bob agree", async ({ page, api, pageAs, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Permitted" } }));
    await page.goto(`/projects/${KEY}`);
    await openProjectSettings(page, KEY);
    await page.locator('[data-settings-tab="permissions"]').click();
    await expect(page).toHaveURL(PERMISSIONS_PATH);
    await expect(grantRow(page, "Everyone", "editor")).toBeVisible();
    await expect(grantRow(page, people.alice.name, "admin")).toBeVisible();
    await expect(page.locator('[data-project-role="reviewer"]')).toContainText("Approve what is up for review");

    await revokeInTable(page, "Everyone", "editor");
    await expect(toast(page, "Everyone no longer holds Editor in this project.")).toBeVisible();
    await dismissToasts(page);
    await grantInForm(page, "Everyone", "viewer");
    await expect(toast(page, "Everyone holds Viewer in this project now.")).toBeVisible();
    await dismissToasts(page);
    await grantInForm(page, people.bob.name, "reviewer");
    await expect(toast(page, `${people.bob.name} holds Reviewer in this project now.`)).toBeVisible();
    await dismissToasts(page);
    await expect(grantRow(page, people.bob.name, "reviewer")).toBeVisible();
    await expect(grantRow(page, "Everyone", "editor")).toHaveCount(0);
    await expectAccessible(page);

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
    await openUntil(bob, `/projects/${KEY}/settings/details`, () => expect(bob.locator("[data-settings-read-only]")).toBeVisible(ONE_LOOK));
    await expect(bob.locator('[data-settings-tab="details"]')).toBeVisible();
    await expect(bob.locator('[data-settings-tab="permissions"]')).toHaveCount(0);
    await bob.goto(PERMISSIONS_PATH);
    const refused = bob.locator('[data-settings-refused="permissions"]');
    await expect(refused).toContainText("Only the project's administrators change its settings");
    await expect(refused).toContainText("Ask one of them, or ask for the role.");
  });

  test("bob's grant revoked takes the project from him, one grant at a time", async ({ page, api, apiAs, pageAs, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Permitted" } }));
    await setProjectGrants(api, KEY, [
      { subject: { type: "user", id: people.alice.id }, role: "admin" },
      { subject: { type: "user", id: people.bob.id }, role: "editor" },
    ]);
    const bobApi = await apiAs("bob");
    const bobSees = async () => (await bobApi.GET("/projects")).data!.projects.some((each) => each.key === KEY);
    await expect.poll(bobSees).toBe(true);

    await page.goto(PERMISSIONS_PATH);
    await expect(grantRow(page, people.bob.name, "editor")).toBeVisible();
    await revokeInTable(page, people.bob.name, "editor");
    await expect(toast(page, `${people.bob.name} no longer holds Editor in this project.`)).toBeVisible();
    await expect(grantRow(page, people.bob.name, "editor")).toHaveCount(0);
    await expect.poll(bobSees).toBe(false);
    const bob = await pageAs("bob");
    await openUntil(bob, PROJECTS_PATH, async () => {
      await expect(bob.locator("main").getByRole("heading", { level: 1 })).toBeVisible(ONE_LOOK);
      await expect(bob.locator(`[data-project-row="${KEY}"]`)).toHaveCount(0, ONE_LOOK);
    });
  });

  test("the last grant that runs the project cannot be revoked", async ({ page, api, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Permitted" } }));
    await page.goto(PERMISSIONS_PATH);
    await revokeInTable(page, people.alice.name, "admin");
    await expect(page.getByText("This is the last grant that lets somebody run the project.", { exact: false })).toBeVisible();
    await page.reload();
    await expect(grantRow(page, people.alice.name, "admin")).toBeVisible();
  });
});
