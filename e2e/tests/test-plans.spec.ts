import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// A test plan's page (#324): alice reorders and drops the cases of a picked plan, changes the query of a plan by
// query, and deletes a plan whose execution keeps its title.

const KEY = uniqueKey("TPL");
const paragraph = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
const path = { params: { path: { projectKey: KEY } } };

async function removeProject(api: ExciterApi) {
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

/** A project with three test cases, and their keys in the order they were written. */
async function threeCases(api: ExciterApi): Promise<string[]> {
  must(await api.POST("/projects", { body: { key: KEY, name: "Brakes" } }));
  const type = must(
    await api.POST("/projects/{projectKey}/types", { ...path, body: { name: "Test case", icon: "requirement", colour: "#4f6bed", role: "testCase" } }),
  ).type;
  const keys: string[] = [];
  for (const title of ["Brake on a wet road", "Brake on ice", "Brake on a wet slope"]) {
    const made = must(
      await api.POST("/projects/{projectKey}/requirements", {
        ...path,
        body: { title, typeId: type.id, testCase: { preconditions: null, steps: [{ action: paragraph("Press the pedal."), expected: null }], parameters: [] } },
      }),
    ).requirement;
    keys.push(made.key);
  }
  return keys;
}

const listed = (page: import("@playwright/test").Page) => page.locator("[data-plan-cases] [data-plan-case]");

test.describe("a test plan's page", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice moves and removes a picked plan's cases, and the order holds", async ({ page, api }) => {
    const [wet, ice, slope] = await threeCases(api);
    const plan = must(
      await api.POST("/projects/{projectKey}/test-plans", { ...path, body: { title: "Every road", scope: "cases", cases: [wet!, ice!, slope!] } }),
    ).plan;
    await page.goto(`/projects/${KEY}/tests/plans/${plan.id}`);
    await expect(listed(page)).toHaveCount(3);

    await page.getByRole("button", { name: `Move ${wet} down` }).click();
    await expect(listed(page).first()).toHaveAttribute("data-plan-case", ice!);
    await page.getByRole("button", { name: `Remove ${slope}` }).click();
    await expect(listed(page)).toHaveCount(2);
    await expectAccessible(page);

    await page.reload();
    await expect(listed(page)).toHaveCount(2);
    expect(await listed(page).evaluateAll((items) => items.map((each) => each.getAttribute("data-plan-case")))).toEqual([ice, wet]);
    await expect(page.getByRole("button", { name: `Move ${ice} up` })).toBeDisabled();
  });

  test("alice changes a plan's query and its cases follow", async ({ page, api }) => {
    const [wet, ice, slope] = await threeCases(api);
    const plan = must(
      await api.POST("/projects/{projectKey}/test-plans", { ...path, body: { title: "By query", scope: "query", query: 'type = "Test case"' } }),
    ).plan;
    await page.goto(`/projects/${KEY}/tests/plans/${plan.id}`);
    await expect(listed(page)).toHaveCount(3);

    await page.getByRole("textbox", { name: "Query" }).fill('type = "Test case" AND title ~ wet');
    await page.locator('[data-action="save-plan-query"]').click();
    await expect(listed(page)).toHaveCount(2);
    await expect(page.locator(`[data-plan-case="${ice}"]`)).toHaveCount(0);
    for (const key of [wet!, slope!]) await expect(page.locator(`[data-plan-case="${key}"]`)).toBeVisible();
    await expectAccessible(page);

    await page.reload();
    await expect(page.getByRole("textbox", { name: "Query" })).toHaveValue('type = "Test case" AND title ~ wet');
    await expect(listed(page)).toHaveCount(2);
  });

  test("alice deletes a plan, and the execution it started keeps its title", async ({ page, api }) => {
    const [wet] = await threeCases(api);
    const me = must(await api.GET("/auth/me")).user;
    const plan = must(await api.POST("/projects/{projectKey}/test-plans", { ...path, body: { title: "Short lived", scope: "cases", cases: [wet!] } })).plan;
    const execution = must(
      await api.POST("/projects/{projectKey}/test-executions", {
        ...path,
        body: { title: "Before the plan went", planId: plan.id, testers: [{ subjectType: "user", userId: me.id }] },
      }),
    ).execution;
    await page.goto(`/projects/${KEY}/tests/plans/${plan.id}`);

    await page.locator('[data-action="delete-plan"]').click();
    const dialog = page.getByRole("dialog", { name: "Delete Short lived" });
    await expect(dialog.getByText("The executions it started keep its title.")).toBeVisible();
    await expectAccessible(page);
    await dialog.locator('[data-action="confirm-delete-plan"]').click();
    await expect(page).toHaveURL(new RegExp(`/projects/${KEY}/tests(\\?|$)`));
    await page.getByRole("tab", { name: "Plans" }).click();
    await expect(page.getByText("No test plan yet.")).toBeVisible();
    await expect(page.locator('[data-plan="Short lived"]')).toHaveCount(0);

    await page.getByRole("tab", { name: "Executions" }).click();
    await expect(page.locator('[data-execution="Before the plan went"]')).toContainText("Short lived");
    await page.goto(`/projects/${KEY}/tests/executions/${execution.id}`);
    await expect(page.getByText(/^Plan: Short lived · /)).toBeVisible();
  });
});
