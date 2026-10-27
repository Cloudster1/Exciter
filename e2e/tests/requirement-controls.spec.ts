import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, openProjectAdmin } from "../fixtures/shell";

// The controls of a requirement's page and of the trash (#309): a lock by hand as another person meets it, a copy
// with what is below it, the owner taken off, and the trash emptied by its administrator.

const KEY = uniqueKey("RCT");
const OTHER = uniqueKey("RCO");

async function removeProject(api: ExciterApi, projectKey: string) {
  const path = { params: { path: { projectKey } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

const project = { params: { path: { projectKey: KEY } } };
const requirement = (n: number) => `/p/${KEY}/r/${KEY}-R${n}`;

async function write(api: ExciterApi, title: string, parentId: string | null = null, projectKey = KEY) {
  return must(
    await api.POST("/projects/{projectKey}/requirements", {
      params: { path: { projectKey } },
      body: { title, ...(parentId ? { place: { parentId, afterId: null } } : {}) },
    }),
  ).requirement;
}

/** Each test walks several pages, some as two people, which outlasts the default budget on a busy machine. */
const FLOW_TIMEOUT_MS = 60_000;

test.describe("requirement controls", { tag: "@desktop" }, () => {
  test.describe.configure({ timeout: FLOW_TIMEOUT_MS });
  test.afterEach(async ({ api }) => {
    await removeProject(api, KEY);
    await removeProject(api, OTHER);
  });

  test("alice locks a requirement by hand, bob reads why and may not edit, and edits once she unlocks it", async ({ page, api, pageAs, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Locked by hand" } }));
    await write(api, "Brakes engage within 100 ms");
    const locked = `${KEY}-R1 is locked by ${people.alice.name}: Released in 2.0`;

    await page.goto(requirement(1));
    await page.locator('[data-action="lock-requirement"]').click();
    const dialog = page.getByRole("dialog", { name: `Lock ${KEY}-R1` });
    await dialog.getByLabel("Why").fill("Released in 2.0");
    await expectAccessible(page);
    await dialog.locator('[data-action="confirm-lock"]').click();
    await expect(page.locator('[data-lock="hand"]')).toContainText(locked);
    await expect(page.locator('[data-action="lock-requirement"]')).toHaveCount(0);

    const bob = await pageAs("bob");
    await openUntil(bob, requirement(1), () => expect(bob.locator('[data-lock="hand"]')).toContainText(locked, ONE_LOOK));
    await expect(bob.locator('[data-action="edit-requirement"]')).toHaveCount(0);
    await expect(bob.locator('[data-action="unlock-requirement"]')).toHaveCount(0);
    await expectAccessible(bob);

    page.once("dialog", (confirm) => void confirm.accept());
    await page.locator('[data-action="unlock-requirement"]').click();
    await expect(page.getByText(`${KEY}-R1 is unlocked.`)).toBeVisible();
    await expect(page.locator("[data-lock]")).toHaveCount(0);

    await openUntil(bob, requirement(1), () => expect(bob.locator('[data-action="edit-requirement"]')).toBeVisible(ONE_LOOK));
    await bob.locator('[data-action="edit-requirement"]').click();
    await bob.getByLabel("Title", { exact: true }).fill("Brakes engage within 80 ms");
    await bob.locator('[data-action="save-requirement"]').click();
    await expect(bob.getByText("Saved as revision 2.")).toBeVisible();
    await expect(bob.locator("[data-revision]")).toHaveText("Revision 2");
  });

  test("alice copies a requirement with its child to the top, and into another project", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Copied from" } }));
    must(await api.POST("/projects", { body: { key: OTHER, name: "Copied into" } }));
    const parent = await write(api, "Pay the basket");
    await write(api, "Pay by card", parent.id);
    must(await api.POST("/requirements/{reqKey}/labels", { params: { path: { reqKey: `${KEY}-R1` } }, body: { name: "checkout" } }));
    await write(api, "Already here", null, OTHER);

    await page.goto(requirement(1));
    await page.locator('[data-action="copy-requirement"]').click();
    const dialog = page.getByRole("dialog", { name: `Copy ${KEY}-R1` });
    await expect(dialog.getByLabel("Below", { exact: true })).toHaveValue("");
    await dialog.getByLabel("Copy everything below it too").check();
    await expectAccessible(page);
    await dialog.locator('[data-action="confirm-copy"]').click();
    await expect(page).toHaveURL(requirement(3));
    await expect(page.locator("[data-revision]")).toHaveText("Revision 1");
    await expect(page.locator("[data-copied-from]")).toHaveText(`Copied from ${KEY}-R1`);
    await expect(page.locator('[data-requirement-labels] [data-label="checkout"]')).toBeVisible();

    await page.locator("[data-history-link]").click();
    await expect(page.locator('[data-revision-row="1"]')).toContainText(`Copied from ${KEY}-R1`);
    const nodes = must(await api.GET("/projects/{projectKey}/tree", project)).nodes;
    const copy = nodes.find((n) => n.key === `${KEY}-R3`);
    expect(nodes.find((n) => n.key === `${KEY}-R4`)).toMatchObject({ title: "Pay by card", parentId: copy?.id });

    await page.goto(requirement(1));
    await page.locator('[data-action="copy-requirement"]').click();
    const into = page.getByRole("dialog", { name: `Copy ${KEY}-R1` });
    await into.getByLabel("Project").selectOption(OTHER);
    await into.locator('[data-action="confirm-copy"]').click();
    await expect(page).toHaveURL(`/p/${OTHER}/r/${OTHER}-R2`);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Pay the basket");
  });

  test("alice takes the owner off, which writes no revision and shows in the activity", async ({ page, api, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Owned" } }));
    await write(api, "Brakes engage within 100 ms");
    must(await api.PATCH("/requirements/{reqKey}", { params: { path: { reqKey: `${KEY}-R1` } }, body: { ownerId: people.bob.id } }));

    await page.goto(requirement(1));
    await expect(page.locator("[data-owner]")).toContainText(people.bob.name);
    await page.locator('[data-action="remove-owner"]').click();
    await expect(page.locator("[data-owner]")).toHaveText("Nobody");
    await expect(page.locator("[data-revision]")).toHaveText("Revision 1");

    await page.goto(`${requirement(1)}/history?tab=activity`);
    await expect(page.locator('[data-activity-entry="change"]').first()).toContainText(`changed the owner from ${people.bob.name} to nothing`);
    await expect(page.locator('[data-activity-entry="revision"]')).toHaveCount(1);
    await expectAccessible(page);
  });

  test("bob may only restore, and alice empties the trash, whose numbers are not given again", async ({ page, api, pageAs }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Trashed" } }));
    const section = must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { kind: "section", title: "Checkout" } })).requirement;
    await write(api, "Pay by card", section.id);
    await write(api, "Pay by invoice", section.id);

    await page.goto(requirement(1));
    await page.locator('[data-action="trash-requirement"]').click();
    await expect(page).toHaveURL(`/p/${KEY}`);

    const bob = await pageAs("bob");
    await openUntil(bob, `/p/${KEY}/trash`, () => expect(bob.locator(`[data-trash-item="${KEY}-R1"]`)).toBeVisible(ONE_LOOK));
    await expect(bob.getByText("Only the project's administrators delete from the trash for good.")).toBeVisible();
    await expect(bob.getByRole("button", { name: "Delete for good" })).toHaveCount(0);
    await expect(bob.locator('[data-action="empty-trash"]')).toHaveCount(0);
    await expect(bob.locator('[data-action="restore-requirement"]')).toBeVisible();

    await openProjectAdmin(page, KEY);
    await page.locator(`[data-project-nav="${KEY}"]`).getByRole("link", { name: "Trash" }).click();
    await expect(page.locator("[data-trash-item]")).toHaveCount(1);
    await page.locator('[data-action="empty-trash"]').click();
    const dialog = page.getByRole("dialog", { name: "Empty the trash for good?" });
    await expectAccessible(page);
    await dialog.locator('[data-action="confirm-purge"]').click();
    await expect(page.getByText("The trash is empty.")).toBeVisible();
    await expect(page.locator('[data-action="empty-trash"]')).toHaveCount(0);

    const next = await write(api, "Pay in instalments");
    expect(next.key).toBe(`${KEY}-R4`);
    expect((await api.GET("/requirements/{reqKey}", { params: { path: { reqKey: `${KEY}-R1` } } })).response.status).toBe(404);
  });
});
