import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Writing rules (#207): the statement is checked once typing pauses, a vague word is marked in the text and listed
// below it, and the mark goes once the words are made measurable; the project's quality page lists what was saved.

const KEY = uniqueKey("QUAL");

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("writing rules", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("a vague word is marked as it is typed, and the mark goes once the words are measurable", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Written well" } }));
    await page.goto(`/p/${KEY}/new`);
    await page.getByLabel("Title", { exact: true }).fill("Respond");
    const statement = page.locator("#new-statement");
    await statement.click();
    await page.keyboard.type("The system shall respond quickly.");

    await expect(statement.locator(".finding", { hasText: "quickly" })).toBeVisible();
    const findings = page.locator("[data-findings]");
    await expect(findings.locator('[data-finding-item][data-rule="vague"]')).toContainText("quickly");
    await expect(findings.getByRole("status")).not.toHaveText("No findings.");
    await expectAccessible(page);

    await statement.click();
    await page.keyboard.press("ControlOrMeta+a");
    await page.keyboard.type("The system shall respond within 2 s.");
    await expect(statement.locator(".finding", { hasText: "quickly" })).toHaveCount(0);
    await expect(findings.locator('[data-finding-item][data-rule="vague"]')).toHaveCount(0);
    await expectAccessible(page);

    await page.locator('[data-action="create-requirement"]').click();
    await expect(page).toHaveURL(`/p/${KEY}/r/${KEY}-R1`);
    await expect(page.locator("[data-quality-badge]")).toBeVisible();

    await page.goto(`/p/${KEY}/quality`);
    await expect(page.getByRole("heading", { level: 1, name: "Quality" })).toBeVisible();
    await expect(page.getByRole("table", { name: "Lowest first" }).locator(`[data-quality-row="${KEY}-R1"]`)).toBeVisible();
    await expectAccessible(page);
  });
});
