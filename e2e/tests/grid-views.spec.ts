import type { Locator, Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import type { Person } from "../fixtures/people";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("GRDV");
const project = { params: { path: { projectKey: KEY } } };

async function removeProject(api: ExciterApi) {
  const found = await api.GET("/projects/{projectKey}", project);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", project);
  await api.DELETE("/projects/{projectKey}", project);
}

/** A section and three requirements of alice's project, with a number and a text attribute, one requirement owned
 *  by alice, one in review and one labelled paper. */
async function makeProject(api: ExciterApi, alice: Person) {
  must(await api.POST("/projects", { body: { key: KEY, name: "Grid" } }));
  const cost = must(await api.POST("/projects/{projectKey}/attributes", { ...project, body: { name: "Cost", kind: "number" } })).attribute;
  const note = must(await api.POST("/projects/{projectKey}/attributes", { ...project, body: { name: "Note", kind: "text" } })).attribute;
  const write = async (body: Record<string, unknown>) =>
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: body as { title: string } })).requirement;
  await write({ kind: "section", title: "Checkout" });
  await write({ title: "Pay the basket", ownerId: alice.id, labels: ["checkout"], attributes: { [cost.id]: 5, [note.id]: "Card or cash" } });
  const refund = await write({ title: "Refund a payment", attributes: { [cost.id]: 12 } });
  await write({ title: "Print the receipt", labels: ["paper"] });
  const submit = must(await api.GET("/requirements/{reqKey}/transitions", { params: { path: { reqKey: refund.key } } })).transitions.find(
    (each) => each.name === "Submit for review",
  )!;
  must(await api.POST("/requirements/{reqKey}/transitions", { params: { path: { reqKey: refund.key } }, body: { transitionId: submit.id } }));
  return { cost, note };
}

/** The grid's rows by key, in order. */
async function rowKeys(page: Page): Promise<string[]> {
  return page.locator("[data-grid-row]").evaluateAll((rows) => rows.map((row) => row.getAttribute("data-grid-row") ?? ""));
}

/** Types a value into a cell and presses Enter, then waits until the grid has read its rows again: the cell is
 *  disabled while it saves, and a cell typed in before the new revision arrives would be drawn again without it. */
async function saveIn(page: Page, cell: Locator, value: string) {
  const reread = page.waitForResponse((r) => r.request().method() === "GET" && new URL(r.url()).pathname === `/api/v1/projects/${KEY}/requirements`);
  await cell.fill(value);
  await cell.press("Enter");
  await reread;
  await expect(cell).toBeEnabled();
  await expect(cell).toHaveValue(value);
}

async function shows(page: Page, keys: number[], count: string) {
  await expect.poll(() => rowKeys(page)).toEqual(keys.map((n) => `${KEY}-R${n}`));
  await expect(page.locator("[data-grid-count]")).toHaveText(count);
}

// The project's key is this worker's own, so it meets no other spec.
test.describe("the grid", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice narrows the grid by words, kind, status, owner, label and an attribute condition", async ({ page, api, people }) => {
    const { cost } = await makeProject(api, people.alice);
    await page.goto(`/p/${KEY}/grid`);
    await shows(page, [1, 2, 3, 4], "4 requirements");
    const clear = page.locator('[data-action="clear-grid-filters"]');

    await page.locator("[data-grid-words]").fill("receipt");
    await page.locator('[data-action="apply-words"]').click();
    await shows(page, [4], "1 requirement");
    await expect(page).toHaveURL(/q=receipt/);
    await clear.click();
    await shows(page, [1, 2, 3, 4], "4 requirements");

    await page.locator('[data-grid-filter="kind"]').selectOption("section");
    await shows(page, [1], "1 requirement");
    await expect(page).toHaveURL(/kind=/);
    await clear.click();
    await page.locator('[data-grid-filter="status"]').selectOption({ label: "In review" });
    await shows(page, [3], "1 requirement");
    await expect(page).toHaveURL(/status=/);
    await clear.click();
    await page.locator('[data-grid-filter="owner"]').selectOption({ label: "Owned by me" });
    await shows(page, [2], "1 requirement");
    await expect(page).toHaveURL(new RegExp(`owner=[^&]*${people.alice.id}`));
    await clear.click();

    const label = page.locator('[data-filter="label"] [data-label-input]');
    await label.fill("paper");
    await label.press("Enter");
    await shows(page, [4], "1 requirement");
    await expect(page).toHaveURL(/label=[^&]*paper/);
    await clear.click();
    await shows(page, [1, 2, 3, 4], "4 requirements");

    await page.locator("[data-condition-attribute]").selectOption(cost.id);
    await page.locator("[data-condition-op]").selectOption("gte");
    await page.locator("[data-condition-value]").fill("10");
    await page.locator('[data-action="add-condition"]').click();
    await shows(page, [3], "1 requirement");
    await expect(page).toHaveURL(new RegExp(`attr=[^&]*${cost.id}`));
    await expectAccessible(page);
    await page.getByRole("button", { name: "Remove condition Cost is at least 10" }).click();
    await shows(page, [1, 2, 3, 4], "4 requirements");
    await expect(page).not.toHaveURL(/attr=/);
  });

  test("alice edits a title and values in place, and a stale edit is refused without overwriting", async ({ page, api, people }) => {
    const { cost, note } = await makeProject(api, people.alice);
    await page.goto(`/p/${KEY}/grid`);
    const pay = page.locator(`[data-grid-row="${KEY}-R2"]`);
    const notice = page.locator("[data-grid-notice]");

    await pay.locator('[data-action="edit-title"]').click();
    await pay.locator('[data-grid-edit="title"]').fill("Pay later");
    await pay.locator('[data-grid-edit="title"]').press("Escape");
    await expect(pay.getByRole("link", { name: "Pay the basket" })).toBeVisible();
    await pay.locator('[data-action="edit-title"]').click();
    await pay.locator('[data-grid-edit="title"]').fill("Pay the whole basket");
    await pay.locator('[data-grid-edit="title"]').press("Enter");
    await expect(notice).toHaveText(`Saved ${KEY}-R2.`);
    await expect(pay.getByRole("link", { name: "Pay the whole basket" })).toBeVisible();

    await page.locator('[data-action="choose-columns"]').click();
    await page.locator(`[data-column-option="${cost.id}"]`).check();
    await page.locator(`[data-column-option="${note.id}"]`).check();
    await saveIn(page, pay.locator('[data-grid-edit="Note"]'), "Card only");
    await saveIn(page, pay.locator('[data-grid-edit="Cost"]'), "7");
    await expect(notice).toHaveText(`Saved ${KEY}-R2.`);
    await expectAccessible(page);

    // Somebody changes the requirement behind the grid, which still shows the revision before.
    const read = async () => must(await api.GET("/requirements/{reqKey}", { params: { path: { reqKey: `${KEY}-R2` } } })).requirement;
    const before = await read();
    expect(before.attributes[cost.id]).toBe(7);
    expect(before.attributes[note.id]).toBe("Card only");
    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: `${KEY}-R2` } },
        body: { attributes: { [cost.id]: 9 }, baseRevision: before.revision },
      }),
    );
    await pay.locator('[data-grid-edit="Cost"]').fill("8");
    await pay.locator('[data-grid-edit="Cost"]').press("Enter");
    await expect(page.getByRole("alert").filter({ hasText: `Somebody saved a newer revision of ${KEY}-R2 while you were editing.` })).toBeVisible();
    expect((await read()).attributes[cost.id]).toBe(9);

    await page.goto(`/p/${KEY}/r/${KEY}-R2`);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Pay the whole basket");
  });

  test("a shared view is seen by a viewer as alice keeps it, until she deletes it", async ({ page, api, people, pageAs }) => {
    await makeProject(api, people.alice);
    must(
      await api.PUT("/projects/{projectKey}/grants", {
        ...project,
        body: {
          grants: [
            { subject: { type: "user", id: people.alice.id }, role: "admin" },
            { subject: { type: "user", id: people.bob.id }, role: "viewer" },
          ],
        },
      }),
    );
    const name = "Team order";
    await page.goto(`/p/${KEY}/grid?sort=title`);
    await page.locator('[data-action="save-view"]').click();
    const dialog = page.getByRole("dialog", { name: "Save as a view" });
    await dialog.getByLabel("Name").fill(name);
    await dialog.getByRole("checkbox", { name: "Share with everybody in the project" }).check();
    await dialog.locator('[data-action="confirm-save-view"]').click();
    await expect(page.locator("[data-grid-notice]")).toHaveText(`Saved the view ${name}.`);
    const viewUrl = page.url();

    // Bob reads from a replica, which may be a moment behind what alice saved.
    const bob = await pageAs("bob");
    await expect(async () => {
      await bob.goto(`/p/${KEY}/grid`);
      await bob.locator("[data-view-select]").selectOption({ label: `${name} (Shared)` }, { timeout: 1000 });
    }).toPass();
    await expect(bob).toHaveURL(/sort=title/);
    await expect.poll(() => rowKeys(bob)).toEqual([`${KEY}-R1`, `${KEY}-R2`, `${KEY}-R4`, `${KEY}-R3`]);
    await expect(bob.locator('[data-action="update-view"]')).toHaveCount(0);
    await expect(bob.locator('[data-action="delete-view"]')).toHaveCount(0);
    await bob.locator('[data-action="save-view"]').click();
    const bobs = bob.getByRole("dialog", { name: "Save as a view" });
    await expect(bobs.getByRole("checkbox", { name: "Share with everybody in the project" })).toBeDisabled();
    await expect(bobs).toContainText("Only the project's editors share views.");
    await expectAccessible(bob);
    await bobs.getByRole("button", { name: "Cancel" }).click();

    await page.goto(viewUrl);
    await page.locator('[data-grid-column="title"] [data-action="sort-column"]').click();
    await expect(page).toHaveURL(/desc=true/);
    await page.locator('[data-action="update-view"]').click();
    await expect(page.locator("[data-grid-notice]")).toHaveText(`Saved the view ${name}.`);

    await expect(async () => {
      await bob.goto(`/p/${KEY}/grid`);
      await bob.locator("[data-view-select]").selectOption({ label: `${name} (Shared)` });
      await expect(bob).toHaveURL(/desc=true/, { timeout: 1000 });
    }).toPass();
    await expect.poll(() => rowKeys(bob)).toEqual([`${KEY}-R3`, `${KEY}-R4`, `${KEY}-R2`, `${KEY}-R1`]);

    await page.locator('[data-action="delete-view"]').click();
    await expect(page.locator("[data-grid-notice]")).toHaveText(`Deleted the view ${name}.`);
    await expect(page.locator("[data-view-select]")).toHaveValue("");
    await expect(async () => {
      await bob.goto(`/p/${KEY}/grid`);
      await expect(bob.locator("[data-view-select] option")).toHaveText(["All requirements"], { timeout: 1000 });
    }).toPass();
  });
});
