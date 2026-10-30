import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// The risk report and the templates (#261): a medical device project is made from its template, a hazard is rated
// on the scheme it brings, and the report prints every risk.

const KEY = uniqueKey("RRP");

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("risk report", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("a medical device project rates a hazard on its template's scheme and prints the report", async ({ page, api }) => {
    // It reads the risks and the printed report, each checked for accessibility.
    test.setTimeout(60_000);
    const path = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Infusion pump", template: "medical" } }));
    const hazard = must(await api.GET("/projects/{projectKey}/types", path)).types.find((type) => type.name === "Hazard")!;
    must(
      await api.POST("/projects/{projectKey}/requirements", {
        ...path,
        body: {
          title: "Free flow of the drug",
          typeId: hazard.id,
          risk: { before: { severity: 5, probability: 3, detection: null }, after: { severity: 5, probability: 1, detection: null } },
        },
      }),
    );

    await page.goto(`/projects/${KEY}/risks`);
    const row = page.locator(`[data-fmea-row="${KEY}-R4"]`);
    await expect(row.locator('[data-risk-level-tag="High"]').first()).toBeVisible();
    await expect(page.locator('[data-action="risk-report-pdf"]')).toHaveAttribute("href", new RegExp(`/projects/${KEY}/risk-report\\.pdf`));
    await expect(page.locator('[data-action="risk-report-docx"]')).toHaveAttribute("href", new RegExp(`/projects/${KEY}/risk-report\\.docx`));
    await expectAccessible(page);

    await page.goto(`/print/risks/${KEY}`);
    await expect(page.getByRole("heading", { name: `${KEY}-R4 Free flow of the drug` })).toBeVisible();
    await expect(page.locator("[data-report-levels]")).toContainText("High");
    await expectAccessible(page);
  });
});
