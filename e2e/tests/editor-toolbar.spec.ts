import type { Locator, Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Every tool of the statement's toolbar, the link it refuses and the one it takes, and a person mentioned in a
// statement (#309).

const KEY = uniqueKey("EDT");
/** The header row of the table a new table starts with, as many as web/src/config.ts NEW_TABLE_COLUMNS. */
const HEADERS = ["Card", "Invoice", "Cash"];
const LINK_REFUSED = "Give a web or mail address, such as https://example.com. Leave it empty to remove the link.";

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

/** Clicks a tool of the statement's toolbar, and waits for the toolbar to hand focus back, which loses a key
 *  pressed before it. */
async function tool(page: Page, statement: Locator, action: string) {
  await page.locator(`[data-action="format-${action}"]`).first().click();
  await expect(statement).toBeFocused();
}

/** Each test walks several pages, some as two people, which outlasts the default budget on a busy machine. */
const FLOW_TIMEOUT_MS = 60_000;

test.describe("editor toolbar", { tag: "@desktop" }, () => {
  test.describe.configure({ timeout: FLOW_TIMEOUT_MS });
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice writes a statement with every tool of the toolbar and a link, and it reads so once saved", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Formatted" } }));
    await page.goto(`/projects/${KEY}/new`);
    await page.getByLabel("Title", { exact: true }).fill("Pay in every format");
    const statement = page.locator("#new-statement");
    await statement.click();
    await page.keyboard.type("Visit the shop");
    for (const each of ["slanted", "struck", "code", "Heading", "bullet", "numbered", "quoted", "x = 1", "last"]) {
      await page.keyboard.press("Enter");
      await page.keyboard.type(each);
    }
    const line = (text: string) => statement.getByText(text, { exact: true });
    /** Puts the caret at the end of a line, and selects back from there as far as `back` says. */
    async function select(text: string, back?: string) {
      await line(text).click();
      await page.keyboard.press("End");
      if (back) await page.keyboard.press(back);
    }

    for (const [text, action] of [
      ["slanted", "italic"],
      ["struck", "strike"],
      ["code", "code"],
    ]) {
      await select(text!, "Shift+Home");
      await tool(page, statement, action!);
    }
    for (const [text, action] of [
      ["Heading", "heading-2"],
      ["bullet", "bullet-list"],
      ["numbered", "ordered-list"],
      ["quoted", "quote"],
      ["x = 1", "code-block"],
    ]) {
      await select(text!);
      await tool(page, statement, action!);
    }

    await select("Visit the shop", "Shift+Control+ArrowLeft");
    await page.locator('[data-action="format-link"]').first().click();
    const dialog = page.getByRole("dialog", { name: "Link" });
    const address = dialog.getByLabel("Address");
    await address.fill("javascript:alert(1)");
    await dialog.locator('[data-action="save-link"]').click();
    await expect(dialog.getByText(LINK_REFUSED)).toBeVisible();
    await expect(address).toHaveAttribute("aria-invalid", "true");
    await expectAccessible(page);
    await address.fill("https://example.com");
    await dialog.locator('[data-action="save-link"]').click();
    await expect(dialog).toHaveCount(0);
    await expect(page).toHaveURL(`/projects/${KEY}/new`);

    await select("last");
    await tool(page, statement, "rule");
    await tool(page, statement, "table");
    // A header the author leaves empty is one a screen reader cannot name, so each gets its words.
    for (const [i, header] of HEADERS.entries()) {
      if (i > 0) await page.keyboard.press("Tab");
      await page.keyboard.type(header);
    }
    await expectAccessible(page);

    await page.locator('[data-action="create-requirement"]').click();
    await expect(page).toHaveURL(`/projects/${KEY}/r/${KEY}-R1`);
    const saved = page.locator("[data-statement]");
    await expect(saved.locator('a[href="https://example.com"]')).toHaveText("shop");
    await expect(saved.locator("em")).toHaveText("slanted");
    await expect(saved.locator("s")).toHaveText("struck");
    await expect(saved.locator("p > code")).toHaveText("code");
    await expect(saved.locator("h2")).toHaveText("Heading");
    await expect(saved.locator("ul > li")).toHaveText("bullet");
    await expect(saved.locator("ol > li")).toHaveText("numbered");
    await expect(saved.locator("blockquote")).toHaveText("quoted");
    await expect(saved.locator("pre > code")).toHaveText("x = 1");
    await expect(saved.locator("hr")).toHaveCount(1);
    await expect(saved.locator("table th")).toHaveText(HEADERS);
    await expectAccessible(page);
  });

  test("alice mentions bob in a statement, and the API reads the mention back as Markdown", async ({ page, api, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Mentioned" } }));
    await page.goto(`/projects/${KEY}/new`);
    await page.getByLabel("Title", { exact: true }).fill("Ask the brakes team");
    const statement = page.locator("#new-statement");
    await statement.click();
    await page.keyboard.type("Ask ");
    // Bob's address is his alone; his first name is every spec's bob's.
    await statement.pressSequentially(`@${people.bob.email.split("@")[0]}`);
    await expect(page.locator(`[data-mention-option="${people.bob.name}"]`)).toBeVisible();
    await expectAccessible(page);
    await page.keyboard.press("Enter");
    await expect(page.locator("[data-mention-list]")).toHaveCount(0);
    await page.keyboard.type("about the pads.");
    await expectAccessible(page);
    await page.locator('[data-action="create-requirement"]').click();

    await expect(page).toHaveURL(`/projects/${KEY}/r/${KEY}-R1`);
    const mention = page.locator("[data-statement] .mention");
    await expect(mention).toHaveText(`@${people.bob.name}`);
    await expect(mention).toHaveAttribute("data-mention", people.bob.id);
    const read = must(await api.GET("/requirements/{reqKey}", { params: { path: { reqKey: `${KEY}-R1` }, query: { format: "markdown" } } })).requirement;
    expect(read.statementMarkdown).toContain(`@${people.bob.name}`);
    expect(read.statementMarkdown).toContain("about the pads.");
  });
});
