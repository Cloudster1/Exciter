import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { armatureURL } from "../fixtures/stack";

// A failed test case filed as an Armature issue (#324): alice fails a step, files it on that step, and the case and
// the requirement it verifies both show the issue.

const KEY = uniqueKey("TISS");
// The connection is the installation's, so the spec runs alone, with a tenant of the stub's of its own.
const TENANT = "e2e-test-issues";
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;
const paragraph = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
const path = { params: { path: { projectKey: KEY } } };

async function removeProject(api: ExciterApi) {
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("filing a failed test case in Armature", { tag: "@desktop" }, () => {
  test.beforeEach(async ({ api }) => {
    await fetch(`${armatureURL()}/_stub/${TENANT}`, { method: "DELETE" });
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL() } }));
    must(await api.PUT("/armature/account/token", { body: { token: patFor(TENANT, "alice") } }));
  });

  test.afterEach(async ({ api }) => {
    await removeProject(api);
    await api.DELETE("/armature/connection");
  });

  test("alice files a failed step as an issue, and the case and its requirement list it", async ({ page, api }) => {
    test.setTimeout(60_000);
    must(await api.POST("/projects", { body: { key: KEY, name: "Brakes" } }));
    const type = must(
      await api.POST("/projects/{projectKey}/types", { ...path, body: { name: "Test case", icon: "requirement", colour: "#4f6bed", role: "testCase" } }),
    ).type;
    must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Stop within 40 m" } }));
    const made = must(
      await api.POST("/projects/{projectKey}/requirements", {
        ...path,
        body: {
          title: "Brake on a wet road",
          typeId: type.id,
          testCase: {
            preconditions: null,
            steps: [
              { action: paragraph("Drive at 50 km/h."), expected: null },
              { action: paragraph("Press the pedal."), expected: paragraph("The car stops.") },
            ],
            parameters: [],
          },
        },
      }),
    ).requirement;
    must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: made.key } }, body: { typeKey: "verifies", otherKey: `${KEY}-R1` } }));
    const me = must(await api.GET("/auth/me")).user;
    const plan = must(await api.POST("/projects/{projectKey}/test-plans", { ...path, body: { title: "Wet road", scope: "cases", cases: [made.key] } })).plan;
    const execution = must(
      await api.POST("/projects/{projectKey}/test-executions", {
        ...path,
        body: { title: "Release 1", planId: plan.id, testers: [{ subjectType: "user", userId: me.id }] },
      }),
    ).execution;

    await page.goto(`/p/${KEY}/tests/executions/${execution.id}/run/${made.key}?iteration=1`);
    await page.getByRole("group", { name: "Outcome of step 1" }).getByRole("button", { name: "Passed" }).click();
    await page.getByRole("group", { name: "Outcome of step 2" }).getByRole("button", { name: "Failed" }).click();
    await page.locator('[data-step-note="2"]').fill("It slid on.");
    await page.locator('[data-action="record-results"]').click();
    await expect(page.locator("[data-case-outcome]")).toHaveAttribute("data-case-outcome", "failed");

    await page.locator('[data-action="file-issue"]').click();
    const dialog = page.getByRole("dialog", { name: "File the failure as an Armature issue" });
    await expect(dialog.getByLabel("Armature project")).toBeVisible();
    await dialog.getByLabel("Failed step").selectOption({ label: "Step 2" });
    await expectAccessible(page);
    await dialog.locator('[data-action="file-issue-send"]').click();
    await expect(dialog).toBeHidden();
    const issues = page.locator("[data-case-issues]");
    await expect(issues).toContainText(/Issues: [A-Z]+-\d+/);
    const issueKey = (await issues.textContent())!.replace(/^Issues: /, "").trim();

    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    const panel = page.getByRole("region", { name: "Armature issues" });
    await expect(panel.locator(`[data-armature-link="${issueKey}"]`)).toBeVisible();
    await expect(panel.locator(`[data-armature-link="${issueKey}"]`)).toContainText(made.key);
  });
});
