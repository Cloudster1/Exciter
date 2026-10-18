import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// The glossary (#210): a term is marked where a statement uses it, inflected, as it is typed, with its definition
// beside the words; the saved requirement lists it among its terms, and the glossary and the term's page list both.

const KEY = uniqueKey("GLOS");
const DEFINITION = "The lever the driver presses to slow the car.";

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("glossary", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("a term is marked as it is typed, explained beside its words, and listed where it is used", async ({ page, api }) => {
    // It writes, reads back and visits three pages, each checked for accessibility.
    test.setTimeout(40_000);
    must(await api.POST("/projects", { body: { key: KEY, name: "Braking words" } }));
    const term = must(
      await api.POST("/projects/{projectKey}/terms", {
        params: { path: { projectKey: KEY } },
        body: {
          name: "Brake pedal",
          definition: { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: DEFINITION }] }] },
          synonyms: [],
          avoid: [],
          status: "approved",
        },
      }),
    ).term;

    await page.goto(`/p/${KEY}/new`);
    await page.getByLabel("Title", { exact: true }).fill("Return the pedal");
    const statement = page.locator("#new-statement");
    await statement.click();
    await page.keyboard.type("The brake pedals shall return within 1 s.");

    const mark = statement.locator(".term-use", { hasText: "brake pedals" });
    await expect(mark).toBeVisible();
    await expect(mark).toHaveAttribute("data-term-use", term.id);
    await mark.hover();
    const popover = page.getByRole("tooltip");
    await expect(popover).toContainText("Brake pedal");
    await expect(popover).toContainText(DEFINITION);
    // The popover stands outside the landmarks, as a tooltip does, so the page is judged once it has gone.
    await page.mouse.move(0, 0);
    await expect(popover).toBeHidden();
    await expectAccessible(page);

    await page.locator('[data-action="create-requirement"]').click();
    await expect(page).toHaveURL(`/p/${KEY}/r/${KEY}-R1`);
    const panel = page.locator("[data-terms-panel]");
    // The uses are kept with the saved revision; a reload reads them if the first read came before.
    await expect(async () => {
      await page.reload();
      await expect(panel.locator('[data-requirement-term="Brake pedal"]')).toBeVisible({ timeout: 2000 });
    }).toPass();
    await expect(panel.getByRole("link", { name: "Brake pedal" })).toHaveAttribute("href", `/p/${KEY}/glossary/${term.id}`);
    await expectAccessible(page);

    await page.goto(`/p/${KEY}/glossary`);
    await expect(page.getByRole("heading", { level: 1, name: "Glossary" })).toBeVisible();
    const row = page.getByRole("table", { name: "Glossary" }).locator('[data-term-row="Brake pedal"]');
    await expect(row).toBeVisible();
    await expectAccessible(page);

    await row.getByRole("link", { name: "Brake pedal" }).click();
    await expect(page).toHaveURL(`/p/${KEY}/glossary/${term.id}`);
    await expect(page.getByRole("heading", { level: 1, name: "Brake pedal" })).toBeVisible();
    await expect(page.getByText(DEFINITION)).toBeVisible();
    await expect(page.locator(`[data-term-use-row="${KEY}-R1"]`)).toBeVisible();
    await expectAccessible(page);
  });
});
