import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, openProjectSettings } from "../fixtures/shell";
import { setProjectGrants } from "../fixtures/grants";

const KEY = uniqueKey("REQ");
const LIST_PATH = `/projects/${KEY}`;

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
test.describe("requirements", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("an editor writes a requirement, revises it, owns and ranks it, and trashes, restores and deletes it", async ({ page, api, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Requirements" } }));
    await page.goto(LIST_PATH);
    await expect(page.getByText("No requirements yet")).toBeVisible();
    await page.locator('[data-action="first-requirement"]').click();
    await expect(page).toHaveURL(`${LIST_PATH}/new`);

    await page.getByLabel("Title", { exact: true }).fill("Pay the basket");
    const statement = page.locator("#new-statement");
    await statement.click();
    await page.keyboard.type("The shopper pays for ");
    await page.locator('[data-action="format-bold"]').first().click();
    // The toolbar hands focus back to the statement a moment after the click; a key before that is lost.
    await expect(statement).toBeFocused();
    await page.keyboard.type("everything");
    await expectAccessible(page);
    await page.locator('[data-action="create-requirement"]').click();
    await expect(page).toHaveURL(`${LIST_PATH}/r/${KEY}-R1`);
    await expect(page.locator("[data-statement] strong")).toHaveText("everything");
    await expect(page.locator("[data-revision]")).toHaveText("Revision 1");
    await expectAccessible(page);

    await page.locator('[data-action="edit-requirement"]').click();
    await page.getByLabel("Title", { exact: true }).fill("Pay the whole basket");
    await page.getByLabel("Comment on this revision").fill("Clearer.");
    await page.locator('[data-action="save-requirement"]').click();
    await expect(page.getByText("Saved as revision 2.")).toBeVisible();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Pay the whole basket");

    await page.getByRole("combobox", { name: "Priority" }).selectOption("must");
    await page.getByRole("combobox", { name: "Change the owner" }).fill(people.bob.name);
    await page.getByRole("option", { name: new RegExp(people.bob.name) }).click();
    await expect(page.locator("[data-owner]")).toContainText(people.bob.name);
    await expect(page.locator("[data-revision]")).toHaveText("Revision 2");

    await page.locator('[data-action="trash-requirement"]').click();
    await expect(page).toHaveURL(LIST_PATH);
    await expect(page.getByText("No requirements yet")).toBeVisible();
    await openProjectSettings(page, KEY);
    await page.locator('[data-settings-tab="trash"]').click();
    await expect(page.locator(`[data-trash-item="${KEY}-R1"]`)).toBeVisible();
    await expectAccessible(page);
    await page.locator('[data-action="restore-requirement"]').click();
    await expect(page.getByText(`${KEY}-R1 is back.`)).toBeVisible();
    await page.goto(LIST_PATH);
    await expect(page.locator(`[data-requirement-row="${KEY}-R1"]`)).toBeVisible();
    await page.locator('[data-action="new-requirement"]').click();
    await expect(page).toHaveURL(`${LIST_PATH}/new`);
    await page.getByLabel("Title", { exact: true }).fill("Refund the basket");
    await page.locator('[data-action="create-requirement"]').click();
    await expect(page).toHaveURL(`${LIST_PATH}/r/${KEY}-R2`);
    await page.goto(LIST_PATH);
    await expect(page.locator(`[data-requirement-row="${KEY}-R2"]`)).toContainText("Refund the basket");

    expect((await api.DELETE("/requirements/{reqKey}", { params: { path: { reqKey: `${KEY}-R1` } } })).response.status).toBe(204);
    await page.goto(`${LIST_PATH}/settings/trash`);
    await page.locator('[data-action="purge-requirement"]').click();
    await page.locator('[data-action="confirm-purge"]').click();
    await expect(page.getByText("The trash is empty.")).toBeVisible();
  });

  test("a save over a revision somebody else saved first keeps the draft and saves over theirs when asked", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Requirements" } }));
    must(
      await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Pay", statement: paragraph("Pay.") } }),
    );
    await page.goto(`${LIST_PATH}/r/${KEY}-R1`);
    await page.locator('[data-action="edit-requirement"]').click();
    await page.getByLabel("Title", { exact: true }).fill("Mine");
    must(await api.PATCH("/requirements/{reqKey}", { params: { path: { reqKey: `${KEY}-R1` } }, body: { title: "Theirs", baseRevision: 1 } }));

    await page.locator('[data-action="save-requirement"]').click();
    await expect(page.getByText(`Somebody saved a newer revision of ${KEY}-R1`)).toBeVisible();
    await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Mine");
    await page.locator('[data-action="save-over-latest"]').click();
    await expect(page.getByText("Saved as revision 3.")).toBeVisible();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Mine");
  });

  test("a viewer reads a requirement by its permalink and changes nothing", async ({ api, pageAs, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Requirements" } }));
    const made = must(
      await api.POST("/projects/{projectKey}/requirements", {
        params: { path: { projectKey: KEY } },
        body: { title: "Read me", statement: paragraph("Only read.") },
      }),
    ).requirement;
    const me = must(await api.GET("/auth/me")).user;
    const bobId = people.bob.id;
    await setProjectGrants(api, KEY, [
      { subject: { type: "user", id: me.id }, role: "admin" },
      { subject: { type: "user", id: bobId }, role: "viewer" },
    ]);

    const bob = await pageAs("bob");
    // Alice wrote it and narrowed the grants, so bob's read may come from a replica a moment behind.
    await openUntil(bob, `/r/${made.id}`, () => expect(bob.getByText("You may read this project but not change its requirements.")).toBeVisible(ONE_LOOK));
    await expect(bob).toHaveURL(`${LIST_PATH}/r/${KEY}-R1`);
    await expect(bob.getByText("Only read.")).toBeVisible();
    await expect(bob.locator('[data-action="edit-requirement"]')).toHaveCount(0);
    await expect(bob.locator('[data-action="trash-requirement"]')).toHaveCount(0);
    await expectAccessible(bob);
  });
});
