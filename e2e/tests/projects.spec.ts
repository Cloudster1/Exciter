import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { openShowing } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { confirm, dismissToasts, expectAccessible, openProjectSettings, toast } from "../fixtures/shell";

const PROJECTS_PATH = "/projects";
const NEW_PROJECT_PATH = "/projects/new";
// A one-word name suggests itself, in capitals, as the key.
const SUGGESTED = uniqueKey("CS");
const RENAMED = uniqueKey("CHK");
const TAKEN = uniqueKey("PAY");
const FORMER = uniqueKey("BIL");
const READ = uniqueKey("SYS");

/** Archives and deletes the projects the tests made, found by any key they had, so each test starts without them. */
async function removeProjects(api: ExciterApi) {
  for (const key of [SUGGESTED, RENAMED, TAKEN, FORMER, READ]) {
    const found = await api.GET("/projects/{projectKey}", { params: { path: { projectKey: key } } });
    if (!found.data) continue;
    const path = { params: { path: { projectKey: found.data.project.key } } };
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
}

// The projects' keys are this worker's own, so they meet no other spec.
test.describe("projects", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProjects(api));

  test("an administrator makes a project with the key suggested, renames the key and archives and deletes it", async ({ page }) => {
    await page.goto(PROJECTS_PATH);
    await page.locator('[data-action="new-project"]').click();
    await expect(page).toHaveURL(NEW_PROJECT_PATH);

    await page.getByLabel("Name", { exact: true }).fill(SUGGESTED[0] + SUGGESTED.slice(1).toLowerCase());
    await expect(page.getByLabel("Key", { exact: true })).toHaveValue(SUGGESTED);
    await expectAccessible(page);
    await page.locator('[data-action="create-project"]').click();
    await expect(page).toHaveURL(`/projects/${SUGGESTED}`);
    await expect(page.getByText("No requirements yet")).toBeVisible();
    await openProjectSettings(page, SUGGESTED);
    await expect(page).toHaveURL(`/projects/${SUGGESTED}/settings/details`);
    await expectAccessible(page);

    await page.getByLabel("New key", { exact: true }).fill(RENAMED.toLowerCase());
    await page.locator('[data-action="change-key"]').click();
    await expect(page).toHaveURL(`/projects/${RENAMED}/settings/details`);
    await expect(toast(page, `The project's key is now ${RENAMED}.`)).toBeVisible();
    await dismissToasts(page);
    await expect(page.locator("[data-former-keys]")).toHaveText(`Former keys, which still work: ${SUGGESTED}`);

    // The old key still finds the project, and the address follows to the current one.
    await page.goto(`/projects/${SUGGESTED}/settings/details`);
    await expect(page).toHaveURL(`/projects/${RENAMED}/settings/details`);

    // Archiving happens at once and offers Undo, since nothing is lost.
    await page.locator('[data-action="archive-project"]').click();
    await expect(page.locator("[data-archived-notice]")).toBeVisible();
    await page.locator("[data-toast]").filter({ hasText: "is archived." }).getByRole("button", { name: "Undo" }).click();
    await expect(page.locator("[data-archived-notice]")).toHaveCount(0);
    await expect(toast(page, "is back from the archive.")).toBeVisible();
    await dismissToasts(page);
    await page.locator('[data-action="archive-project"]').click();
    await expect(page.locator("[data-archived-notice]")).toBeVisible();
    await dismissToasts(page);
    await expect(page.getByLabel("Name", { exact: true })).toHaveAttribute("readonly", "");
    await page.goto(PROJECTS_PATH);
    await expect(page.locator(`[data-project-row="${RENAMED}"]`)).toHaveCount(0);
    await page.getByRole("switch", { name: "Show archived projects" }).click();
    await expect(page.locator(`[data-project-row="${RENAMED}"]`)).toBeVisible();

    await page.goto(`/projects/${RENAMED}/settings/details`);
    await page.locator('[data-action="delete-project"]').click();
    await expectAccessible(page);
    await confirm(page);
    await expect(page).toHaveURL(PROJECTS_PATH);
    await expect(page.locator(`[data-project-row="${RENAMED}"]`)).toHaveCount(0);
  });

  test("a key another project has, or had, is refused beside its field", async ({ page, api }) => {
    const made = must(await api.POST("/projects", { body: { key: TAKEN, name: "Payments" } })).project;
    must(await api.PUT("/projects/{projectKey}/key", { params: { path: { projectKey: made.key } }, body: { key: FORMER } }));

    await page.goto(NEW_PROJECT_PATH);
    await page.getByLabel("Name", { exact: true }).fill("Payroll");
    const key = page.getByLabel("Key", { exact: true });
    await key.fill(TAKEN);
    await expect(page.getByText(`${TAKEN} is taken by another project, now or before.`)).toBeVisible();
    await page.locator('[data-action="create-project"]').click();
    await expect(page.getByText(`The key ${TAKEN} is taken by another project. Choose another.`)).toBeVisible();
    await expect(page).toHaveURL(NEW_PROJECT_PATH);
  });

  test("a member reads projects and their settings but changes nothing", async ({ api, pageAs }) => {
    must(await api.POST("/projects", { body: { key: READ, name: "System", description: "The whole machine." } }));
    const bob = await pageAs("bob");
    // Alice made it, so bob's read may come from a replica a moment behind.
    await openShowing(bob, PROJECTS_PATH, bob.locator(`[data-project-row="${READ}"]`));
    await expect(bob.locator('[data-action="new-project"]')).toHaveCount(0);

    await bob.locator(`[data-project-row="${READ}"]`).getByRole("link", { name: "System" }).click();
    await expect(bob).toHaveURL(`/projects/${READ}`);
    await openProjectSettings(bob, READ);
    await expect(bob).toHaveURL(`/projects/${READ}/settings/details`);
    await expect(bob.locator("[data-settings-read-only]")).toHaveText(
      "Only the project's administrators change its settings. Ask one of them, or ask for the role.",
    );
    await expect(bob.locator('[data-action="save-project"]')).toHaveCount(0);
    await expect(bob.locator('[data-action="archive-project"]')).toHaveCount(0);
    await expectAccessible(bob);
  });
});
