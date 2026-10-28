import type { Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// The glossary in use (#320): a term added, redefined until its use is confirmed, and deleted; a stale edit; words
// linked to a term by hand and unlinked; and a library's glossary chosen to mark this project's requirements.

const KEY = uniqueKey("GCTL");
const LIB = uniqueKey("GLIB");
const DEFINITION = "The lever the driver presses to slow the car.";

async function removeProjects(api: ExciterApi) {
  for (const projectKey of [KEY, LIB]) {
    const path = { params: { path: { projectKey } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (!found.data) continue;
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
}

const said = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

async function project(api: ExciterApi, key = KEY, name = "Braking words") {
  must(await api.POST("/projects", { body: { key, name } }));
}

async function brakePedal(api: ExciterApi, key = KEY) {
  return must(
    await api.POST("/projects/{projectKey}/terms", {
      params: { path: { projectKey: key } },
      body: { name: "Brake pedal", definition: said(DEFINITION), synonyms: [], avoid: [], status: "approved" },
    }),
  ).term;
}

/** The requirement's terms panel once it lists what is asked: uses are kept by the worker, so the page reads again. */
async function panelShows(page: Page, selector: string) {
  const panel = page.locator("[data-terms-panel]");
  await expect(async () => {
    await page.reload();
    await expect(panel.locator(selector).first()).toBeVisible({ timeout: 2000 });
  }).toPass();
  return panel;
}

/** Replaces what an editor holds with new words. */
async function retype(page: Page, selector: string, text: string) {
  const editor = page.locator(selector);
  await editor.click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.type(text);
}

/** Each test walks several pages, which outlasts the default budget on a busy machine. */
const FLOW_TIMEOUT_MS = 90_000;

test.describe("glossary in use", { tag: "@desktop" }, () => {
  test.describe.configure({ timeout: FLOW_TIMEOUT_MS });
  test.afterEach(async ({ api }) => removeProjects(api));

  test("alice adds a term, redefines it, confirms the use that became suspect, and deletes the term", async ({ page, api }) => {
    await project(api);
    must(
      await api.POST("/projects/{projectKey}/requirements", {
        params: { path: { projectKey: KEY } },
        body: { title: "Return the pedal", statement: said("The brake pedal shall return within 1 s.") },
      }),
    );

    await page.goto(`/p/${KEY}/glossary`);
    await page.locator('[data-action="new-term"]').click();
    await page.getByLabel("Name", { exact: true }).fill("Brake pedal");
    await page.locator("#new-term-definition").click();
    await page.keyboard.type(DEFINITION);
    await expectAccessible(page);
    await page.locator('[data-action="save-term"]').click();
    await expect(page.locator("[data-glossary-notice]")).toHaveText("Brake pedal is in the glossary.");
    const row = page.getByRole("table", { name: "Glossary" }).locator('[data-term-row="Brake pedal"]');
    await expect(row).toBeVisible();

    await row.getByRole("link", { name: "Brake pedal" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Brake pedal" })).toBeVisible();
    const termPage = page.url();
    await expect(async () => {
      await page.reload();
      await expect(page.locator(`[data-term-use-row="${KEY}-R1"]`)).toBeVisible({ timeout: 2000 });
    }).toPass();

    await page.locator('[data-action="edit-term"]').click();
    const form = page.locator("[data-term-form]");
    await retype(page, "[data-term-form] [contenteditable=true]", "The lever the driver presses to slow or hold the car.");
    await form.getByLabel("Comment on the change").fill("Holding counts too.");
    await form.locator('[data-action="save-term"]').click();
    await expect(page.locator("[data-term-notice]")).toHaveText("Saved as revision 2.");
    await expect(page.getByText("The lever the driver presses to slow or hold the car.")).toBeVisible();

    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    const use = page.locator('[data-terms-panel] [data-requirement-term="Brake pedal"]');
    await expect(use).toHaveAttribute("data-suspect", "true");
    await expect(use.locator("[data-term-suspect]")).toHaveText("Suspect");
    await use.locator('[data-action="confirm-term"]').click();
    const dialog = page.getByRole("dialog", { name: "Confirm the requirement" });
    await dialog.getByRole("textbox", { name: "Comment" }).fill("Holding the car is what the pedal does at a stop.");
    await expectAccessible(page);
    await dialog.locator('[data-action="send-confirm-term"]').click();
    await expect(dialog).toBeHidden();
    await expect(page.locator("[data-terms-panel]").getByRole("status")).toHaveText("Confirmed under the definition of Brake pedal.");
    await expect(use).toHaveAttribute("data-suspect", "false");
    await expect(use.locator("[data-term-suspect]")).toHaveCount(0);

    await page.goto(termPage);
    await page.locator('[data-action="delete-term"]').click();
    const deleting = page.getByRole("dialog", { name: "Delete the term" });
    await expectAccessible(page);
    await deleting.locator('[data-action="confirm-delete-term"]').click();
    await expect(page).toHaveURL(`/p/${KEY}/glossary`);
    await expect(page.locator('[data-term-row="Brake pedal"]')).toHaveCount(0);
    await expect(page.getByText("No terms yet.")).toBeVisible();
    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    await expect(page.locator("[data-terms-panel]")).toContainText("This requirement uses no glossary term.");
  });

  test("alice's edit of a term another tab changed meanwhile loads the latest revision", async ({ page, api }) => {
    await project(api);
    const term = await brakePedal(api);
    await page.goto(`/p/${KEY}/glossary/${term.id}`);
    await page.locator('[data-action="edit-term"]').click();

    const other = await page.context().newPage();
    try {
      await other.goto(`/p/${KEY}/glossary/${term.id}`);
      await other.locator('[data-action="edit-term"]').click();
      await retype(other, "[data-term-form] [contenteditable=true]", "The lever the driver presses with a foot.");
      await other.locator('[data-action="save-term"]').click();
      await expect(other.locator("[data-term-notice]")).toHaveText("Saved as revision 2.");
    } finally {
      await other.close();
    }

    await retype(page, "[data-term-form] [contenteditable=true]", "The lever the driver pulls by hand.");
    await page.locator('[data-action="save-term"]').click();
    const stale = page.locator("[data-term-stale]");
    await expect(stale).toContainText("Somebody changed the term since you opened it.");
    await expectAccessible(page);
    await stale.locator('[data-action="reload-term"]').click();
    await expect(page.locator("[data-term-form]")).toHaveCount(0);
    await expect(page.getByText("The lever the driver presses with a foot.")).toBeVisible();
    await expect(page.getByText(/^Revision 2, by /)).toBeVisible();
  });

  test("alice links words to a term by hand, and takes the link off again", async ({ page, api }) => {
    await project(api);
    await brakePedal(api);
    await page.goto(`/p/${KEY}/new`);
    await page.getByLabel("Title", { exact: true }).fill("Hold");
    const statement = page.locator("#new-statement");
    await statement.click();
    await page.keyboard.type("The car holds with the stop lever");
    await page.keyboard.press("Shift+ControlOrMeta+ArrowLeft");
    await page.keyboard.press("Shift+ControlOrMeta+ArrowLeft");
    await page.locator('[data-action="format-term"]').first().click();
    const picker = page.getByRole("dialog", { name: "Link the words to a term" });
    await expect(picker).toContainText('The words "stop lever" are linked to the term you pick.');
    await picker.getByRole("combobox", { name: "Find a term" }).fill("Brake");
    await expectAccessible(page);
    await picker.getByRole("option", { name: "Brake pedal" }).click();
    await expect(picker).toBeHidden();
    await page.locator('[data-action="create-requirement"]').click();
    await expect(page).toHaveURL(`/p/${KEY}/r/${KEY}-R1`);

    const panel = await panelShows(page, '[data-requirement-term="Brake pedal"]');
    await expect(panel.locator('[data-requirement-term="Brake pedal"]')).toContainText("Linked");
    await expect(page.locator("[data-statement] [data-term-id]", { hasText: "stop lever" })).toBeVisible();

    await page.locator('[data-action="edit-requirement"]').click();
    await page.locator("#edit-statement").getByText("stop lever").click();
    await page.locator('[data-action="format-term"]').first().click();
    const unlinking = page.getByRole("dialog", { name: "Link the words to a term" });
    await unlinking.locator('[data-action="unlink-term"]').click();
    await expect(unlinking).toBeHidden();
    await page.locator('[data-action="save-requirement"]').click();
    await expect(page.getByText("Saved as revision 2.")).toBeVisible();
    await panelShows(page, "text=This requirement uses no glossary term.");
    await expect(page.locator("[data-statement] [data-term-id]")).toHaveCount(0);
    await expect(page.locator("[data-statement]")).toContainText("The car holds with the stop lever");
  });

  test("alice chooses a library's glossary, whose term then marks this project's requirement", async ({ page, api }) => {
    await project(api, LIB, "Brake parts");
    must(await api.PATCH("/projects/{projectKey}", { params: { path: { projectKey: LIB } }, body: { settings: { library: true } } }));
    must(
      await api.POST("/projects/{projectKey}/terms", {
        params: { path: { projectKey: LIB } },
        body: { name: "Caliper", definition: said("The part that squeezes the disc."), synonyms: [], avoid: [], status: "approved" },
      }),
    );
    await project(api);
    must(
      await api.POST("/projects/{projectKey}/requirements", {
        params: { path: { projectKey: KEY } },
        body: { title: "Grip", statement: said("The calipers shall grip the disc.") },
      }),
    );
    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    await expect(page.locator("[data-terms-panel]")).toContainText("This requirement uses no glossary term.");

    await page.goto(`/p/${KEY}/settings?tab=quality`);
    const card = page.locator("[data-glossaries-card]");
    await card.getByLabel("Add a library").selectOption(LIB);
    await card.locator('[data-action="add-glossary"]').click();
    await expect(card.locator(`[data-glossary-library="${LIB}"]`)).toBeVisible();
    await expectAccessible(page);
    await card.locator('[data-action="save-glossaries"]').click();
    await expect(card.getByRole("status")).toHaveText("The library glossaries are saved.");

    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    const panel = await panelShows(page, '[data-requirement-term="Caliper"]');
    await expect(panel.locator('[data-requirement-term="Caliper"]')).toContainText(LIB);
    await page.goto(`/p/${KEY}/glossary`);
    await expect(page.getByRole("table", { name: "Glossary" }).locator('[data-term-row="Caliper"]')).toContainText(LIB);
    await expectAccessible(page);
  });
});
