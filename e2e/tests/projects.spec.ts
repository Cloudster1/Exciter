import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { openShowing } from "../fixtures/replica";
import { expectAccessible } from "../fixtures/shell";

const PROJECTS_PATH = "/projects";
const NEW_PROJECT_PATH = "/projects/new";

/** Archives and deletes every project left, so each test starts in an organization without any. */
async function removeProjects(api: ExciterApi) {
  const projects = must(await api.GET("/projects", { params: { query: { archived: true } } })).projects;
  for (const project of projects) {
    const path = { params: { path: { projectKey: project.key } } };
    if (!project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
}

// The organization is this file's alone, so its projects meet no other spec.
test.describe("projects", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProjects(api));

  test("an administrator makes a project with the key suggested, renames the key and archives and deletes it", async ({ page }) => {
    await page.goto(PROJECTS_PATH);
    await expect(page.getByText("No projects yet")).toBeVisible();
    await page.locator('[data-action="new-project"]').click();
    await expect(page).toHaveURL(NEW_PROJECT_PATH);

    await page.getByLabel("Name", { exact: true }).fill("Checkout service");
    await expect(page.getByLabel("Key", { exact: true })).toHaveValue("CS");
    await expectAccessible(page);
    await page.locator('[data-action="create-project"]').click();
    await expect(page).toHaveURL("/p/CS");
    await expect(page.getByText("No requirements yet")).toBeVisible();
    await page.locator('[data-project-nav="CS"]').getByRole("link", { name: "Project settings" }).click();
    await expect(page).toHaveURL("/p/CS/settings");
    await expectAccessible(page);

    await page.getByLabel("New key", { exact: true }).fill("chk");
    await page.locator('[data-action="change-key"]').click();
    await expect(page).toHaveURL("/p/CHK/settings");
    await expect(page.locator("[data-former-keys]")).toHaveText("Former keys, which still work: CS");

    // The old key still finds the project, and the address follows to the current one.
    await page.goto("/p/CS/settings");
    await expect(page).toHaveURL("/p/CHK/settings");

    await page.locator('[data-action="archive-project"]').click();
    await expect(page.locator("[data-archived-notice]")).toBeVisible();
    await expect(page.getByLabel("Name", { exact: true })).toHaveAttribute("readonly", "");
    await page.goto(PROJECTS_PATH);
    await expect(page.locator('[data-project-row="CHK"]')).toHaveCount(0);
    await page.getByRole("switch", { name: "Show archived projects" }).click();
    await expect(page.locator('[data-project-row="CHK"]')).toBeVisible();

    await page.goto("/p/CHK/settings");
    await page.locator('[data-action="delete-project"]').click();
    await expectAccessible(page);
    await page.locator('[data-action="confirm-delete-project"]').click();
    await expect(page).toHaveURL(PROJECTS_PATH);
    await expect(page.getByText("No projects yet")).toBeVisible();
  });

  test("a key another project has, or had, is refused beside its field", async ({ page, api }) => {
    const made = must(await api.POST("/projects", { body: { key: "PAY", name: "Payments" } })).project;
    must(await api.PUT("/projects/{projectKey}/key", { params: { path: { projectKey: made.key } }, body: { key: "BIL" } }));

    await page.goto(NEW_PROJECT_PATH);
    await page.getByLabel("Name", { exact: true }).fill("Payroll");
    const key = page.getByLabel("Key", { exact: true });
    await key.fill("PAY");
    await expect(page.getByText("PAY is taken by another project, now or before.")).toBeVisible();
    await page.locator('[data-action="create-project"]').click();
    await expect(page.getByText("The key PAY is taken by another project. Choose another.")).toBeVisible();
    await expect(page).toHaveURL(NEW_PROJECT_PATH);
  });

  test("a member reads projects and their settings but changes nothing", async ({ api, pageAs }) => {
    must(await api.POST("/projects", { body: { key: "SYS", name: "System", description: "The whole machine." } }));
    const bob = await pageAs("bob");
    // Alice made it, so bob's read may come from a replica a moment behind.
    await openShowing(bob, PROJECTS_PATH, bob.locator('[data-project-row="SYS"]'));
    await expect(bob.locator('[data-action="new-project"]')).toHaveCount(0);

    await bob.locator('[data-project-row="SYS"]').getByRole("link", { name: "System" }).click();
    await expect(bob).toHaveURL("/p/SYS");
    await bob.locator('[data-project-nav="SYS"]').getByRole("link", { name: "Project settings" }).click();
    await expect(bob).toHaveURL("/p/SYS/settings");
    await expect(bob.getByText("Only the project's administrators change its settings.")).toBeVisible();
    await expect(bob.locator('[data-action="save-project"]')).toHaveCount(0);
    await expect(bob.locator('[data-action="archive-project"]')).toHaveCount(0);
    await expectAccessible(bob);
  });
});
