import type { Locator } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Writing rules (#320): a finding is shown in the text, accepted with a reason and taken back, and the score follows;
// an administrator adds a word to a list and switches a rule off, and the findings follow both.

const KEY = uniqueKey("QCTL");
/** A full score, which a requirement has once nothing it breaks counts. */
const FULL = "100";

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

const said = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

async function vagueRequirement(api: ExciterApi) {
  must(await api.POST("/projects", { body: { key: KEY, name: "Written well" } }));
  must(
    await api.POST("/projects/{projectKey}/requirements", {
      params: { path: { projectKey: KEY } },
      body: { title: "Respond", statement: said("The system shall respond quickly.") },
    }),
  );
}

/** The score a badge shows, once it shows one. */
async function scoreOf(badge: Locator): Promise<string> {
  await expect(badge).not.toHaveAttribute("data-quality-badge", "none");
  return (await badge.getAttribute("data-quality-badge")) ?? "";
}

/** Each test walks several pages, which outlasts the default budget on a busy machine. */
const FLOW_TIMEOUT_MS = 60_000;

test.describe("writing rules in use", { tag: "@desktop" }, () => {
  test.describe.configure({ timeout: FLOW_TIMEOUT_MS });
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice shows a finding in the text, accepts it with a reason and takes it back, and the score follows", async ({ page, api }) => {
    await vagueRequirement(api);
    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    const badge = page.locator("[data-quality-badge]");
    const before = await scoreOf(badge);
    expect(before).not.toBe(FULL);

    await page.locator('[data-action="show-findings"]').click();
    const list = page.getByRole("region", { name: "Writing rules" });
    await expect(page.locator("[data-statement] [data-finding]", { hasText: "quickly" })).toBeVisible();
    const finding = list.locator('[data-finding-item][data-rule="vague"]');
    await finding.locator('[data-action="accept-finding"]').click();
    const dialog = page.getByRole("dialog", { name: "Accept the finding" });
    await dialog.getByRole("textbox", { name: "Reason" }).fill("Quickly is defined in the hosting contract.");
    await expectAccessible(page);
    await dialog.locator('[data-action="confirm-accept-finding"]').click();
    await expect(dialog).toBeHidden();
    await expect(finding.locator("[data-acceptance]")).toContainText("Quickly is defined in the hosting contract.");
    await expect(badge).toHaveAttribute("data-quality-badge", FULL);
    await expectAccessible(page);

    await finding.locator('[data-action="withdraw-acceptance"]').click();
    await expect(finding.locator("[data-acceptance]")).toHaveCount(0);
    await expect(badge).toHaveAttribute("data-quality-badge", before);

    // In the editor a finding is shown by selecting its words.
    await page.locator('[data-action="edit-requirement"]').click();
    const show = page.getByRole("button", { name: "Show in text: quickly" });
    await show.click();
    await expect.poll(() => page.evaluate(() => window.getSelection()?.toString())).toBe("quickly");
    await expect(page.locator("#edit-statement .finding", { hasText: "quickly" })).toBeVisible();
  });

  test("alice adds a word to a list and switches a rule off, and the findings follow", async ({ page, api }) => {
    await vagueRequirement(api);
    await page.goto(`/p/${KEY}/quality`);
    const row = page.getByRole("table", { name: "Lowest first" }).locator(`[data-quality-row="${KEY}-R1"]`);
    await expect(row.locator('[data-rule="vague"]')).toBeVisible();

    await page.goto(`/p/${KEY}/settings?tab=quality`);
    const words = page.getByRole("form", { name: "Word lists" });
    await words.getByRole("textbox", { name: "Vague word: words to add" }).fill("snappy");
    await words.locator('[data-action="save-quality-words"]').click();
    await expect(page.locator("[data-quality-words]").getByRole("status")).toHaveText("Saved. The requirements are being checked again.");
    await expectAccessible(page);

    await page.goto(`/p/${KEY}/new`);
    await page.getByLabel("Title", { exact: true }).fill("Feel");
    const statement = page.locator("#new-statement");
    await statement.click();
    await page.keyboard.type("The screen shall feel snappy.");
    await expect(statement.locator(".finding", { hasText: "snappy" })).toBeVisible();
    await expect(page.locator('[data-findings] [data-finding-item][data-rule="vague"]')).toContainText("snappy");

    await page.goto(`/p/${KEY}/settings?tab=quality`);
    const rules = page.getByRole("form", { name: "Writing rules" });
    await rules.getByRole("checkbox", { name: "Check Vague word" }).uncheck();
    await rules.locator('[data-action="save-quality-rules"]').click();
    await expect(page.locator("[data-quality-rules]").getByRole("status")).toHaveText("Saved. The scores follow.");

    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    await expect(page.locator("[data-quality-badge]")).toHaveAttribute("data-quality-badge", FULL);
    await page.goto(`/p/${KEY}/quality`);
    await expect(row).toBeVisible();
    await expect(row.locator('[data-rule="vague"]')).toHaveCount(0);
    await expectAccessible(page);
  });
});
