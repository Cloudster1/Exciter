import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Watching (#312): the requirement's watch menu, the project's watch button and the Watching page, each checked
// by what it changes for whoever hears.

const MENU = uniqueKey("WMNU");
const WHOLE = uniqueKey("WPRJ");
const LIST = uniqueKey("WLST");

async function removeProject(api: ExciterApi, key: string) {
  const path = { params: { path: { projectKey: key } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

async function projectWith(api: ExciterApi, key: string, ...titles: string[]) {
  must(await api.POST("/projects", { body: { key, name: `Watched ${key}` } }));
  for (const title of titles) must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: key } }, body: { title } }));
}

test.describe("watching", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => {
    for (const key of [MENU, WHOLE, LIST]) await removeProject(api, key);
  });

  test("alice stops watching, watches the whole project, and sees who else watches", async ({ page, api, apiAs, people }) => {
    await projectWith(api, MENU, "Brakes engage within 100 ms");
    must(await (await apiAs("bob")).PUT("/projects/{projectKey}/watch", { params: { path: { projectKey: MENU } } }));
    await page.goto(`/projects/${MENU}/r/${MENU}-R1`);
    const button = page.locator('[data-action="watch-menu"]');
    // Alice wrote R1, so she watches it.
    await expect(button).toHaveText("Watching");
    await button.click();
    await page.getByRole("menuitem", { name: "Stop watching this requirement" }).click();
    await expect(button).toHaveText("Watch");

    await button.click();
    await page.getByRole("menuitem", { name: "Watch the whole project" }).click();
    await expect(button).toHaveText("Watching");
    await button.click();
    await expect(page.getByRole("menuitem", { name: "Covered by your watch on the whole project" })).toHaveAttribute("aria-disabled", "true");
    await page.getByRole("menuitem", { name: "Watch this requirement" }).click();
    await expect(button).toHaveText("Watching");
    await button.click();
    await expect(page.getByRole("menuitem", { name: "Stop watching this requirement" })).toBeVisible();
    await page.keyboard.press("Escape");

    const dialog = page.getByRole("dialog", { name: `Who is watching ${MENU}-R1` });
    // Bob's project watch was made in a session of his own, so the list may come from a replica a moment behind.
    await expect(async () => {
      await page.reload();
      await button.click();
      await page.getByRole("menuitem", { name: "Who is watching" }).click();
      await expect(dialog.locator(`[data-watcher="${people.bob.name}"]`)).toContainText("Watches the whole project", ONE_LOOK);
    }).toPass();
    await expect(dialog.locator(`[data-watcher="${people.alice.name}"]`)).toContainText("Watches this requirement");
    await expectAccessible(page);
  });

  test("bob watches a project and hears of a requirement he never opened, until he stops", async ({ api, pageAs, people }) => {
    await projectWith(api, WHOLE, "Brakes engage within 100 ms", "Brakes report wear");
    const bob = await pageAs("bob");
    const watch = bob.getByRole("button", { name: "Watch project" });
    await openUntil(bob, `/projects/${WHOLE}`, () => expect(watch).toBeVisible(ONE_LOOK));
    await expect(watch).toHaveAttribute("aria-pressed", "false");
    await watch.click();
    const watching = bob.getByRole("button", { name: "Watching project" });
    await expect(watching).toHaveAttribute("aria-pressed", "true");

    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: `${WHOLE}-R2` } },
        body: { title: "Brakes report wear at once", baseRevision: 1, comment: "Sooner." },
      }),
    );
    const told = new RegExp(`${people.alice.name} saved a new revision of ${WHOLE}-R2`);
    const bell = bob.getByRole("button", { name: /^Notifications, \d+ unread$/ });
    await expect(async () => {
      await bob.goto(`/projects/${WHOLE}`);
      await bell.click(ONE_LOOK);
      await expect(bob.getByRole("dialog", { name: "Notifications" }).getByRole("link", { name: told })).toContainText("Sooner.", ONE_LOOK);
    }).toPass();
    await bob.keyboard.press("Escape");

    await watching.click();
    await expect(watch).toHaveAttribute("aria-pressed", "false");
    await bob.reload();
    await expect(watch).toBeVisible();
  });

  test("bob's Watching page lists what he watches, and stopping each is felt on the requirement", async ({ api, apiAs, pageAs }) => {
    await projectWith(api, LIST, "Brakes engage within 100 ms");
    const bobApi = await apiAs("bob");
    must(await bobApi.PUT("/requirements/{reqKey}/watch", { params: { path: { reqKey: `${LIST}-R1` } } }));
    must(await bobApi.PUT("/projects/{projectKey}/watch", { params: { path: { projectKey: LIST } } }));
    const bob = await pageAs("bob");
    const requirementRow = bob.locator(`[data-watch-row="${LIST}-R1 Brakes engage within 100 ms"]`);
    const projectRow = bob.locator(`[data-watch-row="Project ${LIST}"]`);
    await openUntil(bob, "/settings/watching", () => expect(projectRow).toBeVisible(ONE_LOOK));
    await expect(requirementRow).toContainText("This requirement");
    await expect(projectRow).toContainText("Every requirement of the project");
    await expectAccessible(bob);

    await projectRow.getByRole("button", { name: `Stop watching Project ${LIST}` }).click();
    await expect(bob.getByText(`You no longer watch Project ${LIST}.`)).toBeVisible();
    await expect(projectRow).toHaveCount(0);
    await requirementRow.getByRole("button", { name: `Stop watching ${LIST}-R1 Brakes engage within 100 ms` }).click();
    await expect(bob.getByText(`You no longer watch ${LIST}-R1 Brakes engage within 100 ms.`)).toBeVisible();
    await expect(requirementRow).toHaveCount(0);

    await bob.goto(`/projects/${LIST}/r/${LIST}-R1`);
    await expect(bob.locator('[data-action="watch-menu"]')).toHaveText("Watch");
  });
});
